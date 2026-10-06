// Command dst-windows 是一套 Windows 原生的《饥荒联机版》开服管理系统。
//
// 与 Linux 上的 DMP 不同，它不依赖 screen / bash 等外部工具，
// 而是用 Go 原生的进程管理能力直接控制服务器进程，因此可在 Windows 上原生运行。
//
// 用法：
//
//	dst-windows                 以默认配置启动管理面板
//	dst-windows -port 8080      指定面板端口
//	dst-windows -install        仅安装服务器文件后退出
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"dst-windows/internal/panelcfg"
	"dst-windows/internal/room"
	"dst-windows/internal/server"
	"dst-windows/internal/steamcmd"
	"dst-windows/internal/web"
)

const (
	appVersion = "v0.1.2"
	// 默认存档根目录名，作为 -persistent_storage_root 的值。
	// 指向程序目录而非 Windows 的 Documents\Klei，
	// 可避免 OneDrive 重定向导致的存档位置漂移。
	defaultRootName = "dst_save"
)

// 前端构建产物直接嵌入二进制，发布时只需要一个 exe。
// 代价是改前端后必须先 cd web && npm run build，再 go build。
// all: 前缀让 _/. 开头的文件也参与嵌入（Vite 哈希文件名不受影响，但保证行为一致）。
//
//go:embed all:web/dist
var webDist embed.FS

// pickServerOverride 返回用户显式指定的服务器安装目录及其来源描述。
// 优先级：命令行 > 环境变量 > 面板配置（panel.json，可在 Web 界面里改）。
// 返回空串表示没有显式指定，应当走自动探测。
//
// panel.json 一旦存在就以它为准，包括「ServerDir 为空 = 自动探测」这个明确选择。
// 只有它不存在时才会去看早期版本留下的 serverdir.txt ——
// 否则用户在面板里点「恢复自动探测」会被那个旧文件顶回来，等于没恢复。
func pickServerOverride(workDir, flagVal string, cfg panelcfg.Config, cfgExists bool) (dir, source string) {
	if v := strings.TrimSpace(flagVal); v != "" {
		return v, "-serverdir 参数指定"
	}
	if v := strings.TrimSpace(os.Getenv("DST_SERVER_DIR")); v != "" {
		return v, "环境变量 DST_SERVER_DIR 指定"
	}
	if cfgExists {
		if cfg.ServerDir != "" {
			return cfg.ServerDir, "面板配置指定"
		}
		return "", "" // 面板里明确选了自动探测
	}
	if data, err := os.ReadFile(filepath.Join(workDir, "serverdir.txt")); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v, "serverdir.txt 记录（旧版遗留）"
		}
	}
	return "", ""
}

func main() {
	var (
		port        = flag.Int("port", 8899, "管理面板端口")
		bind        = flag.String("bind", "127.0.0.1", "管理面板监听地址")
		installOnly = flag.Bool("install", false, "仅安装服务器文件后退出")
		showVer     = flag.Bool("v", false, "显示版本号")
		serverDir   = flag.String("serverdir", "", "专用服务器根目录（含 bin/ 的 \"Don't Starve Together Dedicated Server\"）。留空则自动探测 Steam 库；也可用环境变量 DST_SERVER_DIR，或直接在 Web 面板里配置")
		arch        = flag.String("arch", "", "服务器架构：auto（优先 64 位，默认）/ 64 / 32。留空则用面板里配置的值")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("dst-windows", appVersion)
		return
	}

	// 所有运行数据放在可执行文件同级，便于整体迁移与备份
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("获取工作目录失败: %v", err)
	}

	rootDir := filepath.Join(workDir, defaultRootName)
	logDir := filepath.Join(workDir, "logs")
	// 共享工坊模组目录：通过 -ugc_directory 让所有房间、所有分片读同一份模组，
	// 避免默认布局（按房间/分片各存一份）造成的磁盘翻倍与「换房间要搬文件」
	ugcDir := filepath.Join(workDir, "ugc_mods")

	for _, d := range []string{rootDir, logDir, ugcDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			log.Fatalf("创建目录失败 %s: %v", d, err)
		}
	}

	// 架构偏好：命令行 > 面板配置 > auto
	cfg := panelcfg.Load(workDir)
	cfgExists := panelcfg.Exists(workDir)
	archPref := steamcmd.NormalizeArch(*arch)
	if *arch == "" && cfg.Arch != "" {
		archPref = steamcmd.NormalizeArch(cfg.Arch)
	}

	steam := steamcmd.NewManager(workDir, archPref)

	// 优先复用已有安装（Steam 工具里装的那份就是同一套文件，
	// 重新下一遍 2GB 没有意义），找不到才回退到面板自带 SteamCMD 安装。
	if dir, src := pickServerOverride(workDir, *serverDir, cfg, cfgExists); dir != "" {
		got, err := steam.Configure(dir, archPref, src)
		if err != nil {
			log.Fatalf("无法使用 %s 的服务器目录：%v\n"+
				"可在 Web 面板「概览 → 服务器文件」里改回自动探测，或直接编辑 %s",
				src, err, panelcfg.Path(workDir))
		}
		fmt.Printf("使用已安装的专用服务器（%s，%s 位）：%s\n", got, steam.Arch(), steam.Root())
	} else if got, err := steam.Configure("", archPref, ""); err != nil {
		log.Fatalf("自动探测服务器失败：%v", err)
	} else if got != "" {
		fmt.Printf("自动探测到已安装的专用服务器（%s，%s 位）：%s\n", got, steam.Arch(), steam.Root())
	}

	if *installOnly {
		if steam.IsExternal() {
			fmt.Println("服务器复用已有安装，无需下载。")
			fmt.Println("路径：", steam.Root())
			return
		}
		fmt.Println("开始安装 DST 专用服务器，约需下载 2GB…")
		steam.Install()
		for steam.IsInstalling() {
			p, e, _ := steam.Progress()
			if e != "" {
				log.Fatalf("安装失败: %s", e)
			}
			fmt.Printf("  %s\n", p)
			time.Sleep(3 * time.Second)
		}
		fmt.Println("安装完成，服务器路径:", steam.ServerDir())
		return
	}

	// 前端产物已嵌入二进制（编译时要求 web/dist 存在），
	// 这里取出 web/dist 子树作为静态文件根
	staticFS, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		log.Fatalf("前端嵌入产物异常: %v", err)
	}

	// 房间定义是配置的唯一事实来源，所有 ini 都由它生成
	rooms, err := room.NewStore(room.DefaultStorePath(rootDir))
	if err != nil {
		log.Fatalf("加载房间定义失败: %v", err)
	}

	// 首次运行自动建一个房间，让面板一打开就有东西可管
	if len(rooms.List()) == 0 {
		seed, err := rooms.Create(room.Defaults())
		if err != nil {
			log.Fatalf("创建默认房间失败: %v", err)
		}
		fmt.Printf("已创建默认房间「%s」，端口由面板自动分配\n", seed.Name)
	}

	mgr := server.NewManager(rootDir, steam.ServerDir(), logDir)
	// 共享模组目录：启动分片与模组预更新都要用到
	mgr.SetUgcDir(ugcDir)
	// 可执行文件路径在每次启动分片时重新解析：
	// 用户在面板里点「一键安装」之后，64 位可执行文件才出现，此时无需重启面板
	mgr.SetExeResolver(func() (string, string) {
		return steam.Executable(), steam.ServerDir()
	})

	// 逐个生成配置并注册进程层
	for _, rm := range rooms.List() {
		if err := room.Generate(rootDir, server.ConfDir, rm); err != nil {
			// 单个房间配置损坏不应阻断面板启动，否则用户进不去面板修不了
			fmt.Printf("[!] 房间「%s」配置生成失败: %v\n", rm.Name, err)
			continue
		}
		mgr.Register(server.NewCluster(rm))
	}

	api := web.NewServer(web.Options{
		Mgr:      mgr,
		Rooms:    rooms,
		Steam:    steam,
		StaticFS: staticFS,
		RootDir:  rootDir,
		ConfDir:  server.ConfDir,
		WorkDir:  workDir,
		UgcDir:   ugcDir,
		Version:  appVersion,
	})
	// 模组下载清单是安装级的，启动时按所有房间重算一次并集，
	// 避免上次运行遗留的清单与当前房间配置对不上
	api.SyncModsSetup()
	// 更新会话靠「缺失数是否归零」判断进度，把清点逻辑交给 web 层
	mgr.SetModsChecker(api.MissingModIDs)
	// 单模组更新要临时改写下载清单再恢复，同样由 web 层提供
	mgr.SetModsSetupSwap(api.SwapModsSetup)

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *bind, *port),
		Handler: api.Handler(),
		// 写超时留空：SSE 长连接需要保持打开
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Println("========================================")
	fmt.Printf("  DST 开服管理系统 %s\n", appVersion)
	fmt.Println("========================================")
	fmt.Printf("  管理面板  http://%s:%d\n", *bind, *port)
	fmt.Printf("  存档目录  %s\n", rootDir)
	fmt.Printf("  模组目录  %s\n", ugcDir)
	fmt.Printf("  房间定义  %s\n", rooms.Path())
	if steam.IsInstalled() {
		fmt.Printf("  服务器    %s（%s 位 · %s）\n", steam.Root(), steam.Arch(), steam.Source())
		if v := steam.Version(); v != "" {
			fmt.Printf("  游戏版本  %s\n", v)
		}
	} else {
		fmt.Println()
		fmt.Println("  [!] 未检测到已安装的专用服务器，请点击面板中的「一键安装」")
		fmt.Println("      （如果你已在 Steam 中安装该工具，先确认工具已下载完成再重启面板）")
	}
	fmt.Println("========================================")
	fmt.Println("  按 Ctrl+C 停止面板并安全关闭所有服务器")

	// 优雅退出：先关面板，再优雅停服（会先 c_save 再 shutdown）
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		fmt.Println("\n正在安全关闭…")
		_ = srv.Close()
		for _, c := range mgr.Clusters() {
			if err := mgr.Stop(c.Key(), 30*time.Second); err != nil {
				fmt.Printf("关闭房间「%s」失败: %v\n", c.Name, err)
			}
		}
		fmt.Println("已安全退出")
		os.Exit(0)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务启动失败: %v", err)
	}
}
