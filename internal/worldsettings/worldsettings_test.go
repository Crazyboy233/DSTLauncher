package worldsettings

import (
	"strings"
	"testing"
)

// 生成的 lua 必须能被自己解析回来（写一次、读一次不丢信息）。
func TestOverrideRoundTrip(t *testing.T) {
	o := NewOverrideFor(ShardMaster)
	o.Overrides = map[string]string{
		"world_size": "huge",
		"hounds":     "rare",
	}

	got := ParseOverride(o.String())

	if !got.Enabled {
		t.Error("override_enabled 应保持 true")
	}
	if got.WorldGenPreset != o.WorldGenPreset || got.SettingsPreset != o.SettingsPreset {
		t.Errorf("预设丢失: %q / %q", got.WorldGenPreset, got.SettingsPreset)
	}
	if len(got.Overrides) != 2 {
		t.Fatalf("overrides 数量应为 2，实际 %d: %v", len(got.Overrides), got.Overrides)
	}
	for k, v := range o.Overrides {
		if got.Overrides[k] != v {
			t.Errorf("%s: 期望 %q，实际 %q", k, v, got.Overrides[k])
		}
	}
}

// 真实文件的形态：带注释、混用 ["k"] 与裸 key、含遗留 preset。
func TestParseRealWorldFormat(t *testing.T) {
	const content = `return {
	override_enabled = true,
	worldgen_preset = "SURVIVAL_TOGETHER",
	settings_preset = "SURVIVAL_TOGETHER",
	overrides = {
		-- WORLDGEN
		world_size = "huge",
		["hounds"] = "rare",
		-- 下面这行是注释，里面的 key = "value" 不应被当成配置
		-- boons = "often",
	},
}
`
	got := ParseOverride(content)

	if len(got.Overrides) != 2 {
		t.Fatalf("应解析出 2 项，实际 %d: %v", len(got.Overrides), got.Overrides)
	}
	if got.Overrides["world_size"] != "huge" {
		t.Errorf("world_size 解析错误: %q", got.Overrides["world_size"])
	}
	if got.Overrides["hounds"] != "rare" {
		t.Errorf("[\"hounds\"] 形式解析错误: %q", got.Overrides["hounds"])
	}
	if _, ok := got.Overrides["boons"]; ok {
		t.Error("注释掉的行不应被解析")
	}
}

// 遗留的单键 preset 要同时充当两个预设。
func TestParseLegacyPreset(t *testing.T) {
	got := ParseOverride(`return { override_enabled = true, preset = "ENDLESS" }`)
	if got.WorldGenPreset != "ENDLESS" || got.SettingsPreset != "ENDLESS" {
		t.Errorf("遗留 preset 未生效: %q / %q", got.WorldGenPreset, got.SettingsPreset)
	}
}

// override_enabled = false 也要能读出来（不能当成"没写"）。
func TestParseDisabled(t *testing.T) {
	got := ParseOverride(`return { override_enabled = false, overrides = {} }`)
	if got.Enabled {
		t.Error("override_enabled = false 应解析为 false")
	}
}

// 面板清单之外的项（Mod 自定义设置）必须原样保留，否则会悄悄丢配置。
func TestUnknownOptionsPreserved(t *testing.T) {
	o := NewOverrideFor(ShardMaster)
	o.Overrides = map[string]string{
		"some_mod_setting": "custom_value",
		"world_size":       "huge",
	}

	text := o.String()
	if !strings.Contains(text, `some_mod_setting = "custom_value"`) {
		t.Errorf("清单外的项被丢弃了:\n%s", text)
	}

	got := ParseOverride(text)
	if got.Overrides["some_mod_setting"] != "custom_value" {
		t.Error("往返后清单外的项丢失")
	}
}

// 空 overrides 也要生成合法的 lua（服务器会读这个文件）。
func TestEmptyOverridesStillValid(t *testing.T) {
	text := NewOverrideFor(ShardCaves).String()
	if !strings.Contains(text, "overrides = {") {
		t.Errorf("缺少 overrides 块:\n%s", text)
	}
	if got := ParseOverride(text); got.WorldGenPreset != "DST_CAVE" {
		t.Errorf("洞穴默认预设应为 DST_CAVE，实际 %q", got.WorldGenPreset)
	}
}

// 分片过滤：Master 只出 forest 的选项，Caves 只出 cave 的。
func TestGroupsFilterByShard(t *testing.T) {
	if err := Load(); err != nil {
		t.Fatalf("加载选项清单失败: %v", err)
	}

	for _, shard := range []string{ShardMaster, ShardCaves} {
		loc := LocationOf(shard)
		groups := Groups(shard, "")
		if len(groups) == 0 {
			t.Fatalf("%s 没有可用分组", shard)
		}
		total := 0
		for _, g := range groups {
			for _, o := range g.Options {
				total++
				if !appliesTo(o, loc) {
					t.Errorf("%s 出现了不该有的选项 %s (worlds=%v)", shard, o.Name, o.Worlds)
				}
			}
		}
		t.Logf("%s(%s): %d 个分组、%d 个选项", shard, loc, len(groups), total)
	}

	// 两类都要时，worldgen 与 settings 都应在
	all := Groups(ShardMaster, "")
	cats := map[string]bool{}
	for _, g := range all {
		cats[g.Category] = true
	}
	if !cats[CategoryWorldGen] || !cats[CategorySettings] {
		t.Errorf("两个类别都应出现，实际 %v", cats)
	}
}

// 只取一个类别时不应混入另一类。
func TestGroupsSingleCategory(t *testing.T) {
	gs := Groups(ShardMaster, CategorySettings)
	if len(gs) == 0 {
		t.Fatal("没有 settings 分组")
	}
	for _, g := range gs {
		if g.Category != CategorySettings {
			t.Errorf("混入了 %s 类别的分组 %s", g.Category, g.ID)
		}
	}
}

// 预设按分片区分：地表与洞穴的预设不可混用。
func TestPresetsByShard(t *testing.T) {
	forest := Presets(ShardMaster, CategoryWorldGen)
	cave := Presets(ShardCaves, CategoryWorldGen)

	if len(forest) == 0 || len(cave) == 0 {
		t.Fatalf("预设为空: forest=%d cave=%d", len(forest), len(cave))
	}
	for _, p := range cave {
		if !strings.HasPrefix(p.Data, "DST_CAVE") && p.Data != "TERRARIA_CAVE" {
			t.Errorf("洞穴预设里出现了非洞穴预设: %s", p.Data)
		}
	}
}

func TestDefaultPresetByShard(t *testing.T) {
	if got := DefaultPreset(ShardMaster); got != "SURVIVAL_TOGETHER" {
		t.Errorf("地表默认预设错误: %s", got)
	}
	if got := DefaultPreset(ShardCaves); got != "DST_CAVE" {
		t.Errorf("洞穴默认预设错误: %s", got)
	}
}

func TestValidateValue(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{"world_size", "huge", true},
		{"world_size", "gigantic", false}, // 不在允许范围
		{"hounds", "rare", true},
		{"hounds", "'; os.exit() --", false}, // 注入尝试
		{"不存在的选项", "x", false},               // 键名含非 ASCII，拒绝
		// 清单外的键不再一律拒绝：导入存档会带进引擎内部键，
		// 只要名字是合法 lua 标识符、取值安全，就允许原样保留
		{"layout_mode", "LinkNodesByKeys", true},
		{"wormhole_prefab", "wormhole", true},
		{"1bad_name", "x", false},     // 键名以数字开头
		{"bad;name", "x", false},      // 键名含非法字符
		{"ok_name", "'; drop", false}, // 取值含注入字符
	}
	for _, c := range cases {
		err := ValidateValue(c.name, c.value)
		if c.ok && err != nil {
			t.Errorf("%s=%q 应合法，实际报错: %v", c.name, c.value, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s=%q 应被拒绝", c.name, c.value)
		}
	}
}

// 整份配置的校验：任一项非法就整体拒绝。
func TestOverrideValidate(t *testing.T) {
	o := NewOverrideFor(ShardMaster)
	o.Overrides["world_size"] = "huge"
	if err := o.Validate(); err != nil {
		t.Errorf("合法配置被拒绝: %v", err)
	}

	o.Overrides["hounds"] = `"; os.exit() --`
	if err := o.Validate(); err == nil {
		t.Error("含注入字符的配置应被拒绝")
	}
}
