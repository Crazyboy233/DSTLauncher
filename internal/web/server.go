// Package web 提供 HTTP API 与实时日志推送。
package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dst-windows/internal/backup"
	"dst-windows/internal/config"
	"dst-windows/internal/modmgr"
	"dst-windows/internal/panelcfg"
	"dst-windows/internal/room"
	"dst-windows/internal/saveimport"
	"dst-windows/internal/server"
	"dst-windows/internal/steamcmd"
	"dst-windows/internal/workshop"
	"dst-windows/internal/worldsettings"
)

// Options 汇集 Web 层的全部依赖。
// 用结构体而非一长串位置参数，避免以后新增依赖时改乱所有调用点。
type Options struct {
	Mgr       *server.Manager
	Rooms     *room.Store
	Steam     *steamcmd.Manager
	StaticDir string
	RootDir   string // -persistent_storage_root
	ConfDir   string // -conf_dir
	WorkDir   string // 程序目录，用于读写 panel.json
	UgcDir    string // 共享工坊模组目录（-ugc_directory）
	Version   string // 面板版本号，展示在侧边栏，便于确认「跑的是哪个构建」
}

// Server 汇集所有模块的处理器，向前端暴露统一 API。
//
// 模组与备份管理器不预先构造：它们的目录随房间变化，
// 每次请求按 room 参数现算（构造本身只是拼路径，开销可忽略）。
type Server struct {
	mgr     *server.Manager
	rooms   *room.Store
	steam   *steamcmd.Manager
	static  string
	rootDir string
	confDir string
	workDir string
	ugcDir  string
	version string
}

// NewServer 创建 HTTP 服务。
func NewServer(o Options) *Server {
	return &Server{
		mgr:     o.Mgr,
		rooms:   o.Rooms,
		steam:   o.Steam,
		static:  o.StaticDir,
		rootDir: o.RootDir,
		confDir: o.ConfDir,
		workDir: o.WorkDir,
		ugcDir:  o.UgcDir,
		version: o.Version,
	}
}

// respond 统一响应格式。
func respond(w http.ResponseWriter, code int, data interface{}, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": 500, "message": err.Error(), "data": nil,
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"code": code, "message": "success", "data": data,
	})
}

// Handler 构建路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 房间（配置的唯一事实来源）
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/rooms/meta", s.handleRoomMeta)

	// 房间管理员（adminlist.txt）
	mux.HandleFunc("/api/admins", s.handleAdmins)

	// 进程控制
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/stop", s.handleStop)
	mux.HandleFunc("/api/restart", s.handleRestart)
	mux.HandleFunc("/api/cmd", s.handleCmd)

	// 日志
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/logs/stream", s.handleLogStream)

	// 模组
	mux.HandleFunc("/api/mods", s.handleMods)
	mux.HandleFunc("/api/mods/toggle", s.handleModToggle)
	mux.HandleFunc("/api/mods/config", s.handleModConfig)
	mux.HandleFunc("/api/mods/schema", s.handleModSchema)
	mux.HandleFunc("/api/mods/delete", s.handleModDelete)
	mux.HandleFunc("/api/mods/update", s.handleModsUpdate)

	// 创意工坊（搜索 / 详情 / API Key 设置）
	mux.HandleFunc("/api/workshop/details", s.handleWorkshopDetails)
	mux.HandleFunc("/api/workshop/search", s.handleWorkshopSearch)
	mux.HandleFunc("/api/workshop/key", s.handleWorkshopKey)

	// 世界配置（worldgenoverride.lua）
	mux.HandleFunc("/api/worldconfig", s.handleWorldConfig)

	// 备份
	mux.HandleFunc("/api/backups", s.handleBackups)
	mux.HandleFunc("/api/backups/create", s.handleBackupCreate)
	mux.HandleFunc("/api/backups/delete", s.handleBackupDelete)
	mux.HandleFunc("/api/backups/restore", s.handleBackupRestore)
	mux.HandleFunc("/api/backups/download", s.handleBackupDownload)

	// 存档上传（外部存档整包 -> 房间）
	mux.HandleFunc("/api/saves/upload", s.handleSaveUpload)

	// 生成结果预览
	mux.HandleFunc("/api/config", s.handleConfig)

	// SteamCMD 安装
	mux.HandleFunc("/api/install", s.handleInstall)
	mux.HandleFunc("/api/install/update", s.handleUpdate)

	// 服务器文件位置（可在面板里切换安装目录与架构）
	mux.HandleFunc("/api/server", s.handleServer)

	// 静态资源。index.html 必须禁缓存：前端产物带 hash 文件名，
	// 浏览器缓存了旧 index.html 就会继续加载旧 bundle，
	// 表现为「后端明明更新了，页面行为还是老的」
	mux.Handle("/", noCacheHTML(http.FileServer(http.Dir(s.static))))

	return mux
}

// noCacheHTML 对 HTML 文档响应禁用缓存，带 hash 的静态资源不受影响。
func noCacheHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}

// query 读取查询参数。
func query(r *http.Request, key string) string {
	return r.URL.Query().Get(key)
}

// ---- 房间 ----

// roomByID 解析 ?room= 参数。缺省时回退到第一个房间。
func (s *Server) roomByID(r *http.Request) (*room.Room, error) {
	raw := query(r, "room")
	list := s.rooms.List()
	if raw == "" {
		if len(list) == 0 {
			return nil, fmt.Errorf("还没有任何房间，请先在「房间」页新建")
		}
		return list[0], nil
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("房间参数不合法: %s", raw)
	}
	rm, ok := s.rooms.Get(id)
	if !ok {
		return nil, fmt.Errorf("房间不存在: %d", id)
	}
	return rm, nil
}

// roomView 是返回给前端的房间信息：房间配置 + 进程状态 + 存档情况。
type roomView struct {
	*room.Room
	Key       string          `json:"key"`
	Running   bool            `json:"running"`
	Statuses  []server.Status `json:"statuses"`
	HasSave   bool            `json:"hasSave"`
	NeedToken bool            `json:"needToken"`
}

func (s *Server) roomViews() []roomView {
	list := s.rooms.List()
	out := make([]roomView, 0, len(list))
	for _, rm := range list {
		sts := s.mgr.Status(rm.Key())
		running := false
		for _, st := range sts {
			if st.State == "running" || st.State == "starting" || st.State == "stopping" {
				running = true
				break
			}
		}
		out = append(out, roomView{
			Room:      rm,
			Key:       rm.Key(),
			Running:   running,
			Statuses:  sts,
			HasSave:   rm.ExistsSave(s.rootDir, s.confDir),
			NeedToken: rm.NeedsToken() && !rm.HasToken(),
		})
	}
	return out
}

// handleRooms 房间的查询 / 新建 / 修改。
func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respond(w, 200, s.roomViews(), nil)

	case http.MethodPost:
		var rm room.Room
		if err := json.NewDecoder(r.Body).Decode(&rm); err != nil {
			respond(w, 200, nil, fmt.Errorf("解析请求体失败: %w", err))
			return
		}
		created, err := s.rooms.Create(&rm)
		if err != nil {
			respond(w, 200, nil, err)
			return
		}
		// 配置落盘失败不回收房间：用户可在面板上改好端口后重新保存
		if err := room.Generate(s.rootDir, s.confDir, created); err != nil {
			respond(w, 200, created, fmt.Errorf("房间已创建，但配置写入失败: %w", err))
			return
		}
		s.mgr.Register(server.NewCluster(created))
		// 新房间可能自带 modoverrides（存档上传场景），清单要重算并集
		s.syncModsSetup()
		respond(w, 200, created, nil)

	case http.MethodPut:
		var rm room.Room
		if err := json.NewDecoder(r.Body).Decode(&rm); err != nil {
			respond(w, 200, nil, fmt.Errorf("解析请求体失败: %w", err))
			return
		}
		if rm.ID == 0 {
			respond(w, 200, nil, fmt.Errorf("缺少房间 ID"))
			return
		}
		updated, err := s.rooms.Update(&rm)
		if err != nil {
			respond(w, 200, nil, err)
			return
		}
		if err := room.Generate(s.rootDir, s.confDir, updated); err != nil {
			respond(w, 200, updated, fmt.Errorf("房间已保存，但配置写入失败: %w", err))
			return
		}
		// 覆盖注册，让新的端口与分片列表立即生效
		s.mgr.Register(server.NewCluster(updated))
		s.syncModsSetup()
		// 运行中的进程不会重新读取 ini，必须重启才生效，这里如实告知
		if s.mgr.AnyRunning(updated.Key()) {
			respond(w, 200, map[string]interface{}{
				"room": updated,
				"hint": "配置已写入，但房间正在运行，需重启房间后生效",
			}, nil)
			return
		}
		respond(w, 200, updated, nil)

	case http.MethodDelete:
		rm, err := s.roomByID(r)
		if err != nil {
			respond(w, 200, nil, err)
			return
		}
		if s.mgr.AnyRunning(rm.Key()) {
			respond(w, 200, nil, fmt.Errorf("房间正在运行，请先停止再删除"))
			return
		}
		hadSave := rm.ExistsSave(s.rootDir, s.confDir)
		if err := s.rooms.Delete(rm.ID); err != nil {
			respond(w, 200, nil, err)
			return
		}
		// 只注销进程层记录，保留磁盘上的存档目录——
		// 误删存档无法挽回，清理交给用户手工完成
		_ = s.mgr.Unregister(rm.Key(), 30*time.Second)
		// 被删房间启用的模组不再参与并集，清单要跟着收敛
		s.syncModsSetup()
		msg := "房间已删除"
		if hadSave {
			msg = fmt.Sprintf("房间已删除，世界存档仍保留在 %s", rm.ClusterDir(s.rootDir, s.confDir))
		}
		respond(w, 200, map[string]interface{}{"message": msg, "keptDir": hadSave}, nil)

	default:
		respond(w, 200, nil, fmt.Errorf("不支持的方法: %s", r.Method))
	}
}

// handleRoomMeta 返回表单需要的枚举值与默认值，避免前端硬编码。
func (s *Server) handleRoomMeta(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]interface{}{
		"gameModes":    room.ValidGameModes(),
		"languages":    room.ValidLanguages(),
		"tickRates":    []int{15, 30, 60},
		"defaults":     room.Defaults(),
		"roomsFile":    s.rooms.Path(),
		"panelVersion": s.version,
	}, nil)
}

// ---- 房间管理员（adminlist.txt）----

// handleAdmins 管理员名单的查询 / 追加 / 移除。
// 名单在服务器启动时读取：运行中修改不报错，但提示需重启生效。
func (s *Server) handleAdmins(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	clusterDir := rm.ClusterDir(s.rootDir, s.confDir)
	path := room.AdminsPath(clusterDir)

	switch r.Method {
	case http.MethodGet:
		list, err := room.ReadAdmins(clusterDir)
		respond(w, 200, map[string]interface{}{
			"file":        path,
			"admins":      list,
			"needRestart": s.mgr.AnyRunning(rm.Key()),
		}, err)

	case http.MethodPost:
		var in struct {
			KleiID string `json:"kleiId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			respond(w, 200, nil, fmt.Errorf("解析请求体失败: %w", err))
			return
		}
		list, err := room.AddAdmin(clusterDir, in.KleiID)
		if err != nil {
			respond(w, 200, nil, err)
			return
		}
		respond(w, 200, map[string]interface{}{
			"file":        path,
			"admins":      list,
			"needRestart": s.mgr.AnyRunning(rm.Key()),
		}, nil)

	case http.MethodDelete:
		list, err := room.RemoveAdmin(clusterDir, query(r, "id"))
		if err != nil {
			respond(w, 200, nil, err)
			return
		}
		respond(w, 200, map[string]interface{}{
			"file":        path,
			"admins":      list,
			"needRestart": s.mgr.AnyRunning(rm.Key()),
		}, nil)

	default:
		respond(w, 200, nil, fmt.Errorf("不支持的方法: %s", r.Method))
	}
}

// ---- 进程控制 ----

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	respond(w, 200, s.mgr.Status(rm.Key()), nil)
}

// withRoom 把「取房间 -> 执行 -> 回状态」三步收敛成一个调用。
func (s *Server) withRoom(w http.ResponseWriter, r *http.Request, fn func(rm *room.Room) error) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	err = fn(rm)
	respond(w, 200, s.mgr.Status(rm.Key()), err)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	// 在线开服没有令牌会被 Steam 拒绝注册，提前拦下比等服务器报错更省事
	if rm.NeedsToken() && !rm.HasToken() {
		respond(w, 200, nil, fmt.Errorf("当前为在线模式，请先填写 Klei 服务器令牌（cluster_token.txt）"))
		return
	}

	// 服务器以 -skip_update_server_mods 启动，缺模组会直接加载失败；
	// 提前拦下比让用户去翻日志省事
	if s.ugcDir != "" {
		if missing, err := s.modsFor(rm).MissingMods(); err == nil && len(missing) > 0 {
			respond(w, 200, nil, fmt.Errorf(
				"以下模组尚未下载：%s。请先到「模组」页点击「更新模组」",
				strings.Join(missing, "、")))
			return
		}
	}

	shard := query(r, "shard")
	if shard == "" {
		err = s.mgr.Start(rm.Key())
	} else {
		err = s.mgr.StartShard(rm.Key(), shard)
	}
	respond(w, 200, s.mgr.Status(rm.Key()), err)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	timeout := 30 * time.Second
	if v := query(r, "timeout"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	s.withRoom(w, r, func(rm *room.Room) error {
		if shard := query(r, "shard"); shard != "" {
			return s.mgr.StopShard(rm.Key(), shard, timeout)
		}
		return s.mgr.Stop(rm.Key(), timeout)
	})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	// 重启前必须优雅关闭，否则会丢存档
	if shard := query(r, "shard"); shard != "" {
		if err := s.mgr.StopShard(rm.Key(), shard, 30*time.Second); err != nil {
			respond(w, 200, s.mgr.Status(rm.Key()), err)
			return
		}
		time.Sleep(2 * time.Second)
		err = s.mgr.StartShard(rm.Key(), shard)
	} else {
		if err := s.mgr.Stop(rm.Key(), 30*time.Second); err != nil {
			respond(w, 200, s.mgr.Status(rm.Key()), err)
			return
		}
		time.Sleep(2 * time.Second)
		err = s.mgr.Start(rm.Key())
	}
	respond(w, 200, s.mgr.Status(rm.Key()), err)
}

func (s *Server) handleCmd(w http.ResponseWriter, r *http.Request) {
	shard, cmd := query(r, "shard"), query(r, "cmd")
	if strings.TrimSpace(cmd) == "" {
		respond(w, 200, nil, fmt.Errorf("命令不能为空"))
		return
	}
	if shard == "" {
		shard = "Master"
	}
	s.withRoom(w, r, func(rm *room.Room) error {
		return s.mgr.SendCmd(rm.Key(), shard, cmd)
	})
}

// ---- 日志 ----

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	n := 200
	if v := query(r, "lines"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			n = i
		}
	}
	lines, err := s.mgr.ReadLog(rm.Key(), shardOf(r), n)
	respond(w, 200, lines, err)
}

// shardOf 读取分片参数，缺省为 Master。
func shardOf(r *http.Request) string {
	if s := query(r, "shard"); s != "" {
		return s
	}
	return "Master"
}

// matchEvent 判断事件是否命中订阅者的过滤条件。
// 空值表示不过滤（订阅全部）。
func matchEvent(ev server.Event, cluster, shard string) bool {
	if cluster != "" && ev.Cluster != cluster {
		return false
	}
	if shard != "" && ev.Shard != shard {
		return false
	}
	return true
}

// handleLogStream 以 SSE 推送实时日志与进程事件。
//
// 相比 WebSocket，SSE 在本机场景下更简单且自带断线重连。
// 每个连接独立订阅（broker 扇出），多开标签页不会互相抢事件。
// 用 ?room=<id>&shard=<name> 只订阅指定房间的分片，缺省则推送全部。
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// 房间参数不合法时直接拒绝，避免前端静默收到空流
	wantCluster := ""
	if query(r, "room") != "" {
		rm, err := s.roomByID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		wantCluster = rm.Key()
	}
	wantShard := query(r, "shard")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	lines := 200
	if v := query(r, "lines"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			lines = i
		}
	}

	send := func(ev server.Event) {
		data, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}

	// 先补发历史日志，避免前端刚打开时一片空白
	if wantCluster != "" && wantShard != "" {
		if hist, err := s.mgr.ReadLog(wantCluster, wantShard, lines); err == nil {
			for _, l := range hist {
				send(server.Event{
					Time: time.Now(), Cluster: wantCluster, Shard: wantShard,
					Type: "log", Message: l,
				})
			}
		}
	}
	flusher.Flush()

	id, ch := s.mgr.Subscribe()
	defer s.mgr.Unsubscribe(id)

	// 每 15 秒发一个注释心跳，防止中间层把空闲连接掐掉
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			// 把队列里已积压的事件一次性带出去，合并成一次 flush。
			// 启动阶段每秒几百行日志，逐行 flush 会把写系统调用与前端的
			// SSE 事件解析都打满；批量发送对浏览器没有任何语义差别。
			count := 0
		drain:
			for {
				if matchEvent(ev, wantCluster, wantShard) {
					send(ev)
					count++
				}
				select {
				case next, ok2 := <-ch:
					if !ok2 {
						break drain
					}
					ev = next
				default:
					break drain
				}
			}
			if count > 0 {
				flusher.Flush()
			}
		}
	}
}

// ---- 模组 ----

// modsFor 按房间构造模组管理器。
// overrides 写在存档侧（集群目录），模组实体在游戏侧的共享 ugc 目录。
func (s *Server) modsFor(rm *room.Room) *modmgr.Manager {
	return modmgr.NewManager(
		rm.ClusterDir(s.rootDir, s.confDir),
		s.steam.GameDir(),
		s.ugcDir,
		rm.ShardNames(),
	)
}

// SyncModsSetup 供面板启动时重算安装级的模组下载清单。
// 面板关闭期间房间配置可能被手工改动过，启动时按所有房间重算一次并集最稳妥。
func (s *Server) SyncModsSetup() { s.syncModsSetup() }

// ---- 创意工坊（搜索 / 详情） ----

// installedSet 返回共享目录里已下载的模组集合。
func (s *Server) installedSet() map[string]bool {
	installed, err := modmgr.NewManager("", s.steam.GameDir(), s.ugcDir, nil).InstalledMods()
	if err != nil {
		return map[string]bool{}
	}
	return installed
}

// handleWorkshopDetails 按 ID / 链接查询创意工坊模组详情（免 key）。
// input 可以是纯数字、workshop-数字、创意工坊链接，或它们的逗号分隔组合。
func (s *Server) handleWorkshopDetails(w http.ResponseWriter, r *http.Request) {
	input := query(r, "input")
	ids := workshop.NormalizeIDs(strings.Split(input, ","))
	if len(ids) == 0 {
		respond(w, 200, nil, fmt.Errorf("请输入创意工坊模组 ID 或链接"))
		return
	}
	items, err := workshop.Details(ids)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	if len(items) == 0 {
		respond(w, 200, nil, fmt.Errorf("没有查到对应的模组（ID 不存在或已从创意工坊下架）"))
		return
	}
	installed := s.installedSet()
	for i := range items {
		items[i].Installed = installed["workshop-"+items[i].ID]
	}
	respond(w, 200, items, nil)
}

// handleWorkshopSearch 按关键词搜索创意工坊。
// 需要 Steam Web API Key（panel.json 的 steamApiKey），没有时给出明确指引。
func (s *Server) handleWorkshopSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(query(r, "q"))
	if q == "" {
		respond(w, 200, nil, fmt.Errorf("请输入搜索关键词"))
		return
	}
	page, _ := strconv.Atoi(query(r, "page"))
	apiKey := panelcfg.Load(s.workDir).SteamApiKey
	if apiKey == "" {
		respond(w, 200, nil, fmt.Errorf(
			"关键词搜索需要 Steam Web API Key：点击下方「设置 API Key」填入后即可使用；不填也可以直接粘贴模组 ID 或链接查询"))
		return
	}
	items, err := workshop.Search(apiKey, q, page)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	installed := s.installedSet()
	for i := range items {
		items[i].Installed = installed["workshop-"+items[i].ID]
	}
	respond(w, 200, items, nil)
}

// handleWorkshopKey 查询 / 保存 Steam Web API Key（创意工坊搜索用）。
// 单独一个端点而不是并进「服务器位置」的设置接口，避免两处保存互相覆盖。
func (s *Server) handleWorkshopKey(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respond(w, 200, map[string]string{"steamApiKey": panelcfg.Load(s.workDir).SteamApiKey}, nil)

	case http.MethodPut:
		var req struct {
			SteamApiKey string `json:"steamApiKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond(w, 200, nil, fmt.Errorf("解析请求体失败: %w", err))
			return
		}
		cfg := panelcfg.Load(s.workDir)
		cfg.SteamApiKey = strings.TrimSpace(req.SteamApiKey)
		if err := panelcfg.Save(s.workDir, cfg); err != nil {
			respond(w, 200, nil, fmt.Errorf("写入 %s 失败: %w", panelcfg.Path(s.workDir), err))
			return
		}
		respond(w, 200, map[string]string{"steamApiKey": cfg.SteamApiKey}, nil)

	default:
		respond(w, 200, nil, fmt.Errorf("不支持的方法: %s", r.Method))
	}
}

// SwapModsSetup 供单模组更新临时改写安装级下载清单：
// 只写目标模组一条，让引擎这一轮只下载它。
// 返回的 restore 在会话结束后恢复所有房间的并集。
// 必须成对使用——更新会话以 defer 保证 restore 一定执行。
func (s *Server) SwapModsSetup(target string) (func(), error) {
	num := strings.TrimPrefix(target, "workshop-")
	mods := modmgr.NewManager("", s.steam.GameDir(), s.ugcDir, nil)
	if err := mods.WriteSetupLua([]string{num}); err != nil {
		return nil, err
	}
	return s.syncModsSetup, nil
}

// MissingModIDs 汇总所有房间「已启用但尚未下载」的工坊模组（去重）。
// 供模组更新循环判断进度：它只关心这个列表是否清零。
func (s *Server) MissingModIDs() ([]string, error) {
	if s.ugcDir == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, rm := range s.rooms.List() {
		missing, err := s.modsFor(rm).MissingMods()
		if err != nil {
			continue
		}
		for _, id := range missing {
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// syncModsSetup 重写安装级的 dedicated_server_mods_setup.lua。
//
// 服务器开服时按这个文件下载缺失的模组，而它是整个服务器只有一份的安装级文件，
// 内容必须是「所有房间启用模组的并集」。按单个房间写会互相覆盖：
// B 房间保存配置会把 A 房间要用的模组从清单里抹掉。
// 因此每次模组或房间发生增删改之后都要重算一次。
func (s *Server) syncModsSetup() {
	if s.ugcDir == "" {
		return
	}
	seen := map[string]bool{}
	for _, rm := range s.rooms.List() {
		o, err := s.modsFor(rm).LoadOverrides()
		if err != nil {
			fmt.Printf("[!] 读取房间「%s」的模组配置失败: %v\n", rm.Name, err)
			continue
		}
		for _, id := range o.EnabledWorkshopIDs() {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	// 不依赖任何具体房间：没有房间时也要能清空清单
	mods := modmgr.NewManager("", s.steam.GameDir(), s.ugcDir, nil)
	if err := mods.WriteSetupLua(ids); err != nil {
		fmt.Printf("[!] 写入 dedicated_server_mods_setup.lua 失败: %v\n", err)
	}
}

func (s *Server) handleMods(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	mods := s.modsFor(rm)

	installed, err := mods.InstalledMods()
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	o, err := mods.LoadOverrides()
	if err != nil {
		respond(w, 200, nil, err)
		return
	}

	list := make([]*modmgr.Mod, 0)
	seen := make(map[string]bool)
	add := func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		name, _ := mods.ModInfo(id)
		list = append(list, &modmgr.Mod{
			ID:        id,
			Name:      name,
			Enabled:   o.Enabled[id],
			Installed: installed[id],
			Config:    o.Config[id],
			Priority:  o.Priority[id],
		})
	}
	// overrides 中的顺序即用户期望的加载顺序
	for _, id := range o.Order {
		add(id)
	}
	// 已下载但未在 overrides 中出现的模组也列出来
	for id := range installed {
		add(id)
	}

	respond(w, 200, list, nil)
}

func (s *Server) handleModToggle(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	if err := s.modsFor(rm).SetEnabled(query(r, "id"), query(r, "enabled") == "true"); err != nil {
		respond(w, 200, nil, err)
		return
	}
	s.syncModsSetup()
	respond(w, 200, nil, nil)
}

func (s *Server) handleModConfig(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	id, key := query(r, "id"), query(r, "key")
	if id == "" || key == "" {
		respond(w, 200, nil, fmt.Errorf("缺少 id 或 key 参数"))
		return
	}
	// 前端以 JSON 字符串传递值，兼容数字与布尔
	var value interface{}
	raw := query(r, "value")
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		value = raw // 解析失败则按字符串处理
	}
	if err := s.modsFor(rm).SetConfig(id, key, value); err != nil {
		respond(w, 200, nil, err)
		return
	}
	s.syncModsSetup()
	respond(w, 200, nil, nil)
}

func (s *Server) handleModSchema(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	schema, err := s.modsFor(rm).ConfigSchema(query(r, "id"))
	respond(w, 200, schema, err)
}

func (s *Server) handleModDelete(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	if err := s.modsFor(rm).DeleteMod(query(r, "id")); err != nil {
		respond(w, 200, nil, err)
		return
	}
	s.syncModsSetup()
	respond(w, 200, nil, nil)
}

// handleModsUpdate 触发或查询模组预更新。
//
// 更新是一个可能持续几十分钟的异步过程：POST 立即返回，前端轮询 GET 拿进度。
// 进度来自服务器输出里的百分比，完整输出一并返回供界面展示。
func (s *Server) handleModsUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respond(w, 200, s.mgr.ModsUpdateStatus(), nil)

	case http.MethodPost:
		rm, err := s.roomByID(r)
		if err != nil {
			respond(w, 200, nil, err)
			return
		}
		if err := s.mgr.UpdateMods(rm.Key(), query(r, "shard"), query(r, "id")); err != nil {
			respond(w, 200, nil, err)
			return
		}
		respond(w, 200, s.mgr.ModsUpdateStatus(), nil)

	case http.MethodDelete:
		respond(w, 200, nil, s.mgr.StopModsUpdate())

	default:
		respond(w, 200, nil, fmt.Errorf("不支持的方法: %s", r.Method))
	}
}

// ---- 备份 ----

func (s *Server) bakFor(rm *room.Room) *backup.Manager {
	return backup.NewManager(s.rootDir, s.confDir, rm.ID)
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	items, err := s.bakFor(rm).List()
	respond(w, 200, items, err)
}

// handleBackupCreate 备份整个集群目录。
// 备份前对所有运行中的分片发 c_save() 并等待落盘，否则会拷到半截存档。
func (s *Server) handleBackupCreate(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	// 有分片在运行时会先请求落盘并等待，无运行分片则立即返回
	s.mgr.SaveCluster(rm.Key(), 5*time.Second)
	item, err := s.bakFor(rm).Create()
	respond(w, 200, item, err)
}

func (s *Server) handleBackupDelete(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	respond(w, 200, nil, s.bakFor(rm).Delete(query(r, "name")))
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	// 回滚前必须停服，否则运行中的服务器会用内存状态覆盖恢复的存档，
	// 且 Windows 上被占用的文件会导致覆盖失败。
	if s.mgr.AnyRunning(rm.Key()) {
		respond(w, 200, nil, fmt.Errorf("请先停止服务器再回滚存档"))
		return
	}
	respond(w, 200, nil, s.bakFor(rm).Restore(query(r, "name")))
}

// handleBackupDownload 把备份 zip 原样下发给浏览器。
// 用 http.ServeFile 而不是 respond()：这里要的是文件流，
// Content-Disposition 里的文件名取自备份名（Cluster_N_时间戳.zip，纯 ASCII，无需编码）。
func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	name := query(r, "name")
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".zip") {
		respond(w, 200, nil, fmt.Errorf("非法的备份文件名"))
		return
	}
	path := filepath.Join(s.rootDir, "backups", fmt.Sprintf("Cluster_%d", rm.ID), name)
	if _, err := os.Stat(path); err != nil {
		respond(w, 200, nil, fmt.Errorf("备份不存在: %s", name))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, path)
}

// ---- 存档上传 ----

// maxUploadBytes 是上传压缩包的大小上限。
// DST 存档长期运行后可能到 GB 级（地表 + 洞穴），给足余量；
// 上限只是为了挡住误传的超大文件把磁盘写满，不是功能限制。
const maxUploadBytes = 8 << 30

// saveImportSummary 是上传成功后回给前端的导入摘要，
// 用于在界面上说明「这次覆盖了哪些内容」，而不是只弹一句成功。
type saveImportSummary struct {
	NewRoom     bool     `json:"newRoom"`
	RoomID      int      `json:"roomId"`
	RoomName    string   `json:"roomName"`
	Shards      []string `json:"shards"`
	HasSave     bool     `json:"hasSave"`
	TokenSource string   `json:"tokenSource"`
	WorldFiles  []string `json:"worldFiles"`
	ListFiles   []string `json:"listFiles"`
	// ModsTotal 是存档里启用的模组数量；ModsMissing 是本地尚未下载的。
	// 缺失的模组需要用户到「模组」页点「更新模组」补齐后才能启动。
	ModsTotal   int      `json:"modsTotal"`
	ModsMissing []string `json:"modsMissing"`
	Warnings    []string `json:"warnings"`
	Notes       []string `json:"notes"`
}

// handleSaveUpload 处理外部存档整包的上传。
//
// 语义：存档「占用」目标房间——存档里的可覆盖玩法配置写回房间，
// 世界数据拷进房间目录，随后由面板重新生成 ini/token。
// mode=new 时用存档新建房间，mode=overwrite 时覆盖 room 参数指定的房间。
//
// 端口、集群密钥、master_ip 一律保留目标房间的（新建则自动分配），
// 理由见 saveimport.MergeInto。
func (s *Server) handleSaveUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respond(w, 200, nil, fmt.Errorf("上传存档请使用 POST 请求"))
		return
	}

	// 先卡住请求体大小：超大文件不该先落满磁盘再报错
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respond(w, 200, nil, fmt.Errorf("解析上传内容失败（文件可能过大，或未选择文件）: %w", err))
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respond(w, 200, nil, fmt.Errorf("没有收到存档文件: %w", err))
		return
	}
	defer file.Close()

	// 定位目标房间
	var target *room.Room
	if r.FormValue("mode") == "overwrite" {
		id, convErr := strconv.Atoi(r.FormValue("room"))
		if convErr != nil || id <= 0 {
			respond(w, 200, nil, fmt.Errorf("请选择要覆盖的房间"))
			return
		}
		got, ok := s.rooms.Get(id)
		if !ok {
			respond(w, 200, nil, fmt.Errorf("房间不存在: %d", id))
			return
		}
		// 运行中的服务器会占住存档文件（Windows 下覆盖直接失败），
		// 而且退服时会用内存状态把新存档盖回去，所以必须先停服
		if s.mgr.AnyRunning(got.Key()) {
			respond(w, 200, nil, fmt.Errorf("房间「%s」正在运行，请先停止再上传存档", got.Name))
			return
		}
		target = got
	}

	work, err := saveimport.NewWorkDir(s.rootDir)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	defer func() { _ = os.RemoveAll(work) }()

	// 上传的文件名不可信，统一存成固定名字
	zipPath := filepath.Join(work, "upload.zip")
	if err := saveUploadedFile(file, zipPath); err != nil {
		respond(w, 200, nil, err)
		return
	}

	extractDir := filepath.Join(work, "extract")
	if err := saveimport.Extract(zipPath, extractDir); err != nil {
		respond(w, 200, nil, err)
		return
	}
	clusterDir, err := saveimport.FindClusterDir(extractDir)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}
	parsed, err := saveimport.Parse(clusterDir)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}

	// 先落房间定义，再写文件：房间 ID 决定集群目录名，顺序反了会找不到目标目录
	merged := saveimport.MergeInto(parsed, target)
	var saved *room.Room
	if target == nil {
		saved, err = s.rooms.Create(merged)
	} else {
		saved, err = s.rooms.Update(merged)
	}
	if err != nil {
		respond(w, 200, nil, err)
		return
	}

	notes, err := parsed.WriteInto(s.rootDir, s.confDir, saved)
	if err != nil {
		respond(w, 200, nil, fmt.Errorf("房间配置已保存，但写入存档数据失败: %w", err))
		return
	}
	// ini / token 统一由面板按房间配置生成，不照抄存档里的那份
	if err := room.Generate(s.rootDir, s.confDir, saved); err != nil {
		respond(w, 200, nil, fmt.Errorf("存档已导入，但生成配置文件失败: %w", err))
		return
	}

	// 存档自带的模组配置：解析后经面板以规范格式写回全部分片。
	// 刻意不照抄文件原文——否则面板后续的启用/禁用/改配置会跟存档原文打架。
	if parsed.HasMods && parsed.Mods != nil {
		if err := s.modsFor(saved).SaveOverrides(parsed.Mods); err != nil {
			respond(w, 200, nil, fmt.Errorf("存档已导入，但写入模组配置失败: %w", err))
			return
		}
	}
	// 下载清单是安装级的，模组或房间变化后必须重算并集
	s.syncModsSetup()

	s.mgr.Register(server.NewCluster(saved))

	// 汇总这次导入带了哪些模组、缺哪些，界面上直接告诉用户下一步该干嘛
	var modsMissing []string
	if parsed.HasMods && parsed.Mods != nil {
		if missing, mErr := s.modsFor(saved).MissingMods(); mErr == nil {
			modsMissing = missing
		}
	}

	respond(w, 200, map[string]interface{}{
		"room":    saved,
		"summary": buildImportSummary(parsed, saved, notes, target == nil, modsMissing),
	}, nil)
}

// saveUploadedFile 把上传的文件写到 dst。
func saveUploadedFile(src io.Reader, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("保存上传文件失败: %w", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		_ = out.Close()
		return fmt.Errorf("保存上传文件失败: %w", err)
	}
	return out.Close()
}

// buildImportSummary 汇总这次导入实际覆盖了什么。
func buildImportSummary(p *saveimport.Parsed, saved *room.Room, notes []string, isNew bool, modsMissing []string) saveImportSummary {
	sum := saveImportSummary{
		NewRoom:     isNew,
		RoomID:      saved.ID,
		RoomName:    saved.Name,
		HasSave:     p.HasSave,
		Warnings:    p.Warnings,
		Notes:       notes,
		ModsMissing: modsMissing,
	}
	if p.HasMods && p.Mods != nil {
		sum.ModsTotal = len(p.Mods.EnabledWorkshopIDs())
	}
	for _, sh := range p.Shards {
		sum.Shards = append(sum.Shards, sh.Name)
		if sh.HasOverride {
			sum.WorldFiles = append(sum.WorldFiles, fmt.Sprintf(
				"%s ← %s（%d 项）", sh.Name, sh.OverrideSrc, len(sh.Override.Overrides)))
		}
	}
	switch {
	case strings.TrimSpace(p.Token) != "":
		sum.TokenSource = "存档自带"
	case saved.HasToken():
		sum.TokenSource = "保留房间原有令牌"
	default:
		sum.TokenSource = "无（在线模式需自行填写）"
	}
	if p.HasAdmin {
		sum.ListFiles = append(sum.ListFiles, "adminlist.txt")
	}
	if p.HasBlock {
		sum.ListFiles = append(sum.ListFiles, "blocklist.txt")
	}
	if p.HasWhite {
		sum.ListFiles = append(sum.ListFiles, "whitelist.txt")
	}
	return sum
}

// ---- SteamCMD 安装 ----

// handleInstall 查询安装状态，或触发首次安装。
//
// 注意：复用 Steam 里已安装的专用服务器是**正常状态**，不是错误。
// 因此这里照常返回状态，另外把来源信息一并带上，
// 让前端能如实说明「服务器来自哪里」以及「更新该走 Steam」。
func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
	// 显式触发安装
	if r.URL.Query().Get("action") == "start" {
		if s.steam.IsExternal() {
			respond(w, 200, nil, fmt.Errorf(
				"当前已复用安装好的专用服务器（%s），无需在此安装；如需更新请在 Steam 中更新该工具",
				s.steam.Root()))
			return
		}
		if s.steam.IsInstalling() {
			respond(w, 200, nil, fmt.Errorf("安装正在进行中"))
			return
		}
		if err := s.requireNoRunningShards(); err != nil {
			respond(w, 200, nil, err)
			return
		}
		s.steam.Install()
		respond(w, 200, map[string]interface{}{"started": true}, nil)
		return
	}

	progress, errStr, output := s.steam.Progress()
	// errStr 为 "<nil>" 时视为无错误，避免前端拿到无意义字符串
	if errStr == "<nil>" {
		errStr = ""
	}
	respond(w, 200, map[string]interface{}{
		"installed":  s.steam.IsInstalled(),
		"installing": s.steam.IsInstalling(),
		"progress":   progress,
		"error":      errStr,
		"output":     output,
		"version":    s.steam.Version(),
		// 服务器来源
		"external":   s.steam.IsExternal(),
		"root":       s.steam.Root(),
		"executable": s.steam.Executable(),
		"source":     s.steam.Source(),
		"arch":       s.steam.Arch(),
	}, nil)
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if s.steam.IsExternal() {
		respond(w, 200, nil, fmt.Errorf(
			"服务器复用自 %s，请在 Steam 中更新「Don't Starve Together Dedicated Server」工具",
			s.steam.Root()))
		return
	}
	if !s.steam.IsInstalled() {
		respond(w, 200, nil, fmt.Errorf("服务器尚未安装"))
		return
	}
	if err := s.requireNoRunningShards(); err != nil {
		respond(w, 200, nil, err)
		return
	}
	err := s.steam.Update()
	respond(w, 200, map[string]interface{}{"started": err == nil}, err)
}

// requireNoRunningShards 在安装 / 更新服务器文件前确认没有分片在运行。
//
// Windows 下正在运行的 exe 与 dll 是被锁定的，SteamCMD 替换这些文件会失败，
// 最坏情况是留下一个「更新了一半」的安装——那种状态很容易在下次启动时崩溃。
func (s *Server) requireNoRunningShards() error {
	busy := s.mgr.RunningShards()
	if len(busy) == 0 {
		return nil
	}
	return fmt.Errorf(
		"有分片正在运行（%s），请先全部停止再安装或更新："+
			"Windows 下正在使用的服务器文件被锁定，更新会失败或只更新一半",
		strings.Join(busy, "、"))
}

// ---- 服务器文件位置 ----

// serverStatus 是 /api/server 的返回结构。
type serverStatus struct {
	Root        string              `json:"root"`
	Source      string              `json:"source"`
	Arch        string              `json:"arch"`     // 实际使用中的架构
	ArchPref    string              `json:"archPref"` // 用户设置的偏好
	External    bool                `json:"external"`
	Pinned      bool                `json:"pinned"` // 路径是显式指定的，不是自动探测
	Installed   bool                `json:"installed"`
	Installing  bool                `json:"installing"`
	Version     string              `json:"version"`
	Executable  string              `json:"executable"`
	ConfigPath  string              `json:"configPath"`
	Locked      bool                `json:"locked"` // 有分片在运行，此时不允许改
	ArchOptions []string            `json:"archOptions"`
	Candidates  []steamcmd.Detected `json:"candidates"`
}

func (s *Server) serverStatus() serverStatus {
	return serverStatus{
		Root:        s.steam.Root(),
		Source:      s.steam.Source(),
		Arch:        s.steam.Arch(),
		ArchPref:    s.steam.ArchPref(),
		External:    s.steam.IsExternal(),
		Pinned:      s.steam.Pinned(),
		Installed:   s.steam.IsInstalled(),
		Installing:  s.steam.IsInstalling(),
		Version:     s.steam.Version(),
		Executable:  s.steam.Executable(),
		ConfigPath:  panelcfg.Path(s.workDir),
		Locked:      s.anyRunning(),
		ArchOptions: []string{steamcmd.ArchAuto, steamcmd.Arch64, steamcmd.Arch32},
		// 每次查询都重新探测，用户装完 Steam 版工具后刷新页面就能看到
		Candidates: steamcmd.DetectInstalled(),
	}
}

// anyRunning 判断是否还有分片在跑。
// 直接过一遍进程层的全部记录，不依赖集群是否已注册。
func (s *Server) anyRunning() bool {
	for _, st := range s.mgr.Status("") {
		switch st.State {
		case "running", "starting", "stopping":
			return true
		}
	}
	return false
}

// handleServer 查询 / 修改「用哪一份专用服务器安装」。
//
// 运行中不允许改：正在跑的分片用的是旧目录下的可执行文件，
// 中途换掉会让「运行中的进程」与「面板认为的安装位置」对不上。
func (s *Server) handleServer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respond(w, 200, s.serverStatus(), nil)

	case http.MethodPut:
		var req struct {
			ServerDir string `json:"serverDir"`
			Arch      string `json:"arch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond(w, 200, nil, fmt.Errorf("解析请求体失败: %w", err))
			return
		}
		if s.anyRunning() {
			respond(w, 200, nil, fmt.Errorf("还有分片在运行，请先全部停止再更改服务器文件位置"))
			return
		}
		if s.steam.IsInstalling() {
			respond(w, 200, nil, fmt.Errorf("正在安装服务器文件，请等安装结束后再改"))
			return
		}

		// ServerDir 为空表示「保持/恢复自动探测」，此时只改架构
		if _, err := s.steam.Configure(req.ServerDir, req.Arch, ""); err != nil {
			respond(w, 200, nil, err)
			return
		}

		// 落盘，重启面板后仍然生效。
		// 以磁盘上的现有配置为底，只覆盖本次请求涉及的字段——
		// 否则设置页保存一次就会把别的接口写入的字段（如 steamApiKey）抹掉
		cfg := panelcfg.Load(s.workDir)
		cfg.ServerDir = strings.TrimSpace(req.ServerDir)
		cfg.Arch = s.steam.ArchPref()
		if err := panelcfg.Save(s.workDir, cfg); err != nil {
			respond(w, 200, s.serverStatus(),
				fmt.Errorf("设置已生效，但写入 %s 失败: %w", panelcfg.Path(s.workDir), err))
			return
		}
		respond(w, 200, s.serverStatus(), nil)

	default:
		respond(w, 200, nil, fmt.Errorf("不支持的方法: %s", r.Method))
	}
}

// ---- 世界配置（worldgenoverride.lua）----

// WorldConfigView 是返回给前端的世界配置视图。
type WorldConfigView struct {
	Shard   string `json:"shard"`
	File    string `json:"file"`
	Exists  bool   `json:"exists"` // 磁盘上是否已经有这个文件
	HasSave bool   `json:"hasSave"`

	Config  *worldsettings.Override           `json:"config"`
	Presets map[string][]worldsettings.Preset `json:"presets"` // worldgen / settings
	Groups  []worldsettings.Group             `json:"groups"`

	GameVersion string `json:"gameVersion"`
	GeneratedAt string `json:"generatedAt"`
}

// handleWorldConfig 读取或保存某个分片的世界配置。
//
// 只操作 worldgenoverride.lua。同目录下的 leveldataoverride.lua 优先级更高，
// 且按 Klei 的注释会「完全覆盖已保存的世界数据」，那是游戏内部文件，这里不碰。
func (s *Server) handleWorldConfig(w http.ResponseWriter, r *http.Request) {
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}

	shard := query(r, "shard")
	if shard == "" {
		shard = worldsettings.ShardMaster
	}
	if !s.shardEnabled(rm, shard) {
		respond(w, 200, nil, fmt.Errorf("房间未启用分片 %s", shard))
		return
	}

	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		s.saveWorldConfig(w, r, rm, shard)
		return
	}

	clusterDir := rm.ClusterDir(s.rootDir, s.confDir)
	ov, exists, err := worldsettings.ReadFile(clusterDir, shard)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}

	respond(w, 200, WorldConfigView{
		Shard:   shard,
		File:    worldsettings.FilePath(clusterDir, shard),
		Exists:  exists,
		HasSave: rm.ExistsSave(s.rootDir, s.confDir),
		Config:  ov,
		Presets: map[string][]worldsettings.Preset{
			worldsettings.CategoryWorldGen: worldsettings.Presets(shard, worldsettings.CategoryWorldGen),
			worldsettings.CategorySettings: worldsettings.Presets(shard, worldsettings.CategorySettings),
		},
		Groups:      worldsettings.Groups(shard, ""),
		GameVersion: worldsettings.Version(),
		GeneratedAt: worldsettings.GeneratedAt(),
	}, nil)
}

func (s *Server) saveWorldConfig(w http.ResponseWriter, r *http.Request, rm *room.Room, shard string) {
	var in struct {
		Shard string `json:"shard"`
		worldsettings.Override
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		respond(w, 200, nil, fmt.Errorf("解析请求体失败: %w", err))
		return
	}
	if in.Shard != "" {
		if !s.shardEnabled(rm, in.Shard) {
			respond(w, 200, nil, fmt.Errorf("房间未启用分片 %s", in.Shard))
			return
		}
		shard = in.Shard
	}

	if err := in.Override.Validate(); err != nil {
		respond(w, 200, nil, err)
		return
	}

	clusterDir := rm.ClusterDir(s.rootDir, s.confDir)
	if err := worldsettings.WriteFile(clusterDir, shard, &in.Override); err != nil {
		respond(w, 200, nil, err)
		return
	}

	respond(w, 200, map[string]interface{}{
		"file":   worldsettings.FilePath(clusterDir, shard),
		"config": &in.Override,
		// 游戏只在启动时读这个文件，运行中的分片不会感知到改动
		"needRestart": s.mgr.AnyRunning(rm.Key()),
	}, nil)
}

// shardEnabled 判断分片是否在该房间的启用范围内。
func (s *Server) shardEnabled(rm *room.Room, shard string) bool {
	for _, n := range rm.ShardNames() {
		if n == shard {
			return true
		}
	}
	return false
}

// ---- 配置预览 ----

// handleConfig 返回房间将生成的配置文件内容。
//
// 这里刻意只读：cluster.ini / server.ini 由房间表单统一生成，
// 如果额外允许直接改 ini，两边会互相覆盖，用户很难判断哪份才是最新的。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respond(w, 200, nil, fmt.Errorf("配置由「房间」表单统一管理，请到房间页修改后保存"))
		return
	}
	rm, err := s.roomByID(r)
	if err != nil {
		respond(w, 200, nil, err)
		return
	}

	// 预览用未保存的最新生成结果；顺带带上文件是否已落盘
	files := rm.Files(s.rootDir, s.confDir)
	type fileView struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		OnDisk   bool   `json:"onDisk"`
		Redacted bool   `json:"redacted"`
	}
	out := make([]fileView, 0, len(files))
	for path, content := range files {
		// 令牌文件不回传明文：预览场景没有必要，降低误泄露风险
		redact := filepath.Base(path) == "cluster_token.txt"
		if redact {
			content = maskToken(content)
		}
		out = append(out, fileView{
			Path:     path,
			Content:  content,
			OnDisk:   config.FileExists(path),
			Redacted: redact,
		})
	}
	// 按路径排序，保证前端每次展示顺序一致
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	respond(w, 200, map[string]interface{}{
		"clusterDir": rm.ClusterDir(s.rootDir, s.confDir),
		"files":      out,
	}, nil)
}

// maskToken 只保留令牌的首尾各 4 位，中间用星号代替。
func maskToken(t string) string {
	t = strings.TrimSpace(t)
	if t == "" {
		return "（未配置，在线模式下服务器无法注册到 Steam）"
	}
	if len(t) <= 12 {
		return strings.Repeat("*", len(t))
	}
	return t[:4] + strings.Repeat("*", 8) + t[len(t)-4:]
}
