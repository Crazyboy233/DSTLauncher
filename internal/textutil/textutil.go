// Package textutil 提供界面展示文本的清理工具。
//
// 背景：mod 作者喜欢在模组名开头放自己图标字体里的字形
// （如 U+F000A，Unicode 补充私用区 A 平面）。这些字符只对
// 定义了对应图标的那个字体有意义，任何系统字体都没有，
// 网页上只会渲染成豆腐块「□」。展示层统一在这里剥掉。
package textutil

import (
	"regexp"
	"strings"
)

// puaRe 匹配 Unicode 私用区字符：BMP 私用区（U+E000–U+F8FF）、
// 补充私用区 A（U+F0000–U+FFFFD）与 B（U+100000–U+10FFFD）。
var puaRe = regexp.MustCompile(
	`[\x{E000}-\x{F8FF}\x{F0000}-\x{FFFFD}\x{100000}-\x{10FFFD}]`)

// Clean 返回适合在界面上展示的文本：剥掉私用区字符、
// 收拢剥除后留下的多余空白、去掉首尾空白。
func Clean(s string) string {
	s = puaRe.ReplaceAllString(s, "")
	return strings.Join(strings.Fields(s), " ")
}
