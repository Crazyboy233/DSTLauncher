package saveimport

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dst-windows/internal/room"
)

// writeFile 在测试里写一个文件，父目录自动创建。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写文件失败 %s: %v", path, err)
	}
}

// modernCluster 造一份新版专用服务器格式的存档。
func modernCluster(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "cluster.ini"), `[GAMEPLAY]
game_mode = survival
max_players = 8
pvp = false
pause_when_empty = false
vote_enabled = false
vote_kick_enabled = false

[NETWORK]
lan_only_cluster = true
offline_cluster = false
cluster_description = 测试描述
cluster_name = 测试房间
cluster_password = secret
cluster_language = en
tick_rate = 60

[MISC]
console_enabled = true
max_snapshots = 20

[SHARD]
shard_enabled = true
bind_ip = 0.0.0.0
master_ip = 10.0.0.5
master_port = 22222
cluster_key = ABCDEFG
`)
	writeFile(t, filepath.Join(dir, "cluster_token.txt"), "pds-g^TOKEN\n")
	writeFile(t, filepath.Join(dir, "adminlist.txt"), "KU_abc\nKU_def\n")
	writeFile(t, filepath.Join(dir, "Master", "server.ini"), `[NETWORK]
server_port = 10999

[SHARD]
id = 1
is_master = true
name = Master
`)
	writeFile(t, filepath.Join(dir, "Master", "save", "shardindex"), "x")
	writeFile(t, filepath.Join(dir, "Master", "leveldataoverride.lua"), `return {
	override_enabled = true,
	worldgen_preset = "SURVIVAL_TOGETHER",
	settings_preset = "SURVIVAL_TOGETHER",
	overrides = {
		["world_size"] = "large",
		-- 下面这行是垃圾内容，解析时应被跳过而不是报错
		not a valid line,
		["day"] = "long",
	},
}`)
	writeFile(t, filepath.Join(dir, "Caves", "server.ini"), `[SHARD]
id = 2
is_master = false
name = Caves
`)
	writeFile(t, filepath.Join(dir, "Caves", "save", "shardindex"), "y")
	// 两份 modoverrides 故意写得不一致：验证「以地表为准 + 提示不一致」
	writeFile(t, filepath.Join(dir, "Master", "modoverrides.lua"), `return {
	["workshop-362175979"] = {
		enabled = true,
		configuration_options = { ["Draw over FoW"] = "disabled" },
	},
	["workshop-1216718131"] = {
		enabled = false,
	},
}`)
	writeFile(t, filepath.Join(dir, "Caves", "modoverrides.lua"), `return {
	["workshop-999999999"] = { enabled = true },
}`)
	return dir
}

func TestParseModernFormat(t *testing.T) {
	p, err := Parse(modernCluster(t))
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}

	r := p.Room
	checks := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Name", r.Name, "测试房间"},
		{"Description", r.Description, "测试描述"},
		{"GameMode", r.GameMode, "survival"},
		{"MaxPlayers", r.MaxPlayers, 8},
		{"PvP", r.PvP, false},
		{"PauseEmpty", r.PauseEmpty, false},
		{"VoteEnabled", r.VoteEnabled, false},
		{"VoteKick", r.VoteKick, false},
		{"Password", r.Password, "secret"},
		{"Language", r.Language, "en"},
		{"TickRate", r.TickRate, 60},
		{"MaxSnapshots", r.MaxSnapshots, 20},
		{"LANOnly", r.LANOnly, true},
		{"Offline", r.Offline, false},
		{"Caves", r.Caves, true},
		// 机器本地字段一律不导入
		{"MasterIP(不导入)", r.MasterIP, ""},
		{"MasterPort(不导入)", r.MasterPort, 0},
		{"ClusterKey(不导入)", r.ClusterKey, ""},
		{"Token", p.Token, "pds-g^TOKEN"},
		{"HasToken", p.HasToken, true},
		{"HasAdmin", p.HasAdmin, true},
		{"HasSave", p.HasSave, true},
		{"Shards", strings.Join(shardNames(p), ","), "Master,Caves"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, 期望 %v", c.name, c.got, c.want)
		}
	}

	// 世界设置应被解析出来（含自定义键）
	var master *ShardSave
	for i := range p.Shards {
		if p.Shards[i].Name == "Master" {
			master = &p.Shards[i]
		}
	}
	if master == nil || !master.HasOverride {
		t.Fatalf("Master 的世界设置没被解析出来")
	}
	if got := master.Override.Overrides["world_size"]; got != "large" {
		t.Errorf("world_size = %q, 期望 large", got)
	}
	if got := master.Override.Overrides["day"]; got != "long" {
		t.Errorf("day = %q, 期望 long", got)
	}
	if master.OverrideSrc != levelDataOverrideName {
		t.Errorf("OverrideSrc = %q, 期望 %q", master.OverrideSrc, levelDataOverrideName)
	}
	// leveldata 是游戏实际生效的世界数据，转换产物必须是启用状态，
	// 否则写出的 worldgenoverride.lua 会带 override_enabled = false，引擎直接无视
	if !master.Override.Enabled {
		t.Error("来自 leveldataoverride 的世界设置应强制 Enabled=true")
	}

	// 模组配置：以地表为准，且分片间不一致要提示
	if !p.HasMods || p.Mods == nil {
		t.Fatalf("modoverrides.lua 没有被解析出来")
	}
	if !p.Mods.Enabled["workshop-362175979"] {
		t.Errorf("地表启用的模组应为启用: %v", p.Mods.Enabled)
	}
	if p.Mods.Enabled["workshop-1216718131"] {
		t.Errorf("地表禁用的模组应为禁用: %v", p.Mods.Enabled)
	}
	// 只保留地表那份，洞穴里那个模组不应混进来
	if _, leaked := p.Mods.Enabled["workshop-999999999"]; leaked {
		t.Errorf("洞穴独有的模组不应出现在结果里: %v", p.Mods.Enabled)
	}
	inconsistent := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "模组配置不一致") {
			inconsistent = true
		}
	}
	if !inconsistent {
		t.Errorf("分片间模组配置不一致应当提示, warnings=%v", p.Warnings)
	}
	if got := p.Mods.EnabledWorkshopIDs(); len(got) != 1 || got[0] != "362175979" {
		t.Errorf("EnabledWorkshopIDs = %v, 期望只含 362175979", got)
	}
}

// legacyCluster 造一份早期格式的存档：cluster.ini 用扁平键名，
// game_mode / server_password / max_snapshot_count 藏在 server.ini 里，
// 且 Master 的 server.ini 没有 is_master。
func legacyCluster(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "cluster.ini"), `[SHARD]
shard = MyDediServer/Master
master = MyDediServer/Master
caves = MyDediServer/Caves

[MISC]
max_players = 6
pvp = true
pause_when_empty = true
vote_enabled = true
require_vote_by_clients = true
name =
default_server_name = My DST Server
default_server_description = 欢迎来到我的饥荒服务器
server_language = zh
tick_rate = 30
max_snapshot_rate = 33
console_enabled = true

[SERVER]
public = true
steam_authentication = true

[NETWORK]
encrypt = true
`)
	writeFile(t, filepath.Join(dir, "Master", "server.ini"), `[SHARD]
name = Master
description =

[NETWORK]
server_port = 10999

[SERVER]
game_mode = endless
server_password = pw
max_players = 6
pvp = true
max_snapshot_count = 10
`)
	return dir
}

func TestParseLegacyFormat(t *testing.T) {
	p, err := Parse(legacyCluster(t))
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}

	r := p.Room
	checks := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"Name", r.Name, "My DST Server"},
		{"Description", r.Description, "欢迎来到我的饥荒服务器"},
		// game_mode 只在 server.ini 里，必须能从兜底路径取到
		{"GameMode", r.GameMode, "endless"},
		{"MaxPlayers", r.MaxPlayers, 6},
		{"PvP", r.PvP, true},
		{"Password", r.Password, "pw"},
		{"Language", r.Language, "zh"},
		{"TickRate", r.TickRate, 30},
		// 旧版叫 max_snapshot_count
		{"MaxSnapshots", r.MaxSnapshots, 10},
		// 旧版没有独立的投票踢人开关，跟随 vote_enabled
		{"VoteKick", r.VoteKick, true},
		// 只有 Master 一个世界
		{"Caves", r.Caves, false},
		// 旧版样例不带 modoverrides.lua
		{"HasMods", p.HasMods, false},
		{"HasSave", p.HasSave, false},
		{"Shards", strings.Join(shardNames(p), ","), "Master"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, 期望 %v", c.name, c.got, c.want)
		}
	}
}

func TestMergeIntoKeepsLocalFields(t *testing.T) {
	p, err := Parse(modernCluster(t))
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}

	// 新建：机器本地字段留空，交给 Store 分配
	fresh := MergeInto(p, nil)
	if fresh.ID != 0 || fresh.Shards != nil || fresh.ClusterKey != "" || fresh.MasterIP != "" {
		t.Errorf("新建时机器本地字段应全部留空, got id=%d shards=%v key=%q ip=%q",
			fresh.ID, fresh.Shards, fresh.ClusterKey, fresh.MasterIP)
	}

	// 覆盖：端口 / 密钥 / master_ip / ID 沿用目标房间，玩法配置被存档覆盖
	target := &room.Room{
		ID:         7,
		Name:       "旧名字",
		MasterIP:   "127.0.0.1",
		MasterPort: 21007,
		ClusterKey: "OLDKEY",
		Token:      "old-token",
		Shards: []room.Shard{
			{Name: "Master", GameID: 1, IsMaster: true, ServerPort: 11007},
			{Name: "Caves", GameID: 2, ServerPort: 11008},
		},
	}
	got := MergeInto(p, target)

	if got.ID != 7 {
		t.Errorf("ID = %d, 期望沿用 7", got.ID)
	}
	if got.MasterPort != 21007 || got.ClusterKey != "OLDKEY" || got.MasterIP != "127.0.0.1" {
		t.Errorf("机器本地字段被覆盖了: port=%d key=%q ip=%q", got.MasterPort, got.ClusterKey, got.MasterIP)
	}
	if len(got.Shards) != 2 || got.Shards[0].ServerPort != 11007 || got.Shards[1].ServerPort != 11008 {
		t.Errorf("分片端口没沿用目标房间: %+v", got.Shards)
	}
	if got.Name != "测试房间" || got.GameMode != "survival" {
		t.Errorf("玩法配置没被存档覆盖: name=%q mode=%q", got.Name, got.GameMode)
	}
	// 存档带了令牌 -> 用存档的
	if got.Token != "pds-g^TOKEN" {
		t.Errorf("Token = %q, 期望用存档里的", got.Token)
	}
}

func TestMergeIntoKeepsTokenWhenSaveLacksIt(t *testing.T) {
	p, err := Parse(legacyCluster(t))
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	target := &room.Room{ID: 1, Token: "keep-me"}
	if got := MergeInto(p, target); got.Token != "keep-me" {
		t.Errorf("存档没有令牌时应保留房间原值, got %q", got.Token)
	}
}

func TestExtractAndFindClusterDir(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "save.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("创建 zip 失败: %v", err)
	}
	zw := zip.NewWriter(f)
	// 多套一层目录，模拟用户自己重新打包
	entries := map[string]string{
		"备份/Cluster_1/cluster.ini":       "[MISC]\ncluster_name = x\n",
		"备份/Cluster_1/Master/server.ini": "[SHARD]\nname = Master\n",
		"__MACOSX/._cluster.ini":         "junk",
	}
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("写 zip 条目失败: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("写 zip 内容失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	f.Close()

	dst := filepath.Join(root, "out")
	if err := Extract(zipPath, dst); err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	found, err := FindClusterDir(dst)
	if err != nil {
		t.Fatalf("定位 cluster.ini 失败: %v", err)
	}
	if filepath.Base(found) != "Cluster_1" {
		t.Errorf("定位到 %s, 期望 .../Cluster_1", found)
	}
	if _, err := os.Stat(filepath.Join(dst, "__MACOSX")); !os.IsNotExist(err) {
		t.Errorf("__MACOSX 垃圾目录应被跳过")
	}
}

func TestExtractRejectsZipSlip(t *testing.T) {
	root := t.TempDir()
	zipPath := filepath.Join(root, "evil.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("创建 zip 失败: %v", err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatalf("写 zip 条目失败: %v", err)
	}
	_, _ = w.Write([]byte("x"))
	_ = zw.Close()
	f.Close()

	if err := Extract(zipPath, filepath.Join(root, "out")); err == nil {
		t.Fatalf("含 ../ 的压缩包应当被拒绝")
	}
}

func TestFindClusterDirMissing(t *testing.T) {
	if _, err := FindClusterDir(t.TempDir()); err == nil {
		t.Fatalf("没有 cluster.ini 时应报错")
	}
}

func shardNames(p *Parsed) []string {
	out := make([]string, 0, len(p.Shards))
	for _, s := range p.Shards {
		out = append(out, s.Name)
	}
	return out
}
