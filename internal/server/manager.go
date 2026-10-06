// Package server 实现 DST 专用服务器的进程管理层。
//
// 这是整个系统最核心的一层，负责替代 DMP 依赖的 GNU screen：
//   - 启动：exec.Command + CREATE_NEW_PROCESS_GROUP，让进程脱离控制台独立存活
//   - 命令注入：长期持有 cmd.StdinPipe()，直接往进程标准输入写控制台命令
//   - 崩溃检测：cmd.Wait() 监听退出，返回非零退出码即判定为崩溃
//   - 优雅关闭：必须先发 c_save() 再 c_shutdown()，严禁直接 Kill
package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"dst-windows/internal/config"
	"dst-windows/internal/room"
)

// ShardPort 是一个分片占用的端口组。
// 三个端口都必须全局唯一，多房间并存时尤其重要。
type ShardPort struct {
	ServerPort         int `json:"serverPort"`
	MasterServerPort   int `json:"masterServerPort"`
	AuthenticationPort int `json:"authenticationPort"`
}

// Cluster 是进程管理层使用的集群视图，由房间配置派生。
//
// 注意：ConfDir 对所有房间都是同一个值（-conf_dir 只是存档根下的一个命名空间目录），
// 因此它**不能**作为集群标识——真正的唯一标识是 Key()，即 Cluster_<ID>。
type Cluster struct {
	ID         int                  `json:"id"`
	Name       string               `json:"name"`       // 房间名，仅用于展示
	ConfDir    string               `json:"confDir"`    // -conf_dir
	Shards     []string             `json:"shards"`     // 启用中的分片名
	Ports      map[string]ShardPort `json:"ports"`      // 分片名 -> 端口组
	MasterPort int                  `json:"masterPort"` // cluster.ini [SHARD] master_port
}

// Key 返回集群的唯一标识，同时就是存档目录名。
func (c *Cluster) Key() string {
	return fmt.Sprintf("Cluster_%d", c.ID)
}

// NewCluster 由房间配置派生进程层视图。
// 房间未启用洞穴时，Caves 不会出现在 Shards 里，也就不会被启动。
func NewCluster(r *room.Room) *Cluster {
	ports := make(map[string]ShardPort, len(r.Shards))
	for _, s := range r.Shards {
		ports[s.Name] = ShardPort{
			ServerPort:         s.ServerPort,
			MasterServerPort:   s.MasterServerPort,
			AuthenticationPort: s.AuthenticationPort,
		}
	}
	return &Cluster{
		ID:         r.ID,
		Name:       r.Name,
		ConfDir:    ConfDir,
		Shards:     r.ShardNames(),
		Ports:      ports,
		MasterPort: r.MasterPort,
	}
}

// ConfDir 是 -conf_dir 的固定取值，所有房间共用同一层命名空间目录。
const ConfDir = "DST"

// port 返回分片端口，缺失时退化为一组默认值，避免启动参数里出现 0。
func (c *Cluster) port(shard string) ShardPort {
	if p, ok := c.Ports[shard]; ok && p.ServerPort > 0 {
		return p
	}
	base := 11000
	if shard == "Caves" {
		base = 11001
	}
	return ShardPort{ServerPort: base, MasterServerPort: base + 20000, AuthenticationPort: base + 30000}
}

// State 表示分片的运行状态。
type State int

const (
	StateStopped State = iota
	StateStarting
	StateRunning
	StateStopping
	StateCrashed
)

func (s State) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateCrashed:
		return "crashed"
	}
	return "unknown"
}

// process 是一个运行中的分片进程及其配套资源。
type process struct {
	cluster   *Cluster
	shard     string
	cmd       *exec.Cmd
	stdin     io.WriteCloser // 写管道，用于注入控制台命令
	state     State
	pid       int
	startedAt time.Time
	restarts  int // 连续重启次数，用于指数退避
	lastErr   error

	port         int    // 该分片的游戏端口，用于「是否已开始监听」判据
	startVersion string // 启动时的游戏版本，用于发现「游戏更新后没重启」

	mu        sync.RWMutex
	logWriter *LogTailer
	done      chan struct{}
	closeOnce sync.Once

	// res 存上一次的资源采样，CPU 占用率要靠它做差值（见 procResource）
	res procSample
}

// procSample 是资源采样的上一次取值。
// 类型定义在这里而不是 procstat_windows.go，是为了让 process 的字段与平台无关。
type procSample struct {
	at  time.Time     // 上次采样时刻
	cpu time.Duration // 上次的累计 CPU 时间
	pct float64       // 上次算出的占用率，采样间隔过短时直接复用
}

// ExeResolver 返回服务器可执行文件路径与其工作目录。
//
// 做成回调而不是在构造时把路径定死：面板可能先启动、后安装服务器，
// 也可能装完才发现官方还提供了 64 位可执行文件，这些都要在启动分片时才能确定。
type ExeResolver func() (exePath, workDir string)

// 默认可执行文件名（面板自带安装的 32 位版本）。
const defaultExeName = "dontstarve_dedicated_server_nullrenderer.exe"

// Manager 管理所有集群的所有分片进程。
type Manager struct {
	baseDir      string // 存档根目录，对应 -persistent_storage_root
	binDir       string // DST 服务器可执行文件所在目录（未设置 resolver 时的兜底）
	exeResolver  ExeResolver
	clusters     map[string]*Cluster
	procs        map[string]*process // key: "Cluster_N/shard"
	logDir       string
	autoRestart  bool
	maxRestarts  int
	restartDelay time.Duration
	ugcDir       string      // 共享工坊模组目录，对应 -ugc_directory
	modsChecker  ModsChecker // 返回当前缺失的工坊模组，供更新循环判断进度
	// modsSetupSwap 供单模组更新使用：临时把安装级下载清单换成只含目标模组，
	// 返回的 restore 在会话结束后恢复所有房间的并集。由 Web 层注入。
	modsSetupSwap ModsSetupSwap

	mu     sync.RWMutex
	subMu  sync.Mutex
	subs   map[int]chan Event // 每个 SSE 客户端一个通道，避免互相抢事件
	nextID int

	// 模组预更新的状态。单独一把锁：更新可能持续几十分钟，
	// 不能让它的读写挤在管理分片进程的 mu 上。
	modMu      sync.Mutex
	modRunning bool
	modCancel  bool
	modCmd     *exec.Cmd
	modOutput  []string
	modErr     string
}

// SetExeResolver 设置可执行文件解析回调。每次启动分片都会调用它，
// 因此安装完成后无需重启面板即可切到正确的可执行文件。
func (m *Manager) SetExeResolver(fn ExeResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exeResolver = fn
}

// resolveExe 返回要启动的可执行文件与它的工作目录。
func (m *Manager) resolveExe() (string, string) {
	m.mu.RLock()
	fn, dir := m.exeResolver, m.binDir
	m.mu.RUnlock()

	if fn != nil {
		if exe, workDir := fn(); exe != "" {
			return exe, workDir
		}
	}
	return filepath.Join(dir, defaultExeName), dir
}

// Event 是进程状态变化事件，供 Web 前端实时订阅。
// Type 为 "log" 时 Message 是一行服务器原始输出；其余为进程状态事件。
type Event struct {
	Time    time.Time `json:"time"`
	Cluster string    `json:"cluster"`
	Shard   string    `json:"shard"`
	Type    string    `json:"type"` // start / stop / crash / restart / warn / ready / log
	Message string    `json:"message"`
}

// NewManager 创建进程管理器。
// baseDir 为存档根目录，binDir 为 DST 服务器 bin 目录。
func NewManager(baseDir, binDir, logDir string) *Manager {
	return &Manager{
		baseDir:      baseDir,
		binDir:       binDir,
		clusters:     make(map[string]*Cluster),
		procs:        make(map[string]*process),
		logDir:       logDir,
		autoRestart:  true,
		maxRestarts:  5,
		restartDelay: 3 * time.Second,
		subs:         make(map[int]chan Event),
	}
}

// Subscribe 注册一个事件订阅者，返回订阅 ID 与只读通道。
// 每个 Web 客户端独立订阅，事件广播给所有人，因此多个标签页不会互相抢事件。
func (m *Manager) Subscribe() (int, <-chan Event) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	id := m.nextID
	m.nextID++
	ch := make(chan Event, 512)
	m.subs[id] = ch
	return id, ch
}

// Unsubscribe 注销订阅者并关闭其通道。
func (m *Manager) Unsubscribe(id int) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if ch, ok := m.subs[id]; ok {
		delete(m.subs, id)
		close(ch)
	}
}

// emit 向所有订阅者广播一个事件，非阻塞。
func (m *Manager) emit(cluster, shard, typ, msg string) {
	ev := Event{
		Time:    time.Now(),
		Cluster: cluster,
		Shard:   shard,
		Type:    typ,
		Message: msg,
	}
	m.subMu.Lock()
	defer m.subMu.Unlock()
	for _, ch := range m.subs {
		select {
		case ch <- ev:
		default:
			// 单个客户端消费太慢则丢弃其消息，避免拖住整个进程
		}
	}
}

// Register 注册一个集群。若同 ID 已存在则覆盖（用于房间配置更新后重新注册）。
func (m *Manager) Register(c *Cluster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clusters[c.Key()] = c
}

// Unregister 注销一个集群。若仍有分片在运行会一并优雅停止，避免留下孤儿进程。
func (m *Manager) Unregister(clusterKey string, timeout time.Duration) error {
	_ = m.Stop(clusterKey, timeout)

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clusters, clusterKey)
	for k, p := range m.procs {
		if p.cluster.Key() == clusterKey {
			delete(m.procs, k)
		}
	}
	return nil
}

// Clusters 返回所有已注册集群。
func (m *Manager) Clusters() []*Cluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Cluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		list = append(list, c)
	}
	return list
}

// GetCluster 按标识取集群。
func (m *Manager) GetCluster(clusterKey string) (*Cluster, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clusters[clusterKey]
	return c, ok
}

// procKey 是进程表的键。集群标识 + 分片名必须在全局唯一。
func procKey(clusterKey, shard string) string {
	return clusterKey + "/" + shard
}

// shardPath 返回分片的完整目录：
// <baseDir>/<confDir>/Cluster_<id>/<shard>
// 必须与 config.ClusterDir/ShardDir 保持一致，否则配置落在服务器读不到的目录。
func (m *Manager) shardPath(c *Cluster, shard string) string {
	return config.ShardDir(m.baseDir, c.ConfDir, c.ID, shard)
}

// Start 启动指定集群的所有分片。
func (m *Manager) Start(clusterKey string) error {
	m.mu.RLock()
	c, ok := m.clusters[clusterKey]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("房间不存在: %s", clusterKey)
	}

	var firstErr error
	for _, shard := range c.Shards {
		if err := m.startShard(c, shard); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StartShard 启动集群的单个分片。
func (m *Manager) StartShard(clusterKey, shard string) error {
	m.mu.RLock()
	c, ok := m.clusters[clusterKey]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("房间不存在: %s", clusterKey)
	}
	return m.startShard(c, shard)
}

func (m *Manager) startShard(c *Cluster, shard string) error {
	k := procKey(c.Key(), shard)

	m.mu.Lock()
	if p, exists := m.procs[k]; exists {
		if p.isAlive() {
			m.mu.Unlock()
			return nil // 已在运行
		}
		// 上次残留的进程对象已被回收，清理后重建
		delete(m.procs, k)
	}
	m.mu.Unlock()

	// 可执行文件不存在时直接失败，避免创建出一堆僵尸记录
	exePath, workDir := m.resolveExe()
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("未找到服务器可执行文件: %s（请先安装专用服务器，或在 Steam 中确认已安装该工具）", exePath)
	}
	// 确保分片目录存在
	shardDir := m.shardPath(c, shard)
	if err := os.MkdirAll(shardDir, 0755); err != nil {
		return fmt.Errorf("创建分片目录失败: %w", err)
	}

	sp := c.port(shard)

	// 启动参数顺序敏感：storage_root → conf_dir → cluster → shard
	// 工作目录必须是可执行文件所在目录（bin 或 bin64），服务器依赖相对路径定位资源
	//
	// 端口以 server.ini 中的值为准（由房间配置生成），这里传同样的值，
	// 保证命令行与配置文件不会给出两套不同的端口。
	args := append(m.baseArgs(c, shard),
		"-port", strconv.Itoa(sp.ServerPort),
		"-steam_master_server_port", strconv.Itoa(sp.MasterServerPort),
		"-steam_authentication_port", strconv.Itoa(sp.AuthenticationPort),
		"-console", // 必须开启，否则不接受标准输入的控制台命令
		// 模组由面板提前用 -only_update_server_mods 统一下载（见 modupdate.go）。
		// 这里必须跳过：多个分片同时启动会抢着往同一个共享 ugc 目录写，
		// 这是官方文档明确提示的并发坑。
		"-skip_update_server_mods",
	)

	// 必须用绝对路径：exec.Command 不会在 cmd.Dir 下解析相对路径，
	// 传相对名会退化为在 %PATH% 中查找，导致 "executable file not found"
	cmd := exec.Command(exePath, args...)
	cmd.Dir = workDir
	// CREATE_NEW_PROCESS_GROUP 让子进程成为独立进程组，
	// 避免控制台关闭时连带杀掉服务器，也便于整组管理
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建标准输入管道失败: %w", err)
	}

	// stdout/stderr 重定向到日志文件，同时把每一行广播给 Web 前端。
	// 真实服务器只在 stdout 打印运行日志，必须在这里分流，
	// 否则前端"实时日志"拿到的只有进程状态事件，看不到游戏日志。
	tailer := NewLogTailer(m.logPath(c, shard), func(line string) {
		if strings.TrimSpace(line) == "" {
			return
		}
		m.emit(c.Key(), shard, "log", line)
	})
	cmd.Stdout = tailer
	cmd.Stderr = tailer

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("启动进程失败: %w", err)
	}

	p := &process{
		cluster:      c,
		shard:        shard,
		cmd:          cmd,
		stdin:        stdin,
		state:        StateRunning,
		pid:          cmd.Process.Pid,
		startedAt:    time.Now(),
		logWriter:    tailer,
		done:         make(chan struct{}),
		port:         sp.ServerPort,
		startVersion: versionAt(exePath),
	}

	m.mu.Lock()
	m.procs[k] = p
	m.mu.Unlock()

	m.emit(c.Key(), shard, "start", fmt.Sprintf("已启动，PID %d，端口 %d", p.pid, sp.ServerPort))

	// 立刻采一次作为基准：CPU 占用率要靠两次采样的差值算，
	// 提前建立基准，前端第一次拉状态（约 3 秒后）就能看到真实值而不是 0。
	m.procResource(p)

	// 后台监听退出
	go m.watch(p)

	// 等待服务器真正就绪（写入 Running 标记），最多 90 秒
	go m.waitReady(p)

	return nil
}

// waitReady 轮询直到服务器开始接受连接，或超时。
//
// 用两个互相独立的判据，任一命中即认为就绪：
//  1. 日志关键字 —— DST 自己声明已就绪，语义最准，但依赖 Klei 的文案
//  2. 端口归属 —— 该分片的 UDP 端口已被本进程监听，不依赖任何文案
//
// 只用 1 的话，Klei 改一句措辞就会退化成长达 90 秒的"超时"提示；
// 只用 2 的话，无法区分"刚绑定端口"与"世界真的加载完了"。两者互补。
// 无论如何都只是发一条提示事件，判错也不影响服务器运行。
func (m *Manager) waitReady(p *process) {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if !p.isAlive() {
			return
		}
		if m.logContains(p, "Server is now", "Sim paused", "Client authenticated") {
			m.emit(p.cluster.Key(), p.shard, "ready", "服务器已进入可接受连接的运行状态")
			return
		}
		if portOwnedBy(p.port, p.pid) {
			m.emit(p.cluster.Key(), p.shard, "ready", "服务器已开始监听端口，可以尝试连接")
			return
		}
		time.Sleep(2 * time.Second)
	}
	m.emit(p.cluster.Key(), p.shard, "ready", "等待就绪超时，请检查日志确认服务器状态")
}

// versionAt 读取服务器安装目录下的 version.txt。
// exePath 形如 <root>/bin/dontstarve_...exe，version.txt 在其上两级。
func versionAt(exePath string) string {
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(exePath)), "version.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// RunningShards 返回仍在运行 / 启动中 / 停止中的分片标识（Cluster_N/Shard）。
//
// 供「安装或更新服务器文件前必须先停服」使用：Windows 下正在运行的 exe 与 dll
// 是被锁定的，SteamCMD 替换文件会失败，最坏情况是留下一个更新了一半的安装。
func (m *Manager) RunningShards() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []string
	for _, p := range m.procs {
		switch p.state {
		case StateRunning, StateStarting, StateStopping:
			out = append(out, p.cluster.Key()+"/"+p.shard)
		}
	}
	sort.Strings(out)
	return out
}

// logContains 判断日志尾部是否包含任一关键字。
func (m *Manager) logContains(p *process, keywords ...string) bool {
	tail := p.logWriter.Tail(2000)
	for _, kw := range keywords {
		if strings.Contains(tail, kw) {
			return true
		}
	}
	return false
}

// watch 监听进程退出，判定崩溃并按需重启。
func (m *Manager) watch(p *process) {
	// Wait 返回的 error 在正常退出时为 nil，崩溃时非 nil；
	// 这里只依赖退出码判定，error 仅作日志参考
	_ = p.cmd.Wait()

	// cmd.Wait 返回后 ProcessState 才有效
	p.mu.Lock()
	exitedCode := 0
	if p.cmd.ProcessState != nil {
		exitedCode = p.cmd.ProcessState.ExitCode()
	}
	// 主动停止流程已把状态置为 stopping，此时退出属于预期，不算崩溃
	intentional := p.state == StateStopping
	if !intentional {
		if exitedCode != 0 {
			p.state = StateCrashed
		} else {
			// 服务器自行以 0 退出（如内部主动关闭），视为正常停止
			p.state = StateStopped
		}
	} else {
		p.state = StateStopped
	}
	p.mu.Unlock()

	_ = p.logWriter.Close()
	_ = p.stdin.Close()

	crashed := !intentional && exitedCode != 0
	typ := "stop"
	msg := fmt.Sprintf("已停止（退出码 %d）", exitedCode)
	if crashed {
		typ = "crash"
		msg = fmt.Sprintf("崩溃退出（退出码 %d）", exitedCode)
	}
	m.emit(p.cluster.Key(), p.shard, typ, msg)

	if !crashed || !m.autoRestart {
		p.close()
		return
	}

	// 崩溃后自动重启，带指数退避
	m.restartWithBackoff(p)
}

// restartWithBackoff 按指数退避策略重启崩溃的进程。
func (m *Manager) restartWithBackoff(p *process) {
	p.mu.Lock()
	p.restarts++
	attempt := p.restarts
	p.mu.Unlock()

	if attempt > m.maxRestarts {
		m.emit(p.cluster.Key(), p.shard, "crash",
			fmt.Sprintf("连续崩溃 %d 次，已停止自动重启，请检查日志", attempt-1))
		p.close()
		return
	}

	// 3s, 6s, 12s, 24s, 48s
	delay := m.restartDelay * time.Duration(1<<(attempt-1))
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	m.emit(p.cluster.Key(), p.shard, "restart",
		fmt.Sprintf("第 %d 次崩溃，%v 后自动重启", attempt-1, delay))

	select {
	case <-time.After(delay):
	case <-p.done:
		return
	}

	// 继承重启计数，让连续崩溃的退避时间能真正累加，
	// 否则每次重启都是新对象、计数归零，退避永远停在第一档
	prevRestarts := attempt
	if err := m.startShard(p.cluster, p.shard); err != nil {
		m.emit(p.cluster.Key(), p.shard, "crash", fmt.Sprintf("自动重启失败: %v", err))
		return
	}

	// 把计数写回新进程对象
	if np, ok := m.getProcess(p.cluster.Key(), p.shard); ok {
		np.mu.Lock()
		np.restarts = prevRestarts
		np.mu.Unlock()
	}
}

// getProcess 读取指定分片的进程对象。
func (m *Manager) getProcess(clusterKey, shard string) (*process, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.procs[procKey(clusterKey, shard)]
	return p, ok
}

// Stop 优雅停止集群的所有分片。
// 流程：c_save() → 等待落盘 → c_shutdown() → 超时才强制 Kill。
// 严禁跳过 c_save 直接 Kill，否则会丢失当前周期的存档进度。
func (m *Manager) Stop(clusterKey string, timeout time.Duration) error {
	m.mu.RLock()
	c, ok := m.clusters[clusterKey]
	m.mu.RUnlock()
	if !ok {
		// 集群已注销时仍尝试停掉残留进程，避免留下孤儿
		for _, sh := range []string{"Master", "Caves"} {
			_ = m.StopShard(clusterKey, sh, timeout)
		}
		return nil
	}

	for _, shard := range c.Shards {
		if err := m.StopShard(clusterKey, shard, timeout); err != nil {
			return err
		}
	}
	return nil
}

// StopShard 优雅停止单个分片。
func (m *Manager) StopShard(clusterKey, shard string, timeout time.Duration) error {
	m.mu.RLock()
	p, ok := m.procs[procKey(clusterKey, shard)]
	m.mu.RUnlock()
	if !ok {
		return nil // 未运行
	}

	if !p.isAlive() {
		p.close()
		return nil
	}

	p.mu.Lock()
	p.state = StateStopping
	p.mu.Unlock()

	// 第一步：请求存档，让世界状态落盘
	if err := p.SendCmd("c_save()"); err != nil {
		m.emit(clusterKey, shard, "warn", fmt.Sprintf("发送 c_save 失败，将直接关闭: %v", err))
	}

	// 等待存档完成，DST 存盘需要几秒
	m.emit(clusterKey, shard, "stop", "正在保存存档…")
	time.Sleep(5 * time.Second)

	// 第二步：请求正常关闭
	if err := p.SendCmd("c_shutdown()"); err != nil {
		m.emit(clusterKey, shard, "warn", fmt.Sprintf("发送 c_shutdown 失败: %v", err))
	}

	// 等待进程自行退出
	waited := 0
	for p.isAlive() && waited < int(timeout.Seconds()) {
		time.Sleep(time.Second)
		waited++
	}

	if p.isAlive() {
		// 超时才强杀
		m.emit(clusterKey, shard, "warn", "优雅关闭超时，强制结束进程")
		_ = p.cmd.Process.Kill()
	}

	m.emit(clusterKey, shard, "stop", "已停止")

	// 等待 watch 协程完成善后
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.close()
	}

	// 停止成功后重置重启计数
	p.mu.Lock()
	p.restarts = 0
	p.state = StateStopped
	p.mu.Unlock()

	// 不删除 m.procs 中的记录：Status 依赖它呈现"已停止"状态，
	// 删除会导致前端把已停的分片误显示为"未启动"。
	// 下次启动时 startShard 会复用或覆盖该记录。

	return nil
}

// SaveCluster 让集群内所有运行中的分片立即把世界落盘。
// 备份前必须调用：直接拷贝正在写入的 save 目录会拿到半截存档，
// 回滚后世界状态损坏。返回是否存在存活分片。
func (m *Manager) SaveCluster(clusterKey string, wait time.Duration) bool {
	m.mu.RLock()
	var alive []*process
	for _, p := range m.procs {
		if p.cluster.Key() == clusterKey && p.isAlive() {
			alive = append(alive, p)
		}
	}
	m.mu.RUnlock()

	if len(alive) == 0 {
		return false
	}
	for _, p := range alive {
		if err := p.SendCmd("c_save()"); err != nil {
			m.emit(clusterKey, p.shard, "warn", fmt.Sprintf("备份前 c_save 失败: %v", err))
		} else {
			m.emit(clusterKey, p.shard, "log", "已请求落盘（c_save），等待写入完成…")
		}
	}
	// DST 存盘需要数秒，等待后再拷贝
	time.Sleep(wait)
	return true
}

// AnyRunning 判断集群下是否有分片处于运行或启动中状态。
func (m *Manager) AnyRunning(clusterKey string) bool {
	for _, st := range m.Status(clusterKey) {
		if st.State == "running" || st.State == "starting" || st.State == "stopping" {
			return true
		}
	}
	return false
}

// SendCmd 向指定分片的控制台注入一条命令。
func (m *Manager) SendCmd(clusterKey, shard, cmd string) error {
	m.mu.RLock()
	p, ok := m.procs[procKey(clusterKey, shard)]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("分片未运行: %s/%s", clusterKey, shard)
	}
	return p.SendCmd(cmd)
}

// SendCmd 向运行中的进程写入控制台命令。
// 命令后必须补换行符，否则服务器不会执行。
func (p *process) SendCmd(cmd string) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.stdin == nil {
		return fmt.Errorf("进程标准输入不可用")
	}
	// io.WriteCloser 接口没有 WriteString，用 Fprintf 写入并自动补换行
	if _, err := fmt.Fprintln(p.stdin, cmd); err != nil {
		return fmt.Errorf("写入命令失败: %w", err)
	}
	return nil
}

// Status 返回分片状态快照。
type Status struct {
	Cluster     string    `json:"cluster"` // 集群标识 Cluster_N，前端调用接口时使用
	ClusterID   int       `json:"clusterId"`
	ClusterName string    `json:"clusterName"` // 房间名，仅用于展示
	Shard       string    `json:"shard"`
	State       string    `json:"state"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"startedAt"`
	Uptime      string    `json:"uptime"`
	Restarts    int       `json:"restarts"`
	Port        int       `json:"port"`

	// 资源占用，仅在进程存活时有效（首次采样 cpuPct 为 0）
	MemBytes uint64  `json:"memBytes"`
	CPUPct   float64 `json:"cpuPct"`

	// Version 是该分片启动时的游戏版本。
	// 与当前安装版本不一致，说明游戏更新过而这个进程还跑着旧版本，需要重启。
	Version string `json:"version"`
}

// Status 返回全部或指定集群的分片状态。
func (m *Manager) Status(clusterKey string) []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []Status
	for _, p := range m.procs {
		if clusterKey != "" && p.cluster.Key() != clusterKey {
			continue
		}
		st := Status{
			Cluster:     p.cluster.Key(),
			ClusterID:   p.cluster.ID,
			ClusterName: p.cluster.Name,
			Shard:       p.shard,
			State:       p.state.String(),
			StartedAt:   p.startedAt,
			Restarts:    p.restarts,
			Port:        p.cluster.port(p.shard).ServerPort,
			Version:     p.startVersion,
		}
		if p.isAlive() {
			st.PID = p.pid
			st.Uptime = time.Since(p.startedAt).Truncate(time.Second).String()
			st.MemBytes, st.CPUPct = m.procResource(p)
		}
		out = append(out, st)
	}
	// 按集群、分片排序，保证前端每次拿到的顺序一致
	sort.Slice(out, func(i, j int) bool {
		if out[i].ClusterID != out[j].ClusterID {
			return out[i].ClusterID < out[j].ClusterID
		}
		return out[i].Shard < out[j].Shard
	})
	return out
}

// isAlive 判断进程是否仍存活。
//
// 注意：Go 中 cmd.ProcessState 只有在 cmd.Wait() 返回之后才会被填充，
// 进程运行期间它一直是 nil。因此 nil 必须视为"存活"，
// 否则启动后立即会被误判为已退出（这正是 DMP 用 screen 时不会遇到的语义差异）。
func (p *process) isAlive() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	if p.cmd.ProcessState == nil {
		// Wait 尚未返回，进程仍在运行
		return true
	}
	return !p.cmd.ProcessState.Exited()
}

// close 释放进程资源，关闭 done 通道。
func (p *process) close() {
	p.closeOnce.Do(func() {
		close(p.done)
	})
}

// logPath 返回分片日志路径。
// 必须带上集群标识：所有房间共用同一个 -conf_dir，
// 若只用 "<confDir>_<shard>" 命名，Cluster_1 与 Cluster_2 的 Master 日志会互相覆盖。
func (m *Manager) logPath(c *Cluster, shard string) string {
	return m.logPathFor(c.Key(), shard)
}

func (m *Manager) logPathFor(clusterKey, shard string) string {
	return filepath.Join(m.logDir, fmt.Sprintf("%s_%s.log", clusterKey, shard))
}

// LogTailer 是一个同时写入文件、保留内存尾部、并把每一行回调出去的分流器。
//
//   - 写文件：保留历史日志，供进程停止后仍可回看
//   - 内存环形缓冲：前端拉取最近日志时比读文件快
//   - onLine 回调：把服务器 stdout 的每一行实时广播给 Web 前端（SSE）
//
// 之所以要按行切分而不是原样透传：stdout 是字节流，一次 Write 可能包含
// 半行或好几行，直接推送会让前端出现半截断行。
type LogTailer struct {
	file    *os.File
	mu      sync.Mutex
	buf     []byte
	maxLen  int
	closed  bool
	onLine  func(string)
	partial []byte // 尚未收到换行符的残留行
}

const defaultTailSize = 512 * 1024 // 保留最近 512KB

// 单行超过该长度即丢弃，防止服务器输出无换行的刷屏内容把内存撑爆
const maxLineLen = 16 * 1024

// NewLogTailer 打开日志文件并创建尾随缓冲。onLine 可为 nil。
func NewLogTailer(path string, onLine func(string)) *LogTailer {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		// 日志目录创建失败不应阻断进程启动
		_ = err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		f = nil
	}
	return &LogTailer{file: f, maxLen: defaultTailSize, onLine: onLine}
}

// Write 实现 io.Writer：写文件 + 更新内存缓冲 + 按行回调。
func (l *LogTailer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		_, _ = l.file.Write(p)
	}

	l.buf = append(l.buf, p...)
	if len(l.buf) > l.maxLen {
		// 重新分配底层数组，避免切片不断右移导致底层数组无限增长
		l.buf = append([]byte(nil), l.buf[len(l.buf)-l.maxLen:]...)
	}

	if l.onLine != nil {
		l.partial = append(l.partial, p...)
		for {
			i := bytes.IndexByte(l.partial, '\n')
			if i < 0 {
				break
			}
			line := strings.TrimRight(string(l.partial[:i]), "\r")
			l.partial = append([]byte(nil), l.partial[i+1:]...)
			l.onLine(line)
		}
		if len(l.partial) > maxLineLen {
			// 超长且迟迟不换行，直接丢弃，避免内存堆积
			l.partial = nil
		}
	}
	return len(p), nil
}

// Tail 返回最近的 n 字节日志。
func (l *LogTailer) Tail(n int) string {
	l.mu.Lock()
	defer l.mu.Unlock()

	if n > len(l.buf) {
		n = len(l.buf)
	}
	return string(l.buf[len(l.buf)-n:])
}

// Lines 返回最近 n 行日志。
func (l *LogTailer) Lines(n int) []string {
	tail := l.Tail(64 * 1024)
	all := strings.Split(strings.ReplaceAll(tail, "\r\n", "\n"), "\n")
	// 去掉末尾的空行
	for len(all) > 0 && strings.TrimSpace(all[len(all)-1]) == "" {
		all = all[:len(all)-1]
	}
	if n > 0 && n < len(all) {
		all = all[len(all)-n:]
	}
	return all
}

// Close 关闭日志文件。
func (l *LogTailer) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// ReadLog 从磁盘读取指定分片的最近 n 行日志（用于进程已停止的情况）。
func (m *Manager) ReadLog(clusterKey, shard string, n int) ([]string, error) {
	path := m.logPathFor(clusterKey, shard)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > 4*n {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return lines, err
	}
	return lines, nil
}
