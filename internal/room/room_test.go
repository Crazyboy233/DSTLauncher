package room

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dst-windows/internal/config"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "rooms.json"))
	if err != nil {
		t.Fatalf("创建 Store 失败: %v", err)
	}
	return s
}

// 两个房间的端口不能重叠，否则同一台机器上第二个房间起不来。
func TestCreateAllocatesDistinctPorts(t *testing.T) {
	s := newTestStore(t)

	a, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建房间 A 失败: %v", err)
	}
	b, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建房间 B 失败: %v", err)
	}

	if a.ID == b.ID {
		t.Fatalf("房间 ID 重复: %d", a.ID)
	}
	if a.MasterPort == b.MasterPort {
		t.Errorf("分片协调端口重复: %d", a.MasterPort)
	}

	portOf := func(r *Room) []int {
		var out []int
		for _, sh := range r.Shards {
			out = append(out, sh.ServerPort, sh.MasterServerPort, sh.AuthenticationPort)
		}
		out = append(out, r.MasterPort)
		return out
	}
	seen := make(map[int]bool)
	for _, p := range append(portOf(a), portOf(b)...) {
		if seen[p] {
			t.Errorf("端口 %d 被两个房间同时占用", p)
		}
		seen[p] = true
	}

	// 每个房间都必须分到 Master + Caves 两套端口，启用洞穴时才有得用
	if len(a.Shards) != 2 {
		t.Fatalf("期望 2 个分片，实际 %d", len(a.Shards))
	}
	if !a.Shards[0].IsMaster || a.Shards[1].IsMaster {
		t.Errorf("IsMaster 标记不正确: %+v", a.Shards)
	}
}

// 编辑房间时不能因为没回传端口就重新分配，否则改个房名会把端口也换掉。
func TestUpdateKeepsPorts(t *testing.T) {
	s := newTestStore(t)
	created, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	// 模拟前端只改了房名、未提交分片端口
	edited := *created
	edited.Name = "改过名字的房间"
	edited.Shards = nil
	edited.MasterPort = 0

	updated, err := s.Update(&edited)
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if updated.Name != "改过名字的房间" {
		t.Errorf("房名未更新: %s", updated.Name)
	}
	if updated.MasterPort != created.MasterPort {
		t.Errorf("分片协调端口被改动: %d -> %d", created.MasterPort, updated.MasterPort)
	}
	for i := range created.Shards {
		if updated.Shards[i].ServerPort != created.Shards[i].ServerPort {
			t.Errorf("分片 %s 端口被改动: %d -> %d",
				created.Shards[i].Name, created.Shards[i].ServerPort, updated.Shards[i].ServerPort)
		}
	}
}

// 手工把端口改成与别人相同必须被拒绝，并且报出是哪个端口。
func TestPortConflictRejected(t *testing.T) {
	s := newTestStore(t)
	a, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建 A 失败: %v", err)
	}
	b, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建 B 失败: %v", err)
	}

	b.Shards[0].ServerPort = a.Shards[0].ServerPort
	if _, err := s.Update(b); err == nil {
		t.Fatal("端口冲突未被拦截")
	} else if !strings.Contains(err.Error(), "占用") {
		t.Errorf("错误信息不够明确: %v", err)
	}
}

// 删除后重新创建应复用最小可用 ID。
func TestDeleteThenCreateReusesID(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create(Defaults())
	b, _ := s.Create(Defaults())

	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	c, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("重新创建失败: %v", err)
	}
	if c.ID != a.ID {
		t.Errorf("期望复用 ID %d，实际 %d", a.ID, c.ID)
	}
	if c.ID == b.ID {
		t.Errorf("复用了仍在使用中的 ID %d", b.ID)
	}
}

// 删除房间只删定义、刻意保留磁盘存档：新房间必须跳过还留着内容的 Cluster_<id>，
// 否则 DST 会直接加载遗留 save/，表现为「新建的房间进到了以前的存档」。
func TestCreateSkipsClusterDirWithLeftoverSave(t *testing.T) {
	root := t.TempDir()
	clusterRoot := config.ClusterRoot(root, "DST")

	s, err := NewStore(filepath.Join(root, "rooms.json"))
	if err != nil {
		t.Fatalf("创建 Store 失败: %v", err)
	}
	s.SetClusterRoot(clusterRoot)

	// 模拟被删房间留下的世界存档
	leftover := filepath.Join(clusterRoot, "Cluster_1", "Master", "save", "session", "ABC123")
	if err := os.MkdirAll(leftover, 0755); err != nil {
		t.Fatalf("创建遗留目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "0000000001"), []byte("x"), 0644); err != nil {
		t.Fatalf("写入遗留存档失败: %v", err)
	}

	created, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建房间失败: %v", err)
	}
	if created.ID != 2 {
		t.Errorf("期望跳过留有存档的 Cluster_1 拿到 ID 2，实际 %d", created.ID)
	}
}

// 空壳目录不算占用：房间在生成世界前也会建出空的 save/session，
// 把它当成冲突会让 ID 一直往后漂。
func TestCreateReusesEmptyClusterDir(t *testing.T) {
	root := t.TempDir()
	clusterRoot := config.ClusterRoot(root, "DST")

	s, err := NewStore(filepath.Join(root, "rooms.json"))
	if err != nil {
		t.Fatalf("创建 Store 失败: %v", err)
	}
	s.SetClusterRoot(clusterRoot)

	if err := os.MkdirAll(filepath.Join(clusterRoot, "Cluster_1", "Master", "save", "session"), 0755); err != nil {
		t.Fatalf("创建空目录失败: %v", err)
	}

	created, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建房间失败: %v", err)
	}
	if created.ID != 1 {
		t.Errorf("空目录不应阻止复用 ID 1，实际拿到 %d", created.ID)
	}
}

// 落盘再读回应保持一致，避免重启面板后配置漂移。
func TestStorePersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rooms.json")

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("创建 Store 失败: %v", err)
	}
	created, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	created.Token = "test-token-value"
	if _, err := s.Update(created); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	again, err := NewStore(path)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	list := again.List()
	if len(list) != 1 {
		t.Fatalf("期望 1 个房间，实际 %d", len(list))
	}
	got := list[0]
	if got.Token != "test-token-value" {
		t.Errorf("令牌未持久化: %q", got.Token)
	}
	if got.ClusterKey != created.ClusterKey {
		t.Errorf("通信密钥未持久化: %q vs %q", got.ClusterKey, created.ClusterKey)
	}
	if got.MasterPort != created.MasterPort {
		t.Errorf("端口未持久化: %d vs %d", got.MasterPort, created.MasterPort)
	}
}

// 生成的 ini 必须严格落在 DST 认识的节里。
// 这是本项目踩过最深的坑：节名写错不会报错，只会让设置静默失效。
func TestGeneratedIniStructure(t *testing.T) {
	r := Defaults()
	r.ID = 1
	r.Name = "测试房间"
	r.Token = "abcdefghijklmnop"
	r.MasterPort = 21001
	r.ClusterKey = "testkey1234567"
	r.Normalize()
	r.Shards = []Shard{
		{Name: "Master", WorldType: WorldTypeForest, GameID: 1, IsMaster: true, ServerPort: 11000, MasterServerPort: 31000, AuthenticationPort: 41000},
		{Name: "Caves", WorldType: WorldTypeCave, GameID: 2, ServerPort: 11001, MasterServerPort: 31001, AuthenticationPort: 41001},
	}

	root := t.TempDir()
	if err := Generate(root, "DST", r); err != nil {
		t.Fatalf("生成配置失败: %v", err)
	}

	clusterIni := readFile(t, filepath.Join(root, "DST", "Cluster_1", "cluster.ini"))
	// 洞穴能否连上地表完全取决于这几个键
	for _, want := range []string{
		"[GAMEPLAY]", "[NETWORK]", "[MISC]", "[SHARD]",
		"shard_enabled = true",
		"master_ip = 127.0.0.1",
		"master_port = 21001",
		"cluster_key = testkey1234567",
		"game_mode = endless",
		"cluster_name = 测试房间",
		"tick_rate = 30",
	} {
		if !strings.Contains(clusterIni, want) {
			t.Errorf("cluster.ini 缺少 %q\n---\n%s", want, clusterIni)
		}
	}
	// 这些节名在 cluster.ini 中不存在，写了也不会被 DST 读取
	for _, bad := range []string{"[SERVER]", "[WORLD]"} {
		if strings.Contains(clusterIni, bad) {
			t.Errorf("cluster.ini 出现无效节 %s", bad)
		}
	}

	for _, shard := range []struct {
		name string
		port string
		msp  string
		ap   string
		mstr string
	}{
		{"Master", "11000", "31000", "41000", "true"},
		{"Caves", "11001", "31001", "41001", "false"},
	} {
		ini := readFile(t, filepath.Join(root, "DST", "Cluster_1", shard.name, "server.ini"))
		for _, want := range []string{
			"[NETWORK]", "[SHARD]", "[STEAM]", "[ACCOUNT]",
			"server_port = " + shard.port,
			"is_master = " + shard.mstr,
			"name = " + shard.name,
			"master_server_port = " + shard.msp,
			"authentication_port = " + shard.ap,
			"encode_user_path = true",
		} {
			if !strings.Contains(ini, want) {
				t.Errorf("%s/server.ini 缺少 %q\n---\n%s", shard.name, want, ini)
			}
		}
		if strings.Contains(ini, "[SERVER]") || strings.Contains(ini, "[WORLD]") {
			t.Errorf("%s/server.ini 出现无效节", shard.name)
		}
	}

	// 令牌必须原样写入，且不带 BOM
	token := readFile(t, filepath.Join(root, "DST", "Cluster_1", "cluster_token.txt"))
	if token != "abcdefghijklmnop" {
		t.Errorf("令牌内容不正确: %q", token)
	}
	if strings.HasPrefix(token, "\xEF\xBB\xBF") {
		t.Error("令牌文件带 BOM")
	}

	// 名单文件必须存在，否则 DST 启动时报错
	for _, name := range []string{"adminlist.txt", "blocklist.txt", "whitelist.txt"} {
		if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_1", name)); err != nil {
			t.Errorf("缺少 %s: %v", name, err)
		}
	}
	// save/session 必须预先建好，否则首次生成世界失败
	if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_1", "Master", "save", "session")); err != nil {
		t.Errorf("缺少 Master/save/session: %v", err)
	}
}

// 停用的分片不应生成 server.ini，避免留下会误导人的过期配置；
// 但定义要保留，否则再启用时端口和世界 id 全变。
func TestGenerateSkipsDisabledShard(t *testing.T) {
	r := Defaults()
	r.ID = 2
	r.Shards[1].Disabled = true
	r.Normalize()

	root := t.TempDir()
	if err := Generate(root, "DST", r); err != nil {
		t.Fatalf("生成配置失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_2", "Caves", "server.ini")); !os.IsNotExist(err) {
		t.Errorf("分片已停用却生成了 Caves/server.ini")
	}
	if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_2", "Master", "server.ini")); err != nil {
		t.Errorf("Master/server.ini 未生成: %v", err)
	}
	if len(r.ShardNames()) != 1 || r.ShardNames()[0] != "Master" {
		t.Errorf("分片列表不正确: %v", r.ShardNames())
	}
	if len(r.Shards) != 2 {
		t.Errorf("停用不应删除分片定义: %+v", r.Shards)
	}
}

// 旧版配置用 caves=false 关洞穴，加载后必须迁移成「Caves 分片停用」，
// 不能因为字段消失就把洞穴重新启用。
func TestLegacyCavesMigratesToDisabledShard(t *testing.T) {
	raw := `{
	  "id": 7, "name": "旧房间", "caves": false,
	  "shards": [
	    {"name": "Master", "gameId": 1, "isMaster": true,
	     "serverPort": 11000, "masterServerPort": 31000, "authenticationPort": 41000},
	    {"name": "Caves", "gameId": 2, "isMaster": false,
	     "serverPort": 11001, "masterServerPort": 31001, "authenticationPort": 41001}
	  ]
	}`
	var r Room
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("解析旧配置失败: %v", err)
	}
	r.Normalize()

	if got := r.ShardNames(); len(got) != 1 || got[0] != "Master" {
		t.Errorf("旧配置的洞穴应保持停用，实际启用分片: %v", got)
	}
	if len(r.Shards) != 2 {
		t.Fatalf("分片定义不应丢失: %+v", r.Shards)
	}
	if !r.Shards[1].Disabled {
		t.Errorf("Caves 应被迁移为停用: %+v", r.Shards[1])
	}
	// 类型必须补上，否则世界设置页会拿不到正确的预设
	if r.Shards[0].WorldType != WorldTypeForest || r.Shards[1].WorldType != WorldTypeCave {
		t.Errorf("世界类型推断不正确: %+v", r.Shards)
	}
}

// 局域网/离线模式不需要令牌，在线模式必须要有。
func TestNeedsToken(t *testing.T) {
	r := Defaults()
	if !r.NeedsToken() {
		t.Error("默认（在线）模式应当需要令牌")
	}
	r.LANOnly = true
	if r.NeedsToken() {
		t.Error("仅局域网模式不应要求令牌")
	}
	r.LANOnly, r.Offline = false, true
	if r.NeedsToken() {
		t.Error("离线模式不应要求令牌")
	}
}

// validRoom 返回一个校验能通过的房间（端口由 Store 分配，测试里直接给一套）。
func validRoom() *Room {
	r := Defaults()
	r.ID = 1
	r.Normalize()
	for i := range r.Shards {
		r.Shards[i].ServerPort = 11000 + i
		r.Shards[i].MasterServerPort = 31000 + i
		r.Shards[i].AuthenticationPort = 41000 + i
	}
	r.MasterPort = 21000
	return r
}

// 非法取值必须被拦下，而不是写出 DST 无法识别的配置。
func TestValidateRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name  string
		mutin func(*Room)
	}{
		{"空房名", func(r *Room) { r.Name = "  " }},
		{"非法游戏模式", func(r *Room) { r.GameMode = "custom" }},
		{"人数越界", func(r *Room) { r.MaxPlayers = 200 }},
		{"tick 非法", func(r *Room) { r.TickRate = 45 }},
		{"端口越界", func(r *Room) { r.Shards[0].ServerPort = 80 }},
		{"密钥为空", func(r *Room) { r.ClusterKey = "" }},
		{"分片名含空格", func(r *Room) { r.Shards[0].Name = "My World" }},
		{"分片名含斜杠", func(r *Room) { r.Shards[0].Name = "a/b" }},
		{"分片名是保留名", func(r *Room) { r.Shards[0].Name = "CON" }},
		{"世界 ID 重复", func(r *Room) { r.Shards[1].GameID = r.Shards[0].GameID }},
		{"主分片停用", func(r *Room) { r.Shards[0].Disabled = true }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := validRoom()
			c.mutin(r)
			if err := r.Validate(); err == nil {
				t.Errorf("%s 未被拦截", c.name)
			}
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(data)
}

// 第三个世界：分片数量不设上限，每个分片都要有自己的目录、端口与 server.ini。
func TestThirdShardGeneratesOwnConfig(t *testing.T) {
	s := newTestStore(t)
	created, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	edited := *created
	edited.Shards = append(append([]Shard(nil), created.Shards...),
		Shard{Name: "Underworld", WorldType: WorldTypeCave})
	updated, err := s.Update(&edited)
	if err != nil {
		t.Fatalf("新增分片失败: %v", err)
	}
	if len(updated.Shards) != 3 {
		t.Fatalf("期望 3 个分片，实际 %d", len(updated.Shards))
	}

	// 新分片必须拿到非零端口，且全房间端口不得重复
	seen := make(map[int]string)
	for _, sh := range updated.Shards {
		for _, p := range []int{sh.ServerPort, sh.MasterServerPort, sh.AuthenticationPort} {
			if p < 1024 {
				t.Errorf("%s 端口未分配: %d", sh.Name, p)
			}
			if other, ok := seen[p]; ok {
				t.Errorf("端口 %d 被 %s 与 %s 同时占用", p, other, sh.Name)
			}
			seen[p] = sh.Name
		}
	}

	root := t.TempDir()
	if err := Generate(root, "DST", updated); err != nil {
		t.Fatalf("生成配置失败: %v", err)
	}

	for i, sh := range updated.Shards {
		ini := readFile(t, filepath.Join(root, "DST", fmt.Sprintf("Cluster_%d", updated.ID), sh.Name, "server.ini"))
		for _, want := range []string{
			"name = " + sh.Name,
			fmt.Sprintf("id = %d", i+1),
			fmt.Sprintf("server_port = %d", sh.ServerPort),
		} {
			if !strings.Contains(ini, want) {
				t.Errorf("%s/server.ini 缺少 %q\n---\n%s", sh.Name, want, ini)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "DST", fmt.Sprintf("Cluster_%d", updated.ID), sh.Name, "save", "session")); err != nil {
			t.Errorf("%s/save/session 未创建: %v", sh.Name, err)
		}
	}
}

// 新增分片不能沿用别的分片端口，也不能因为没回传端口而拿 0。
func TestAddShardAllocatesFreshPorts(t *testing.T) {
	s := newTestStore(t)
	first, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建房间 A 失败: %v", err)
	}
	second, err := s.Create(Defaults())
	if err != nil {
		t.Fatalf("创建房间 B 失败: %v", err)
	}

	edited := *first
	edited.Shards = append(append([]Shard(nil), first.Shards...),
		Shard{Name: "Third", WorldType: WorldTypeForest})
	updated, err := s.Update(&edited)
	if err != nil {
		t.Fatalf("新增分片失败: %v", err)
	}

	third := updated.Shards[len(updated.Shards)-1]
	if third.ServerPort == 0 {
		t.Fatal("新分片端口未分配")
	}
	// 不能撞到别的房间
	for _, sh := range second.Shards {
		for _, p := range []int{sh.ServerPort, sh.MasterServerPort, sh.AuthenticationPort} {
			for _, mine := range []int{third.ServerPort, third.MasterServerPort, third.AuthenticationPort} {
				if p == mine {
					t.Errorf("新分片端口 %d 与房间 B 撞号", p)
				}
			}
		}
	}
}

// id 撞号要自动改掉冲突的那一条，不能整体重排：
// 玩家的传送门记的是 id，改一次就得重新配一次。
func TestNormalizeKeepsGameIDAndFixesCollision(t *testing.T) {
	r := Defaults()
	r.Shards[1].GameID = 1 // 与主分片撞号
	r.Normalize()

	if len(r.Shards) != 2 {
		t.Fatalf("分片数变了: %+v", r.Shards)
	}
	if r.Shards[0].GameID != 1 {
		t.Errorf("主分片 id 被改动: %d", r.Shards[0].GameID)
	}
	if r.Shards[1].GameID == 1 || r.Shards[1].GameID == 0 {
		t.Errorf("冲突的 id 未被重新分配: %d", r.Shards[1].GameID)
	}
}

// 主分片永远排首位，ShardNames 的输出顺序决定 tab 顺序与启动顺序。
func TestShardNamesMasterFirst(t *testing.T) {
	r := Defaults()
	// 手工把主分片挪到中间
	r.Shards = []Shard{
		{Name: "Caves", WorldType: WorldTypeCave, GameID: 2},
		{Name: "Master", WorldType: WorldTypeForest, GameID: 1, IsMaster: true},
		{Name: "Third", WorldType: WorldTypeForest, GameID: 3},
	}
	r.Normalize()

	names := r.ShardNames()
	if len(names) != 3 || names[0] != "Master" {
		t.Errorf("主分片未排在首位: %v", names)
	}
}

// 新分片首次启动前必须有世界预设：缺文件时引擎按地表生成，
// 名叫 Caves 的分片会长出第二张地表图（这是实际踩过的坑）。
func TestGenerateWritesWorldPreset(t *testing.T) {
	r := validRoom()
	r.Shards = append(r.Shards, Shard{
		Name: "World3", WorldType: WorldTypeCave, GameID: 3,
		ServerPort: 11002, MasterServerPort: 31002, AuthenticationPort: 41002,
	})

	root := t.TempDir()
	if err := Generate(root, "DST", r); err != nil {
		t.Fatalf("生成配置失败: %v", err)
	}

	cases := map[string]string{
		"Master": "SURVIVAL_TOGETHER",
		"Caves":  "DST_CAVE",
		"World3": "DST_CAVE",
	}
	for shard, preset := range cases {
		content := readFile(t, filepath.Join(root, "DST", "Cluster_1", shard, "worldgenoverride.lua"))
		if !strings.Contains(content, fmt.Sprintf("worldgen_preset = %q", preset)) {
			t.Errorf("%s 的预设不是 %s:/n%s", shard, preset, content)
		}
	}
}

// 已有的世界配置不能被覆盖：那是用户在「世界配置」页逐项调过的设置。
func TestGenerateKeepsExistingWorldPreset(t *testing.T) {
	r := validRoom()
	root := t.TempDir()
	dir := filepath.Join(root, "DST", "Cluster_1", "Caves")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	custom := "return {\n\toverride_enabled = true,\n\toverrides = { day = \"long\" },\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "worldgenoverride.lua"), []byte(custom), 0644); err != nil {
		t.Fatalf("写自定义配置失败: %v", err)
	}

	if err := Generate(root, "DST", r); err != nil {
		t.Fatalf("生成配置失败: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "worldgenoverride.lua")); got != custom {
		t.Errorf("已有的世界配置被覆盖了:\n%s", got)
	}
}
