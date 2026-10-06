package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DST 专用服务器的存档路径规则（已与官方行为核对）：
//
//	<persistent_storage_root>/<conf_dir>/<cluster>/<shard>
//
// 本项目启动时传入 -persistent_storage_root <root> -conf_dir DST，
// 因此真实目录是 <root>/DST/Cluster_<id>/<shard>。
//
// 全项目所有需要定位存档的地方都必须走下面两个函数。
// 曾经的 Bug：manager 用了完整三层路径，而 config.Generate 与 backup 漏掉了
// <conf_dir> 这一层，导致"服务器读的目录"和"生成配置 / 备份的目录"根本不是同一个。

// ClusterDir 返回集群根目录：<root>/<confDir>/Cluster_<id>
func ClusterDir(root, confDir string, clusterID int) string {
	return filepath.Join(root, confDir, fmt.Sprintf("Cluster_%d", clusterID))
}

// ShardDir 返回分片目录：<root>/<confDir>/Cluster_<id>/<shard>
func ShardDir(root, confDir string, clusterID int, shard string) string {
	return filepath.Join(ClusterDir(root, confDir, clusterID), shard)
}

// EnsureDirExists 确保目录存在。
func EnsureDirExists(path string) error {
	return os.MkdirAll(path, 0755)
}

// FileExists 判断文件是否存在。
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ShardHasSave 判断分片目录下是否已经生成过世界存档。
//
// 不写死 <shard>/save/session 这两层：那是 DST 的内部布局，换个版本就可能调整，
// 一旦变了「备份」与「删除房间时的存档提醒」会一起失灵。
//
// 判据放宽成「save 目录下存在任何实际内容」，实测依据：
//   - 未生成世界时，save 下只有一个**空的** session 目录
//   - 一旦服务器真正加载或生成过世界，save 下就会出现 shardindex / profile /
//     session/<会话ID>/ 等文件
func ShardHasSave(shardDir string) bool {
	return dirHasContent(filepath.Join(shardDir, "save"))
}

// dirHasContent 递归判断目录下是否存在至少一个非空条目（文件，或含内容的子目录）。
func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
		if dirHasContent(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}
