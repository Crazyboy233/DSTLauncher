// Package config 负责 DST 的各类配置文件读写。
//
// 涉及三类文件：
//   - cluster.ini    集群总配置（房间名、描述、tick 频率等）
//   - server.ini     分片配置（端口、最大人数、模式、密码、白天时长）
//   - modoverrides.lua  模组启用状态与配置项（Lua table 格式）
//
// Windows 平台注意：所有文件必须以 UTF-8 无 BOM 写入，否则游戏内中文会乱码。
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Ini 是一个 INI 文件的内存表示。
// DST 的 ini 使用 [sectionName] 作为分节，key = value 形式。
type Ini struct {
	order    []string // 保持分节与键的原始顺序，避免重写时打乱文件
	sections map[string]map[string]string
	// 记录注释行，重写时保留，提升可维护性
	comments map[string]string
}

// NewIni 创建一个空的 Ini。
func NewIni() *Ini {
	return &Ini{
		sections: make(map[string]map[string]string),
		comments: make(map[string]string),
	}
}

// ParseIni 解析 INI 文本。
func ParseIni(content string) *Ini {
	ini := NewIni()
	current := ""
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// 保留注释，供写回时使用
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			ini.comments[line] = line
			continue
		}
		// 分节头
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			ini.ensureSection(current)
			continue
		}
		// key = value
		idx := strings.Index(line, "=")
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		ini.Set(current, k, v)
	}
	return ini
}

func (i *Ini) ensureSection(name string) map[string]string {
	if _, ok := i.sections[name]; !ok {
		i.sections[name] = make(map[string]string)
		i.order = append(i.order, name)
	}
	return i.sections[name]
}

// Get 读取键值，不存在时返回空串。
func (i *Ini) Get(section, key string) string {
	if s, ok := i.sections[section]; ok {
		return s[key]
	}
	return ""
}

// GetInt 读取整型键值。
func (i *Ini) GetInt(section, key string, def int) int {
	v := i.Get(section, key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// GetBool 读取布尔键值，DST 中 true/false 均以字符串形式出现。
func (i *Ini) GetBool(section, key string, def bool) bool {
	v := strings.ToLower(i.Get(section, key))
	switch v {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	return def
}

// Set 写入键值，保留原有顺序。
func (i *Ini) Set(section, key, value string) {
	s := i.ensureSection(section)
	if _, exists := s[key]; !exists {
		// 记录新键的插入位置：放在该分节已有键之后
		i.order = append(i.order, "\x00"+section+"\x00"+key)
	}
	s[key] = value
}

// Has 判断键是否存在。
func (i *Ini) Has(section, key string) bool {
	s, ok := i.sections[section]
	if !ok {
		return false
	}
	_, exists := s[key]
	return exists
}

// Delete 删除一个键。
func (i *Ini) Delete(section, key string) {
	s, ok := i.sections[section]
	if !ok {
		return
	}
	delete(s, key)
}

// Sections 返回所有分节名。
func (i *Ini) Sections() []string {
	var out []string
	for _, name := range i.order {
		if strings.HasPrefix(name, "\x00") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// Keys 返回分节下的所有键（按写入顺序）。
func (i *Ini) Keys(section string) []string {
	s, ok := i.sections[section]
	if !ok {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	for _, name := range i.order {
		if strings.HasPrefix(name, "\x00") {
			parts := strings.Split(strings.TrimPrefix(name, "\x00"), "\x00")
			if len(parts) == 2 && parts[0] == section && !seen[parts[1]] {
				seen[parts[1]] = true
				out = append(out, parts[1])
			}
		}
	}
	// 补上初始化时已存在但未记录顺序的键
	for k := range s {
		if !seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// String 渲染回 INI 文本。
func (i *Ini) String() string {
	var sb strings.Builder
	// 按分节顺序输出
	written := make(map[string]bool)
	for _, name := range i.order {
		if strings.HasPrefix(name, "\x00") || written[name] {
			continue
		}
		s, ok := i.sections[name]
		if !ok {
			continue
		}
		written[name] = true
		sb.WriteString("[" + name + "]\n")
		for _, k := range i.Keys(name) {
			sb.WriteString(k + " = " + s[k] + "\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// Save 原子写入 INI 文件。
// 先写临时文件再改名，避免写入中断导致配置文件损坏——
// 游戏正在运行时配置文件损坏会导致启动失败。
func (i *Ini) Save(path string) error {
	return WriteFileAtomic(path, []byte(i.String()))
}

// LoadIni 从文件加载 INI。
func LoadIni(path string) (*Ini, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewIni(), nil // 文件不存在时返回空配置
		}
		return nil, err
	}
	// 去除可能存在的 UTF-8 BOM，否则第一个分节名会带上不可见字符
	return ParseIni(stripBOM(string(data))), nil
}

// WriteFileAtomic 以 UTF-8 无 BOM 原子写入文件。
// 这是全项目统一的写文件入口，确保 Windows 下不出现中文乱码。
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	tmp := path + ".tmp"
	// 显式指定无 BOM 的 UTF-8
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows 上目标文件被占用时 rename 会失败，清理临时文件后重试一次
		_ = os.Remove(tmp)
		return fmt.Errorf("替换目标文件失败: %w", err)
	}
	return nil
}

// stripBOM 去除 UTF-8 BOM 头。
func stripBOM(s string) string {
	if strings.HasPrefix(s, "\xEF\xBB\xBF") {
		return s[3:]
	}
	return s
}
