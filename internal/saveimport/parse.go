package saveimport

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dst-windows/internal/config"
	"dst-windows/internal/modmgr"
	"dst-windows/internal/room"
	"dst-windows/internal/worldsettings"
)

// Parse 解析已定位到的存档目录（含 cluster.ini 的那一层）。
//
// 兼容两种 ini 结构，因为线上确实同时存在：
//   - 新版专用服务器：cluster.ini 用 [GAMEPLAY]/[NETWORK]/[MISC]/[SHARD]，
//     键名形如 cluster_name / max_snapshots；
//   - 早期版本（含从游戏客户端导出的存档）：cluster.ini 用 [MISC]/[SERVER]/[NETWORK]，
//     键名形如 name / default_server_name / max_snapshot_count，
//     而 game_mode、server_password 等又与 max_players 混在 server.ini 里。
//
// 所以解析策略是「拍平键值 + 按优先级取第一个非空值」，而不是认死分节名。
func Parse(clusterDir string) (*Parsed, error) {
	clusterIni, err := config.LoadIni(filepath.Join(clusterDir, clusterIniName))
	if err != nil {
		return nil, fmt.Errorf("读取 cluster.ini 失败: %w", err)
	}

	p := &Parsed{ClusterDir: clusterDir}

	scans, err := scanShards(clusterDir, p)
	if err != nil {
		return nil, err
	}
	if len(scans) == 0 {
		return nil, fmt.Errorf("存档里没有找到任何世界目录（应包含 Master/ 且其中有 server.ini）")
	}

	p.Shards = make([]ShardSave, 0, len(scans))
	for _, sc := range scans {
		p.Shards = append(p.Shards, sc.ShardSave)
		if sc.HasSave {
			p.HasSave = true
		}
	}

	// Master/server.ini 的键值作为 cluster.ini 的兜底：旧版存档把 game_mode、
	// server_password、max_snapshot_count 放在了分片配置里
	masterFlat := map[string]string{}
	for _, sc := range scans {
		if sc.IsMaster {
			masterFlat = sc.flat
			break
		}
	}
	maps := []map[string]string{flatten(clusterIni), masterFlat}

	p.Token, p.HasToken = readText(filepath.Join(clusterDir, "cluster_token.txt"))

	p.AdminList, p.HasAdmin = readText(filepath.Join(clusterDir, "adminlist.txt"))
	p.BlockList, p.HasBlock = readText(filepath.Join(clusterDir, "blocklist.txt"))
	p.WhiteList, p.HasWhite = readText(filepath.Join(clusterDir, "whitelist.txt"))

	// 模组配置：存档里每个分片各有一份 modoverrides.lua，而面板的模型是
	// 「一个房间一份、所有分片共用」。以地表为准；地表没有时退回任意一个
	// 带配置的分片。各分片内容不一致时提示，避免用户以为两边的模组相同。
	var modsRaw string
	var modsFrom []string
	for _, sc := range scans {
		if !sc.hasMods {
			continue
		}
		modsFrom = append(modsFrom, sc.Name)
		if sc.IsMaster || modsRaw == "" {
			modsRaw = sc.modsRaw
		}
	}
	if len(modsFrom) > 0 {
		p.Mods = modmgr.ParseOverrides(modsRaw)
		p.HasMods = true
		if len(modsFrom) > 1 {
			base := modsRaw
			for _, sc := range scans {
				if sc.hasMods && sc.modsRaw != base {
					p.warnf("各分片的模组配置不一致，已统一采用地表那份（面板的模组配置全房间共用）")
					break
				}
			}
		}
	}

	p.Room = buildRoom(p, maps, scans)
	return p, nil
}

// buildRoom 把 ini 键值映射成房间草稿。
// 机器本地字段（端口、集群密钥、master_ip、分片端口）刻意不填，见 MergeInto。
func buildRoom(p *Parsed, maps []map[string]string, scans []shardScan) *room.Room {
	r := room.Defaults()

	// 新版键名优先，旧版键名兜底；pick 会跳过空值，因此空字符串不会顶掉有效值
	if v := pick(maps, "cluster_name", "default_server_name", "name"); v != "" {
		r.Name = v
	}
	if v := pick(maps, "cluster_description", "default_server_description"); v != "" {
		r.Description = v
	}

	// game_mode 在旧版存档里位于 Master/server.ini 的 [SERVER]
	if v := pick(maps, "game_mode"); v != "" {
		mode := strings.ToLower(v)
		if !isValidGameMode(mode) {
			p.warnf("存档里的游戏模式 %q 面板不支持，已回退为默认模式", v)
		} else {
			r.GameMode = mode
		}
	}

	// 语言：面板只支持 zh / en，其余取值会被 Normalize 兜底成 zh，这里提前说明
	if v := pick(maps, "cluster_language", "server_language"); v != "" {
		lang := strings.ToLower(v)
		if lang != "zh" && lang != "en" {
			p.warnf("存档里的语言 %q 面板暂不支持，已回退为中文", v)
		}
		r.Language = lang
	}

	if n, ok := pickInt(maps, "max_players"); ok {
		r.MaxPlayers = n
	}
	if n, ok := pickInt(maps, "tick_rate"); ok {
		r.TickRate = n
	}
	// 回档点数：新版是 cluster.ini [MISC] max_snapshots，
	// 旧版是 server.ini [SERVER] max_snapshot_count
	if n, ok := pickInt(maps, "max_snapshots", "max_snapshot_count"); ok {
		r.MaxSnapshots = n
	}

	if v, ok := pickBool(maps, "pvp"); ok {
		r.PvP = v
	}
	if v, ok := pickBool(maps, "pause_when_empty"); ok {
		r.PauseEmpty = v
	}
	if v, ok := pickBool(maps, "vote_enabled"); ok {
		r.VoteEnabled = v
	}
	// 旧版没有独立的「投票踢人」开关，跟随投票总开关
	if v, ok := pickBool(maps, "vote_kick_enabled"); ok {
		r.VoteKick = v
	} else {
		r.VoteKick = r.VoteEnabled
	}
	if v, ok := pickBool(maps, "lan_only_cluster"); ok {
		r.LANOnly = v
	}
	if v, ok := pickBool(maps, "offline_cluster"); ok {
		r.Offline = v
	}

	// 密码：新版是 cluster_password，旧版在 server.ini 里叫 server_password。
	// 取不到时保持空串（= 无密码），与 DST 的默认行为一致。
	r.Password = pick(maps, "cluster_password", "server_password")

	// 洞穴是否启用取决于存档里有没有非地表分片
	r.Caves = false
	for _, sc := range scans {
		if !sc.IsMaster {
			r.Caves = true
			break
		}
	}

	// 留给调用方：新建走自动分配，覆盖沿用目标房间
	r.MasterIP = ""
	r.MasterPort = 0
	r.ClusterKey = ""
	r.Shards = nil

	return r
}

// shardScan 是分片扫描的中间结果，附带 server.ini 的扁平键值供兜底取值。
type shardScan struct {
	ShardSave
	flat map[string]string
	// modsRaw 是该分片的 modoverrides.lua 原文；hasMods 表示确实存在且非空。
	modsRaw string
	hasMods bool
}

// scanShards 扫描集群目录下的一级子目录，识别出分片。
func scanShards(clusterDir string, p *Parsed) ([]shardScan, error) {
	entries, err := os.ReadDir(clusterDir)
	if err != nil {
		return nil, fmt.Errorf("读取存档目录失败: %w", err)
	}

	var scans []shardScan
	for _, e := range entries {
		if !e.IsDir() || isJunkName(e.Name()) {
			continue
		}
		dir := filepath.Join(clusterDir, e.Name())
		iniPath := filepath.Join(dir, "server.ini")
		hasIni := config.FileExists(iniPath)
		hasSave := config.ShardHasSave(dir)
		// 既没有 server.ini 又没有 save 的目录与分片无关（mods/、备份目录等）
		if !hasIni && !hasSave {
			continue
		}

		sc := shardScan{
			ShardSave: ShardSave{Dir: e.Name(), HasSave: hasSave},
			flat:      map[string]string{},
		}
		if hasIni {
			if ini, err := config.LoadIni(iniPath); err != nil {
				p.warnf("读取 %s/server.ini 失败，该分片按默认值处理: %v", e.Name(), err)
			} else {
				sc.flat = flatten(ini)
			}
		}

		// is_master：以 server.ini 为准；缺失时按目录名兜底。
		// 旧版存档的 Master 分片 server.ini 里根本不写 is_master，只有 Caves 写 false。
		if v, ok := pickBool([]map[string]string{sc.flat}, "is_master"); ok {
			sc.IsMaster = v
		} else {
			sc.IsMaster = strings.EqualFold(e.Name(), "Master")
		}

		// 面板只支持 Master / Caves 两个分片，把逻辑名归一化到这两者，
		// 原始目录名保留在 Dir 里供拷贝使用
		if sc.IsMaster {
			sc.Name = "Master"
		} else {
			sc.Name = "Caves"
		}

		if ov, src, ok := readOverride(dir); ok {
			sc.Override, sc.HasOverride, sc.OverrideSrc = ov, true, src
		}
		// 模组配置原样带回去，由 Parse 统一挑选与解析
		if raw, ok := readText(filepath.Join(dir, "modoverrides.lua")); ok && raw != "" {
			sc.modsRaw, sc.hasMods = raw, true
		}
		scans = append(scans, sc)
	}

	// 没有明确的主分片时，把第一个提升为主分片（存档至少得有一个世界）
	hasMaster := false
	for _, sc := range scans {
		if sc.IsMaster {
			hasMaster = true
			break
		}
	}
	if !hasMaster && len(scans) > 0 {
		p.warnf("存档里没有标记 is_master 的分片，已按 Master 处理")
		scans[0].IsMaster = true
		scans[0].Name = "Master"
	}

	// 归一化后可能出现重名（两个目录都映射到 Caves），保留带世界数据的那一个
	out := make([]shardScan, 0, len(scans))
	seen := map[string]int{}
	for _, sc := range scans {
		idx, ok := seen[sc.Name]
		if !ok {
			seen[sc.Name] = len(out)
			out = append(out, sc)
			continue
		}
		p.warnf("存档里有多个 %s 分片，已保留 %s 并忽略 %s", sc.Name, out[idx].Dir, sc.Dir)
		if !out[idx].HasSave && sc.HasSave {
			out[idx] = sc
		}
	}

	// Master 排在最前，导入日志读起来才顺
	sort.SliceStable(out, func(i, j int) bool { return out[i].IsMaster && !out[j].IsMaster })
	return out, nil
}

// readOverride 读取分片的世界设置，优先 leveldataoverride.lua。
// 返回的文件名用于在界面上说明来源。
func readOverride(shardDir string) (*worldsettings.Override, string, bool) {
	for _, name := range []string{levelDataOverrideName, worldsettings.OverrideFileName} {
		data, err := os.ReadFile(filepath.Join(shardDir, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
		if content == "" {
			continue // 空文件视作没带世界设置
		}
		ov := worldsettings.ParseOverride(content)
		// leveldataoverride.lua 是游戏实际生效的世界数据，天然不带
		// override_enabled 字段；不强制置 true 的话，转换产物会以
		// override_enabled = false 落盘，引擎直接无视，改了也白改。
		if name == levelDataOverrideName {
			ov.Enabled = true
		}
		return ov, name, true
	}
	return nil, "", false
}

// readText 读取一个文本文件并去掉首尾空白，同时返回文件是否存在。
func readText(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff")), true
}

/* ---------- ini 取值工具 ---------- */

// flatten 把 INI 拍平成「键 -> 值」。
//
// 面板关心的是键名而不是分节：同一份设置在 [GAMEPLAY]、[NETWORK]、[MISC]、
// [SERVER] 之间搬来搬去，写死分节名会在版本更新后静默失效。
// 同名键只保留第一次出现的值，避免后面的空值覆盖前面的有效值。
func flatten(ini *config.Ini) map[string]string {
	out := make(map[string]string)
	for _, sec := range ini.Sections() {
		for _, k := range ini.Keys(sec) {
			if _, ok := out[k]; ok {
				continue
			}
			out[k] = strings.TrimSpace(ini.Get(sec, k))
		}
	}
	return out
}

// pick 按「先遍历 map 再遍历 key」的顺序返回第一个非空值。
// maps 的顺序即优先级：cluster.ini 在 server.ini 之前。
func pick(maps []map[string]string, keys ...string) string {
	for _, m := range maps {
		if m == nil {
			continue
		}
		for _, k := range keys {
			if v := strings.TrimSpace(m[k]); v != "" {
				return v
			}
		}
	}
	return ""
}

// pickBool 读取布尔值，第二个返回值表示是否取到了有效值。
func pickBool(maps []map[string]string, keys ...string) (bool, bool) {
	switch strings.ToLower(pick(maps, keys...)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// pickInt 读取整数，第二个返回值表示是否取到了有效值。
func pickInt(maps []map[string]string, keys ...string) (int, bool) {
	v := pick(maps, keys...)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

// isValidGameMode 判断游戏模式是否在面板支持的范围内。
func isValidGameMode(mode string) bool {
	for _, m := range room.ValidGameModes() {
		if m == mode {
			return true
		}
	}
	return false
}
