package modmgr

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseOverridesRoundTrip 覆盖曾经的回归缺陷：
// 用非贪婪正则提取模组块时，configuration_options 的右大括号会提前截断模组体，
// 导致参数丢失并解析出一个名为 "configuration_options" 的伪配置项。
func TestParseOverridesRoundTrip(t *testing.T) {
	src := `return {
    ["workshop-1234567890"] = {
        enabled = true,
        priority = 3,
        configuration_options = {
            difficulty = 3,
            enable_boss = false,
            mode = "Easy"
        },
    },
    ["workshop-9876543210"] = {
        enabled = false,
    },
}`

	o := ParseOverrides(src)
	if len(o.Order) != 2 {
		t.Fatalf("模组数量错误: %v", o.Order)
	}
	if !o.Enabled["workshop-1234567890"] {
		t.Error("第一个模组应为启用")
	}
	if o.Enabled["workshop-9876543210"] {
		t.Error("第二个模组应为禁用")
	}
	if o.Priority["workshop-1234567890"] != 3 {
		t.Errorf("priority 解析错误: %v", o.Priority)
	}

	cfg := o.Config["workshop-1234567890"]
	if cfg == nil {
		t.Fatal("configuration_options 未解析")
	}
	if _, bogus := cfg["configuration_options"]; bogus {
		t.Errorf("解析出了伪配置项: %v", cfg)
	}
	if cfg["difficulty"] != float64(3) {
		t.Errorf("difficulty = %v", cfg["difficulty"])
	}
	if cfg["enable_boss"] != false {
		t.Errorf("enable_boss = %v", cfg["enable_boss"])
	}
	if cfg["mode"] != "Easy" {
		t.Errorf("mode = %v", cfg["mode"])
	}

	// 序列化后再解析一次，结果必须一致（写读幂等）
	o2 := ParseOverrides(o.String())
	if !o2.Enabled["workshop-1234567890"] || o2.Enabled["workshop-9876543210"] {
		t.Error("往返后启用状态不一致")
	}
	cfg2 := o2.Config["workshop-1234567890"]
	if cfg2 == nil {
		t.Fatal("往返后配置丢失")
	}
	if _, bogus := cfg2["configuration_options"]; bogus {
		t.Errorf("往返后出现伪配置项: %v", cfg2)
	}
	if cfg2["difficulty"] != float64(3) || cfg2["enable_boss"] != false || cfg2["mode"] != "Easy" {
		t.Errorf("往返后配置不一致: %v", cfg2)
	}
	if o2.Priority["workshop-1234567890"] != 3 {
		t.Errorf("往返后 priority 丢失: %v", o2.Priority)
	}
}

// TestParseOverridesIgnoresNonWorkshopKeys 确认本地模组（非 workshop- 前缀）也能识别。
func TestParseOverridesIgnoresNonWorkshopKeys(t *testing.T) {
	o := ParseOverrides(`return {
    ["my_local_mod"] = {
        enabled = true,
        configuration_options = { level = 5 },
    },
}`)
	if !o.Enabled["my_local_mod"] {
		t.Fatalf("本地模组未解析: %+v", o.Enabled)
	}
	if o.Config["my_local_mod"]["level"] != float64(5) {
		t.Errorf("本地模组配置错误: %v", o.Config["my_local_mod"])
	}
}

// TestParseConfigOptionNestedOptions 覆盖 description 被嵌套 options 污染的问题：
// options 里每个候选项都有 description，不能被当成配置项自身的描述。
func TestParseConfigOptionNestedOptions(t *testing.T) {
	item := `{
        name = "difficulty",
        label = "难度",
        options = {
            {description = "简单", data = 1},
            {description = "普通", data = 2},
            {description = "困难", data = 3},
        },
        default = 2,
    }`

	opt := parseConfigOption(item)
	if opt.Name != "difficulty" {
		t.Errorf("name = %q", opt.Name)
	}
	if opt.Label != "难度" {
		t.Errorf("label = %q", opt.Label)
	}
	if opt.Description != "" {
		t.Errorf("description 被 options 污染: %q", opt.Description)
	}
	if opt.Default != "2" {
		t.Errorf("default = %q（数字默认值应能解析）", opt.Default)
	}
	if len(opt.Options) != 3 || opt.Options[0] != "1" || opt.Options[2] != "3" {
		t.Errorf("options = %v（应取 data 值）", opt.Options)
	}
}

// TestParseConfigOptionScalarTypes 覆盖数字/布尔/上下限的解析。
func TestParseConfigOptionScalarTypes(t *testing.T) {
	item := `{
        name = "spawn_rate",
        label = "刷新倍率",
        type = "number",
        default = 1.5,
        min = 0,
        max = 10,
        description = "世界刷新倍率",
    }`
	opt := parseConfigOption(item)
	if opt.Default != "1.5" || opt.Min != "0" || opt.Max != "10" {
		t.Errorf("default/min/max = %q/%q/%q", opt.Default, opt.Min, opt.Max)
	}
	if opt.Description != "世界刷新倍率" {
		t.Errorf("description = %q", opt.Description)
	}
	if opt.Type != "number" {
		t.Errorf("type = %q", opt.Type)
	}
}

// TestStringEscapesQuotes 确认含引号/反斜杠的取值不会破坏 Lua 语法。
func TestStringEscapesQuotes(t *testing.T) {
	o := NewOverrides()
	o.Order = []string{"workshop-1"}
	o.Enabled["workshop-1"] = true
	o.Config["workshop-1"] = map[string]interface{}{
		"title": `say "hi" \ ok`,
	}
	back := ParseOverrides(o.String())
	if back.Config["workshop-1"]["title"] != `say "hi" \ ok` {
		t.Errorf("转义往返失败: %v", back.Config["workshop-1"])
	}
}

// TestExtractModInfoName 覆盖 modinfo.lua 的三种名字写法，
// 以及曾经修掉的 bug：全局搜第一个 name = "..." 会抓到
// configuration_options 里配置项的名字。
func TestExtractModInfoName(t *testing.T) {
	cases := []struct {
		desc string
		text string
		want string
	}{
		{
			desc: "字面量",
			text: "name = \"Simple Health Bar DST\"\nauthor = \"x\"",
			want: "Simple Health Bar DST",
		},
		{
			desc: "return 表内、带逗号",
			text: "return {\n    name = \"Show Me\",\n    configuration_options = {\n        { name = \"lang\", label = \"语言\" },\n    },\n}",
			want: "Show Me",
		},
		{
			desc: "双语表达式，应取 or 兜底的中文",
			text: "local L = locale ~= \"zh\"\nname = L and \"Show Me (New)\" or \"Show Me (中文)\"\nconfiguration_options = {\n    { name = \"language_switch\", label = \"语言切换\" },\n}",
			want: "Show Me (中文)",
		},
		{
			desc: "条件变量反着写（isCh and 中文 or 英文）",
			text: "local isCh = locale == \"zh\"\nname = isCh and \"能力勋章\" or \"Functional Medal\"",
			want: "能力勋章",
		},
		{
			desc: "整份 modinfo 压成一行，只取 name 的第一个字符串",
			text: "name = \" Simple Health Bar DST\" version = \"2.16\" description = \"Version:DST\"..version..\"\\nHealth bar\" author = \"DYC\"",
			want: "Simple Health Bar DST",
		},
		{
			desc: "配置项写在 name 之前，也不能抓到",
			text: "configuration_options = {\n    { name = \"Title\", label = \"标题\" },\n}\nname = \"真正的名字\"",
			want: "真正的名字",
		},
		{
			desc: "整行注释里的写法示例要跳过",
			text: "-- name = ChooseTranslationTable({ zh = \"注释里的\" })\nname = L and \"A\" or \"棱镜\"",
			want: "棱镜",
		},
		{
			desc: "行尾注释不影响取值",
			text: "name = \"X\" -- 英文名",
			want: "X",
		},
		{
			desc: "配置项的 name 是嵌套表也不能误抓",
			text: "configuration_options = {\n    {\n        name = \"lang\",\n        options = {\n            { description = \"中文\", data = 1 },\n        },\n    },\n}\nname = \"神话书说\"",
			want: "神话书说",
		},
		{
			desc: "没有 name 字段时返回空串（由调用方退回 ID）",
			text: "author = \"x\"\nversion = \"1.0\"",
			want: "",
		},
	}

	for _, c := range cases {
		if got := extractModInfoName(c.text); got != c.want {
			t.Errorf("[%s] extractModInfoName = %q, 期望 %q", c.desc, got, c.want)
		}
	}
}

// TestUnusableMods 覆盖「启用了却不生效」的两种成因与两类豁免：
//
//   - 目录存在但 Steam 没在 acf 里登记 → unregistered（引擎静默忽略）
//   - 目录不存在 → missing
//   - 客户端专属模组不参与统计（引擎在专用服务器上本来就不加载）
//   - 未配置共享目录时放弃登记校验，避免把所有模组误判成未登记
func TestUnusableMods(t *testing.T) {
	root := t.TempDir()
	clusterDir := filepath.Join(root, "cluster")
	shardDir := filepath.Join(clusterDir, "Master")
	gameDir := filepath.Join(root, "game")
	ugcDir := filepath.Join(root, "ugc")

	// 三个工坊模组：111 已登记，222 只有目录，333 只有 overrides 记录
	for _, id := range []string{"111", "222", "444"} {
		dir := filepath.Join(ugcDir, "content", "322330", id)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		info := "name = \"mod " + id + "\"\n"
		if id == "444" {
			info += "client_only_mod = true\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "modinfo.lua"), []byte(info), 0644); err != nil {
			t.Fatal(err)
		}
	}
	acf := `"AppWorkshop"
{
	"appid"		"322330"
	"WorkshopItemsInstalled"
	{
		"111"
		{
			"size"		"1"
			"timeupdated"		"1"
			"manifest"		"1"
		}
	}
}`
	if err := os.WriteFile(filepath.Join(ugcDir, "appworkshop_322330.acf"), []byte(acf), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(shardDir, 0755); err != nil {
		t.Fatal(err)
	}
	overrides := `return {
    ["workshop-111"] = { enabled = true },
    ["workshop-222"] = { enabled = true },
    ["workshop-333"] = { enabled = true },
    ["workshop-444"] = { enabled = true },
    ["workshop-555"] = { enabled = false },
    ["mymod"] = { enabled = true },
}`
	if err := os.WriteFile(filepath.Join(shardDir, "modoverrides.lua"), []byte(overrides), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(clusterDir, gameDir, ugcDir, []string{"Master"})
	missing, unregistered, err := m.UnusableMods()
	if err != nil {
		t.Fatalf("UnusableMods 出错: %v", err)
	}
	if len(missing) != 1 || missing[0] != "workshop-333" {
		t.Errorf("missing = %v, 期望 [workshop-333]", missing)
	}
	if len(unregistered) != 1 || unregistered[0] != "workshop-222" {
		t.Errorf("unregistered = %v, 期望 [workshop-222]", unregistered)
	}

	// 客户端模组可以被识别出来（供界面打标与 toggle 提示）
	if !m.IsClientOnly("workshop-444") {
		t.Error("workshop-444 应被识别为客户端模组")
	}
	if m.IsClientOnly("workshop-111") {
		t.Error("workshop-111 不是客户端模组")
	}

	// 下载清单要排除客户端模组
	ids, err := m.EnabledDownloadIDs()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == "444" {
			t.Errorf("客户端模组不该写进下载清单: %v", ids)
		}
	}
	if len(ids) != 3 { // 111 222 333
		t.Errorf("下载清单 = %v, 期望 3 项", ids)
	}
}

// TestEnsureOverridesFillsNewShard 覆盖「新加的分片加载零个模组」：
// 房间保存只写 ini，modoverrides.lua 只在模组配置变动时才重写，
// 于是后加的分片长期缺这份文件。
func TestEnsureOverridesFillsNewShard(t *testing.T) {
	root := t.TempDir()
	clusterDir := filepath.Join(root, "cluster")
	for _, s := range []string{"Master", "Caves", "World3"} {
		if err := os.MkdirAll(filepath.Join(clusterDir, s), 0755); err != nil {
			t.Fatal(err)
		}
	}
	masterContent := "return {\n    [\"workshop-111\"] = { enabled = true },\n}"
	if err := os.WriteFile(filepath.Join(clusterDir, "Master", "modoverrides.lua"), []byte(masterContent), 0644); err != nil {
		t.Fatal(err)
	}
	// Caves 已有一份不同内容：必须保留，不能被主分片覆盖
	if err := os.WriteFile(filepath.Join(clusterDir, "Caves", "modoverrides.lua"), []byte("return {\n}"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(clusterDir, filepath.Join(root, "game"), "", []string{"Master", "Caves", "World3"})
	if err := m.EnsureOverrides(); err != nil {
		t.Fatalf("EnsureOverrides 出错: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(clusterDir, "World3", "modoverrides.lua"))
	if err != nil {
		t.Fatalf("新分片未补齐 modoverrides.lua: %v", err)
	}
	if string(got) != masterContent {
		t.Errorf("新分片内容 = %q, 期望与主分片一致", string(got))
	}
	keep, _ := os.ReadFile(filepath.Join(clusterDir, "Caves", "modoverrides.lua"))
	if string(keep) != "return {\n}" {
		t.Errorf("已有分片被覆盖了: %q", string(keep))
	}
}
