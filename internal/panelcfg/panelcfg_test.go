package panelcfg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	want := Config{
		ServerDir: `F:\steam\steamapps\common\Don't Starve Together Dedicated Server`,
		Arch:      "64",
	}
	if err := Save(dir, want); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	got := Load(dir)
	if got != want {
		t.Errorf("往返后不一致：\n得到 %+v\n期望 %+v", got, want)
	}
}

// 文件不存在时应返回零值（等同「自动探测」），而不是报错。
func TestLoadMissingFile(t *testing.T) {
	got := Load(t.TempDir())
	if got != (Config{}) {
		t.Errorf("文件不存在时应返回零值，实际 %+v", got)
	}
}

// 内容损坏时也不能让面板起不来：回到零值即可。
func TestLoadCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{ this is not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := Load(dir); got != (Config{}) {
		t.Errorf("内容损坏时应返回零值，实际 %+v", got)
	}
}

// 用户手工编辑时可能带上 BOM 或多余空白，要能容忍。
func TestLoadTolerantOfBOMAndWhitespace(t *testing.T) {
	dir := t.TempDir()
	content := "\xEF\xBB\xBF" + "{\n  \"serverDir\": \"  D:\\\\dst  \",\n  \"arch\": \" 32 \"\n}\n"
	if err := os.WriteFile(Path(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got := Load(dir)
	if got.ServerDir != `D:\dst` {
		t.Errorf("serverDir 未正确去空白: %q", got.ServerDir)
	}
	if got.Arch != "32" {
		t.Errorf("arch 未正确去空白: %q", got.Arch)
	}
}

// 保存必须原子（先写临时文件再改名），不能留下 .tmp 残骸。
func TestSaveLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, Config{Arch: "auto"}); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}

// Exists 用来判断「用户是否在本机配置过」：
// 它必须能区分「文件不存在」与「文件存在但内容为空值」——
// 后者代表用户明确选择了自动探测，不能再回落到旧来源。
func TestExistsDistinguishesEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	if Exists(dir) {
		t.Error("文件不存在时 Exists 应为 false")
	}

	// 明确写入空配置（= 自动探测）
	if err := Save(dir, Config{ServerDir: "", Arch: "auto"}); err != nil {
		t.Fatal(err)
	}
	if !Exists(dir) {
		t.Error("文件存在时 Exists 应为 true（即使内容为空值）")
	}
	if got := Load(dir); got.ServerDir != "" {
		t.Errorf("空配置应读出空 ServerDir，实际 %q", got.ServerDir)
	}
}
