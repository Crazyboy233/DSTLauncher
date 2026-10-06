// Package workshop 对接 Steam Web API，为面板提供创意工坊模组的
// 详情查询与关键词搜索。
//
// 网络现实（本机实测，2026-10）：steamcommunity.com 的工坊网页在境内
// 不可直连，游戏加速器通常也只放行 Steam 的 CDN 与 api/store 域名；
// 而 api.steampowered.com 恰好可达。因此：
//   - 按 ID 查详情走 ISteamRemoteStorage/GetPublishedFileDetails，免 key；
//   - 关键词搜索走 IPublishedFileService/QueryFiles，需要免费的
//     Steam Web API Key（存 panel.json，可选配置）。
//
// 下载不在这里：拿到 ID 后复用模组预更新链路（-only_update_server_mods），
// 见 server.Manager.UpdateMods 的单模组模式。
package workshop

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Item 是创意工坊物品的展示信息。
type Item struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	ShortDesc   string   `json:"shortDesc,omitempty"`
	PreviewURL  string   `json:"previewUrl,omitempty"`
	FileSize    int64    `json:"fileSize"` // 字节
	Tags        []string `json:"tags,omitempty"`
	TimeUpdated int64    `json:"timeUpdated,omitempty"` // Unix 秒
	Installed   bool     `json:"installed"`
}

const (
	detailsURL = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"
	searchURL  = "https://api.steampowered.com/IPublishedFileService/QueryFiles/v1/"
	// dstAppID 是 DST 创意工坊的 appid（注意不是专用服务器的 343050）。
	dstAppID = "322330"

	userAgent = "dst-windows-panel/1.0"
)

// client 走默认 Transport（自动继承 HTTP_PROXY 等环境变量，
// 游戏加速器改系统代理时同样生效）。
var client = &http.Client{Timeout: 20 * time.Second}

// idRe 匹配 4~15 位数字，覆盖工坊 ID 的实际长度范围。
var idRe = regexp.MustCompile(`[0-9]{4,15}`)

// NormalizeID 从任意用户输入里提取工坊 ID。
// 支持三种写法：纯数字（1392778117）、workshop-数字、创意工坊链接
// （https://steamcommunity.com/sharedfiles/filedetails/?id=1392778117...）。
func NormalizeID(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	if i := strings.Index(input, "id="); i >= 0 {
		input = input[i+3:]
	}
	input = strings.TrimPrefix(input, "workshop-")
	m := idRe.FindString(input)
	return m
}

// NormalizeIDs 批量提取并去重，保持输入顺序。
func NormalizeIDs(inputs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(inputs))
	for _, s := range inputs {
		for _, part := range strings.Split(s, ",") {
			id := NormalizeID(part)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// Details 按 ID 批量查询模组详情（免 key）。
// 单次最多 100 个，超出自动分批；查不到的 ID 不出现在结果里。
func Details(ids []string) ([]Item, error) {
	ids = NormalizeIDs(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]Item, 0, len(ids))
	for len(ids) > 0 {
		batch := ids
		if len(batch) > 100 {
			batch = batch[:100]
		}
		ids = ids[len(batch):]

		form := url.Values{}
		form.Set("itemcount", strconv.Itoa(len(batch)))
		for i, id := range batch {
			form.Set(fmt.Sprintf("publishedfileids[%d]", i), id)
		}
		resp, err := postForm(detailsURL, form)
		if err != nil {
			return nil, fmt.Errorf("查询创意工坊详情失败: %w", err)
		}
		out = append(out, parseDetails(resp)...)
	}
	return out, nil
}

/* ---------- 详情响应 ---------- */

// detailsResp 的字段几乎全是「字符串数字」，解析时统一转 int64。
type detailsResp struct {
	Response struct {
		Result      int             `json:"result"`
		ResultCount int             `json:"resultcount"`
		Details     []rawDetailItem `json:"publishedfiledetails"`
	} `json:"response"`
}

type rawDetailItem struct {
	PublishedFileID string `json:"publishedfileid"`
	Result          int    `json:"result"` // 1 = OK
	Title           string `json:"title"`
	ShortDesc       string `json:"short_description"`
	Description     string `json:"description"`
	FileSize        any    `json:"file_size"` // 字符串或数字
	TimeUpdated     any    `json:"time_updated"`
	PreviewURL      string `json:"preview_url"`
	Tags            []struct {
		Tag string `json:"tag"`
	} `json:"tags"`
}

func parseDetails(body []byte) []Item {
	var dr detailsResp
	if err := json.Unmarshal(body, &dr); err != nil {
		return nil
	}
	out := make([]Item, 0, len(dr.Response.Details))
	for _, d := range dr.Response.Details {
		if d.Result != 1 || d.PublishedFileID == "" {
			continue // 查不到 / 已下架的 ID
		}
		item := Item{
			ID:         d.PublishedFileID,
			Title:      d.Title,
			ShortDesc:  d.ShortDesc,
			PreviewURL: d.PreviewURL,
		}
		if item.ShortDesc == "" {
			item.ShortDesc = firstLine(d.Description)
		}
		item.FileSize = toInt64(d.FileSize)
		item.TimeUpdated = toInt64(d.TimeUpdated)
		for _, t := range d.Tags {
			item.Tags = append(item.Tags, t.Tag)
		}
		out = append(out, item)
	}
	return out
}

// firstLine 取描述的第一行并剥掉 BBCode 标记，作为无短描述时的兜底。
func firstLine(desc string) string {
	desc = strings.TrimSpace(desc)
	if i := strings.IndexAny(desc, "\r\n"); i >= 0 {
		desc = desc[:i]
	}
	var bb strings.Builder
	inTag := false
	for _, r := range desc {
		switch r {
		case '[':
			inTag = true
		case ']':
			inTag = false
		default:
			if !inTag {
				bb.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(bb.String())
}

// toInt64 容忍 Steam 接口里字符串与数字混用的字段。
func toInt64(v any) int64 {
	switch n := v.(type) {
	case string:
		n = strings.TrimSpace(n)
		if n == "" {
			return 0
		}
		x, _ := strconv.ParseInt(n, 10, 64)
		return x
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		x, _ := n.Int64()
		return x
	}
	return 0
}

/* ---------- 关键词搜索 ---------- */

// Search 用关键词搜索创意工坊（需要免费的 Steam Web API Key）。
// page 从 1 开始，每页 20 条。
func Search(apiKey, query string, page int) ([]Item, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("未配置 Steam Web API Key，无法按关键词搜索；可改用「ID / 链接」直接查询")
	}
	if page < 1 {
		page = 1
	}
	q := url.Values{}
	q.Set("key", apiKey)
	q.Set("appid", dstAppID)
	q.Set("query_type", "1") // 按文本匹配
	q.Set("search_text", query)
	q.Set("numperpage", "20")
	q.Set("page", strconv.Itoa(page))
	q.Set("return_short_description", "true")
	q.Set("return_metadata", "true")

	req, err := http.NewRequest(http.MethodGet, searchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := readAll(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("搜索接口返回 HTTP %d: %s", resp.StatusCode, truncateErr(body))
	}

	var sr struct {
		Response struct {
			Details []rawDetailItem `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}
	out := make([]Item, 0, len(sr.Response.Details))
	for _, d := range sr.Response.Details {
		if d.PublishedFileID == "" {
			continue
		}
		item := Item{
			ID:         d.PublishedFileID,
			Title:      d.Title,
			ShortDesc:  d.ShortDesc,
			PreviewURL: d.PreviewURL,
			FileSize:   toInt64(d.FileSize),
		}
		if item.ShortDesc == "" {
			item.ShortDesc = firstLine(d.Description)
		}
		for _, t := range d.Tags {
			item.Tags = append(item.Tags, t.Tag)
		}
		out = append(out, item)
	}
	return out, nil
}

/* ---------- HTTP 工具 ---------- */

func postForm(target string, form url.Values) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readAll(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateErr(body))
	}
	return body, nil
}

func readAll(resp *http.Response) ([]byte, error) {
	return io.ReadAll(resp.Body)
}

func truncateErr(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
