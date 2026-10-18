package workshop

import "testing"

func TestParseACF(t *testing.T) {
	text := `"AppWorkshop"
{
	"appid"		"322330"
	"SizeOnDisk"		"1413589489"
	"WorkshopItemsInstalled"
	{
		"1207269058"
		{
			"size"		"751551"
			"timeupdated"		"1663424752"
			"manifest"		"1414249909561468204"
		}
		"1392778117"
		{
			"size"		"110660380"
			"timeupdated"		"1790864417"
			"manifest"		"8145995609209102369"
		}
	}
}`

	got, err := ParseACF(text)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望 2 条记录，实际 %d", len(got))
	}

	a := got["1207269058"]
	if a.Size != 751551 || a.TimeUpdated != 1663424752 {
		t.Errorf("第一条记录解析错误: %+v", a)
	}
	if a.Manifest != "1414249909561468204" {
		t.Errorf("manifest 解析错误: %q", a.Manifest)
	}
	if got["1392778117"].TimeUpdated != 1790864417 {
		t.Errorf("第二条记录解析错误: %+v", got["1392778117"])
	}
}

// 一个模组都没装时返回空表，不报错。
func TestParseACFNoItems(t *testing.T) {
	got, err := ParseACF("\"AppWorkshop\"\n{\n\t\"appid\"\t\"322330\"\n}\n")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("应为空表: %+v", got)
	}
}

// 文件不存在时返回空表（从没下载过工坊模组）。
func TestLoadInstalledMissing(t *testing.T) {
	got, err := LoadInstalled(t.TempDir())
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("应为空表: %+v", got)
	}
}
