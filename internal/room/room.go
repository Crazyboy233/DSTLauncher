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
	"fmt"
	"sort"
	"strings"
)

// Shard 是一个分片（世界）及其独占端口。
//
// 三个端口都必须全局唯一，否则同一台机器上开第二个房间时，
// Master 与 Caves 会互相抢占端口导致后启动的一方直接失败。
type Shard struct {
	Name               string `json:"name"`               // Master / Caves
	GameID             int    `json:"gameId"`             // server.ini [SHARD] id
	IsMaster           bool   `json:"isMaster"`           //
	ServerPort         int    `json:"serverPort"`         // 游戏端口
	MasterServerPort   int    `json:"masterServerPort"`   // Steam 主服务器端口
	AuthenticationPort int    `json:"authenticationPort"` // Steam 认证端口
}

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

	Caves  bool    `json:"caves"` // 是否启用洞穴分片
	Shards []Shard `json:"shards"`
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

// ShardNames 返回启用中的分片名，顺序固定为 Master 在前。
func (r *Room) ShardNames() []string {
	names := make([]string, 0, 2)
	names = append(names, "Master")
	if r.Caves {
		names = append(names, "Caves")
	}
	return names
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
		Caves:        true,
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
	if len(r.Shards) == 0 {
		r.Shards = defaultShards()
	}
	for i := range r.Shards {
		if r.Shards[i].ServerPort == 0 {
			r.Shards[i].ServerPort = 11000 + i + 1
		}
		if r.Shards[i].MasterServerPort == 0 {
			r.Shards[i].MasterServerPort = 31000 + i + 1
		}
		if r.Shards[i].AuthenticationPort == 0 {
			r.Shards[i].AuthenticationPort = 41000 + i + 1
		}
		if r.Shards[i].GameID == 0 {
			r.Shards[i].GameID = i + 1
		}
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
	for _, s := range r.Shards {
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
func defaultShards() []Shard {
	return []Shard{
		{Name: "Master", GameID: 1, IsMaster: true},
		{Name: "Caves", GameID: 2, IsMaster: false},
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
