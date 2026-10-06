//go:build windows

// 模拟 DST 专用服务器进程，用于验证进程管理层。
// 真实服务器是原生 exe，这里编译一个行为一致的最小实现：
//   - 从 stdin 逐行读取控制台命令
//   - 回显到 stdout（真实服务器是写日志文件）
//   - 收到 c_shutdown() 时以 0 退出
//   - 收到 c_crash() 时以非零码退出，模拟崩溃
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	fmt.Println("*** 模拟服务器启动 ***")
	fmt.Printf("参数: %v\n", os.Args[1:])

	// 与真实服务器一致：启动一段时间后输出就绪标记
	go func() {
		// 真实服务器需要数十秒加载资源，这里缩短以便快速验证
		time.Sleep(800 * time.Millisecond)
		fmt.Println("Server is now accepting connections on 10999")
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			// stdin 关闭说明父进程断开
			return
		}
		cmd := strings.TrimSpace(line)
		if cmd == "" {
			continue
		}

		switch {
		case strings.Contains(cmd, "c_shutdown()"):
			fmt.Println("收到关闭指令，正在保存并退出…")
			fmt.Println("*** 模拟服务器已退出 ***")
			os.Exit(0)
		case strings.Contains(cmd, "c_crash()"):
			fmt.Println("*** 模拟服务器崩溃 ***")
			os.Exit(3)
		case strings.Contains(cmd, "c_save()"):
			fmt.Println("已保存世界状态")
		default:
			fmt.Println("CMD_ECHO: " + cmd)
		}
	}
}
