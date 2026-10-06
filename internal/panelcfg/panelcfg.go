// Package panelcfg 保存面板级的「本机配置」。
//
// 与房间定义（dst_save/rooms.json）的区别：
//   - 房间定义跟着存档走，换机器也要保留
//   - 这里的配置与这台机器绑定（专用服务器装在哪、用哪个架构的可执行文件），
//     换机器就应该重新探测，所以单独放一份，不进存档目录
package panelcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"dst-windows/internal/config"
)

// FileName 是配置文件名，位于程序目录（与 exe 同级）。
const FileName = "panel.json"

// Config 是面板级的本机配置。
type Config struct {
	// ServerDir 为空表示「自动探测 Steam 库」。
	// 非空时必须是专用服务器安装根目录（含 bin/ 或 bin64/ 的那一层）。
	ServerDir string `json:"serverDir"`
	// Arch 取 auto / 64 / 32。空值等同 auto。
	Arch string `json:"arch"`
	// SteamApiKey 是免费的 Steam Web API Key，用于创意工坊关键词搜索。
	// 留空只影响搜索：按 ID/链接查详情与下载不受影响（那些接口免 key）。
	SteamApiKey string `json:"steamApiKey,omitempty"`
}

// Path 返回配置文件路径。
func Path(workDir string) string {
	return filepath.Join(workDir, FileName)
}

// Exists 判断配置文件是否存在。
//
// 这个判断很重要：一旦用户在本机配置过（文件存在），
// 就必须以它为准——包括「ServerDir 为空 = 自动探测」这个明确选择。
// 否则清空配置后会回落到早期版本的 serverdir.txt，等于「恢复自动探测」不生效。
func Exists(workDir string) bool {
	_, err := os.Stat(Path(workDir))
	return err == nil
}

// Load 读取配置。任何失败（文件不存在、内容损坏）都返回零值：
// 配置坏了最多是回到自动探测，不该拦住面板启动。
func Load(workDir string) Config {
	var c Config
	data, err := os.ReadFile(Path(workDir))
	if err != nil {
		return c
	}
	// 容忍用户手工编辑时加上的 BOM
	text := strings.TrimPrefix(string(data), "\xEF\xBB\xBF")
	if err := json.Unmarshal([]byte(text), &c); err != nil {
		return Config{}
	}
	c.ServerDir = strings.TrimSpace(c.ServerDir)
	c.Arch = strings.TrimSpace(c.Arch)
	c.SteamApiKey = strings.TrimSpace(c.SteamApiKey)
	return c
}

// Save 原子写入配置。
func Save(workDir string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(Path(workDir), append(data, '\n'))
}
