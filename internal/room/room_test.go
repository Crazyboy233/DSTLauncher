package room

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		{Name: "Master", GameID: 1, IsMaster: true, ServerPort: 11000, MasterServerPort: 31000, AuthenticationPort: 41000},
		{Name: "Caves", GameID: 2, IsMaster: false, ServerPort: 11001, MasterServerPort: 31001, AuthenticationPort: 41001},
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

// 未启用洞穴时不应生成 Caves 的 server.ini，避免留下会误导人的过期配置。
func TestGenerateSkipsDisabledCaves(t *testing.T) {
	r := Defaults()
	r.ID = 2
	r.Caves = false
	r.Normalize()

	root := t.TempDir()
	if err := Generate(root, "DST", r); err != nil {
		t.Fatalf("生成配置失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_2", "Caves", "server.ini")); !os.IsNotExist(err) {
		t.Errorf("未启用洞穴却生成了 Caves/server.ini")
	}
	if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_2", "Master", "server.ini")); err != nil {
		t.Errorf("Master/server.ini 未生成: %v", err)
	}
	if len(r.ShardNames()) != 1 || r.ShardNames()[0] != "Master" {
		t.Errorf("分片列表不正确: %v", r.ShardNames())
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Defaults()
			r.ID = 1
			r.Normalize()
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
