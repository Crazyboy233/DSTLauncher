package room

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dst-windows/internal/config"
)

// 本文件负责把房间配置渲染成 DST 真正读取的那几个文件。
//
//
// 注意：这里不生成 leveldataoverride.lua。世界生成参数（世界尺寸、季节长度、
// 资源密度等）本身是一整套庞大的选项，DST 在文件缺失时会用默认值自动生成世界，
// 强行写一份不完整的覆盖文件反而会让世界生成失败。

// 文件名常量。
const (
	clusterIniName = "cluster.ini"
	serverIniName  = "server.ini"
	tokenName      = "cluster_token.txt"
)

// clusterIni 渲染 cluster.ini。
// whitelistSlots 为白名单预留名额，由外部按 whitelist.txt 的实际条目数传入。
func (r *Room) clusterIni(whitelistSlots int) string {
	var sb strings.Builder

	sb.WriteString("[GAMEPLAY]\n")
	sb.WriteString("game_mode = " + r.GameMode + "\n")
	sb.WriteString(fmt.Sprintf("max_players = %d\n", r.MaxPlayers))
	sb.WriteString("pvp = " + b(r.PvP) + "\n")
	sb.WriteString("pause_when_empty = " + b(r.PauseEmpty) + "\n")
	sb.WriteString("vote_enabled = " + b(r.VoteEnabled) + "\n")
	sb.WriteString("vote_kick_enabled = " + b(r.VoteKick) + "\n")
	sb.WriteString("\n")

	sb.WriteString("[NETWORK]\n")
	sb.WriteString("lan_only_cluster = " + b(r.LANOnly) + "\n")
	sb.WriteString("offline_cluster = " + b(r.Offline) + "\n")
	sb.WriteString("cluster_description = " + r.Description + "\n")
	sb.WriteString(fmt.Sprintf("whitelist_slots = %d\n", whitelistSlots))
	sb.WriteString("cluster_name = " + r.Name + "\n")
	sb.WriteString("cluster_password = " + r.Password + "\n")
	sb.WriteString("cluster_language = " + r.Language + "\n")
	sb.WriteString(fmt.Sprintf("tick_rate = %d\n", r.TickRate))
	sb.WriteString("\n")

	sb.WriteString("[MISC]\n")
	sb.WriteString("console_enabled = true\n")
	sb.WriteString(fmt.Sprintf("max_snapshots = %d\n", r.MaxSnapshots))
	sb.WriteString("\n")

	// [SHARD] 是洞穴能否连上地表的关键：缺 cluster_key 或缺 master_port
	// 会让 Caves 分片直接启动失败。
	sb.WriteString("[SHARD]\n")
	sb.WriteString("shard_enabled = true\n")
	sb.WriteString("bind_ip = 0.0.0.0\n")
	sb.WriteString("master_ip = " + r.MasterIP + "\n")
	sb.WriteString(fmt.Sprintf("master_port = %d\n", r.MasterPort))
	sb.WriteString("cluster_key = " + r.ClusterKey + "\n")

	return sb.String()
}

// serverIni 渲染某个分片的 server.ini。
func (r *Room) serverIni(s Shard) string {
	var sb strings.Builder

	sb.WriteString("[NETWORK]\n")
	sb.WriteString(fmt.Sprintf("server_port = %d\n", s.ServerPort))
	sb.WriteString("\n")

	sb.WriteString("[SHARD]\n")
	sb.WriteString(fmt.Sprintf("id = %d\n", s.GameID))
	sb.WriteString("is_master = " + b(s.IsMaster) + "\n")
	sb.WriteString("name = " + s.Name + "\n")
	sb.WriteString("\n")

	// 两个 Steam 端口必须写在这里，否则地表与洞穴会各自使用默认值而冲突
	sb.WriteString("[STEAM]\n")
	sb.WriteString(fmt.Sprintf("master_server_port = %d\n", s.MasterServerPort))
	sb.WriteString(fmt.Sprintf("authentication_port = %d\n", s.AuthenticationPort))
	sb.WriteString("\n")

	sb.WriteString("[ACCOUNT]\n")
	sb.WriteString("encode_user_path = true\n")

	return sb.String()
}

// whitelistSlots 统计白名单条目数。
// DST 用它为白名单玩家在 max_players 之外额外预留名额，文件缺失或为空时为 0。
func (r *Room) whitelistSlots(clusterDir string) int {
	data, err := os.ReadFile(filepath.Join(clusterDir, "whitelist.txt"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// shardNamed 按名字取分片定义。
func (r *Room) shardNamed(name string) (Shard, bool) {
	for _, s := range r.Shards {
		if s.Name == name {
			return s, true
		}
	}
	return Shard{}, false
}

// Files 返回房间需要落盘的全部文件（相对路径 -> 内容），供写入与预览复用。
// 过滤掉未启用的分片，避免留下过期配置误导用户。
func (r *Room) Files(root, confDir string) map[string]string {
	out := make(map[string]string)

	clusterDir := config.ClusterDir(root, confDir, r.ID)
	out[filepath.Join(clusterDir, clusterIniName)] = r.clusterIni(r.whitelistSlots(clusterDir))
	out[filepath.Join(clusterDir, tokenName)] = r.Token

	for _, name := range r.ShardNames() {
		s, ok := r.shardNamed(name)
		if !ok {
			continue
		}
		dir := config.ShardDir(root, confDir, r.ID, name)
		out[filepath.Join(dir, serverIniName)] = r.serverIni(s)
	}

	return out
}

// Generate 把房间配置写入磁盘，并建好 DST 运行所需的目录与名单文件。
//
// 目录结构（必须与 server.Manager 的启动参数一致）：
//
//	<root>/<confDir>/Cluster_<ID>/cluster.ini
//	<root>/<confDir>/Cluster_<ID>/cluster_token.txt
//	<root>/<confDir>/Cluster_<ID>/<shard>/server.ini
//	<root>/<confDir>/Cluster_<ID>/<shard>/save/session/
//
// 已存在的 save 目录不会被清空——那里是玩家的世界存档。
func Generate(root, confDir string, r *Room) error {
	clusterDir := config.ClusterDir(root, confDir, r.ID)
	if err := config.EnsureDirExists(clusterDir); err != nil {
		return fmt.Errorf("创建集群目录失败: %w", err)
	}

	// 名单文件缺失时 DST 会报错，先建空文件
	for _, name := range []string{"adminlist.txt", "blocklist.txt", "whitelist.txt"} {
		p := filepath.Join(clusterDir, name)
		if !config.FileExists(p) {
			if err := config.WriteFileAtomic(p, []byte("")); err != nil {
				return fmt.Errorf("创建 %s 失败: %w", name, err)
			}
		}
	}

	// 只写启用中的分片，并保证其 save/session 目录存在（首次生成世界需要）
	for _, name := range r.ShardNames() {
		dir := config.ShardDir(root, confDir, r.ID, name)
		if err := config.EnsureDirExists(filepath.Join(dir, "save", "session")); err != nil {
			return fmt.Errorf("创建 %s 存档目录失败: %w", name, err)
		}
	}

	for path, content := range r.Files(root, confDir) {
		if err := config.WriteFileAtomic(path, []byte(content)); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", path, err)
		}
	}
	return nil
}

// TokenFile 返回令牌文件路径，供接口展示提示信息。
func (r *Room) TokenFile(root, confDir string) string {
	return filepath.Join(config.ClusterDir(root, confDir, r.ID), tokenName)
}

// HasToken 判断房间是否已配置令牌。
func (r *Room) HasToken() bool { return strings.TrimSpace(r.Token) != "" }

// NeedsToken 判断当前开服模式是否必须配置令牌。
// 局域网与离线模式不向 Klei 注册，因此不需要令牌。
func (r *Room) NeedsToken() bool { return !r.LANOnly && !r.Offline }

// ClusterDir 返回房间的集群目录。
func (r *Room) ClusterDir(root, confDir string) string {
	return config.ClusterDir(root, confDir, r.ID)
}

// ExistsSave 判断房间是否已经生成过世界存档。
// 删除房间或改动世界参数前用它提醒用户，避免误操作毁掉存档。
func (r *Room) ExistsSave(root, confDir string) bool {
	for _, name := range r.ShardNames() {
		if config.ShardHasSave(config.ShardDir(root, confDir, r.ID, name)) {
			return true
		}
	}
	return false
}

// b 把布尔值渲染成 DST 认识的 true/false。
func b(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
