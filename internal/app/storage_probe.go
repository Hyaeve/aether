package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func (a *App) probeStorage(ctx context.Context, s Storage) error {
	switch {
	case s.Type == "115":
		c, err := client115(ctx, s)
		if err != nil {
			return err
		}
		if err = a.waitAPI(ctx, s.ID); err != nil {
			return err
		}
		_, err = driver.GetFiles(c.NewRequest().ForceContentType("application/json"), rootOf(s), driver.WithLimit(1))
		if errors.Is(err, driver.ErrNotLogin) || errors.Is(err, driver.ErrBadCookie) {
			return authStorageError(errors.New("115 CK已失效，请重新扫码"), true)
		}
		return err
	case s.Type == "quark":
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return err
		}
		var result struct {
			Code   int `json:"code"`
			Status int `json:"status"`
			Data   *struct {
				List []any `json:"list"`
			} `json:"data"`
		}
		address := "https://drive.quark.cn/1/clouddrive/file/sort?pr=ucpro&fr=pc&_size=1&_fetch_total=1&_page=1&pdir_fid=" + url.QueryEscape(rootOf(s))
		if err := requestJSON(ctx, "GET", address, cloudHeaders(s), nil, &result); err != nil {
			return err
		}
		if result.Code == 41001 || result.Code == 31001 || result.Status == 401 || result.Status == 403 {
			return authStorageError(errors.New("夸克CK已失效或无访问权限"), true)
		}
		if result.Code != 0 || result.Status >= 400 || result.Data == nil {
			return errors.New("夸克健康检查返回异常")
		}
		return nil
	case nativeMobile(s):
		host, err := a.mobileHost(ctx, s)
		if err != nil {
			return err
		}
		var result struct {
			Items []any `json:"items"`
			Files []any `json:"fileList"`
		}
		err = a.mobilePost(ctx, s, host+"/file/list", map[string]any{"parentFileId": rootOf(s), "orderBy": "updated_at", "orderDirection": "DESC", "pageInfo": map[string]any{"pageSize": 1, "pageCursor": ""}}, false, &result)
		if err == nil && result.Items == nil && result.Files == nil {
			return errors.New("移动健康检查目录响应不完整")
		}
		return err
	case nativeTianyi(s):
		var result struct {
			List *struct{} `json:"fileListAO"`
			XML  *struct{} `xml:"fileList"`
		}
		err := a.tianyiRequest(ctx, s, "GET", tianyiAPI+"/listFiles.action", url.Values{"folderId": {rootOf(s)}, "fileType": {"0"}, "pageNum": {"1"}, "pageSize": {"1"}, "recursive": {"0"}, "orderBy": {"filename"}}, &result)
		if err != nil {
			return err
		}
		if result.List == nil && result.XML == nil {
			return errors.New("天翼健康检查返回异常")
		}
		return nil
	case s.Type == "local":
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return err
		}
		defer root.Close()
		name, err := relative(rootOf(s))
		if err != nil {
			return err
		}
		f, err := root.Open(name)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.ReadDir(1)
		if err == io.EOF {
			return nil
		}
		return err
	case s.Type == "webdav":
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return err
		}
		address, err := davURL(s, rootOf(s))
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, "PROPFIND", address, strings.NewReader(`<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`))
		if err != nil {
			return err
		}
		req.Header.Set("Depth", "0")
		req.Header.Set("Content-Type", "application/xml")
		req.SetBasicAuth(s.Config["username"], s.Config["password"])
		client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := client.Do(req)
		if err != nil {
			return errors.New("WebDAV健康检查连接失败")
		}
		defer res.Body.Close()
		if res.StatusCode == 401 || res.StatusCode == 403 {
			return authStorageError(errors.New("WebDAV认证或访问权限异常"), false)
		}
		if res.StatusCode != 207 {
			return errors.New("WebDAV健康检查返回异常")
		}
		return nil
	case s.Type == "openlist" || s.Type == "mobile":
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return err
		}
		var result struct {
			Code int `json:"code"`
		}
		err := openlistJSON(ctx, s, "list", map[string]any{"path": path.Join("/", s.Config["root"]), "password": openlistDirectoryPassword(s), "page": 1, "per_page": 1, "refresh": false}, &result)
		if err != nil {
			return err
		}
		if result.Code == 401 || result.Code == 403 {
			return authStorageError(errors.New("上游认证或访问权限异常"), false)
		}
		if result.Code != 200 {
			return errors.New("上游健康检查返回异常")
		}
		return nil
	}
	return errors.New("不支持的存储类型")
}
