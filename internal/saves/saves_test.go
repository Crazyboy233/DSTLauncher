package saves

import (
	"os"
	"path/filepath"
	"testing"

	"dst-windows/internal/room"
)

// 造一个集群目录：<root>/DST/Cluster_N/<shard>/{server.ini,save/<file>}
func writeCluster(t *testing.T, root, cluster string, shards ...string) {
	t.Helper()
	for _, sh := range shards {
		dir := filepath.Join(root, "DST", cluster, sh)
		if err := os.MkdirAll(filepath.Join(dir, "save"), 0755); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "server.ini"), []byte("[SHARD]\n"), 0644); err != nil {
			t.Fatalf("写 server.ini 失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "save", "shardindex"), []byte("x"), 0644); err != nil {
			t.Fatalf("写存档失败: %v", err)
		}
	}
}

func TestScanMarksOrphanAndRunning(t *testing.T) {
	root := t.TempDir()
	writeCluster(t, root, "Cluster_1", "Master", "Caves")
	writeCluster(t, root, "Cluster_2", "Master")

	rooms := []*room.Room{{ID: 1, Name: "主房间"}}
	list, err := Scan(root, "DST", rooms, map[string]bool{"Cluster_2": true})
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 份存档，实际 %d", len(list))
	}

	// 升序：Cluster_1 在前
	if list[0].ID != 1 || list[1].ID != 2 {
		t.Errorf("排序不正确: %+v", list)
	}

	// Cluster_1 关联到房间
	if !list[0].RoomExists || list[0].RoomName != "主房间" {
		t.Errorf("房间归属错误: %+v", list[0])
	}
	if len(list[0].Shards) != 2 || !list[0].HasSave {
		t.Errorf("分片扫描错误: %+v", list[0])
	}

	// Cluster_2 是孤儿，且正在运行
	if list[1].RoomExists || list[1].RoomName != "" {
		t.Errorf("应识别为孤儿存档: %+v", list[1])
	}
	if !list[1].Running {
		t.Errorf("应标记为运行中: %+v", list[1])
	}
	if list[1].Size <= 0 {
		t.Errorf("大小未统计: %+v", list[1])
	}
}

// 目录不存在（还没开过服）不该报错。
func TestScanMissingRoot(t *testing.T) {
	list, err := Scan(filepath.Join(t.TempDir(), "nope"), "DST", nil, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("应为空列表: %+v", list)
	}
}

// 删除只允许 Cluster_<数字>，且路径必须落在存档根目录下。
func TestDeleteGuards(t *testing.T) {
	root := t.TempDir()
	writeCluster(t, root, "Cluster_1", "Master")

	cases := []string{
		"",
		"..",
		"Cluster_1/../../etc",
		"Cluster_abc",
		"Cluster_1/Master",
		"other",
	}
	for _, c := range cases {
		if err := Delete(root, "DST", c); err == nil {
			t.Errorf("非法目录名 %q 未被拒绝", c)
		}
	}

	if err := Delete(root, "DST", "Cluster_1"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "DST", "Cluster_1")); !os.IsNotExist(err) {
		t.Error("目录仍然存在")
	}
}
