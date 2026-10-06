// Command extract-worldsettings 从 DST 专用服务器的安装文件里提取「世界设置」选项清单。
//
// 为什么从游戏文件提取而不是手工维护一份：
// 选项名、默认值、可选值全部由 Klei 在 scripts/map/customize.lua 里定义，
// 手工抄写既容易出错，也会随游戏更新而过时。游戏更新后重跑本命令即可刷新。
//
// 用法：
//
//	go run ./tools/extract-worldsettings                 # 自动探测 Steam 安装
//	go run ./tools/extract-worldsettings -server "F:\steam\..."   # 显式指定
//	go run ./tools/extract-worldsettings -out internal/worldsettings/options.json
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"dst-windows/internal/steamcmd"
)

const (
	scriptsZipPath = "data/databundles/scripts.zip"
	customizePath  = "scripts/map/customize.lua"
	forestPath     = "scripts/map/levels/forest.lua"
	cavesPath      = "scripts/map/levels/caves.lua"
	poPath         = "scripts/languages/chinese_s.po"
)

// 数据模型（同时是输出 JSON 的结构）

type optionValue struct {
	Data  string `json:"data"`
	Label string `json:"label,omitempty"`
}

type option struct {
	Name     string        `json:"name"`
	Label    string        `json:"label,omitempty"`
	Default  string        `json:"default"`
	Order    int           `json:"order,omitempty"`
	Worlds   []string      `json:"worlds,omitempty"` // forest / cave
	Values   []optionValue `json:"values,omitempty"`
	Dynamic  bool          `json:"dynamic,omitempty"` // 取值由函数运行时生成（如 task_set）
	Category string        `json:"category"`          // worldgen / settings
}

type group struct {
	ID       string   `json:"id"`
	Label    string   `json:"label,omitempty"`
	Category string   `json:"category"`
	Order    int      `json:"order,omitempty"`
	Options  []option `json:"options"`

	// 分组自身的 desc：选项没写 desc 时会继承它
	// （customize.lua 里的 `option.desc or option.group.desc`）
	descValues []optionValue
}

type preset struct {
	Data  string `json:"data"`
	Label string `json:"label,omitempty"`
}

type dataset struct {
	Version     int                 `json:"version"`
	GameVersion string              `json:"gameVersion"`
	GeneratedAt string              `json:"generatedAt"`
	Source      string              `json:"source"`
	Presets     map[string][]preset `json:"presets"` // forest / cave
	Groups      []group             `json:"groups"`
}

func main() {
	serverDir := flag.String("server", "", "专用服务器安装根目录（含 data/ 的那一层）。留空则自动探测 Steam 库")
	out := flag.String("out", filepath.Join("internal", "worldsettings", "options.json"), "输出文件")
	flag.Parse()

	root, err := resolveRoot(*serverDir)
	if err != nil {
		fatal(err)
	}
	fmt.Println("服务器目录:", root)

	zipData, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(scriptsZipPath)))
	if err != nil {
		fatal(fmt.Errorf("读取 %s 失败: %w（确认这是专用服务器安装目录）", scriptsZipPath, err))
	}
	files, err := readZipEntries(zipData, customizePath, forestPath, cavesPath, poPath)
	if err != nil {
		fatal(err)
	}
	customize := files[customizePath]
	po := files[poPath]
	if customize == "" {
		fatal(fmt.Errorf("scripts.zip 里没有 %s，游戏目录结构可能变了", customizePath))
	}
	fmt.Printf("customize.lua %d 字节；语言包 %d 字节\n", len(customize), len(po))

	// 标签表：msgid -> msgstr
	labels := parsePO(po, []string{
		"STRINGS.UI.CUSTOMIZATIONSCREEN.",
		"STRINGS.UI.SANDBOXMENU.",
	})
	fmt.Println("中文标签条目:", len(labels))

	descTables := parseDescriptionTables(customize)
	fmt.Println("可选值表:", len(descTables))

	groups := parseGroups(customize, descTables, labels)
	sortGroups(groups)

	presets := parsePresets(files[forestPath]+"\n"+files[cavesPath], labels)

	total := 0
	for _, g := range groups {
		total += len(g.Options)
	}
	fmt.Printf("提取到 %d 个分组、%d 个选项\n", len(groups), total)
	for k, v := range presets {
		fmt.Printf("  预设 %-16s %d 个\n", k, len(v))
	}

	data := dataset{
		Version:     1,
		GameVersion: readVersion(root),
		GeneratedAt: time.Now().Format(time.RFC3339),
		Source:      "scripts/map/customize.lua",
		Presets:     presets,
		Groups:      groups,
	}

	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, append(buf, '\n'), 0644); err != nil {
		fatal(err)
	}
	fmt.Println("已写入:", *out)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}

// resolveRoot 定位专用服务器安装根目录。
func resolveRoot(explicit string) (string, error) {
	if explicit != "" {
		if _, err := steamcmd.PickLayout(explicit, "auto", "参数指定"); err != nil {
			return "", err
		}
		return explicit, nil
	}
	for _, d := range steamcmd.DetectInstalled() {
		if _, err := steamcmd.PickLayout(d.Root, "auto", d.Source); err == nil {
			return d.Root, nil
		}
	}
	return "", fmt.Errorf("未找到专用服务器安装，请用 -server 指定")
}

func readVersion(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "version.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var (
	groupRe       = regexp.MustCompile(`^\s*\["([\w_]+)"\]\s*=\s*\{\s*$`)
	containerRe   = regexp.MustCompile(`^local (WORLDGEN|WORLDSETTINGS)_(GROUP|MISC)\s*=\s*\{`)
	itemsRe       = regexp.MustCompile(`\bitems\s*=\s*\{`)
	fieldValueRe  = regexp.MustCompile(`\bvalue\s*=\s*"([^"]*)"`)
	fieldDescRe   = regexp.MustCompile(`\bdesc\s*=\s*([\w_.]+)`)
	fieldOrderRe  = regexp.MustCompile(`\border\s*=\s*(\d+)`)
	fieldWorldRe  = regexp.MustCompile(`\bworld\s*=\s*\{([^}]*)\}`)
	textFieldRe   = regexp.MustCompile(`\btext\s*=\s*([\w_.]+)`)
	descEntryRe   = regexp.MustCompile(`\{\s*text\s*=\s*([\w_.]+)\s*,\s*data\s*=\s*"([^"]*)"\s*\}`)
	descTableRe   = regexp.MustCompile(`(?m)^\s*(?:local\s+)?(\w+_descriptions)\s*=\s*\{`)
	presetDataRe  = regexp.MustCompile(`LEVELCATEGORY\.(WORLDGEN|SETTINGS),\s*LEVELTYPE\.(\w+)\)`)
	quotedStringR = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)

// parseDescriptionTables 提取所有 * _descriptions 表：表名 -> 取值列表。
//
// 同一个表会按平台定义多次，而且**两个方向的写法都有**：
//
//	if IsPS4() then ... else ... end          -- 主机在前、PC 在后
//	if IsNotConsole() then ... else ... end   -- PC 在前、主机在后
//
// 所以既不能取第一次也不能取最后一次，必须看条件表达式判断哪个分支是 PC 的，
// 否则会拿到主机版被裁剪过的取值列表（例如世界大小只剩两档）。
func parseDescriptionTables(src string) map[string][]optionValue {
	type cand struct {
		priority int
		values   []optionValue
	}
	best := map[string]cand{}

	lines := strings.Split(src, "\n")

	// 行首在 src 中的偏移，用于把行内的 '{' 映射回全文位置
	offsets := make([]int, len(lines))
	pos := 0
	for i, l := range lines {
		offsets[i] = pos
		pos += len(l) + 1
	}

	for i, raw := range lines {
		if descTableRe.FindStringSubmatch(stripComment(raw)) == nil {
			continue
		}
		open := strings.Index(raw, "{")
		if open < 0 {
			continue
		}
		body, ok := extractBraces(src, offsets[i]+open)
		if !ok {
			continue
		}

		name := descTableRe.FindStringSubmatch(stripComment(raw))[1]
		cond, inElse := branchOf(lines, i)

		var vals []optionValue
		// 逐行处理并去掉注释：被注释掉的取值在游戏里并不可选
		for _, bl := range strings.Split(body, "\n") {
			bl = stripComment(bl)
			if m := descEntryRe.FindStringSubmatch(bl); m != nil {
				vals = append(vals, optionValue{Data: m[2], Label: m[1]})
			}
		}
		if len(vals) == 0 {
			continue
		}

		p := pcPriority(cond, inElse)
		if cur, seen := best[name]; !seen || p > cur.priority {
			best[name] = cand{priority: p, values: vals}
		}
	}

	out := make(map[string][]optionValue, len(best))
	for k, v := range best {
		out[k] = v.values
	}
	return out
}

// branchOf 反向查找某一行所处的条件分支。
//
// 只需处理 customize.lua 里的简单单层 if/else，遇到 end 就停止回溯。
func branchOf(lines []string, idx int) (cond string, inElse bool) {
	for i := idx - 1; i >= 0 && i > idx-40; i-- {
		l := strings.TrimSpace(stripComment(lines[i]))
		switch {
		case l == "":
		case l == "else":
			// 继续往前找到对应的 if
			for j := i - 1; j >= 0 && j > i-40; j-- {
				ll := strings.TrimSpace(stripComment(lines[j]))
				if strings.HasPrefix(ll, "if ") && strings.HasSuffix(ll, "then") {
					return ll, true
				}
				if ll == "end" || ll == "else" {
					break
				}
			}
			return "", true
		case l == "end" || strings.HasPrefix(l, "end "):
			return "", false
		case strings.HasPrefix(l, "if ") && strings.HasSuffix(l, "then"):
			return l, false
		}
	}
	return "", false
}

// pcPriority 给一个分支打「PC 适用度」分，分越高越优先。
//
//	2 = PC 分支
//	1 = 无条件定义 / 认不出的条件
//	0 = 主机分支
func pcPriority(cond string, inElse bool) int {
	c := strings.ToLower(cond)
	switch {
	case strings.Contains(c, "isnotconsole"):
		// IsNotConsole() 在 PC 上为真 ⇒ then 是 PC 分支
		if inElse {
			return 0
		}
		return 2
	case strings.Contains(c, "isconsole"), strings.Contains(c, "isps4"),
		strings.Contains(c, "isxbox"), strings.Contains(c, "isswitch"):
		// 主机判定 ⇒ else 才是 PC 分支
		if inElse {
			return 2
		}
		return 0
	default:
		return 1
	}
}

// parseGroups 遍历两个顶层容器，按括号深度区分「分组」与「选项」。
func parseGroups(src string, descs map[string][]optionValue, labels map[string]string) []group {
	var out []group

	lines := strings.Split(src, "\n")
	var (
		category   string // worldgen / settings
		cur        *group
		inItems    bool
		depth      int
		itemsDepth int // items 表内部的行深度，选项就在这一层
	)

	flush := func() {
		if cur != nil && len(cur.Options) > 0 {
			out = append(out, *cur)
		}
		cur = nil
		inItems = false
	}

	for _, raw := range lines {
		// line 保留字符串，用于提取名字与取值；clean 抹掉字符串，只用于括号计数
		line := strings.TrimSpace(stripComment(raw))
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		clean := stripStrings(line)

		if m := containerRe.FindStringSubmatch(line); m != nil {
			flush()
			if m[1] == "WORLDGEN" {
				category = "worldgen"
			} else {
				category = "settings"
			}
			depth = 1
			continue
		}
		if category == "" {
			continue
		}

		// 进入 items 表：选项位于它内部的那一层
		if !inItems && itemsRe.MatchString(line) {
			depth += braceDelta(clean)
			inItems = true
			itemsDepth = depth
			continue
		}

		if inItems {
			if depth == itemsDepth {
				if opt, ok := parseOptionLine(line, category, descs, labels); ok && cur != nil {
					// 选项自己没写 desc 时继承分组的（等价于官方代码里的
					// `option.desc or option.group.desc`），否则大部分开关类
					// 选项会拿不到可选值列表
					if len(opt.Values) == 0 && !opt.Dynamic {
						opt.Values = cloneValues(cur.descValues)
					}
					cur.Options = append(cur.Options, opt)
				}
			}
		} else {
			// 分组定义行在 depth 1；它的属性行（order / text / desc）在 depth 2
			if m := groupRe.FindStringSubmatch(line); m != nil && depth == 1 {
				flush()
				cur = &group{ID: m[1], Category: category}
				if cur.Label = labels["STRINGS.UI.SANDBOXMENU."+strings.ToUpper(m[1])]; cur.Label == "" {
					cur.Label = humanize(m[1])
				}
			} else if cur != nil {
				if m := fieldOrderRe.FindStringSubmatch(line); m != nil {
					cur.Order, _ = strconv.Atoi(m[1])
				} else if m := textFieldRe.FindStringSubmatch(line); m != nil {
					if l := labels[m[1]]; l != "" {
						cur.Label = l
					}
				} else if m := fieldDescRe.FindStringSubmatch(line); m != nil {
					if vals, ok := descs[m[1]]; ok {
						cur.descValues = vals
					}
				}
			}
		}

		depth += braceDelta(clean)
		// items 表闭合后要复位，否则后续的分组定义会被当成选项吞掉，
		// 表现是所有分组被合并成一个
		if inItems && depth < itemsDepth {
			inItems = false
		}
	}
	flush()

	// 给每个分组内的选项定序并填标签
	for gi := range out {
		for oi := range out[gi].Options {
			o := &out[gi].Options[oi]
			// customize.lua 里没写 world 字段的选项两个分片都适用
			if len(o.Worlds) == 0 {
				o.Worlds = []string{"forest", "cave"}
			}
			if o.Label == "" {
				o.Label = labels["STRINGS.UI.CUSTOMIZATIONSCREEN."+strings.ToUpper(o.Name)]
			}
			if o.Label == "" {
				o.Label = humanize(o.Name)
			}
			for vi := range o.Values {
				if o.Values[vi].Label == "" {
					continue
				}
				if l := labels[o.Values[vi].Label]; l != "" {
					o.Values[vi].Label = l
				} else {
					o.Values[vi].Label = humanize(strings.TrimPrefix(o.Values[vi].Label, "STRINGS.UI.SANDBOXMENU."))
				}
			}
		}
		sort.SliceStable(out[gi].Options, func(a, b int) bool {
			if out[gi].Options[a].Order != out[gi].Options[b].Order {
				return out[gi].Options[a].Order < out[gi].Options[b].Order
			}
			return out[gi].Options[a].Name < out[gi].Options[b].Name
		})
	}
	return out
}

// parseOptionLine 解析单行形式的选项定义。
func parseOptionLine(line string, category string, descs map[string][]optionValue, labels map[string]string) (option, bool) {
	m := groupRe.FindStringSubmatch(line)
	if m == nil {
		// 单行定义形如： ["grass"] = {value = "default", ...},
		m = regexp.MustCompile(`^\s*\["([\w_]+)"\]\s*=\s*\{(.+)\}\s*,?\s*$`).FindStringSubmatch(line)
	}
	if m == nil {
		return option{}, false
	}

	opt := option{Name: m[1], Category: category}

	if v := fieldValueRe.FindStringSubmatch(line); v != nil {
		opt.Default = v[1]
	}
	if o := fieldOrderRe.FindStringSubmatch(line); o != nil {
		opt.Order, _ = strconv.Atoi(o[1])
	}
	if w := fieldWorldRe.FindStringSubmatch(line); w != nil {
		for _, p := range strings.Split(w[1], ",") {
			p = strings.Trim(strings.TrimSpace(p), `"`)
			if p != "" {
				opt.Worlds = append(opt.Worlds, p)
			}
		}
	}
	if d := fieldDescRe.FindStringSubmatch(line); d != nil {
		if vals, ok := descs[d[1]]; ok {
			opt.Values = cloneValues(vals)
		} else {
			// desc 指向函数（如 tasksets.GetGenTaskLists）——取值运行时才有
			opt.Dynamic = true
		}
	}
	return opt, true
}

// parsePresets 从 levels 文件里提取可用的预设。
//
//	AddLevel(...)         -> settings_preset（世界规则）
//	AddWorldGenLevel(...) -> worldgen_preset（地图生成）
//
// 两者的 level 定义里都有 location 字段，用它区分 forest / cave。
// 返回的 key 形如 forest_settings / cave_worldgen。
func parsePresets(src string, labels map[string]string) map[string][]preset {
	type info struct{ id, location string }

	// 变量名 -> 定义体里的 id 与 location（AddLevel 常以变量传入）
	vars := map[string]info{}
	for _, m := range regexp.MustCompile(`(?m)^local\s+(\w+)\s*=\s*\{`).FindAllStringSubmatchIndex(src, -1) {
		body, ok := extractBraces(src, m[1]-1)
		if !ok {
			continue
		}
		id := firstSub(body, `\bid\s*=\s*"([A-Z_0-9]+)"`)
		if id == "" {
			continue
		}
		vars[src[m[2]:m[3]]] = info{id: id, location: firstSub(body, `\blocation\s*=\s*"(\w+)"`)}
	}

	// 另一批预设是派生出来的：local relaxed = deepcopy(survival_together) + relaxed.id = "RELAXED"
	// 不处理这种写法会漏掉一半以上的预设。
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		m := regexp.MustCompile(`^local\s+(\w+)\s*=\s*deepcopy\((\w+)\)`).FindStringSubmatch(l)
		if m == nil {
			continue
		}
		name, base := m[1], m[2]
		// 只在紧随其后的若干行里找它被改写成什么
		end := i + 12
		if end > len(lines) {
			end = len(lines)
		}
		block := strings.Join(lines[i:end], "\n")
		id := firstSub(block, `\b`+regexp.QuoteMeta(name)+`\.id\s*=\s*"([A-Z_0-9]+)"`)
		if id == "" {
			continue
		}
		loc := firstSub(block, `\b`+regexp.QuoteMeta(name)+`\.location\s*=\s*"(\w+)"`)
		if loc == "" {
			loc = vars[base].location
		}
		vars[name] = info{id: id, location: loc}
	}

	out := map[string][]preset{}
	seen := map[string]bool{}
	callRe := regexp.MustCompile(`(AddWorldGenLevel|AddLevel)\(LEVELTYPE\.\w+\s*,\s*`)

	for _, m := range callRe.FindAllStringSubmatchIndex(src, -1) {
		kind := src[m[2]:m[3]]
		rest := src[m[1]:]

		var id, loc string
		if i := strings.Index(rest, "{"); i >= 0 && strings.TrimSpace(rest[:i]) == "" {
			body, ok := extractBraces(src, m[1]+i)
			if !ok {
				continue
			}
			id = firstSub(body, `\bid\s*=\s*"([A-Z_0-9]+)"`)
			loc = firstSub(body, `\blocation\s*=\s*"(\w+)"`)
		} else if nm := regexp.MustCompile(`^\s*(\w+)`).FindStringSubmatch(rest); nm != nil {
			v, ok := vars[nm[1]]
			if !ok {
				continue
			}
			id, loc = v.id, v.location
		}

		if id == "" || (loc != "forest" && loc != "cave") {
			continue
		}
		category := "settings"
		if kind == "AddWorldGenLevel" {
			category = "worldgen"
		}
		key := loc + "_" + category
		if seen[key+"/"+id] {
			continue
		}
		seen[key+"/"+id] = true
		out[key] = append(out[key], preset{
			Data:  id,
			Label: labels["STRINGS.UI.CUSTOMIZATIONSCREEN.PRESETLEVELS."+id],
		})
	}

	for k := range out {
		sort.Slice(out[k], func(a, b int) bool { return out[k][a].Data < out[k][b].Data })
	}
	return out
}

// firstSub 返回第一个捕获组的内容，没有匹配则返回空串。
func firstSub(src, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(src)
	if m == nil || len(m) < 2 {
		return ""
	}
	return m[1]
}

/* ---------- 文本处理 ---------- */

// parsePO 解析 gettext 的 .po 文件，只保留指定前缀的条目（msgctxt -> msgstr）。
//
// 注意 DST 的语言包把「字符串 key」放在 msgctxt 里，msgid 是英文原文：
//
//	#. STRINGS.UI.CUSTOMIZATIONSCREEN.WORLD_SIZE
//	msgctxt "STRINGS.UI.CUSTOMIZATIONSCREEN.WORLD_SIZE"
//	msgid "World size"
//	msgstr "世界大小"
//
// 用 msgid 当 key 会一条都取不到。
func parsePO(src string, prefixes []string) map[string]string {
	out := map[string]string{}
	var key, val, mode string

	flush := func() {
		if key != "" && val != "" {
			for _, p := range prefixes {
				if strings.HasPrefix(key, p) {
					out[key] = val
					break
				}
			}
		}
		key, val, mode = "", "", ""
	}

	for _, l := range strings.Split(src, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			flush()
		case strings.HasPrefix(t, "#"):
			// 注释行（其中 #. 那行也带着 key，但 msgctxt 更可靠）
		case strings.HasPrefix(t, "msgctxt "):
			if key != "" {
				flush()
			}
			key = poUnquote(strings.TrimPrefix(t, "msgctxt "))
			mode = "ctxt"
		case strings.HasPrefix(t, "msgid "):
			mode = "id"
		case strings.HasPrefix(t, "msgstr "):
			val = poUnquote(strings.TrimPrefix(t, "msgstr "))
			mode = "str"
		case strings.HasPrefix(t, `"`):
			switch mode {
			case "ctxt":
				key += poUnquote(t)
			case "str":
				val += poUnquote(t)
			}
		}
	}
	flush()
	return out
}

// poUnquote 去掉 gettext 字符串的引号并还原转义。
func poUnquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	s = strings.ReplaceAll(s, `\"`, "\x00")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return strings.ReplaceAll(s, "\x00", `"`)
}

// readZipEntries 从 zip 数据里取出若干条目（路径 -> 文本内容）。
func readZipEntries(data []byte, names ...string) (map[string]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("解析 scripts.zip 失败: %w", err)
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	out := make(map[string]string, len(names))
	for _, f := range zr.File {
		if !want[f.Name] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = string(b)
	}
	return out, nil
}

// stripComment 去掉行内注释，但**保留字符串字面量**——
// 提取名字与取值时需要它们（`["grass"] = {value = "default"}`）。
func stripComment(line string) string {
	i := strings.Index(line, "--")
	if i < 0 {
		return line
	}
	// 确认这个 -- 不在字符串里
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

// stripStrings 把字符串字面量替换成空串，只用于按括号计数。
// 注意：不要在提取字段时用它，否则 `["grass"]` 会变成 `[""]`。
func stripStrings(line string) string {
	return quotedStringR.ReplaceAllString(line, `""`)
}

func braceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
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

// humanize 把 snake_case 变成可读的英文短语（找不到中文时的兜底）。
func humanize(s string) string {
	s = strings.TrimPrefix(s, "STRINGS.UI.SANDBOXMENU.")
	s = strings.TrimPrefix(s, "STRINGS.UI.CUSTOMIZATIONSCREEN.")

	// 已经含非 ASCII（译好了或本来就是中文）就原样返回。
	// 否则下面的 p[:1] 会把多字节字符切成非法 UTF-8。
	for _, r := range s {
		if r > unicode.MaxASCII {
			return s
		}
	}

	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '.' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}

// cloneValues 复制一份取值列表。
//
// 必须复制：多个选项会共用同一个 desc 表，若就地改写标签会互相污染，
// 表现为标签被反复拼接、越来越长并最终变成乱码。
func cloneValues(v []optionValue) []optionValue {
	if len(v) == 0 {
		return nil
	}
	out := make([]optionValue, len(v))
	copy(out, v)
	return out
}

func sortGroups(gs []group) {
	sort.SliceStable(gs, func(a, b int) bool {
		if gs[a].Category != gs[b].Category {
			return gs[a].Category < gs[b].Category
		}
		if gs[a].Order != gs[b].Order {
			return gs[a].Order < gs[b].Order
		}
		return gs[a].ID < gs[b].ID
	})
}
