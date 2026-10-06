// Package worldsettings 提供 DST 世界配置的选项元数据，以及 worldgenoverride.lua 的读写。
//
// 选项清单不是手工维护的：它由 tools/extract-worldsettings 从游戏安装目录的
// scripts.zip 里提取（scripts/map/customize.lua + chinese_s.po），
// 游戏更新后重新生成 options.json 即可。
//
// 关于「该写哪个文件」——Klei 在服务器源码注释里写得很清楚：
//
//	leveldataoverride.lua is for GAME USE. It completely overrides existing saved world data.
//	worldgenoverride.lua is for USER USE.
//
// 所以这里只读写 worldgenoverride.lua，绝不碰 leveldataoverride.lua。
package worldsettings

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

//go:embed options.json
var rawOptions []byte

// Value 是一个可选值。
type Value struct {
	Data  string `json:"data"`
	Label string `json:"label,omitempty"`
}

// Option 是一个世界设置项。
type Option struct {
	Name    string   `json:"name"`
	Label   string   `json:"label,omitempty"`
	Default string   `json:"default"`
	Order   int      `json:"order,omitempty"`
	Worlds  []string `json:"worlds,omitempty"` // forest / cave，决定属于哪个分片
	Values  []Value  `json:"values,omitempty"`

	// Dynamic 表示取值由游戏运行时生成（如 task_set、start_location），
	// 面板只能提供一个文本框让用户自己填。
	Dynamic  bool   `json:"dynamic,omitempty"`
	Category string `json:"category"` // worldgen / settings
}

// Group 是一组选项（官方分组，用作前端折叠面板）。
type Group struct {
	ID       string   `json:"id"`
	Label    string   `json:"label,omitempty"`
	Category string   `json:"category"`
	Order    int      `json:"order,omitempty"`
	Options  []Option `json:"options"`
}

// Preset 是一个世界预设（玩法风格）。
type Preset struct {
	Data  string `json:"data"`
	Label string `json:"label,omitempty"`
}

// Dataset 是 options.json 的结构。
type Dataset struct {
	Version     int                 `json:"version"`
	GameVersion string              `json:"gameVersion"`
	GeneratedAt string              `json:"generatedAt"`
	Source      string              `json:"source"`
	Presets     map[string][]Preset `json:"presets"` // 键形如 forest_settings / cave_worldgen
	Groups      []Group             `json:"groups"`
}

// 选项的两大类，对应官方 customize.lua 里的两个顶层容器。
const (
	CategoryWorldGen = "worldgen" // 生成时：决定地图长什么样
	CategorySettings = "settings" // 运行设置：决定世界规则
)

// 分片名。
const (
	ShardMaster = "Master"
	ShardCaves  = "Caves"
)

var (
	loadOnce sync.Once
	loadErr  error
	dataset  Dataset
	byName   map[string]*Option
)

// Load 解析内嵌的选项清单（只做一次）。
func Load() error {
	loadOnce.Do(func() {
		if err := json.Unmarshal(rawOptions, &dataset); err != nil {
			loadErr = fmt.Errorf("解析内置世界选项清单失败: %w", err)
			return
		}
		byName = make(map[string]*Option, len(dataset.Groups)*8)
		for gi := range dataset.Groups {
			for oi := range dataset.Groups[gi].Options {
				o := &dataset.Groups[gi].Options[oi]
				byName[o.Name] = o
			}
		}
	})
	return loadErr
}

// Version 返回清单基于的游戏版本，供界面展示。
func Version() string {
	if err := Load(); err != nil {
		return ""
	}
	return dataset.GameVersion
}

// GeneratedAt 返回清单的生成时间。
func GeneratedAt() string {
	if err := Load(); err != nil {
		return ""
	}
	return dataset.GeneratedAt
}

// LocationOf 把分片名映射成游戏内部的世界位置标识。
func LocationOf(shard string) string {
	if strings.EqualFold(shard, ShardCaves) {
		return "cave"
	}
	return "forest"
}

// Groups 返回指定分片、指定类别下可用的分组（已按分片过滤掉不适用的选项）。
//
// 过滤依据是选项自带的 world 字段——它由游戏自己标注，不用维护两份清单。
// category 传空表示两类都要。
func Groups(shard, category string) []Group {
	if err := Load(); err != nil {
		return nil
	}
	loc := LocationOf(shard)

	out := make([]Group, 0, len(dataset.Groups))
	for _, g := range dataset.Groups {
		if category != "" && g.Category != category {
			continue
		}
		filtered := Group{
			ID: g.ID, Label: g.Label, Category: g.Category, Order: g.Order,
			Options: make([]Option, 0, len(g.Options)),
		}
		for _, o := range g.Options {
			if !appliesTo(o, loc) {
				continue
			}
			filtered.Options = append(filtered.Options, o)
		}
		if len(filtered.Options) > 0 {
			out = append(out, filtered)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// appliesTo 判断选项是否适用于该位置。
func appliesTo(o Option, location string) bool {
	if len(o.Worlds) == 0 {
		return true // 没标 world 的视为通用
	}
	for _, w := range o.Worlds {
		if w == location {
			return true
		}
	}
	return false
}

// Presets 返回指定分片、指定类别的预设清单。
func Presets(shard, category string) []Preset {
	if err := Load(); err != nil {
		return nil
	}
	key := LocationOf(shard) + "_" + category
	list := dataset.Presets[key]
	out := make([]Preset, len(list))
	copy(out, list)
	return out
}

// Lookup 按名字取选项定义。
func Lookup(name string) (Option, bool) {
	if err := Load(); err != nil {
		return Option{}, false
	}
	o, ok := byName[name]
	if !ok {
		return Option{}, false
	}
	return *o, true
}

// ValidateValue 校验一个取值是否合法。
//
// 动态选项（取值由游戏运行时决定）只做字符集校验——面板没法穷举，
// 但至少挡住能破坏 lua 文件的字符。
//
// 清单外的键不拒绝：导入存档会带来引擎内部键（layout_mode、
// wormhole_prefab 等）与 mod 自定义键，世界页承诺「原样保留」，
// 校验就必须与之自洽——只检查名字与取值能否安全写进 lua。
func ValidateValue(name, value string) error {
	opt, ok := Lookup(name)
	if !ok {
		if !isSafeName(name) {
			return fmt.Errorf("设置项名称 %q 含非法字符", name)
		}
		if value != "" && !isSafeValue(value) {
			return fmt.Errorf("%s 的取值 %q 含非法字符", name, value)
		}
		return nil
	}
	if value == "" || value == opt.Default {
		return nil // 等于默认值等于没改，无需校验
	}
	if isSafeValue(value) {
		if opt.Dynamic || len(opt.Values) == 0 {
			return nil
		}
		for _, v := range opt.Values {
			if v.Data == value {
				return nil
			}
		}
		return fmt.Errorf("%s 的取值 %q 不在允许范围内", name, value)
	}
	return fmt.Errorf("%s 的取值 %q 含非法字符", name, value)
}

// isSafeName 判断设置项的键名能否安全地写进 lua。
// 键名在生成时不加引号（name = "value"），必须限定为合法标识符，
// 否则恶意构造的键名可以逃出赋值语句注入任意 lua 代码。
func isSafeName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false // 键名不能以数字开头
			}
		default:
			return false
		}
	}
	return true
}

// isSafeValue 判断取值是否可以安全地写进 lua 字符串。
// 游戏里的取值都是 never / rare / veryshortseason / "highly random" 这类，
// 允许字母数字、下划线、空格和少量符号。
func isSafeValue(v string) bool {
	if len(v) > 64 {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == ' ' || r == '-' || r == '|' || r == '.' || r == '+':
		default:
			return false
		}
	}
	return true
}
