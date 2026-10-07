package app

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type File struct {
	SizeKnown bool      `json:"sizeKnown,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	MD5       string    `json:"md5,omitempty"`
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	IsDir     bool      `json:"isDir"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	PickCode  string    `json:"pickCode,omitempty"`
}

type Download struct {
	URL     string
	Headers http.Header
	Local   string
}

var apiClient = &http.Client{Timeout: 45 * time.Second}

func requestJSON(ctx context.Context, method, address string, headers http.Header, body any, out any) error {
	var reader io.Reader
	if body != nil {
		if v, ok := body.(url.Values); ok {
			reader = strings.NewReader(v.Encode())
			headers.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			reader = bytes.NewReader(b)
			headers.Set("Content-Type", "application/json")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return err
	}
	req.Header = headers
	res, err := apiClient.Do(req)
	if err != nil {
		return errors.New("上游连接失败，请检查地址及网络")
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("上游返回 HTTP %d，请检查凭据及权限", res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out)
}

func cloudHeaders(s Storage) http.Header {
	h := http.Header{"User-Agent": {"Mozilla/5.0 quark-cloud-drive/2.5.20"}, "Accept": {"application/json"}}
	if s.Type == "115" {
		h.Set("Cookie", s.Config["cookie"])
		h.Set("User-Agent", pan115UA)
	}
	if s.Type == "quark" {
		h.Set("Cookie", s.Config["cookie"])
		h.Set("Referer", "https://pan.quark.cn/")
	}
	return h
}

func rootOf(s Storage) string {
	if s.Type == "tianyi" {
		if s.Config["root"] != "" && s.Config["root"] != "/" {
			return s.Config["root"]
		}
		return "-11"
	}
	if nativeMobile(s) {
		if s.Config["root"] != "" {
			return s.Config["root"]
		}
		return "/"
	}
	if s.Type == "115" || s.Type == "quark" {
		if s.Config["root"] != "" {
			return s.Config["root"]
		}
		return "0"
	}
	return "/"
}

func relative(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.Contains(part, ":") {
			return "", errors.New("不允许访问上级目录")
		}
	}
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" {
		name = "."
	}
	return name, nil
}

func (a *App) listFiles(ctx context.Context, s Storage, dir string, ttl int, fresh bool) ([]File, error) {
	if dir == "" || dir == "/" {
		dir = rootOf(s)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := s.ID + ":" + dir
	cfg := a.store.snapshotWithLogLimit(0).Settings
	if !fresh && cfg.CacheEnabled {
		if f, ok := a.cache.get(key); ok {
			return f, nil
		}
	}
	generation := a.cache.revision()
	// Coalesce equivalent misses, but never join requests from before a clear.
	callKey := fmt.Sprintf("%s:%d:%d:%t", key, generation, ttl, fresh)
	call, leader := a.cache.begin(callKey)
	if !leader {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return append([]File{}, call.files...), call.err
		}
	}
	var files []File
	var err error
	defer func() { a.cache.finish(callKey, call, files, err) }()
	if s.Type == "openlist" || s.Type == "webdav" {
		if err = a.waitAPI(ctx, s.ID); err != nil {
			return nil, err
		}
	}
	files, err = a.rawList(ctx, s, dir)
	if err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = s.CacheTTL
	}
	if ttl <= 0 {
		ttl = cfg.CacheTTL
	}
	a.cache.putGeneration(key, files, ttl, cfg, &generation)
	return files, nil
}

func (a *App) rawList(ctx context.Context, s Storage, dir string) ([]File, error) {
	if s.Type == "tianyi" {
		return a.tianyiList(ctx, s, dir)
	}
	if nativeMobile(s) {
		host, err := a.mobileHost(ctx, s)
		if err != nil {
			return nil, err
		}
		return a.mobileListAt(ctx, s, host, dir)
	}
	out := []File{}
	switch s.Type {
	case "local":
		name, err := relative(dir)
		if err != nil {
			return nil, err
		}
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return nil, errors.New("无法打开本地根目录")
		}
		defer root.Close()
		f, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		entries, err := f.ReadDir(-1)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			out = append(out, File{ID: path.Join("/", dir, entry.Name()), Name: entry.Name(), IsDir: entry.IsDir(), Size: info.Size(), Modified: info.ModTime()})
		}
	case "openlist", "mobile":
		for page := 1; ; page++ {
			var res struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    struct {
					Content []struct {
						Name     string    `json:"name"`
						Size     int64     `json:"size"`
						IsDir    bool      `json:"is_dir"`
						Modified time.Time `json:"modified"`
					} `json:"content"`
					Total int `json:"total"`
				} `json:"data"`
			}
			err := openlistJSON(ctx, s, "list",
				map[string]any{"path": path.Join("/", s.Config["root"], dir), "password": openlistDirectoryPassword(s), "page": page, "per_page": 200, "refresh": false}, &res)
			if err != nil {
				return nil, err
			}
			if res.Code != 200 {
				return nil, fmt.Errorf("OpenList 接口错误 (%d)", res.Code)
			}
			for _, f := range res.Data.Content {
				out = append(out, File{ID: path.Join("/", dir, f.Name), Name: f.Name, Size: f.Size, IsDir: f.IsDir, Modified: f.Modified})
			}
			if len(res.Data.Content) < 200 || (res.Data.Total > 0 && len(out) >= res.Data.Total) {
				break
			}
			if err := a.waitAPI(ctx, s.ID); err != nil {
				return nil, err
			}
		}
	case "webdav":
		return davList(ctx, s, dir)
	case "quark":
		for page := 1; ; page++ {
			var res struct {
				Code   int `json:"code"`
				Status int `json:"status"`
				Data   struct {
					List []struct {
						ID      string        `json:"fid"`
						Name    string        `json:"file_name"`
						Dir     bool          `json:"dir"`
						Type    int           `json:"file_type"`
						Size    int64         `json:"size"`
						Updated fileTimestamp `json:"updated_at"`
					} `json:"list"`
				} `json:"data"`
			}
			u := "https://drive.quark.cn/1/clouddrive/file/sort?pr=ucpro&fr=pc&_size=200&_fetch_total=1&pdir_fid=" + url.QueryEscape(dir) + "&_page=" + strconv.Itoa(page)
			if err := a.waitAPI(ctx, s.ID); err != nil {
				return nil, err
			}
			if err := requestJSON(ctx, "GET", u, cloudHeaders(s), nil, &res); err != nil {
				return nil, err
			}
			if res.Code != 0 || res.Status >= 400 {
				return nil, errors.New("夸克授权已失效或接口访问被拒绝")
			}
			for _, f := range res.Data.List {
				out = append(out, File{ID: f.ID, Name: f.Name, IsDir: f.Dir || f.Type == 0, Size: f.Size, Modified: f.Updated.Time})
			}
			if len(res.Data.List) < 200 {
				break
			}
		}
	case "115":
		return a.list115(ctx, s, dir)
	default:
		return nil, errors.New("不支持的存储类型")
	}
	return out, nil
}

func scalarFields(data map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		raw := data[key]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			if text != "" {
				return text
			}
			continue
		}
		var number json.Number
		if json.Unmarshal(raw, &number) == nil {
			return number.String()
		}
	}
	return ""
}

func file115(data map[string]json.RawMessage) (File, error) {
	category := scalarFields(data, "file_category", "fc")
	fid := scalarFields(data, "file_id", "fid")
	isDir := category == "0"
	if category == "" && (fid == "" || fid == "0") {
		isDir = true
		fid = scalarFields(data, "cid")
	}
	name := scalarFields(data, "file_name", "fn", "n")
	if fid == "" || name == "" {
		return File{}, errors.New("115 返回了无法识别的目录条目，请检查接口版本")
	}
	size, _ := strconv.ParseInt(scalarFields(data, "fs", "size_byte", "s", "size"), 10, 64)
	return File{ID: fid, Name: name, IsDir: isDir, Size: size, Modified: parseFileTime(scalarFields(data, "user_utime", "upt", "te", "update_time")), PickCode: scalarFields(data, "pick_code", "pickcode", "pc", "code")}, nil
}

func davURL(s Storage, dir string) (string, error) {
	u, err := url.Parse(s.Config["address"])
	if err != nil || u.Host == "" {
		return "", errors.New("WebDAV 地址无效")
	}
	rel, err := relative(dir)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, s.Config["root"], rel)
	return u.String(), nil
}

func davList(ctx context.Context, s Storage, dir string) ([]File, error) {
	address, err := davURL(s, dir)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", address, strings.NewReader(`<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(s.Config["username"], s.Config["password"])
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	res, err := apiClient.Do(req)
	if err != nil {
		return nil, errors.New("WebDAV 连接失败")
	}
	defer res.Body.Close()
	if res.StatusCode != 207 {
		return nil, fmt.Errorf("WebDAV HTTP %d", res.StatusCode)
	}
	var doc struct {
		Responses []struct {
			Href  string `xml:"href"`
			Props []struct {
				Status string `xml:"status"`
				Prop   struct {
					Name       string    `xml:"displayname"`
					Collection *struct{} `xml:"resourcetype>collection"`
					Size       int64     `xml:"getcontentlength"`
					Modified   string    `xml:"getlastmodified"`
				} `xml:"prop"`
			} `xml:"propstat"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	base, _ := url.Parse(address)
	out := []File{}
	for _, r := range doc.Responses {
		href, err := url.Parse(r.Href)
		if err != nil || path.Clean(href.Path) == path.Clean(base.Path) {
			continue
		}
		if path.Dir(strings.TrimRight(href.Path, "/")) != path.Clean(base.Path) {
			continue
		}
		for _, p := range r.Props {
			if !strings.Contains(p.Status, "200") {
				continue
			}
			name := path.Base(strings.TrimRight(href.Path, "/"))
			mod, _ := http.ParseTime(p.Prop.Modified)
			out = append(out, File{ID: path.Join("/", dir, name), Name: name, IsDir: p.Prop.Collection != nil, Size: p.Prop.Size, Modified: mod})
			break
		}
	}
	return out, nil
}

func (a *App) download(ctx context.Context, s Storage, fileID, pick string) (Download, error) {
	if s.Type == "tianyi" {
		return a.tianyiLink(ctx, s, fileID)
	}
	if nativeMobile(s) {
		host, err := a.mobileHost(ctx, s)
		if err != nil {
			return Download{}, err
		}
		return a.mobileLinkAt(ctx, s, host, fileID)
	}
	d := Download{Headers: http.Header{}}
	switch s.Type {
	case "local":
		p, err := relative(fileID)
		d.Local = p
		return d, err
	case "webdav":
		u, err := davURL(s, fileID)
		if err != nil {
			return d, err
		}
		req, _ := http.NewRequest("GET", u, nil)
		req.SetBasicAuth(s.Config["username"], s.Config["password"])
		d.URL, d.Headers = u, req.Header
	case "openlist", "mobile":
		var res struct {
			Code int `json:"code"`
			Data struct {
				URL string `json:"raw_url"`
			} `json:"data"`
		}
		err := openlistJSON(ctx, s, "get",
			map[string]any{"path": path.Join("/", s.Config["root"], fileID), "password": openlistDirectoryPassword(s)}, &res)
		if err != nil {
			return d, err
		}
		if res.Code != 200 {
			return d, fmt.Errorf("OpenList 下载接口错误 (%d)", res.Code)
		}
		d.URL = res.Data.URL
	case "quark":
		var res struct {
			Code int `json:"code"`
			Data []struct {
				URL string `json:"download_url"`
			} `json:"data"`
		}
		err := requestJSON(ctx, "POST", "https://drive.quark.cn/1/clouddrive/file/download?pr=ucpro&fr=pc", cloudHeaders(s), map[string]any{"fids": []string{fileID}}, &res)
		if err != nil {
			return d, err
		}
		if res.Code != 0 || len(res.Data) == 0 {
			return d, errors.New("夸克获取下载链接失败")
		}
		d.URL, d.Headers = res.Data[0].URL, cloudHeaders(s)
	case "115":
		if pick == "" {
			return d, errors.New("缺少 pick_code，请刷新目录后重试")
		}
		c, err := client115(ctx, s)
		if err != nil {
			return d, err
		}
		info, err := c.DownloadWithUA(pick, pan115UA)
		if err != nil {
			return d, err
		}
		d.URL, d.Headers = info.Url.Url, info.Header
	default:
		return d, errors.New("存储类型暂不支持下载")
	}
	u, err := url.Parse(d.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return d, errors.New("上游未返回有效下载链接")
	}
	return d, nil
}
