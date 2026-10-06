package worldsettings

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"dst-windows/internal/config"
)

// OverrideFileName 是每分片一份的世界配置文件。
//
// 只写这一个文件。同目录下的 leveldataoverride.lua 优先级更高、且会完全覆盖
// 已保存的世界数据，那是游戏内部用的，面板绝不碰。
const OverrideFileName = "worldgenoverride.lua"

// Override 是 worldgenoverride.lua 的内容。
type Override struct {
	Enabled        bool              `json:"overrideEnabled"`
	WorldGenPreset string            `json:"worldgenPreset"`
	SettingsPreset string            `json:"settingsPreset"`
	Overrides      map[string]string `json:"overrides"`
}

// DefaultPreset 返回某分片的默认预设。
// Master 与 Caves 的预设不可混用：地表用 SURVIVAL_TOGETHER 一系，洞穴只有 DST_CAVE 一系。
func DefaultPreset(shard string) string {
	if strings.EqualFold(shard, ShardCaves) {
		return "DST_CAVE"
	}
	return "SURVIVAL_TOGETHER"
}

// NewOverrideFor 返回某分片的初始配置（等同游戏默认）。
func NewOverrideFor(shard string) *Override {
	p := DefaultPreset(shard)
	return &Override{
		Enabled:        true,
		WorldGenPreset: p,
		SettingsPreset: p,
		Overrides:      map[string]string{},
	}
}

var (
	enabledRe        = regexp.MustCompile(`\boverride_enabled\s*=\s*(true|false)`)
	worldgenPresetRe = regexp.MustCompile(`\bworldgen_preset\s*=\s*"([^"]*)"`)
	settingsPresetRe = regexp.MustCompile(`\bsettings_preset\s*=\s*"([^"]*)"`)
	legacyPresetRe   = regexp.MustCompile(`\bpreset\s*=\s*"([^"]*)"`)
	overridesBlockRe = regexp.MustCompile(`\boverrides\s*=\s*\{`)
	overrideItemRe   = regexp.MustCompile(`^(?:\["([^"]+)"\]|([A-Za-z_]\w*))\s*=\s*"([^"]*)"`)
)

// ParseOverride 解析 worldgenoverride.lua。
//
// 宽松解析：认不出的行一律跳过而不是报错。这个文件是给用户手工编辑的，
// 可能带注释、Mod 字段，甚至来自游戏客户端的导出。
func ParseOverride(content string) *Override {
	o := &Override{Overrides: map[string]string{}}

	// 去掉 BOM 与 Klei 导出头
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.TrimSpace(content)

	if m := enabledRe.FindStringSubmatch(content); m != nil {
		o.Enabled = m[1] == "true"
	}
	if m := worldgenPresetRe.FindStringSubmatch(content); m != nil {
		o.WorldGenPreset = m[1]
	}
	if m := settingsPresetRe.FindStringSubmatch(content); m != nil {
		o.SettingsPreset = m[1]
	}
	// 遗留的单键 preset：同时充当上面两个
	if o.WorldGenPreset == "" || o.SettingsPreset == "" {
		if m := legacyPresetRe.FindStringSubmatch(content); m != nil {
			if o.WorldGenPreset == "" {
				o.WorldGenPreset = m[1]
			}
			if o.SettingsPreset == "" {
				o.SettingsPreset = m[1]
			}
		}
	}

	// overrides 块：块结构必须用括号配对，正则会被嵌套大括号截断
	if loc := overridesBlockRe.FindStringIndex(content); loc != nil {
		if body, ok := extractBraces(content, loc[1]-1); ok {
			for _, raw := range strings.Split(body, "\n") {
				line := strings.TrimSpace(stripLuaComment(raw))
				if line == "" {
					continue
				}
				m := overrideItemRe.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				name := m[1]
				if name == "" {
					name = m[2]
				}
				if name != "" {
					o.Overrides[name] = m[3]
				}
			}
		}
	}
	return o
}

// String 渲染成 worldgenoverride.lua。
//
// 只写用户改动过的项——官方文档明确说等于默认值的项可以直接省略，
// 而且省略能避免游戏以后新增选项时被面板里的旧默认值覆盖。
// 分段注释的写法对齐官方生成工具（scripts/tools/generate_worldgenoverride.lua）。
func (o *Override) String() string {
	var sb strings.Builder
	sb.WriteString("return {\n")
	sb.WriteString("\toverride_enabled = " + luaBool(o.Enabled) + ",\n")
	if o.WorldGenPreset != "" {
		sb.WriteString(fmt.Sprintf("\tworldgen_preset = %q,\n", o.WorldGenPreset))
	}
	if o.SettingsPreset != "" {
		sb.WriteString(fmt.Sprintf("\tsettings_preset = %q,\n", o.SettingsPreset))
	}

	sb.WriteString("\toverrides = {\n")

	names := make([]string, 0, len(o.Overrides))
	for n := range o.Overrides {
		names = append(names, n)
	}
	sort.Strings(names)

	// 等于默认值的项一律省略：官方文档说可以直接省略，而且省略能避免
	// 游戏以后新增选项时被这里的旧默认值覆盖。
	// 前端已经过滤过一遍，这里再兜一层——文件也可能被手工编辑过。
	handled := map[string]bool{}
	section := func(category, title string) {
		var rows []string
		for _, n := range names {
			opt, ok := Lookup(n)
			if !ok || opt.Category != category {
				continue
			}
			handled[n] = true // 无论是否写出，都算已归位
			if opt.Default == o.Overrides[n] {
				continue
			}
			rows = append(rows, n)
		}
		if len(rows) == 0 {
			return
		}
		sb.WriteString("\t\t-- " + title + "\n")
		for _, n := range rows {
			sb.WriteString(fmt.Sprintf("\t\t%s = %q,\n", n, o.Overrides[n]))
		}
		sb.WriteString("\n")
	}
	section(CategoryWorldGen, "WORLDGEN（决定地图生成）")
	section(CategorySettings, "WORLDSETTINGS（世界规则）")

	// 清单里没有的项（Mod 自定义设置等）原样保留，不能丢
	var others []string
	for _, n := range names {
		if !handled[n] {
			others = append(others, n)
		}
	}
	if len(others) > 0 {
		sb.WriteString("\t\t-- 其它（面板清单未收录，原样保留）\n")
		for _, n := range others {
			sb.WriteString(fmt.Sprintf("\t\t%s = %q,\n", n, o.Overrides[n]))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\t},\n}\n")
	return sb.String()
}

// FilePath 返回某分片的配置文件路径：<clusterDir>/<shard>/worldgenoverride.lua
func FilePath(clusterDir, shard string) string {
	return filepath.Join(clusterDir, shard, OverrideFileName)
}

// ReadFile 读取某分片的配置。
// 文件不存在时返回该分片的默认配置，exists 为 false。
func ReadFile(clusterDir, shard string) (*Override, bool, error) {
	path := FilePath(clusterDir, shard)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewOverrideFor(shard), false, nil
		}
		return nil, false, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	return ParseOverride(string(b)), true, nil
}

// WriteFile 写入某分片的配置（原子写，避免写一半被服务器读到）。
func WriteFile(clusterDir, shard string, o *Override) error {
	path := FilePath(clusterDir, shard)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建分片目录失败: %w", err)
	}
	if err := config.WriteFileAtomic(path, []byte(o.String())); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	return nil
}

// Validate 校验整份配置：选项必须存在，取值必须在允许范围内。
func (o *Override) Validate() error {
	for name, value := range o.Overrides {
		if err := ValidateValue(name, value); err != nil {
			return err
		}
	}
	for _, p := range []struct {
		name  string
		value string
	}{
		{"worldgen_preset", o.WorldGenPreset},
		{"settings_preset", o.SettingsPreset},
	} {
		if p.value != "" && !isSafeValue(p.value) {
			return fmt.Errorf("%s 含非法字符: %q", p.name, p.value)
		}
	}
	return nil
}

/* ---------- 小工具 ---------- */

func luaBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// stripLuaComment 去掉行内注释（保留字符串字面量）。
func stripLuaComment(line string) string {
	i := strings.Index(line, "--")
	if i < 0 {
		return line
	}
	inStr := false
	for j := 0; j < i; j++ {
		if line[j] == '"' && (j == 0 || line[j-1] != '\\') {
			inStr = !inStr
		}
	}
	if inStr {
		return line
	}
	return line[:i]
}

// extractBraces 从 src[open] 处的 '{' 开始，返回配对括号内的内容。
func extractBraces(src string, open int) (string, bool) {
	if open < 0 || open >= len(src) || src[open] != '{' {
		return "", false
	}
	depth := 0
	inStr := false
	for i := open; i < len(src); i++ {
		c := src[i]
		if c == '"' && (i == 0 || src[i-1] != '\\') {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open+1 : i], true
			}
		}
	}
	return "", false
}
