// Package saves 盘点磁盘上的集群存档目录。
//
// 房间定义（rooms.json）与存档目录是两套生命周期：删除房间**只删定义**，
// 存档目录会原样留在磁盘上（存档是玩家心血，面板不敢自动删）。
// 时间一长，<root>/<confDir> 下会堆出一批没有房间认领的 Cluster_N，
// 占着几个 GB，却没人知道它们分别是什么。这个包负责把它们列出来。
package saves

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dst-windows/internal/config"
	"dst-windows/internal/room"
)

// ShardInfo 是集群下的一个分片目录。
type ShardInfo struct {
	Name    string    `json:"name"`
	HasSave bool      `json:"hasSave"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// Info 是一个磁盘上的集群存档。
type Info struct {
	Cluster    string      `json:"cluster"`    // 目录名 Cluster_<id>
	ID         int         `json:"id"`         // 从目录名解析出的 id
	RoomID     int         `json:"roomId"`     // 关联房间 id，0 表示没有房间
	RoomName   string      `json:"roomName"`   // 关联房间名
	RoomExists bool        `json:"roomExists"` // 面板里是否还有这个房间
	Running    bool        `json:"running"`    // 是否被运行中的服务器占用
	HasSave    bool        `json:"hasSave"`    // 任一分片是否已生成世界
	Size       int64       `json:"size"`       // 目录总大小
	ModTime    time.Time   `json:"modTime"`    // 最近修改时间
	Shards     []ShardInfo `json:"shards"`
}

// ParseClusterID 从 Cluster_<id> 解析出 id。
func ParseClusterID(name string) (int, bool) {
	if !strings.HasPrefix(name, "Cluster_") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "Cluster_"))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// Scan 扫描 <root>/<confDir> 下的全部 Cluster_* 目录。
//
// rooms 用于标注归属；running 由调用方给出（集群标识 -> 是否有进程在跑），
// 因为「是否运行」只有进程管理层知道。
func Scan(root, confDir string, rooms []*room.Room, running map[string]bool) ([]Info, error) {
	base := config.ClusterRoot(root, confDir)
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // 还没开过服
		}
		return nil, fmt.Errorf("读取存档根目录失败: %w", err)
	}

	byID := make(map[int]*room.Room, len(rooms))
	for _, r := range rooms {
		if r != nil {
			byID[r.ID] = r
		}
	}

	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, ok := ParseClusterID(e.Name())
		if !ok {
			continue
		}
		dir := filepath.Join(base, e.Name())

		info := Info{Cluster: e.Name(), ID: id, Running: running[e.Name()]}
		if rm, ok := byID[id]; ok {
			info.RoomID = rm.ID
			info.RoomName = rm.Name
			info.RoomExists = true
		}

		info.Size, info.ModTime = dirSize(dir)
		info.Shards, info.HasSave = scanShards(dir)
		out = append(out, info)
	}

	// 目录 id 升序，和房间列表保持一致，肉眼好对
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// scanShards 找出集群目录下的分片。
// 判据：含 server.ini 或 save 的子目录——只有这两个东西能说明它是分片，
// 顺带自动跳过 logs/、backup/ 这类附属目录。
func scanShards(clusterDir string) ([]ShardInfo, bool) {
	entries, err := os.ReadDir(clusterDir)
	if err != nil {
		return nil, false
	}

	var out []ShardInfo
	hasSave := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(clusterDir, e.Name())
		hasIni := config.FileExists(filepath.Join(dir, "server.ini"))
		hasSaveDir := config.FileExists(filepath.Join(dir, "save"))
		if !hasIni && !hasSaveDir {
			continue
		}
		size, mod := dirSize(dir)
		sh := ShardInfo{
			Name:    e.Name(),
			HasSave: config.ShardHasSave(dir),
			Size:    size,
			ModTime: mod,
		}
		if sh.HasSave {
			hasSave = true
		}
		out = append(out, sh)
	}
	return out, hasSave
}

// dirSize 统计目录总大小与最近修改时间。
// 只用于界面展示，不追求精确：读不了的文件直接跳过。
func dirSize(dir string) (int64, time.Time) {
	var size int64
	var latest time.Time
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			size += info.Size()
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return size, latest
}

// Delete 删除一整个集群存档目录。
//
// 调用方必须已经确认：房间定义已删除、没有任何进程在跑、用户已二次确认。
// 这里只做路径安全校验，避免任何形式的目录穿越把别的东西删掉。
func Delete(root, confDir, cluster string) error {
	if _, ok := ParseClusterID(cluster); !ok {
		return fmt.Errorf("非法的集群目录名: %s", cluster)
	}

	base, err := filepath.Abs(config.ClusterRoot(root, confDir))
	if err != nil {
		return err
	}
	target, err := filepath.Abs(filepath.Join(base, cluster))
	if err != nil {
		return err
	}
	// 双保险：解析后的路径必须仍在存档根目录之下
	if target == base || !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return fmt.Errorf("非法的集群目录: %s", cluster)
	}

	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("存档目录不存在: %s", cluster)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("删除存档目录失败: %w", err)
	}
	return nil
}
