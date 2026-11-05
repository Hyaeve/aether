package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"aether/internal/linkcore/config"
)

// Dashboard clients reuse each provider's authentication, including the fnOS
// login exchange. They never follow a credential-bearing request to another URL.
func NewDashboard(cfg config.Upstream) (Provider, error) {
	p, err := New(cfg)
	if err != nil {
		return nil, err
	}
	c := dashboardClient(p)
	if c == nil {
		return nil, errors.New("不支持的媒体服务")
	}
	c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return p, nil
}
func dashboardClient(p Provider) *apiClient {
	switch p := p.(type) {
	case *embyProvider:
		return p.client
	case *fnosProvider:
		return p.client
	case *absProvider:
		return p.client
	}
	return nil
}

var dashboardEndpoint = regexp.MustCompile(`^(/(Items|Users|Sessions|Library/VirtualFolders)(/[A-Za-z0-9_-]+){0,3}|/Shows/[A-Za-z0-9_-]+/(Episodes|Seasons)|/api/(libraries|items|users|sessions|me)(/[A-Za-z0-9_-]+){0,3})$`)

func DashboardRead(ctx context.Context, p Provider, endpoint string, q url.Values, out any) error {
	c := dashboardClient(p)
	if c == nil || !dashboardEndpoint.MatchString(endpoint) {
		return errors.New("媒体读取范围无效")
	}
	if err := c.getJSON(ctx, endpoint, q, out); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("媒体接口读取失败，请检查以链授权及服务接口支持")
	}
	return nil
}
func DashboardImage(ctx context.Context, p Provider, itemID, kind string) ([]byte, string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`).MatchString(itemID) || (kind != "Primary" && kind != "Backdrop") {
		return nil, "", errors.New("媒体图片标识无效")
	}
	c := dashboardClient(p)
	if c == nil {
		return nil, "", errors.New("媒体服务不可用")
	}
	endpoint := "/Items/" + itemID + "/Images/" + kind
	if p.Type() == config.UpstreamAudiobookshelf {
		endpoint = "/api/items/" + itemID + "/cover"
	}
	token, err := c.credentials(ctx)
	if err != nil {
		return nil, "", errors.New("媒体图片授权不可用")
	}
	resp, err := c.sendJSONGet(ctx, endpoint, url.Values{"maxWidth": {"1280"}, "format": {"webp"}}, token)
	if err != nil {
		return nil, "", errors.New("媒体图片读取失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, "", errors.New("媒体图片不可用")
	}
	ct := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
	switch ct {
	case "image/jpeg", "image/png", "image/webp", "image/avif", "image/gif":
	default:
		return nil, "", errors.New("媒体图片格式不支持")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(b) > 8<<20 {
		return nil, "", errors.New("媒体图片读取失败或过大")
	}
	return b, ct, nil
}
