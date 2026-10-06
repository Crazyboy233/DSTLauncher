package room

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidKleiID(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"KU_ABCdef123", true},
		{"KU_" + strings.Repeat("a", 32), true},
		{"KU_", false},                           // 缺 ID 段
		{"KU_含中文", false},                        // 非字母数字
		{"KU_" + strings.Repeat("a", 33), false}, // 超长
		{"abc", false},                           // 缺前缀
		{"", false},
		{" KU_abc ", false}, // 未 trim 的原始串不合法
	}
	for _, c := range cases {
		if got := ValidKleiID(c.id); got != c.want {
			t.Errorf("ValidKleiID(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestAdminsRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// 文件不存在时读出空名单
	list, err := ReadAdmins(dir)
	if err != nil || len(list) != 0 {
		t.Fatalf("空目录 ReadAdmins = %v, %v", list, err)
	}

	list, err = AddAdmin(dir, "KU_player1")
	if err != nil || len(list) != 1 {
		t.Fatalf("AddAdmin = %v, %v", list, err)
	}

	// 重复添加幂等（大小写不敏感去重，前缀自动归一为大写）
	list, err = AddAdmin(dir, "ku_player1")
	if err != nil || len(list) != 1 {
		t.Fatalf("重复 AddAdmin = %v, %v", list, err)
	}
	if list[0] != "KU_player1" {
		t.Errorf("前缀应归一为大写: %q", list[0])
	}

	// 再加一个 + 混入空行注释
	list, err = AddAdmin(dir, "KU_player2")
	if err != nil || len(list) != 2 {
		t.Fatalf("AddAdmin 第二个 = %v, %v", list, err)
	}

	// 落盘内容可读、带注释头
	data, err := os.ReadFile(filepath.Join(dir, AdminsFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# 房间管理员名单") {
		t.Errorf("文件缺少注释头: %q", data)
	}

	// 移除
	list, err = RemoveAdmin(dir, "KU_player1")
	if err != nil || len(list) != 1 || list[0] != "KU_player2" {
		t.Fatalf("RemoveAdmin = %v, %v", list, err)
	}

	// 移除不存在的报错
	if _, err := RemoveAdmin(dir, "KU_nobody"); err == nil {
		t.Error("移除不存在的管理员应报错")
	}

	// 非法 ID 拒绝
	if _, err := AddAdmin(dir, "not-a-kleid"); err == nil {
		t.Error("非法 ID 应被拒绝")
	}
}

func TestNormalizeAdmins(t *testing.T) {
	content := "\n# 注释\nKU_a\nKU_b\n  KU_a  \n\n"
	got := normalizeAdmins(content)
	if len(got) != 2 || got[0] != "KU_a" || got[1] != "KU_b" {
		t.Errorf("normalizeAdmins = %v, want [KU_a KU_b]", got)
	}
}
