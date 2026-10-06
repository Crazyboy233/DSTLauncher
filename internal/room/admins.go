package room

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"dst-windows/internal/config"
)

// 本文件管理房间的管理员名单（adminlist.txt）。
//
// DST 在集群目录下（与 cluster.ini 同目录）读取 adminlist.txt，
// 每行一个 Klei 用户 ID（形如 KU_xxxxxxxxxx）。
// 名单在服务器启动时加载，运行中修改要等下次重启才生效；
// 需要立即给运行中的服务器加管理员的，用控制台命令处理，这里不代办。

// AdminsFileName 是集群目录下的管理员名单文件名。
const AdminsFileName = "adminlist.txt"

// kleiIDRe 校验 Klei 用户 ID：KU_ 前缀 + 字母数字。
// 放宽长度到 1-32，Klei 并没有承诺 ID 段的固定长度。
var kleiIDRe = regexp.MustCompile(`^KU_[A-Za-z0-9]{1,32}$`)

// ValidKleiID 判断是否是合法的 Klei 用户 ID。
func ValidKleiID(id string) bool { return kleiIDRe.MatchString(id) }

// NormalizeKleiID 整理用户输入：去空白，前缀统一为大写 KU_
// （实际抄写时容易敲成小写 ku_，ID 段保持用户输入原样）。
func NormalizeKleiID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 3 && strings.EqualFold(id[:3], "KU_") {
		id = "KU_" + id[3:]
	}
	return id
}

// AdminsPath 返回管理员名单文件路径：<clusterDir>/adminlist.txt
func AdminsPath(clusterDir string) string {
	return filepath.Join(clusterDir, AdminsFileName)
}

// ReadAdmins 读取管理员名单。文件不存在视为空名单（Generate 会建空文件，
// 这里兜底兼容手工删掉的情况）。
// 顺序保留文件中的书写顺序，重复 ID 只保留第一条。
func ReadAdmins(clusterDir string) ([]string, error) {
	data, err := os.ReadFile(AdminsPath(clusterDir))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", AdminsPath(clusterDir), err)
	}
	return normalizeAdmins(string(data)), nil
}

// AddAdmin 校验并追加一个管理员，返回更新后的名单。
func AddAdmin(clusterDir, id string) ([]string, error) {
	id = NormalizeKleiID(id)
	if !ValidKleiID(id) {
		return nil, fmt.Errorf("Klei ID 不合法: %q（应为 KU_ 开头、后接字母数字，可在游戏内用 TheNet:GetUserID() 查看）", id)
	}

	list, err := ReadAdmins(clusterDir)
	if err != nil {
		return nil, err
	}
	for _, existing := range list {
		if strings.EqualFold(existing, id) {
			return list, nil // 已存在视为成功，幂等
		}
	}
	list = append(list, id)
	if err := writeAdmins(clusterDir, list); err != nil {
		return nil, err
	}
	return list, nil
}

// RemoveAdmin 按 ID（忽略大小写）移除一个管理员，返回更新后的名单。
func RemoveAdmin(clusterDir, id string) ([]string, error) {
	list, err := ReadAdmins(clusterDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list))
	removed := false
	for _, existing := range list {
		if strings.EqualFold(existing, strings.TrimSpace(id)) {
			removed = true
			continue
		}
		out = append(out, existing)
	}
	if !removed {
		return list, fmt.Errorf("管理员不存在: %s", id)
	}
	if err := writeAdmins(clusterDir, out); err != nil {
		return nil, err
	}
	return out, nil
}

// normalizeAdmins 把文件内容整理成干净的去重列表。
// 忽略空行与注释行（# 开头），方便用户手工维护。
func normalizeAdmins(content string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 8)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := strings.ToLower(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	return out
}

// writeAdmins 原子写入名单（每行一个 ID，带说明注释头）。
func writeAdmins(clusterDir string, ids []string) error {
	var sb strings.Builder
	sb.WriteString("# 房间管理员名单（每行一个 Klei ID，KU_ 开头）\n")
	sb.WriteString("# 服务器启动时读取，修改后需重启房间生效\n")
	for _, id := range ids {
		sb.WriteString(id + "\n")
	}
	if err := config.WriteFileAtomic(AdminsPath(clusterDir), []byte(sb.String())); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", AdminsFileName, err)
	}
	return nil
}
