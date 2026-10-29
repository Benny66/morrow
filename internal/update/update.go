// Package update 检查 GitHub Releases 上的最新稳定版本。
//
// M1 只读版本元数据（tag / 说明 / 发布页），不下载、不安装（见 ADR 0013）。
// 检查有两条通道：GitHub API（带说明与发布页）与 releases/latest 的 302
// （只有 tag）。后者是前者的降级路径——api.github.com 在部分网络下不可达，
// 而 github.com 往往还能通。
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultRepo 是发布仓库（owner/repo）。
	DefaultRepo = "Benny66/morrow"
	// DefaultAPIBase / DefaultWebBase 是两条检查通道的根地址。
	DefaultAPIBase = "https://api.github.com"
	DefaultWebBase = "https://github.com"
	// requestTimeout 是单次检查的超时；调用方（自动检查）另有整体截止时间。
	requestTimeout = 10 * time.Second
	// maxBodyBytes 防止异常响应把内存打满（release 说明可能很长，1 MiB 足够）。
	maxBodyBytes = 1 << 20
)

// Release 是一次检查得到的发布元数据。Version 不带 v 前缀。
type Release struct {
	Version string
	Notes   string
	URL     string
}

// State 是下发给前端的检查结果。
type State struct {
	Available bool   `json:"available"`
	AutoCheck bool   `json:"auto_check"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Notes     string `json:"notes,omitempty"`
	URL       string `json:"url,omitempty"`
	Skipped   bool   `json:"skipped"`
	CheckedAt int64  `json:"checked_at"`
}

// Version 是数字点分的三段版本号。
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion 解析 "1.2.3" 或 "v1.2.3"；带预发布后缀（-rc1）的一律视为不可比较。
func ParseVersion(s string) (Version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" || strings.ContainsAny(s, "-+") {
		return Version{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Version{}, false
	}
	var nums [3]int
	for i, p := range parts {
		if p == "" {
			return Version{}, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, false
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, true
}

// IsNewer 判断 latest 是否严格新于 current；任一版本不可解析时返回 false，
// 即「无法比较就不提示更新」。
func IsNewer(latest, current string) bool {
	l, ok := ParseVersion(latest)
	if !ok {
		return false
	}
	c, ok := ParseVersion(current)
	if !ok {
		return false
	}
	switch {
	case l.Major != c.Major:
		return l.Major > c.Major
	case l.Minor != c.Minor:
		return l.Minor > c.Minor
	default:
		return l.Patch > c.Patch
	}
}

// Client 是 GitHub Releases 的只读客户端。
type Client struct {
	Repo      string
	APIBase   string
	WebBase   string
	UserAgent string
	HTTP      *http.Client
}

// NewClient 构造检查器。userAgent 必须非空（GitHub API 拒绝无 UA 的请求）。
func NewClient(repo, userAgent string) *Client {
	return &Client{
		Repo:      repo,
		APIBase:   DefaultAPIBase,
		WebBase:   DefaultWebBase,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: requestTimeout},
	}
}

// Latest 返回最新稳定发布；没有发布时返回 (nil, nil)。
//
// 先走 API（能拿到说明与发布页），失败再走 releases/latest 的 302（只拿 tag）。
// 两条都失败才报错，错误里同时保留两段原因，便于日志定位。
func (c *Client) Latest(ctx context.Context) (*Release, error) {
	apiRelease, apiErr := c.latestFromAPI(ctx)
	if apiErr == nil {
		return apiRelease, nil
	}
	webRelease, webErr := c.latestFromWeb(ctx)
	if webErr == nil {
		return webRelease, nil
	}
	return nil, fmt.Errorf("GitHub API 检查失败（%v），releases/latest 回退也失败（%v）", apiErr, webErr)
}

type apiRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func (c *Client) latestFromAPI(ctx context.Context) (*Release, error) {
	endpoint := strings.TrimRight(c.APIBase, "/") + "/repos/" + c.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var payload apiRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	if payload.Draft || payload.Prerelease {
		return nil, nil
	}
	version := strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")
	if _, ok := ParseVersion(version); !ok {
		return nil, fmt.Errorf("最新发布的 tag %q 不是数字点分版本，已跳过", payload.TagName)
	}
	return &Release{Version: version, Notes: payload.Body, URL: payload.HTMLURL}, nil
}

func (c *Client) latestFromWeb(ctx context.Context) (*Release, error) {
	endpoint := strings.TrimRight(c.WebBase, "/") + "/" + c.Repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	// 不跟随跳转：Location 里的 tag 就是版本号，跟随过去要下载整页 HTML。
	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d（期望跳转）", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return nil, fmt.Errorf("跳转响应缺少 Location")
	}
	resolved, err := resp.Request.URL.Parse(location)
	if err != nil {
		return nil, fmt.Errorf("解析跳转地址失败: %w", err)
	}
	path := strings.TrimSuffix(resolved.Path, "/")
	const marker = "/releases/tag/"
	idx := strings.LastIndex(path, marker)
	if idx < 0 {
		return nil, fmt.Errorf("跳转地址不是 releases/tag：%s", resolved.String())
	}
	tag, err := url.PathUnescape(path[idx+len(marker):])
	if err != nil {
		return nil, fmt.Errorf("解析 tag 失败: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if _, ok := ParseVersion(version); !ok {
		return nil, fmt.Errorf("tag %q 不是数字点分版本", tag)
	}
	return &Release{Version: version, URL: resolved.String()}, nil
}
