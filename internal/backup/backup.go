// Package backup 实现存档的备份、列表、清理与回滚。
//
// 备份粒度是「整个集群目录」(<root>/<confDir>/Cluster_N)，一次性覆盖
// cluster.ini、adminlist/whitelist/blocklist、Master 与 Caves 两个分片，
// 因此不存在"只备份了地表、洞穴没备份"的问题。
//
// 关键设计：备份前必须先发 c_save() 并等待落盘完成。
// 直接拷贝 save 目录会拿到写了一半的存档，回滚后世界状态损坏。
// 该动作由调用方（Web 层）通过 server.Manager.SaveCluster 完成。
package backup

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dst-windows/internal/config"
)

// Manager 管理备份的创建、列表与清理。
type Manager struct {
	rootDir     string
	confDir     string
	clusterName string // Cluster_N
	clusterDir  string // <root>/<confDir>/Cluster_N
	backupDir   string // <root>/backups/Cluster_N
	keepCount   int    // 保留最近 N 份，超出自动清理
}

// NewManager 创建备份管理器。
// rootDir 对应 -persistent_storage_root，confDir 对应 -conf_dir。
// 备份目录刻意放在 clusterDir 之外，避免被下次备份包进自己内部。
func NewManager(rootDir, confDir string, clusterID int) *Manager {
	clusterName := fmt.Sprintf("Cluster_%d", clusterID)
	return &Manager{
		rootDir:     rootDir,
		confDir:     confDir,
		clusterName: clusterName,
		clusterDir:  config.ClusterDir(rootDir, confDir, clusterID),
		backupDir:   filepath.Join(rootDir, "backups", clusterName),
		keepCount:   10,
	}
}

// BackupItem 描述一个备份文件。
type BackupItem struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	Path      string    `json:"path"`
}

// hasSave 判断集群下是否已经生成过世界存档（任一分片有实际存档内容）。
// 没有存档时拒绝生成空备份，避免用户以为备份成功、实际什么都没存。
func (m *Manager) hasSave() bool {
	entries, err := os.ReadDir(m.clusterDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if config.ShardHasSave(filepath.Join(m.clusterDir, e.Name())) {
			return true
		}
	}
	return false
}

// Create 创建一个新的备份：压缩整个集群目录为 zip。
func (m *Manager) Create() (*BackupItem, error) {
	src := m.clusterDir
	if _, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("集群目录不存在: %s（服务器可能尚未运行过）", src)
	}
	if !m.hasSave() {
		return nil, fmt.Errorf("尚未生成世界存档，暂无可备份内容")
	}

	if err := os.MkdirAll(m.backupDir, 0755); err != nil {
		return nil, err
	}

	name := fmt.Sprintf("%s_%s.zip", m.clusterName, time.Now().Format("20060102_150405"))
	dst := filepath.Join(m.backupDir, name)

	if err := m.zipDir(src, dst); err != nil {
		// 备份失败时清理半成品文件，避免占用磁盘
		_ = os.Remove(dst)
		return nil, err
	}

	fi, err := os.Stat(dst)
	if err != nil {
		return nil, err
	}

	item := &BackupItem{
		Name:      name,
		Size:      fi.Size(),
		CreatedAt: fi.ModTime(),
		Path:      dst,
	}

	// 备份成功后自动清理超出保留份数的旧备份
	if err := m.Clean(); err != nil {
		// 清理失败不影响本次备份结果
		fmt.Printf("清理旧备份失败: %v\n", err)
	}

	return item, nil
}

// zipDir 将目录递归压缩为 zip。
func (m *Manager) zipDir(srcDir, dstPath string) error {
	f, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	// srcDir 必须以分隔符结尾，否则 filepath.Walk 会把父目录也遍历进来
	err = filepath.Walk(srcDir+string(os.PathSeparator), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// zip 条目统一用正斜杠，保证压缩包跨平台可解
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()

		_, err = io.Copy(w, src)
		return err
	})
	if err != nil {
		_ = zw.Close()
		return fmt.Errorf("压缩失败: %w", err)
	}

	return zw.Close()
}

// List 返回所有备份，按时间倒序。
func (m *Manager) List() ([]BackupItem, error) {
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []BackupItem{}, nil
		}
		return nil, err
	}

	var out []BackupItem
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BackupItem{
			Name:      e.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
			Path:      filepath.Join(m.backupDir, e.Name()),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Clean 删除超出保留份数的旧备份。
func (m *Manager) Clean() error {
	items, err := m.List()
	if err != nil {
		return err
	}
	if len(items) <= m.keepCount {
		return nil
	}
	// 保留最新的 keepCount 份，删除更早的
	for _, item := range items[m.keepCount:] {
		if err := os.Remove(item.Path); err != nil {
			return fmt.Errorf("删除旧备份失败: %w", err)
		}
	}
	return nil
}

// Delete 删除指定备份。
func (m *Manager) Delete(name string) error {
	// 校验文件名，防止路径穿越
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".zip") {
		return fmt.Errorf("非法的备份文件名")
	}
	return os.Remove(filepath.Join(m.backupDir, name))
}

// Restore 从备份回滚整个集群目录。
// 注意：调用方必须确保服务器已停止，否则文件占用会导致解压失败，
// 且运行中的服务器会在退出时用内存状态覆盖刚恢复的存档。
func (m *Manager) Restore(name string) error {
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".zip") {
		return fmt.Errorf("非法的备份文件名")
	}
	zipPath := filepath.Join(m.backupDir, name)
	if _, err := os.Stat(zipPath); err != nil {
		return fmt.Errorf("备份不存在: %s", name)
	}

	dstDir := m.clusterDir
	// 旧存档先改名保留而非直接删除，回滚失败时可人工恢复
	oldDir := dstDir + ".old"
	_ = os.RemoveAll(oldDir)
	if _, err := os.Stat(dstDir); err == nil {
		if err := os.Rename(dstDir, oldDir); err != nil {
			return fmt.Errorf("备份现有存档失败: %w", err)
		}
	}

	if err := unzipTo(zipPath, dstDir); err != nil {
		// 解压失败则把旧存档恢复回去
		_ = os.Rename(oldDir, dstDir)
		return fmt.Errorf("解压备份失败: %w", err)
	}

	return nil
}

// unzipTo 解压 zip 到目标目录，并防止 Zip Slip 路径穿越攻击。
func unzipTo(zipPath, dstDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	absDst, err := filepath.Abs(dstDir)
	if err != nil {
		return err
	}

	for _, f := range r.File {
		// 校验解压目标仍在目标目录内
		target := filepath.Join(absDst, filepath.FromSlash(f.Name))
		if !strings.HasPrefix(target, absDst+string(os.PathSeparator)) && target != absDst {
			return fmt.Errorf("备份包中存在非法路径: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}

		src, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
