package workshop

import "testing"

// NormalizeID 要吃下三种输入：纯数字、workshop- 前缀、创意工坊链接。
func TestNormalizeID(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1392778117", "1392778117"},
		{"workshop-1392778117", "1392778117"},
		{"https://steamcommunity.com/sharedfiles/filedetails/?id=1392778117&searchtext=x", "1392778117"},
		{"  https://steamcommunity.com/sharedfiles/filedetails/?id=2287303119  ", "2287303119"},
		{"", ""},
		{"随便一段话", ""},
		{"12", ""}, // 太短，不像工坊 ID
	}
	for _, c := range cases {
		if got := NormalizeID(c.in); got != c.want {
			t.Errorf("NormalizeID(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeIDs(t *testing.T) {
	got := NormalizeIDs([]string{"1392778117", "workshop-1392778117", "id=2287303119,", "bad"})
	want := "1392778117,2287303119"
	if len(got) != 2 || got[0]+","+got[1] != want {
		t.Errorf("NormalizeIDs = %v, 期望 [%s]", got, want)
	}
}

// firstLine 剥 BBCode：创意工坊描述普遍带 [h1] [b] 之类标记。
func TestFirstLine(t *testing.T) {
	got := firstLine("[h1]棱镜[/h1]\r\n这是一段介绍")
	if got != "棱镜" {
		t.Errorf("firstLine = %q, 期望 棱镜", got)
	}
}
