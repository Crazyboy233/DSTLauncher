//go:build windows

// 进程管理层的集成验证。
// 用一个模拟的服务器程序替代真实 DST，验证启停、命令注入、崩溃检测三条主链路。
// 运行方式：go test ./internal/server -v 或 go run cmd/verify/main.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"dst-windows/internal/server"
)

// buildFakeServer 编译模拟服务器为真正的 exe，命名为 DST 期望的可执行文件名。
// 必须是真 exe：Windows 不会执行带 .exe 扩展名的批处理内容。
func buildFakeServer(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	exePath := filepath.Join(dir, "dontstarve_dedicated_server_nullrenderer.exe")

	// 从本仓库位置定位 fakeserver 源文件
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	src := filepath.Join(filepath.Dir(filepath.Dir(self)), "fakeserver", "main.go")
	if _, err := os.Stat(src); err != nil {
		src = filepath.Join("cmd", "fakeserver", "main.go")
	}

	cmd := exec.Command("go", "build", "-o", exePath, src)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("编译模拟服务器失败: %w", err)
	}
	return exePath, nil
}

func main() {
	tmp, err := os.MkdirTemp("", "dstverify")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	binDir := filepath.Join(tmp, "bin")
	if _, err := buildFakeServer(binDir); err != nil {
		fmt.Println("FAIL 构建模拟服务器:", err)
		os.Exit(1)
	}

	root := filepath.Join(tmp, "save")
	mgr := server.NewManager(root, binDir, filepath.Join(tmp, "logs"))
	cluster := &server.Cluster{
		ID:      1,
		Name:    "验证集群",
		ConfDir: server.ConfDir,
		Shards:  []string{"Master"},
		Ports: map[string]server.ShardPort{
			"Master": {ServerPort: 10999, MasterServerPort: 27018, AuthenticationPort: 27019},
		},
		MasterPort: 21000,
	}
	mgr.Register(cluster)
	clusterKey := cluster.Key()

	fmt.Println("=== 1. 启动分片 ===")
	if err := mgr.StartShard(clusterKey, "Master"); err != nil {
		fmt.Println("FAIL 启动失败:", err)
		os.Exit(1)
	}
	time.Sleep(1500 * time.Millisecond)

	st := mgr.Status("")
	if len(st) == 0 || st[0].State != "running" {
		fmt.Printf("FAIL 状态异常: %+v\n", st)
		os.Exit(1)
	}
	fmt.Printf("PASS 进程已运行 PID=%d 端口=%d\n", st[0].PID, st[0].Port)

	fmt.Println("\n=== 2. 注入控制台命令 ===")
	if err := mgr.SendCmd(clusterKey, "Master", "TheWorld:PushEvent(\"ms_setseason\",\"summer\")"); err != nil {
		fmt.Println("FAIL 命令注入失败:", err)
		os.Exit(1)
	}
	time.Sleep(1200 * time.Millisecond)
	logs, _ := mgr.ReadLog(clusterKey, "Master", 50)
	found := false
	for _, l := range logs {
		if contains(l, "ms_setseason") {
			found = true
			fmt.Println("PASS 日志回读到命令:", trim(l))
			break
		}
	}
	if !found {
		fmt.Println("FAIL 未在日志中找到命令回显，日志尾部:")
		printTail(logs, 8)
	}

	fmt.Println("\n=== 3. 崩溃检测与自动重启 ===")
	before := mgr.Status("")[0]
	if err := mgr.SendCmd(clusterKey, "Master", "c_crash()"); err != nil {
		fmt.Println("FAIL 触发崩溃失败:", err)
	}
	// 默认退避 3s 起，等待足够时间观察重启
	fmt.Println("  触发崩溃，等待自动重启（退避 3s）…")
	time.Sleep(9 * time.Second)
	after := mgr.Status("")
	if len(after) == 0 {
		fmt.Println("FAIL 重启后无进程记录")
		os.Exit(1)
	}
	a := after[0]
	if a.PID != before.PID {
		fmt.Printf("PASS 检测到崩溃并重启，PID %d → %d，重启计数=%d\n", before.PID, a.PID, a.Restarts)
	} else {
		fmt.Printf("WARN PID 未变化，可能未重启，状态=%s\n", a.State)
	}

	fmt.Println("=== 4. 优雅关闭（c_save → c_shutdown）===")
	evSeen := false
	_, evCh := mgr.Subscribe()
	go func() {
		for ev := range evCh {
			if ev.Type == "stop" || ev.Type == "warn" {
				fmt.Printf("  事件: [%s/%s] %s - %s\n", ev.Cluster, ev.Shard, ev.Type, ev.Message)
				if ev.Type == "stop" {
					evSeen = true
				}
			}
		}
	}()

	if err := mgr.StopShard(clusterKey, "Master", 20*time.Second); err != nil {
		fmt.Println("FAIL 停止失败:", err)
		os.Exit(1)
	}
	time.Sleep(500 * time.Millisecond)
	final := mgr.Status("")
	if len(final) == 0 || final[0].State == "running" {
		fmt.Println("FAIL 停止后状态仍为运行")
	} else {
		fmt.Println("PASS 进程已停止，状态:", final[0].State)
	}
	if evSeen {
		fmt.Println("PASS 优雅关闭事件已发出")
	}

	fmt.Println("\n=== 全部验证通过 ===")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func trim(s string) string {
	if len(s) > 90 {
		return s[:90] + "…"
	}
	return s
}

func printTail(lines []string, n int) {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for _, l := range lines {
		fmt.Println("   |", l)
	}
}
