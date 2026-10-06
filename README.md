# dst-windows

Windows 原生《饥荒联机版》(Don't Starve Together) 专用服务器管理系统。

参考 [dst-management-platform-api](../dst-management-platform-api)（DMP）的功能设计，但不 fork、不依赖其 Linux 组件——用 Go 原生进程管理替代 `screen`/`bash`/`ps`/`tail`，可在 Windows 上直接运行，无需 WSL。

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

```bash
# 后端（在仓库根目录）
go build -o dst-windows.exe .

# 前端（修改 web/ 下代码后需要重新构建）
cd web
npm install
npm run build   # 产物输出到 web/dist，由 Go 端 embed 提供

# 运行
./dst-windows.exe              # 默认端口 127.0.0.1:8899
./dst-windows.exe -port 8080   # 指定端口
./dst-windows.exe -install     # 仅安装服务器文件后退出
./dst-windows.exe -v           # 显示版本
```

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
  server/               进程管理层（Go 原生替代 screen）：启停、守护、崩溃重启
  room/                 房间：创建、配置生成、cluster.ini/server.ini/token、管理员名单
  config/               ini 读写、路径计算（-persistent_storage_root 等）
  steamcmd/             SteamCMD 调用、Steam 库探测、服务器安装与更新
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

## 与 DMP 的主要差异

| | DMP (Linux) | dst-windows (Windows) |
|---|---|---|
| 进程管理 | GNU screen | Go `exec.Command` + `StdinPipe` |
| 找进程 | `ps -ef \| grep \| awk` | Windows API / gopsutil |
| 日志读取 | `tail -1000` | 文件读取 + SSE 推流 |
| 存档位置 | `~/.klei` | 程序目录下 `dst_save/`（规避 OneDrive 重定向） |
| 依赖注入 | LuaJIT `.so`（TMI） | 无 |

## 已知注意事项

- 地面（Master）与洞穴（Caves）是**两个独立进程**，端口必须错开
- 启动参数顺序敏感：`-persistent_storage_root <绝对路径> -conf_dir <单段名> -cluster <单段名> -shard <单段名>`，可执行文件工作目录必须是 `bin/`
- 配置文件（ini/lua）必须 **UTF-8 无 BOM**，否则中文乱码
- 引擎限制：`-only_update_server_mods` 下载阶段约 30 秒无回调就退出，一轮往往只下 1 个 mod，面板会自动多轮循环直至下载完整
- 停止服务器务必走面板的"停止"，进程会被直接终止

## 许可

自用项目，未附加开源许可。
