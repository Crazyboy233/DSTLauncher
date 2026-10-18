// Package room 管理"房间"。
//
//	房间 Room  = 一个 DST 集群，对应存档目录 <root>/<confDir>/Cluster_<ID>
//	分片 Shard = 集群下的一个世界进程，Master（地表）与 Caves（洞穴）各是一个独立进程
//
// 房间是配置的唯一事实来源：面板里的房间表单会据此生成
// cluster.ini / server.ini / cluster_token.txt，因此不再需要手工编辑 ini。
package room

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// 分片的世界类型。名字只是目录名与 -shard 参数，**类型**才决定
// 世界生成预设与可用设置项，两者必须分开存：
// 第三个世界完全可能叫 Caves2 或 Underworld，靠名字猜类型一定会错。
const (
	WorldTypeForest = "forest"
	WorldTypeCave   = "cave"
)

// Shard 是一个分片（世界）及其独占端口。
//
// 三个端口都必须全局唯一，否则同一台机器上开多个房间时，
// 各分片会互相抢占端口导致后启动的一方直接失败。
type Shard struct {
	Name      string `json:"name"`      // 目录名 + -shard 参数
	WorldType string `json:"worldType"` // forest / cave
	GameID    int    `json:"gameId"`    // server.ini [SHARD] id，玩家迁移按它寻址
	IsMaster  bool   `json:"isMaster"`  // 全房间只能有一个

	// Disabled 表示「保留定义与存档，但不启动该分片」。
	// 刻意用反向语义：JSON 里缺这个字段时零值 false = 启用，
	// 老配置（没有该字段）与前端新建的分片都会落到「启用」这一侧。
	Disabled bool `json:"disabled,omitempty"`

	ServerPort         int `json:"serverPort"`         // 游戏端口
	MasterServerPort   int `json:"masterServerPort"`   // Steam 主服务器端口
	AuthenticationPort int `json:"authenticationPort"` // Steam 认证端口
}

// Enabled 表示该分片是否参与启停。
func (s Shard) Enabled() bool { return !s.Disabled }

// Room 是一个房间的全部可配置项。
type Room struct {
	ID          int    `json:"id"`          // 自增，同时决定 Cluster_<ID>
	Name        string `json:"name"`        // cluster_name
	Description string `json:"description"` // cluster_description
	GameMode    string `json:"gameMode"`    // survival / endless / wilderness
	MaxPlayers  int    `json:"maxPlayers"`
	PvP         bool   `json:"pvp"`
	PauseEmpty  bool   `json:"pauseEmpty"`
	VoteEnabled bool   `json:"voteEnabled"`
	VoteKick    bool   `json:"voteKick"`

	Password string `json:"password"` // cluster_password，空表示无密码
	Language string `json:"language"` // cluster_language
	TickRate int    `json:"tickRate"` // 15 / 30 / 60

	// Token 是 Klei 下发的服务器令牌，写入 cluster_token.txt。
	// 在线开服必需；局域网（LANOnly）或离线（Offline）模式下可以不填。
	Token string `json:"token"`

	LANOnly  bool   `json:"lanOnly"`  // lan_only_cluster
	Offline  bool   `json:"offline"`  // offline_cluster
	MasterIP string `json:"masterIp"` // cluster.ini [SHARD] master_ip
	// MasterPort 是 Master 分片对外提供分片协调的端口（cluster.ini [SHARD] master_port），
	// 与 Master 的 server_port 不是同一个值，不要混淆。
	MasterPort int `json:"masterPort"`
	// ClusterKey 是分片之间的通信密钥，一旦改动所有分片必须同时重启。
	ClusterKey string `json:"clusterKey"`

	MaxSnapshots int `json:"maxSnapshots"` // 回档点数

	// Shards 是房间的全部分片定义，是分片的唯一事实来源。
	// 数组里可以有多条，启停与配置生成都按它遍历；是否启用看 Shard.Disabled。
	Shards []Shard `json:"shards"`

	// legacyCaves 只用于迁移早期 rooms.json：那一版用 `caves` 布尔值
	// 表示洞穴开关，分片列表固定两条。读到非 nil 时按它补 Enabled，
	// 随后置 nil，保存时不再写回（omitempty + 指针）。
	legacyCaves *bool
}

// UnmarshalJSON 读取旧版 `caves` 字段供 Normalize 迁移，对外不暴露。
// 不做这一步的话，旧配置的「洞穴关闭」会在升级后被当成「字段缺失」而重新启用。
func (r *Room) UnmarshalJSON(data []byte) error {
	type alias Room // 避免递归调用自己
	aux := struct {
		*alias
		Caves *bool `json:"caves"`
	}{alias: (*alias)(r)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.legacyCaves = aux.Caves
	return nil
}

// 合法的游戏模式，与 cluster.ini [GAMEPLAY] game_mode 取值一致（大小写敏感）。
var validGameModes = map[string]bool{
	"survival":   true,
	"endless":    true,
	"wilderness": true,
}

// 合法的界面语言，与 cluster_language 取值一致。
var validLanguages = map[string]bool{
	"zh": true, "en": true,
}

// ValidGameModes 返回可选游戏模式，供前端渲染下拉框。
func ValidGameModes() []string { return []string{"survival", "endless", "wilderness"} }

// ValidLanguages 返回可选语言。
func ValidLanguages() []string { return []string{"zh", "en"} }

// ShardNames 返回启用中的分片名，主分片固定排在首位。
//
// 顺序即前端 tab 顺序与端口分配顺序，因此只在 master / 非 master 之间分组，
// 同组内保持配置里的原始次序，避免保存一次就重新洗牌。
func (r *Room) ShardNames() []string {
	names := make([]string, 0, len(r.Shards))
	var rest []string
	for _, s := range r.Shards {
		if !s.Enabled() {
			continue
		}
		if s.IsMaster {
			names = append(names, s.Name)
		} else {
			rest = append(rest, s.Name)
		}
	}
	return append(names, rest...)
}

// Shard 按名字取分片定义。
func (r *Room) Shard(name string) (Shard, bool) {
	for _, s := range r.Shards {
		if s.Name == name {
			return s, true
		}
	}
	return Shard{}, false
}

// MasterShard 返回主分片名。分片表异常时退化为 "Master"，
// 保证调用方（日志、模组、控制台命令）总有一个可用的默认值。
func (r *Room) MasterShard() string {
	for _, s := range r.Shards {
		if s.IsMaster && s.Enabled() {
			return s.Name
		}
	}
	for _, s := range r.Shards {
		if s.IsMaster {
			return s.Name
		}
	}
	if len(r.Shards) > 0 && r.Shards[0].Name != "" {
		return r.Shards[0].Name
	}
	return "Master"
}

// WorldTypeOf 返回分片的世界类型（forest / cave）。
// 分片不存在时按名字推断，供日志与历史数据兜底使用。
func (r *Room) WorldTypeOf(name string) string {
	if s, ok := r.Shard(name); ok && s.WorldType != "" {
		return s.WorldType
	}
	return InferWorldType(name)
}

// InferWorldType 按分片名猜世界类型。
// 只用于旧数据兜底与导入存档的初值，正常路径一律读 Shard.WorldType。
func InferWorldType(name string) string {
	if strings.Contains(strings.ToLower(name), "cave") {
		return WorldTypeCave
	}
	return WorldTypeForest
}

// Key 返回进程层使用的唯一标识，同时也是集群目录名。
func (r *Room) Key() string {
	return fmt.Sprintf("Cluster_%d", r.ID)
}

// Defaults 返回新建房间的默认值。
// 端口留空由 Store.Create 统一分配，保证不与已有房间冲突。
func Defaults() *Room {
	return &Room{
		Name:         "我的饥荒服务器",
		Description:  "欢迎来到我的饥荒服务器",
		GameMode:     "endless",
		MaxPlayers:   12,
		PvP:          true,
		PauseEmpty:   true,
		VoteEnabled:  true,
		VoteKick:     true,
		Language:     "zh",
		TickRate:     30,
		LANOnly:      false,
		Offline:      false,
		MasterIP:     "127.0.0.1",
		MaxSnapshots: 10,
		Shards:       defaultShards(),
	}
}

// Normalize 补齐缺省值并统一取值大小写。
// 前端传来的表单可能缺字段（例如新增字段后的旧数据），这里兜底避免写出非法配置。
func (r *Room) Normalize() {
	d := Defaults()
	if strings.TrimSpace(r.Name) == "" {
		r.Name = d.Name
	}
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)

	r.GameMode = strings.ToLower(strings.TrimSpace(r.GameMode))
	if !validGameModes[r.GameMode] {
		r.GameMode = d.GameMode
	}

	r.Language = strings.ToLower(strings.TrimSpace(r.Language))
	if !validLanguages[r.Language] {
		r.Language = d.Language
	}

	if r.MaxPlayers < 1 || r.MaxPlayers > 64 {
		r.MaxPlayers = d.MaxPlayers
	}
	// DST 只支持 15 / 30 / 60 三档 tick rate
	switch r.TickRate {
	case 15, 30, 60:
	default:
		r.TickRate = d.TickRate
	}
	if r.MaxSnapshots < 1 || r.MaxSnapshots > 100 {
		r.MaxSnapshots = d.MaxSnapshots
	}
	if strings.TrimSpace(r.MasterIP) == "" {
		r.MasterIP = d.MasterIP
	}
	r.MasterIP = strings.TrimSpace(r.MasterIP)
	r.Token = strings.TrimSpace(r.Token)
	if strings.TrimSpace(r.ClusterKey) == "" {
		r.ClusterKey = RandomKey(14)
	}
	if r.MasterPort < 1024 || r.MasterPort > 65535 {
		r.MasterPort = 21000
	}

	// 分片端口缺失时（例如旧数据）补一套不与自身冲突的值
	r.normalizeShards()
}

// normalizeShards 统一分片表：迁移旧字段、补齐名字与类型、保证唯一主分片与唯一 id。
//
// 端口刻意不在这里兜底：端口必须在全局端口池里分配（Store.allocPorts），
// 这里随手写 11000+i+1 会在多房间场景下与别的房间撞号。
func (r *Room) normalizeShards() {
	// 1. 旧版 `caves` 布尔值 → 分片启用状态
	if r.legacyCaves != nil {
		caves := *r.legacyCaves
		r.legacyCaves = nil
		if len(r.Shards) == 0 {
			r.Shards = defaultShards()
		}
		if caves && len(r.Shards) == 1 && r.Shards[0].IsMaster {
			r.Shards = append(r.Shards, Shard{Name: "Caves", WorldType: WorldTypeCave})
		}
		if !caves {
			for i := range r.Shards {
				if !r.Shards[i].IsMaster {
					r.Shards[i].Disabled = true
				}
			}
		}
	}

	if len(r.Shards) == 0 {
		r.Shards = defaultShards()
	}

	// 2. 名字与类型兜底。类型缺失时按名字猜：新加的分片由前端显式给类型，
	//    只有历史数据才会走到这条路径。
	for i := range r.Shards {
		s := &r.Shards[i]
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			s.Name = fmt.Sprintf("World%d", i+1)
		}
		if s.WorldType != WorldTypeForest && s.WorldType != WorldTypeCave {
			s.WorldType = InferWorldType(s.Name)
		}
	}

	// 3. 主分片有且只有一个，并挪到首位：
	//    端口分配、目录生成、前端 tab 顺序都以首位为准。
	masterIdx := 0
	for i := range r.Shards {
		if r.Shards[i].IsMaster {
			masterIdx = i
			break
		}
	}
	for i := range r.Shards {
		r.Shards[i].IsMaster = i == masterIdx
	}
	if masterIdx != 0 {
		first := r.Shards[masterIdx]
		rest := append(r.Shards[:masterIdx:masterIdx], r.Shards[masterIdx+1:]...)
		r.Shards = append([]Shard{first}, rest...)
	}
	// 房间至少要有一个世界在跑，主分片不允许停用
	r.Shards[0].Disabled = false

	// 4. 世界 id 唯一且非零。被占用的 id 只改冲突的那一条，
	//    不整体重排——玩家的传送门记的是 id，改一次就得重新配一次。
	used := make(map[int]bool, len(r.Shards))
	for i := range r.Shards {
		id := r.Shards[i].GameID
		if id > 0 && !used[id] {
			used[id] = true
			continue
		}
		r.Shards[i].GameID = 0
	}
	nextID := 1
	for i := range r.Shards {
		if r.Shards[i].GameID != 0 {
			continue
		}
		for used[nextID] {
			nextID++
		}
		r.Shards[i].GameID = nextID
		used[nextID] = true
	}
}

// Validate 校验房间配置。返回的错误直接展示给用户，因此要说清是哪一项不对。
func (r *Room) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("房间名不能为空")
	}
	if !validGameModes[r.GameMode] {
		return fmt.Errorf("游戏模式不合法: %s（可选 %s）", r.GameMode, strings.Join(ValidGameModes(), " / "))
	}
	if !validLanguages[r.Language] {
		return fmt.Errorf("语言不合法: %s", r.Language)
	}
	if r.MaxPlayers < 1 || r.MaxPlayers > 64 {
		return fmt.Errorf("最大人数必须在 1-64 之间")
	}
	if r.TickRate != 15 && r.TickRate != 30 && r.TickRate != 60 {
		return fmt.Errorf("tick 频率只能是 15 / 30 / 60")
	}
	if r.MasterPort < 1024 || r.MasterPort > 65535 {
		return fmt.Errorf("主分片端口必须在 1024-65535 之间")
	}
	if strings.TrimSpace(r.ClusterKey) == "" {
		return fmt.Errorf("分片通信密钥不能为空")
	}

	if len(r.ShardNames()) == 0 {
		return fmt.Errorf("至少需要启用一个分片（世界）")
	}

	master := ""
	seenName := make(map[string]bool, len(r.Shards))
	seenID := make(map[int]bool, len(r.Shards))
	for _, s := range r.Shards {
		if err := validShardName(s.Name); err != nil {
			return err
		}
		if seenName[strings.ToLower(s.Name)] {
			return fmt.Errorf("分片名重复: %s（名字同时是存档目录名，不能重名）", s.Name)
		}
		seenName[strings.ToLower(s.Name)] = true

		if s.WorldType != WorldTypeForest && s.WorldType != WorldTypeCave {
			return fmt.Errorf("%s 的世界类型不合法: %q（只能是 forest 或 cave）", s.Name, s.WorldType)
		}
		if s.GameID <= 0 {
			return fmt.Errorf("%s 的世界 ID 不合法: %d", s.Name, s.GameID)
		}
		if seenID[s.GameID] {
			return fmt.Errorf("世界 ID 重复: %d（每个分片必须有唯一 id，玩家迁移按 id 寻址）", s.GameID)
		}
		seenID[s.GameID] = true

		if s.IsMaster {
			if master != "" {
				return fmt.Errorf("只能有一个主分片，当前同时标记了 %s 与 %s", master, s.Name)
			}
			master = s.Name
			if !s.Enabled() {
				return fmt.Errorf("主分片 %s 不能停用（房间至少要有一个世界）", s.Name)
			}
		}

		for _, p := range []struct {
			name string
			val  int
		}{
			{"游戏端口", s.ServerPort},
			{"Steam 主服务器端口", s.MasterServerPort},
			{"Steam 认证端口", s.AuthenticationPort},
		} {
			if p.val < 1024 || p.val > 65535 {
				return fmt.Errorf("%s 的%s必须在 1024-65535 之间", s.Name, p.name)
			}
		}
	}
	if master == "" {
		return fmt.Errorf("必须有一个主分片（is_master = true）")
	}
	return nil
}

// windowsReservedNames 是 Windows 不允许用作目录名的保留名。
var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// validShardName 校验分片名。
//
// 分片名有三个去处，一个都不能出错：
//   - 存档目录名（Windows 下不能含 < > : " / \ | ? *、不能以点结尾、不能是保留名）
//   - 启动参数 -shard <name>（含空格会被拆成两个参数）
//   - server.ini 的 name = <name>
func validShardName(name string) error {
	if name == "" {
		return fmt.Errorf("分片名不能为空")
	}
	if len(name) > 32 {
		return fmt.Errorf("分片名 %q 过长（最多 32 字节）", name)
	}
	for _, c := range name {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("分片名 %q 含控制字符", name)
		}
		if strings.ContainsRune(`<>:"/\|?* `, c) {
			return fmt.Errorf(`分片名 %q 含非法字符（不能包含空格与 < > : " / \ | ? *）`, name)
		}
	}
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return fmt.Errorf("分片名 %q 不能以点开头或结尾", name)
	}
	if windowsReservedNames[strings.ToUpper(name)] {
		return fmt.Errorf("分片名 %q 是 Windows 保留名，请换一个", name)
	}
	return nil
}

// ConflictPorts 返回与 others 冲突的端口列表。
// excludeID 用于编辑场景：跳过房间自身。
func (r *Room) ConflictPorts(others []*Room, excludeID int) []int {
	used := make(map[int]string)
	for _, o := range others {
		if o.ID == excludeID {
			continue
		}
		for _, s := range o.Shards {
			used[s.ServerPort] = fmt.Sprintf("房间「%s」", o.Name)
			used[s.MasterServerPort] = fmt.Sprintf("房间「%s」", o.Name)
			used[s.AuthenticationPort] = fmt.Sprintf("房间「%s」", o.Name)
		}
		used[o.MasterPort] = fmt.Sprintf("房间「%s」", o.Name)
	}

	var out []int
	seen := make(map[int]bool)
	add := func(p int) {
		if p == 0 || seen[p] {
			return
		}
		if _, ok := used[p]; ok {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, s := range r.Shards {
		add(s.ServerPort)
		add(s.MasterServerPort)
		add(s.AuthenticationPort)
	}
	add(r.MasterPort)
	sort.Ints(out)
	return out
}

// selfConflict 检查房间内部端口是否自相重复。
func (r *Room) selfConflict() []int {
	seen := make(map[int]int)
	var out []int
	add := func(p int) {
		if p == 0 {
			return
		}
		if seen[p] > 0 {
			out = append(out, p)
			return
		}
		seen[p] = 1
	}
	for _, s := range r.Shards {
		add(s.ServerPort)
		add(s.MasterServerPort)
		add(s.AuthenticationPort)
	}
	add(r.MasterPort)
	return out
}

// defaultShards 生成 Master + Caves 的默认分片定义。
// 端口留空由 Store 分配；这是新房间的初始形态，用户可以在房间页增删分片。
func defaultShards() []Shard {
	return []Shard{
		{Name: "Master", WorldType: WorldTypeForest, GameID: 1, IsMaster: true},
		{Name: "Caves", WorldType: WorldTypeCave, GameID: 2},
	}
}

// RandomKey 生成 n 位随机字符串，用作 cluster_key。
// 使用 crypto/rand 而非 math/rand：这是分片间的认证密钥。
func RandomKey(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 极端情况下退化为固定值，至少保证配置可用
		for i := range buf {
			buf[i] = charset[i%len(charset)]
		}
		return string(buf)
	}
	for i := range buf {
		buf[i] = charset[int(buf[i])%len(charset)]
	}
	return string(buf)
}
