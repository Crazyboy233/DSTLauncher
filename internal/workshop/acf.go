package workshop

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ACFName 是 Steam 记录的工坊状态文件名（DST 创意工坊的 appid 是 322330）。
const ACFName = "appworkshop_322330.acf"

// InstalledItem 是 acf 里一条已安装工坊物品的记录。
type InstalledItem struct {
	Size int64
	// TimeUpdated 是 Steam 下载时记录的「工坊条目更新时间」。
	// 拿它和远端 time_updated 比较，就能判断本地是不是旧版。
	TimeUpdated int64
	Manifest    string
}

// LoadInstalled 读取 <ugcDir>/appworkshop_322330.acf。
//
// 这是判断「本地模组是不是旧版」唯一可靠的依据：
// 目录 mtime 会被拷贝、解压、杀软扫描改得面目全非，
// 而 acf 里的 timeupdated 是 Steam 自己写下的工坊版本时间。
// 文件不存在（从没下载过工坊模组）时返回空表，不算错误。
func LoadInstalled(ugcDir string) (map[string]InstalledItem, error) {
	path := filepath.Join(ugcDir, ACFName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]InstalledItem{}, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	return ParseACF(string(data))
}

// ParseACF 解析 acf（Valve KeyValues 格式）里我们需要的部分。
//
// 只实现这个文件用得到的子集：嵌套块 + 带引号的键值。
// 完整 VDF 还支持条件块 [#if]、转义等，工坊状态文件里不会出现。
func ParseACF(text string) (map[string]InstalledItem, error) {
	p := &vdfParser{data: text}
	root, err := p.parseBlock()
	if err != nil {
		return nil, err
	}

	app := root["AppWorkshop"]
	if app == nil || app.obj == nil {
		return nil, fmt.Errorf("acf 里找不到 AppWorkshop 节点")
	}
	installed := app.obj["WorkshopItemsInstalled"]
	if installed == nil || installed.obj == nil {
		return map[string]InstalledItem{}, nil // 一个都没装
	}

	out := make(map[string]InstalledItem, len(installed.obj))
	for id, node := range installed.obj {
		if node.obj == nil {
			continue
		}
		out[id] = InstalledItem{
			Size:        parseInt64(node.obj["size"]),
			TimeUpdated: parseInt64(node.obj["timeupdated"]),
			Manifest:    strOf(node.obj["manifest"]),
		}
	}
	return out, nil
}

/* ---------- 极简 VDF 解析 ---------- */

type vdfNode struct {
	str string
	obj map[string]*vdfNode
}

type vdfParser struct {
	data string
	pos  int
}

func (p *vdfParser) parseBlock() (map[string]*vdfNode, error) {
	out := make(map[string]*vdfNode)
	for {
		p.skipSpace()
		if p.pos >= len(p.data) {
			return out, nil
		}
		if p.data[p.pos] == '}' {
			p.pos++
			return out, nil
		}

		key, err := p.readToken()
		if err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.data) {
			return out, nil
		}
		if p.data[p.pos] == '{' {
			p.pos++
			sub, err := p.parseBlock()
			if err != nil {
				return nil, err
			}
			out[key] = &vdfNode{obj: sub}
			continue
		}
		val, err := p.readToken()
		if err != nil {
			return nil, err
		}
		out[key] = &vdfNode{str: val}
	}
}

func (p *vdfParser) readToken() (string, error) {
	p.skipSpace()
	if p.pos >= len(p.data) {
		return "", fmt.Errorf("acf 格式错误：意外的文件结尾")
	}

	if p.data[p.pos] == '"' {
		p.pos++
		var sb strings.Builder
		for p.pos < len(p.data) {
			c := p.data[p.pos]
			if c == '\\' && p.pos+1 < len(p.data) {
				p.pos++
				sb.WriteByte(p.data[p.pos])
				p.pos++
				continue
			}
			if c == '"' {
				p.pos++
				return sb.String(), nil
			}
			sb.WriteByte(c)
			p.pos++
		}
		return "", fmt.Errorf("acf 格式错误：字符串未闭合")
	}

	start := p.pos
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if isVDFSpace(c) || c == '{' || c == '}' {
			break
		}
		p.pos++
	}
	if start == p.pos {
		return "", fmt.Errorf("acf 格式错误：位置 %d 处无法解析", p.pos)
	}
	return p.data[start:p.pos], nil
}

func (p *vdfParser) skipSpace() {
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if isVDFSpace(c) {
			p.pos++
			continue
		}
		// 行注释：acf 偶尔会带
		if c == '/' && p.pos+1 < len(p.data) && p.data[p.pos+1] == '/' {
			for p.pos < len(p.data) && p.data[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		return
	}
}

func isVDFSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

func strOf(n *vdfNode) string {
	if n == nil {
		return ""
	}
	return n.str
}

func parseInt64(n *vdfNode) int64 {
	if n == nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(n.str), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
