package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ShardHasSave 的判据必须能区分这两种真实情况：
//   - 建好房间但从未跑过：save/ 下只有一个空的 session/  → false
//   - 服务器加载过世界：save/ 下出现 shardindex / profile / session/<会话ID>/ → true
func TestShardHasSave(t *testing.T) {
	cases := []struct {
		name  string
		setup func(shardDir string)
		want  bool
	}{
		{
			name:  "分片目录不存在",
			setup: func(string) {},
			want:  false,
		},
		{
			name: "只有空的 save",
			setup: func(d string) {
				mkdir(t, filepath.Join(d, "save"))
			},
			want: false,
		},
		{
			name: "空的 save/session（刚建好房间、没跑过）",
			setup: func(d string) {
				mkdir(t, filepath.Join(d, "save", "session"))
			},
			want: false,
		},
		{
			name: "save 下直接有文件",
			setup: func(d string) {
				mkdir(t, filepath.Join(d, "save"))
				write(t, filepath.Join(d, "save", "shardindex"), "x")
			},
			want: true,
		},
		{
			name: "session 下有会话目录（真实存档）",
			setup: func(d string) {
				mkdir(t, filepath.Join(d, "save", "session", "1A276D0C417B57EC"))
				write(t, filepath.Join(d, "save", "session", "1A276D0C417B57EC", "0000000002.meta"), "{}")
			},
			want: true,
		},
		{
			name: "会话目录是空的（世界生成失败过）",
			setup: func(d string) {
				mkdir(t, filepath.Join(d, "save", "session", "1A276D0C417B57EC"))
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shardDir := filepath.Join(t.TempDir(), "Master")
			mkdir(t, shardDir)
			tc.setup(shardDir)

			if got := ShardHasSave(shardDir); got != tc.want {
				t.Errorf("ShardHasSave = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
