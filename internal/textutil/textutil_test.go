package textutil

import "testing"

func TestCleanPUA(t *testing.T) {
	cases := [][2]string{
		{"\U000F000A Uncompromising Mode", "Uncompromising Mode"},
		{"\U000F000A UM永不妥协//汉化", "UM永不妥协//汉化"},
		{"\uE000图标名\uE001", "图标名"},            // BMP 私用区
		{"【爬屿】中文mod", "【爬屿】中文mod"},            // 正常字符不受影响
		{"  \U000F000A  双空格  名字  ", "双空格 名字"}, // 空白收拢
		{"\U000F000A", ""}, // 只有 PUA 字符 → 空
	}
	for _, c := range cases {
		if got := Clean(c[0]); got != c[1] {
			t.Errorf("Clean(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}
