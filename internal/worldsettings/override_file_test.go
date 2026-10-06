package worldsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 落盘路径必须是 <clusterDir>/<shard>/worldgenoverride.lua，
// 否则服务器读不到面板写的配置。
func TestWriteAndReadFile(t *testing.T) {
	clusterDir := t.TempDir()

	o := NewOverrideFor(ShardCaves)
	o.Overrides["world_size"] = "small"
	if err := WriteFile(clusterDir, ShardCaves, o); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	path := filepath.Join(clusterDir, ShardCaves, OverrideFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件未落在预期位置 %s: %v", path, err)
	}

	// 绝不能生成 leveldataoverride.lua：它优先级更高且会覆盖已保存的世界数据
	if _, err := os.Stat(filepath.Join(clusterDir, ShardCaves, "leveldataoverride.lua")); err == nil {
		t.Error("不应生成 leveldataoverride.lua")
	}

	got, exists, err := ReadFile(clusterDir, ShardCaves)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if !exists {
		t.Error("文件已写入，exists 应为 true")
	}
	if got.WorldGenPreset != "DST_CAVE" {
		t.Errorf("洞穴预设错误: %q", got.WorldGenPreset)
	}
	if got.Overrides["world_size"] != "small" {
		t.Errorf("取值未保持: %v", got.Overrides)
	}
}

// 文件不存在时返回该分片的默认配置，且 exists 为 false。
func TestReadMissingFile(t *testing.T) {
	clusterDir := t.TempDir()

	got, exists, err := ReadFile(clusterDir, ShardMaster)
	if err != nil {
		t.Fatalf("缺失文件不应报错: %v", err)
	}
	if exists {
		t.Error("文件不存在时 exists 应为 false")
	}
	if got.WorldGenPreset != "SURVIVAL_TOGETHER" {
		t.Errorf("地表默认预设错误: %q", got.WorldGenPreset)
	}
	if len(got.Overrides) != 0 {
		t.Errorf("默认配置不应带改动项: %v", got.Overrides)
	}
}

// 只写改动过的项：等于默认值的项不应出现在文件里，
// 否则游戏以后新增选项时会被面板保存的旧默认值覆盖。
func TestOnlyChangedOptionsAreWritten(t *testing.T) {
	o := NewOverrideFor(ShardMaster)
	o.Overrides["world_size"] = "huge"
	// 故意塞一个等于默认值的项
	o.Overrides["hounds"] = "default"

	text := o.String()
	if !strings.Contains(text, `world_size = "huge"`) {
		t.Error("改动项未写入")
	}
	if strings.Contains(text, `hounds = "default"`) {
		t.Errorf("等于默认值的项不该写进文件:\n%s", text)
	}
}

// 两个文件互不影响：不同分片写各自的目录。
func TestShardsAreIsolated(t *testing.T) {
	clusterDir := t.TempDir()

	master := NewOverrideFor(ShardMaster)
	master.Overrides["world_size"] = "huge"
	if err := WriteFile(clusterDir, ShardMaster, master); err != nil {
		t.Fatal(err)
	}

	caves := NewOverrideFor(ShardCaves)
	caves.Overrides["caveday"] = "long"
	if err := WriteFile(clusterDir, ShardCaves, caves); err != nil {
		t.Fatal(err)
	}

	gotMaster, _, err := ReadFile(clusterDir, ShardMaster)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := gotMaster.Overrides["caveday"]; ok {
		t.Error("Master 的配置里混入了 Caves 的项")
	}

	gotCaves, _, err := ReadFile(clusterDir, ShardCaves)
	if err != nil {
		t.Fatal(err)
	}
	if gotCaves.Overrides["caveday"] != "long" {
		t.Errorf("Caves 配置读取错误: %v", gotCaves.Overrides)
	}
}
