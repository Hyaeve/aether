package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Protocol fields follow the 139Strm personal_new client; see THIRD_PARTY_NOTICES.md.
func nativeMobile(s Storage) bool {
	return s.Type == "mobile" && s.Config["mode"] == "native"
}

func mobileAccount(s Storage) (string, string, error) {
	auth := strings.TrimSpace(s.Config["authorization"])
	if len(auth) >= 6 && strings.EqualFold(auth[:6], "Basic ") {
		auth = strings.TrimSpace(auth[6:])
	}
	raw, err := base64.StdEncoding.DecodeString(auth)
	parts := strings.SplitN(string(raw), ":", 3)
	if err != nil || len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", errors.New("移动云盘 Authorization 应为 pc:账号:令牌 的 Base64")
	}
	return parts[1], auth, nil
}

func mobileMD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func mobileSign(body, timestamp, nonce string) string {
	encoded := url.QueryEscape(body)
	encoded = strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*").Replace(encoded)
	chars := strings.Split(encoded, "")
	sort.Strings(chars)
	sorted := base64.StdEncoding.EncodeToString([]byte(strings.Join(chars, "")))
	return strings.ToUpper(mobileMD5(mobileMD5(sorted) + mobileMD5(timestamp+":"+nonce)))
}

func (a *App) mobilePost(ctx context.Context, s Storage, address string, payload any, pc bool, out any) error {
	if err := a.waitAPI(ctx, s.ID); err != nil {
		return err
	}
	account, auth, err := mobileAccount(s)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", address, bytes.NewReader(body))
	if err != nil {
		return err
	}
	timestamp := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	nonce := id()[:16]
	device := "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||"
	headers := map[string]string{
		"Authorization": "Basic " + auth, "Content-Type": "application/json", "Accept": "application/json",
		"User-Agent": "Mozilla/5.0", "Caller": "web", "Cms-Device": "default",
		"Mcloud-Channel": "1000101", "Mcloud-Client": "10701", "Mcloud-Route": "001",
		"Mcloud-Version": "7.14.0", "Mcloud-Sign": timestamp + "," + nonce + "," + mobileSign(string(body), timestamp, nonce),
		"x-DeviceInfo": device, "x-huawei-channelSrc": "10000034", "x-inner-ntwk": "2",
		"x-m4c-caller": "PC", "x-m4c-src": "10002", "x-SvcType": "1",
		"X-Yun-Api-Version": "v1", "X-Yun-App-Channel": "10000034", "X-Yun-Channel-Source": "10000034",
		"X-Yun-Client-Info": device + "dW5kZWZpbmVk||", "X-Yun-Module-Type": "100", "X-Yun-Svc-Type": "1",
		"Origin": "https://yun.139.com", "Referer": "https://yun.139.com/w/", "Inner-Hcy-Router-Https": "1",
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if pc {
		deviceID := "OPENLIST" + strings.ToUpper(mobileMD5(account)[:16]) + "-PC"
		device = "||11|8.7.2.20260519|PC|QkYtMjAyMDAzMTAxNjQ3|" + deviceID + "|| Windows 10 (10.0)|1920X1040|Q2hpbmVzZSAoU2ltcGxpZmllZCk=|||"
		for name, value := range map[string]string{
			"x-DeviceInfo": device, "x-huawei-channelSrc": "10200153", "x-MM-Source": "000",
			"x-yun-app-channel": "10200153", "x-yun-client-info": device, "x-yun-device-id": deviceID,
			"x-yun-device-info": device, "x-yun-market-source": "000", "x-yun-op-type": "1",
			"x-ExpRoute-Code": "routeCode=" + account + ",type=2",
		} {
			request.Header.Set(name, value)
		}
	}
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("移动云盘连接失败")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("移动云盘 HTTP %d，请检查授权或网络", response.StatusCode)
	}
	var result struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&result); err != nil {
		return errors.New("移动云盘响应格式异常")
	}
	if !result.Success {
		return errors.New("移动云盘拒绝请求，请检查授权有效期、账号权益或目录权限")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(result.Data, out)
}

func validMobileHost(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		u.Port() == "" && strings.HasSuffix(u.Hostname(), ".yun.139.com")
}

func (a *App) mobileHost(ctx context.Context, s Storage) (string, error) {
	account, _, err := mobileAccount(s)
	if err != nil {
		return "", err
	}
	var data struct {
		Routes []struct {
			Name string `json:"modName"`
			URL  string `json:"httpsUrl"`
		} `json:"routePolicyList"`
	}
	err = a.mobilePost(ctx, s, "https://user-njs.yun.139.com/user/route/qryRoutePolicy",
		map[string]any{"userInfo": map[string]any{"userType": 1, "accountType": 1, "accountName": account}, "modAddrType": 1}, false, &data)
	if err != nil {
		return "", err
	}
	for _, route := range data.Routes {
		if route.Name == "personal" && validMobileHost(route.URL) {
			return strings.TrimRight(route.URL, "/"), nil
		}
	}
	return "", errors.New("未获取有效的移动个人云路由")
}

func (a *App) mobileListAt(ctx context.Context, s Storage, host, dir string) ([]File, error) {
	var files []File
	cursor := ""
	seen := map[string]bool{}
	for {
		type item struct {
			Hash          string `json:"contentHash"`
			HashAlgorithm string `json:"contentHashAlgorithm"`
			ID            string `json:"fileId"`
			Name          string `json:"name"`
			Size          int64  `json:"size"`
			Type          string `json:"type"`
			Category      string `json:"category"`
		}
		var data struct {
			Items []item `json:"items"`
			Files []item `json:"fileList"`
			Next  string `json:"nextPageCursor"`
		}
		err := a.mobilePost(ctx, s, host+"/file/list", map[string]any{
			"parentFileId": dir, "orderBy": "updated_at", "orderDirection": "DESC",
			"imageThumbnailStyleList": []string{"Small", "Large"}, "pageInfo": map[string]any{"pageCursor": cursor, "pageSize": 100},
		}, false, &data)
		if err != nil {
			return nil, err
		}
		if data.Items == nil {
			data.Items = data.Files
		}
		for _, f := range data.Items {
			if f.ID == "" || !safeName(f.Name) {
				continue
			}
			hash := ""
			if strings.EqualFold(f.HashAlgorithm, "SHA256") {
				hash = f.Hash
			}
			files = append(files, File{ID: f.ID, Name: f.Name, Size: f.Size, SHA256: hash, IsDir: strings.EqualFold(f.Type, "folder") || strings.EqualFold(f.Category, "folder")})
		}
		if data.Next == "" {
			return files, nil
		}
		if seen[data.Next] || len(files) > 1000000 {
			return nil, errors.New("移动云盘目录分页异常或条目过多")
		}
		seen[data.Next], cursor = true, data.Next
	}
}

func (a *App) mobileLinkAt(ctx context.Context, s Storage, host, fid string) (Download, error) {
	var data struct {
		URL string `json:"url"`
		CDN string `json:"cdnUrl"`
	}
	err := a.mobilePost(ctx, s, host+"/file/getDownloadUrl", map[string]string{"fileId": fid}, false, &data)
	if err != nil {
		return Download{}, err
	}
	address := data.CDN
	if address == "" {
		address = data.URL
	}
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return Download{}, errors.New("移动云盘返回无效下载地址")
	}
	return Download{URL: address, Headers: http.Header{}}, nil
}
