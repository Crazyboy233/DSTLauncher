# DSTWin · 饥荒联机版 Windows 开服面板

Windows 原生《饥荒联机版》(Don't Starve Together) 专用服务器管理系统。用 Go 原生进程管理（`exec.Command` + stdin 管道）替代 Linux 下的 `screen`/`bash`/`ps`/`tail`，在 Windows 上直接运行，无需 WSL。

自用工具，非通用分发软件。

## 功能

- **Web 管理面板**：房间管理、启停/重启、控制台命令、实时日志（SSE 流式输出）
- **模组管理**：mod 启用/禁用/配置/删除/更新，通过 `-only_update_server_mods` 多轮预下载，所有房间共享一份 mod 目录
- **存档备份与回滚**：手动备份、一键恢复
- **进程守护 + 崩溃重启**：`cmd.Wait()` 监听退出自动拉起；停止前自动发送 `c_save()` + `c_shutdown()`，避免丢档
- **房间创建**：世界设置（worldgenoverride 可视化编辑）、管理员名单（adminlist.txt）
- **存档上传**：导入外部存档整包，自动解析新旧两版 ini 结构、转换 leveldataoverride
- **服务器文件安装/更新**：自动探测 Steam 库 + SteamCMD 安装 DST 专用服务器（appid 343050）

## 环境

- Windows 10/11
- Go 1.25+
- Steam / SteamCMD（服务器文件安装用，已有安装则自动探测）

## 构建与运行

前端产物通过 `go:embed` 打进二进制，**构建产物只有一个 exe**。改了前端代码后，必须先重新构建前端再编译 Go：

```bash
# 前端（修改 web/ 下代码后需要重新构建）
cd web
npm install
npm run build   # 产物输出到 web/dist，编译时嵌入 exe

# 后端（在仓库根目录，要求 web/dist 已存在）
go build -o dstwin.exe .

# 运行
./dstwin.exe              # 默认端口 127.0.0.1:8899
./dstwin.exe -port 8080   # 指定端口
./dstwin.exe -install     # 仅安装服务器文件后退出
./dstwin.exe -v           # 显示版本
```

发布 release 只需带上 `dstwin.exe` 一个文件；`dst_save/`、`ugc_mods/`、`logs/`、`panel.json` 首次运行自动生成，SteamCMD 缺失时面板会自动下载。

### 命令行参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| `-port` | `8899` | 面板端口 |
| `-bind` | `127.0.0.1` | 监听地址 |
| `-install` | `false` | 仅安装服务器文件后退出 |
| `-serverdir` | 自动探测 | 专用服务器根目录（含 `bin/` 的 "Don't Starve Together Dedicated Server"） |
| `-arch` | `auto` | 服务器架构：auto / 64 / 32 |

服务器目录也可以通过环境变量 `DST_SERVER_DIR` 或 Web 面板里的配置页指定（写入 `panel.json`）。

## 目录结构

```
main.go                 入口：参数解析、目录探测、面板启动
internal/
  server/               进程管理层（Go 原生进程管理）：启停、守护、崩溃重启
  room/                 房间：创建、配置生成、cluster.ini/server.ini/token、管理员名单
  config/               ini 读写、路径计算（-persistent_storage_root 等）
  steamcmd/             SteamCMD 调用、Steam 库探测、服务器安装与更新
  workshop/             创意工坊 mod 下载
  modmgr/               mod 管理：modoverrides.lua、dedicated_server_mods_setup.lua 同步
  saveimport/           外部存档整包导入
  backup/               存档备份与回滚
  worldsettings/        世界设置（worldgenoverride.lua 生成）
  panelcfg/             panel.json 面板配置
  web/                  HTTP API + 静态资源服务
web/                    Vue 3 + Vite 前端
dst_save/               运行时数据：存档根目录（-persistent_storage_root）、rooms.json
ugc_mods/               共享 mod 目录（-ugc_directory）
logs/                   运行日志、mod 更新日志
```

## 关键实现说明

- **进程管理**：`exec.Command` 启动分片进程（地面/洞穴各一个），`CREATE_NEW_PROCESS_GROUP` 脱离终端控制；长期持有 stdin 管道写入控制台命令；`cmd.Wait()` 监听退出实现崩溃检测与自动重启
- **存档路径**：用 `-persistent_storage_root` 指向程序目录下的 `dst_save/`，规避 Windows 默认 `文档\Klei` 路径受 OneDrive 重定向的影响
- **mod 预下载**：启动分片前以 `-only_update_server_mods` 跑一次预更新（引擎约 30 秒无回调即退出、一轮只下 1 个，面板自动多轮循环直至下载完整），分片启动时带 `-skip_update_server_mods` 避免多分片抢写共享目录
- **mods 并集同步**：`dedicated_server_mods_setup.lua` 是安装级文件，面板在 mod/房间增删改时自动重算为所有房间启用 mod 的并集

## 已知注意事项

- 地面（Master）与洞穴（Caves）是**两个独立进程**，端口必须错开
- 启动参数顺序敏感：`-persistent_storage_root <绝对路径> -conf_dir <单段名> -cluster <单段名> -shard <单段名>`，可执行文件工作目录必须是 `bin/`
- 配置文件（ini/lua）必须 **UTF-8 无 BOM**，否则中文乱码
- 停止服务器务必走面板的"停止"，停止前会自动保存存档

## 许可

自用项目，未附加开源许可。
