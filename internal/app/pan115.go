package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
	"github.com/go-resty/resty/v2"
	"golang.org/x/net/publicsuffix"
)

const pan115UA = "Mozilla/5.0 115Browser/27.0.5.7"

// The SDK's errors can contain entire API bodies and its headers contain the
// login Cookie. Keep diagnostics numeric and CDN credentials response-scoped.
func download115(ctx context.Context, s Storage, pick, ua string) (*driver.DownloadInfo, error) {
	c, err := client115(ctx, s)
	if err != nil {
		return nil, err
	}
	endpoint := "chrome/downurl"
	if s.Config["device"] == "android" {
		endpoint = "android/2.0/ufile/download"
	}
	status, code := 0, int64(0)
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	c.Client.OnAfterResponse(func(_ *resty.Client, r *resty.Response) error {
		status = r.StatusCode()
		var envelope struct {
			Errno json.Number `json:"errno"`
			Code  json.Number `json:"code"`
			ErrNo json.Number `json:"errNo"`
		}
		if json.Unmarshal(r.Body(), &envelope) == nil {
			code, _ = envelope.Errno.Int64()
			if code == 0 {
				code, _ = envelope.ErrNo.Int64()
			}
			if code == 0 {
				code, _ = envelope.Code.Int64()
			}
		}
		if r.Request.RawRequest != nil {
			set115DownloadCookies(jar, r.Request.RawRequest.URL, r.Cookies())
		}
		return nil
	})
	var info *driver.DownloadInfo
	if s.Config["device"] == "android" {
		info, err = c.DownloadWithUAByAndroidAPI(pick, ua)
	} else {
		info, err = c.DownloadWithUA(pick, ua)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		reason := "请求或响应解析失败"
		if errors.Is(err, driver.ErrUnexpected) {
			reason = "未知上游错误或空下载记录"
		}
		return nil, fmt.Errorf("115 获取下载链接失败（接口 %s，HTTP %d，错误码 %d）：%s", endpoint, status, code, reason)
	}
	return finish115Download(info, jar, ua, endpoint)
}

func set115DownloadCookies(jar http.CookieJar, origin *url.URL, cookies []*http.Cookie) {
	allowed := []*http.Cookie{}
	for _, cookie := range cookies {
		if cookie == nil {
			continue
		}
		switch strings.ToUpper(cookie.Name) {
		case "UID", "CID", "SEID", "KID":
			continue
		}
		allowed = append(allowed, cookie)
	}
	jar.SetCookies(origin, allowed)
}

func finish115Download(info *driver.DownloadInfo, jar http.CookieJar, ua, endpoint string) (*driver.DownloadInfo, error) {
	if info == nil {
		return nil, errors.New("115 下载接口未返回文件记录")
	}
	u, err := url.Parse(info.Url.Url)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("115 下载接口 %s 未返回有效链接", endpoint)
	}
	info.Header = http.Header{"User-Agent": {ua}}
	req := &http.Request{Header: info.Header}
	for _, cookie := range jar.Cookies(u) {
		req.AddCookie(cookie)
	}
	return info, nil
}

func valid115Device(device string) bool {
	switch device {
	case "web", "android", "ios", "tv", "alipaymini", "wechatmini", "qandroid":
		return true
	}
	return false
}

func credential115(cookie string) (*driver.Credential, error) {
	if strings.ContainsAny(cookie, "\r\n") {
		return nil, errors.New("115 CK 不能包含换行")
	}
	// Browser cookies may contain unrelated values with '=' or a trailing semicolon.
	r := &http.Request{Header: http.Header{"Cookie": {cookie}}}
	values := map[string]string{}
	for _, c := range r.Cookies() {
		values[strings.ToUpper(c.Name)] = c.Value
	}
	cr := &driver.Credential{UID: values["UID"], CID: values["CID"], SEID: values["SEID"], KID: values["KID"]}
	if cr.UID == "" || cr.CID == "" || cr.SEID == "" {
		return nil, errors.New("115 请填写包含 UID、CID、SEID 的 CK；旧 Open Token 不能用于此驱动")
	}
	return cr, nil
}

func new115Client(ctx context.Context) *driver.Pan115Client {
	c := driver.New().SetHttpClient(&http.Client{
		Transport: apiClient.Transport, Timeout: 45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}).SetUserAgent(pan115UA)
	c.Client.OnBeforeRequest(func(_ *resty.Client, r *resty.Request) error {
		r.SetContext(ctx)
		return ctx.Err()
	})
	return c
}

func client115(ctx context.Context, s Storage) (*driver.Pan115Client, error) {
	device := s.Config["device"]
	if device != "" && !valid115Device(device) {
		return nil, errors.New("无效的 115 CK 设备类型")
	}
	cr, err := credential115(s.Config["cookie"])
	if err != nil {
		return nil, err
	}
	c := new115Client(ctx).ImportCredential(cr)
	// UID begins with the account ID. Avoid the disruptive LoginCheck endpoint.
	c.UserID, _ = strconv.ParseInt(strings.Split(cr.UID, "_")[0], 10, 64)
	return c, nil
}

func (a *App) list115(ctx context.Context, s Storage, dir string) ([]File, error) {
	if dir == "" || dir == "/" {
		dir = rootOf(s)
	}
	c, err := client115(ctx, s)
	if err != nil {
		return nil, err
	}
	out := []File{}
	for offset := int64(0); ; offset += 200 {
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return nil, err
		}
		page, err := driver.GetFiles(c.NewRequest().ForceContentType("application/json"), dir, driver.WithOffset(offset), driver.WithLimit(200))
		if err != nil {
			return nil, err
		}
		if string(page.CategoryID) != dir {
			return nil, errors.New("115 返回的目录与请求不一致，请刷新后重试")
		}
		for _, raw := range page.Files {
			f := (&driver.File{}).From(&raw)
			out = append(out, File{ID: f.FileID, Name: f.Name, IsDir: f.IsDirectory, Size: f.Size, Modified: f.UpdateTime, Created: f.CreateTime, PickCode: f.PickCode})
		}
		if offset+int64(len(page.Files)) >= int64(page.Count) {
			return out, nil
		}
		if len(page.Files) == 0 {
			return nil, errors.New("115 目录分页中断，请重试")
		}
	}
}

func submit115URIs(ctx context.Context, s Storage, parent string, urls []string) (results []offlineResult) {
	for _, address := range urls {
		item := offlineResult{Name: address}
		c, err := client115(ctx, s)
		if err == nil {
			var hashes []string
			hashes, err = c.AddOfflineTaskURIs([]string{address}, parent)
			if err == nil && (len(hashes) != 1 || hashes[0] == "") {
				err = errors.New("115 未确认离线任务，请检查云下载权益或链接")
			}
		}
		item.Success = err == nil
		item.Message = "已提交到 115 云下载"
		if err != nil {
			item.Message = err.Error()
		}
		results = append(results, item)
	}
	return results
}
