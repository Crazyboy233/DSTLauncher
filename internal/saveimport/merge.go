package saveimport

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"dst-windows/internal/config"
	"dst-windows/internal/room"
	"dst-windows/internal/worldsettings"
)

// MergeInto 把存档解析结果合并成最终的房间配置。
//
// target 为 nil 表示「用存档新建一个房间」：
// 端口、分片协调端口、集群密钥、master_ip 全部留空，交给 room.Store.Create 自动分配。
//
// target 非 nil 表示「用存档覆盖已有房间」：
// 这些机器本地字段沿用目标房间。存档里的值来自另一台机器，照抄必然出问题——
// master_port / server_port 可能与本机其它房间撞号，cluster_key 一旦与分片不一致
// 会让地表连不上洞穴。所以只覆盖玩法配置，机器相关的一律不动。
func MergeInto(p *Parsed, target *room.Room) *room.Room {
	src := p.Room

	out := &room.Room{
		Name:         src.Name,
		Description:  src.Description,
		GameMode:     src.GameMode,
		MaxPlayers:   src.MaxPlayers,
		PvP:          src.PvP,
		PauseEmpty:   src.PauseEmpty,
		VoteEnabled:  src.VoteEnabled,
		VoteKick:     src.VoteKick,
		Password:     src.Password,
		Language:     src.Language,
		TickRate:     src.TickRate,
		MaxSnapshots: src.MaxSnapshots,
		LANOnly:      src.LANOnly,
		Offline:      src.Offline,
		Caves:        src.Caves,
	}

	// 令牌：存档里带了（且非空）就用存档的，否则保留房间现有的
	if strings.TrimSpace(p.Token) != "" {
		out.Token = strings.TrimSpace(p.Token)
	} else if target != nil {
		out.Token = target.Token
	}

	if target != nil {
		out.ID = target.ID
		out.MasterIP = target.MasterIP
		out.MasterPort = target.MasterPort
		out.ClusterKey = target.ClusterKey
		// 端口必须整套沿用：只改一部分会让 Normalize 给缺失项兜底成默认值，
		// 那些默认值在其他房间眼里就是冲突端口
		out.Shards = append([]room.Shard(nil), target.Shards...)
	}

	return out
}

// WriteInto 把存档的数据文件写进目标房间目录。
//
// 只写三类内容：
//   - 各分片的 save 世界数据（存档的本体）
//   - 各分片的世界设置 worldgenoverride.lua（面板据此展示与二次编辑）
//   - 三份玩家名单（存档里有才覆盖）
//
// 刻意不写 cluster.ini / server.ini / cluster_token.txt：
// 前三份由 room.Generate 依据房间配置统一生成，否则会出现两套互相矛盾的事实来源。
//
// 模组配置也不在这里写——它必须经 modmgr 以面板的规范格式写回**全部分片**
// 并重算安装级的下载清单，因此由调用方（Web 层）在拿到目标房间之后处理。
//
// 返回的 notes 是应当提示给用户的说明（跳过的分片、没有数据的存档等），
// 不是错误。
func (p *Parsed) WriteInto(root, confDir string, r *room.Room) ([]string, error) {
	clusterDir := config.ClusterDir(root, confDir, r.ID)
	if err := config.EnsureDirExists(clusterDir); err != nil {
		return nil, fmt.Errorf("创建集群目录失败: %w", err)
	}

	var notes []string

	// 只导入目标房间启用中的分片。存档里多出来的（例如房间停用了洞穴但存档带洞穴）
	// 会被忽略，避免留下一个与地表对不上的孤立世界。
	enabled := map[string]bool{}
	for _, n := range r.ShardNames() {
		enabled[n] = true
	}

	for _, s := range p.Shards {
		if !enabled[s.Name] {
			if s.HasSave {
				notes = append(notes, fmt.Sprintf("存档包含 %s 世界，但目标房间未启用该分片，已跳过", s.Name))
			}
			continue
		}

		shardDir := config.ShardDir(root, confDir, r.ID, s.Name)
		if err := config.EnsureDirExists(shardDir); err != nil {
			return notes, fmt.Errorf("创建 %s 分片目录失败: %w", s.Name, err)
		}

		// 1. 世界存档数据：整个 save 目录替换掉
		if s.HasSave {
			if err := replaceDir(filepath.Join(p.ClusterDir, s.Dir, "save"), filepath.Join(shardDir, "save")); err != nil {
				return notes, fmt.Errorf("导入 %s 世界数据失败: %w", s.Name, err)
			}
		} else {
			notes = append(notes, fmt.Sprintf(
				"存档里的 %s 没有世界数据（save 目录为空），该分片沿用房间现有存档", s.Name))
		}

		// 2. 世界设置：统一写成面板管理的 worldgenoverride.lua。
		// 存档里通常只有 leveldataoverride.lua，解析后转换过来，世界配置页才能二次编辑。
		if s.HasOverride {
			if err := worldsettings.WriteFile(clusterDir, s.Name, s.Override); err != nil {
				return notes, fmt.Errorf("写入 %s 世界设置失败: %w", s.Name, err)
			}
			notes = append(notes, fmt.Sprintf(
				"%s 的世界设置已从 %s 导入，共 %d 项", s.Name, s.OverrideSrc, len(s.Override.Overrides)))
		}
	}

	// 3. 名单：只有存档里确实存在该文件时才覆盖，
	// 否则会把用户在面板里维护好的名单清空
	for _, f := range []struct {
		name    string
		content string
		exists  bool
	}{
		{"adminlist.txt", p.AdminList, p.HasAdmin},
		{"blocklist.txt", p.BlockList, p.HasBlock},
		{"whitelist.txt", p.WhiteList, p.HasWhite},
	} {
		if !f.exists {
			continue
		}
		if err := config.WriteFileAtomic(filepath.Join(clusterDir, f.name), []byte(f.content)); err != nil {
			return notes, fmt.Errorf("写入 %s 失败: %w", f.name, err)
		}
	}

	return notes, nil
}

// replaceDir 用 src 整体替换 dst 目录。
//
// 先把旧目录改名暂存，拷贝成功后再删除；拷贝失败则把旧目录还原回去。
// 直接 RemoveAll 再拷贝的话，中途失败会留下一个「旧的没了、新的也没拷全」的目录。
// 注意这只是拷贝期间的中转，成功后会删除，不是备份。
func replaceDir(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("源目录不存在: %s", src)
	}

	stash := dst + ".replaced"
	if err := os.RemoveAll(stash); err != nil {
		return err
	}
	hadOld := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, stash); err != nil {
			return fmt.Errorf("暂存旧存档失败: %w", err)
		}
		hadOld = true
	}

	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		if hadOld {
			_ = os.Rename(stash, dst)
		}
		return err
	}

	if hadOld {
		_ = os.RemoveAll(stash)
	}
	return nil
}

// copyTree 递归拷贝目录，保持相对结构。
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

// copyFile 拷贝单个文件。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
