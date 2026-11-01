package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const aliAPI = "https://api.alipan.com"
const aliAuth = "https://auth.alipan.com"
const aliPassport = "https://passport.aliyundrive.com"

var aliURLPattern = regexp.MustCompile(`https://(?:www\.)?(?:alipan\.com|aliyundrive\.com)/s/[^\s<>"，。]+`)
var aliIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,160}$`)
var aliPassword = regexp.MustCompile(`^[A-Za-z0-9]{4}$`)

type aliShare struct{ Code, Password, Folder, URL string }

func ali115SafeName(name string) bool {
	if !safeName(name) || !utf8.ValidString(name) || len(name) > 1024 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseAliShare(text string) (aliShare, error) {
	var out aliShare
	if len(text) > 8192 || strings.ContainsAny(text, "\x00\r") {
		return out, errors.New("阿里云盘分享链接无效")
	}
	matches := aliURLPattern.FindAllString(text, -1)
	if len(matches) != 1 {
		return out, errors.New("请填写一条阿里云盘分享链接")
	}
	u, err := url.Parse(matches[0])
	if err != nil || u.User != nil || u.Port() != "" {
		return out, errors.New("阿里云盘分享链接无效")
	}
	p := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(p) != 2 && len(p) != 4 || p[0] != "s" || !aliIDPattern.MatchString(p[1]) || len(p) == 4 && (p[2] != "folder" || !aliIDPattern.MatchString(p[3])) || u.Fragment != "" {
		return out, errors.New("阿里云盘分享路径无效")
	}
	out.Code, out.Folder = p[1], "root"
	if len(p) == 4 {
		out.Folder = p[3]
	}
	codes := map[string]bool{}
	for _, value := range u.Query()["pwd"] {
		if value != "" {
			codes[value] = true
		}
	}
	for _, m := range sharePasswordPattern.FindAllStringSubmatch(text, -1) {
		codes[m[1]] = true
	}
	if len(codes) > 1 {
		return out, errors.New("分享链接含不同提取码，请核对")
	}
	for code := range codes {
		if !aliPassword.MatchString(code) {
			return out, errors.New("提取码须为四位字母或数字")
		}
		out.Password = code
	}
	out.URL = "https://www.alipan.com/s/" + out.Code
	if out.Folder != "root" {
		out.URL += "/folder/" + out.Folder
	}
	if out.Password != "" {
		out.URL += "?pwd=" + out.Password
	}
	return out, nil
}

type aliSession struct {
	Access, Refresh, UserID, Nick, Drive, Device, Signature, Revision string
	Until                                                             time.Time
}

// Fixed API origins, no redirect or automatic replay. Error bodies may contain secrets.
func aliRequest(ctx context.Context, method, address string, body any, session *aliSession, share string, out any) error {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Host != "api.alipan.com" && u.Host != "auth.alipan.com" && u.Host != "passport.aliyundrive.com") {
		return errors.New("阿里云盘接口地址无效")
	}
	var raw []byte
	contentType := "application/json"
	if form, ok := body.(url.Values); ok {
		raw, contentType = []byte(form.Encode()), "application/x-www-form-urlencoded"
	} else if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return errors.New("阿里云盘请求格式无效")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, address, bytes.NewReader(raw))
	if err != nil {
		return errors.New("阿里云盘请求创建失败")
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", "https://www.alipan.com")
	req.Header.Set("Referer", "https://www.alipan.com/")
	if u.Host == "passport.aliyundrive.com" {
		req.Header.Set("Origin", aliPassport)
		req.Header.Set("Referer", aliPassport+"/mini_login.htm?appName=aliyun_drive")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 Aether")
	req.Header.Set("X-Canary", "client=Android,app=adrive,version=v4.1.0")
	if strings.Contains(u.Path, "/share_link/") {
		req.Header.Set("X-Canary", "client=web,app=share,version=v2.3.1")
	}
	if session != nil {
		req.Header.Set("Authorization", "Bearer "+session.Access)
		req.Header.Set("X-Device-Id", session.Device)
		req.Header.Set("X-Signature", session.Signature)
	}
	if share != "" {
		req.Header.Set("X-Share-Token", share)
		req.Header.Set("X-Canary", "client=web,app=share,version=v2.3.1")
	}
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("阿里云盘请求已取消或超时；写入结果未确认时不自动重试")
		}
		return errors.New("阿里云盘连接失败；未自动重试")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return errors.New("阿里云盘响应不完整或过大")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("阿里云盘 HTTP %d，请检查账号、分享权限或容量；未自动重试", res.StatusCode)
	}
	if len(data) == 0 && out == nil {
		return nil
	}
	var envelope struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code != "" {
		return errors.New("阿里云盘拒绝请求，请检查账号、分享权限或容量；未自动重试")
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return errors.New("阿里云盘响应格式无效")
	}
	return nil
}

// Caller owns ali115Gate. Persist rotating refresh tokens before any later request.
func (a *App) aliLogin(ctx context.Context, refresh string, explicit bool) (*aliSession, error) {
	if len(refresh) < 16 || len(refresh) > 8192 || strings.ContainsAny(refresh, " \t\r\n") {
		return nil, errors.New("refresh token格式无效")
	}
	previous := a.store.snapshotWithLogLimit(0).Ali115
	var key *secp.PrivateKey
	var err error
	if previous.DeviceKey != "" {
		raw, e := hex.DecodeString(previous.DeviceKey)
		if e != nil || len(raw) != 32 {
			return nil, errors.New("阿里设备密钥无效")
		}
		key = secp.PrivKeyFromBytes(raw)
		if !bytes.Equal(key.Serialize(), raw) || key.Key.IsZero() {
			key.Zero()
			return nil, errors.New("阿里设备密钥无效")
		}
	} else {
		key, err = secp.GeneratePrivateKey()
		if err != nil {
			return nil, errors.New("阿里设备密钥初始化失败")
		}
	}
	defer key.Zero()
	var token struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		User    string `json:"user_id"`
		Nick    string `json:"nick_name"`
		Drive   string `json:"default_drive_id"`
	}
	if err := aliRequest(ctx, "POST", aliAuth+"/v2/account/token", map[string]string{"refresh_token": refresh, "grant_type": "refresh_token"}, nil, "", &token); err != nil {
		return nil, err
	}
	if token.Refresh == "" || token.Access == "" || !aliIDPattern.MatchString(token.User) {
		return nil, errors.New("阿里云盘登录未返回有效账号与令牌")
	}
	revision := previous.Revision
	if explicit || revision == "" {
		revision = id()
	}
	if !explicit && previous.UserID != "" && token.User != previous.UserID {
		return nil, errors.New("阿里云盘账号身份已变化，已停止")
	}
	err = a.store.update(func(st *State) error {
		if !explicit && st.Ali115.RefreshToken != refresh {
			return errors.New("阿里云盘账号配置已变化")
		}
		st.Ali115.RefreshToken, st.Ali115.Revision = token.Refresh, revision
		st.Ali115.UserID = token.User
		st.Ali115.DeviceKey = hex.EncodeToString(key.Serialize())
		if explicit {
			st.Ali115.Nick, st.Ali115.Drive = "", ""
		}
		return nil
	})
	if err != nil {
		return nil, errors.New("阿里云盘轮换令牌保存失败，已停止后续操作")
	}
	deviceSum := sha256.Sum256(key.PubKey().SerializeCompressed())
	device := hex.EncodeToString(deviceSum[:16])
	sum := sha256.Sum256([]byte("5dde4e1bdf9e4966b387ba58f4b3fdc3:" + device + ":" + token.User + ":0"))
	compact := ecdsa.SignCompact(key, sum[:], false)
	signature := append(append([]byte{}, compact[1:]...), compact[0]-27)
	session := &aliSession{Access: token.Access, Refresh: token.Refresh, UserID: token.User, Nick: token.Nick, Drive: token.Drive, Device: device, Signature: hex.EncodeToString(signature), Revision: revision, Until: time.Now().Add(20 * time.Minute)}
	var user struct {
		User     string `json:"user_id"`
		Nick     string `json:"nick_name"`
		Drive    string `json:"default_drive_id"`
		Resource string `json:"resource_drive_id"`
	}
	if err := aliRequest(ctx, "POST", aliAPI+"/v2/user/get", map[string]any{}, session, "", &user); err != nil {
		return nil, err
	}
	if user.User != token.User {
		return nil, errors.New("阿里云盘登录账号核验失败")
	}
	session.Nick, session.Drive = user.Nick, user.Drive
	if user.Resource != "" {
		session.Drive = user.Resource
	}
	if !aliIDPattern.MatchString(session.Drive) {
		return nil, errors.New("阿里云盘资源盘标识无效")
	}
	if err := aliRequest(ctx, "POST", aliAPI+"/users/v1/users/device/create_session", map[string]any{"deviceName": "Aether", "modelName": "Windows", "nonce": 0, "pubKey": hex.EncodeToString(key.PubKey().SerializeUncompressed()[1:]), "refreshToken": token.Refresh}, session, "", nil); err != nil {
		return nil, err
	}
	if err := a.store.update(func(st *State) error {
		if st.Ali115.Revision != revision {
			return errors.New("账号配置已变化")
		}
		st.Ali115.Nick, st.Ali115.Drive = session.Nick, session.Drive
		return nil
	}); err != nil {
		return nil, err
	}
	a.ali115Session = session
	return session, nil
}

func (a *App) aliAccount(ctx context.Context) (*aliSession, error) {
	cfg := a.store.snapshotWithLogLimit(0).Ali115
	if a.ali115Session != nil && a.ali115Session.Revision == cfg.Revision && a.ali115Session.Refresh == cfg.RefreshToken && time.Now().Before(a.ali115Session.Until) {
		return a.ali115Session, nil
	}
	return a.aliLogin(ctx, cfg.RefreshToken, false)
}

type aliQR struct {
	T         json.Number
	CK, Owner string
	Until     time.Time
}

func aliQRRefresh(raw []byte) (string, error) {
	var tree any
	if len(raw) > 64<<10 || json.Unmarshal(raw, &tree) != nil {
		return "", errors.New("扫码凭据格式无效")
	}
	values := map[string]bool{}
	nodes := 0
	var walk func(any, int) error
	walk = func(v any, depth int) error {
		nodes++
		if nodes > 1000 || depth > 12 {
			return errors.New("扫码凭据结构过大")
		}
		switch value := v.(type) {
		case map[string]any:
			for key, child := range value {
				if key == "refreshToken" {
					token, ok := child.(string)
					if !ok || len(token) < 16 || len(token) > 8192 {
						return errors.New("扫码令牌无效")
					}
					values[token] = true
				}
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range value {
				if err := walk(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(tree, 0); err != nil {
		return "", err
	}
	if len(values) != 1 {
		return "", errors.New("扫码令牌缺失或不一致")
	}
	for value := range values {
		return value, nil
	}
	return "", errors.New("扫码令牌缺失")
}

func (a *App) aliQRAPI(w http.ResponseWriter, r *http.Request) {
	owner, _ := r.Cookie("aether_session")
	if owner == nil {
		fail(w, 401, errors.New("请重新登录"))
		return
	}
	a.ali115QRMu.Lock()
	defer a.ali115QRMu.Unlock()
	if a.ali115QRs == nil {
		a.ali115QRs = map[string]aliQR{}
	}
	for k, v := range a.ali115QRs {
		if time.Now().After(v.Until) {
			delete(a.ali115QRs, k)
		}
	}
	if r.Method == "POST" {
		if len(a.ali115QRs) >= 20 {
			fail(w, 409, errors.New("扫码请求过多，请稍后重试"))
			return
		}
		var out struct {
			Content struct {
				Data struct {
					T    json.Number `json:"t"`
					CK   string      `json:"ck"`
					Code string      `json:"codeContent"`
				} `json:"data"`
			} `json:"content"`
		}
		err := aliRequest(r.Context(), "GET", aliPassport+"/newlogin/qrcode/generate.do?appName=aliyun_drive&fromSite=52&appEntrance=web&isMobile=false&lang=zh_CN&returnUrl=", nil, nil, "", &out)
		image := ""
		if err == nil {
			image, err = quarkQRImage(out.Content.Data.Code)
		}
		if err == nil && (out.Content.Data.CK == "" || out.Content.Data.T == "") {
			err = errors.New("阿里云盘二维码会话无效")
		}
		if err != nil {
			fail(w, 502, err)
			return
		}
		key := id()
		a.ali115QRs[key] = aliQR{T: out.Content.Data.T, CK: out.Content.Data.CK, Owner: sessionKey(owner.Value), Until: time.Now().Add(3 * time.Minute)}
		jsonResponse(w, 200, map[string]string{"id": key, "image": image})
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	key := r.URL.Query().Get("id")
	qr, ok := a.ali115QRs[key]
	if !ok || qr.Owner != sessionKey(owner.Value) {
		fail(w, 410, errors.New("二维码已过期，请重新获取"))
		return
	}
	var out struct {
		Content struct {
			Data struct {
				Status string `json:"qrCodeStatus"`
				Biz    string `json:"bizExt"`
			} `json:"data"`
		} `json:"content"`
	}
	err := aliRequest(r.Context(), "POST", aliPassport+"/newlogin/qrcode/query.do?appName=aliyun_drive&fromSite=52", url.Values{"t": {qr.T.String()}, "ck": {qr.CK}, "appName": {"aliyun_drive"}, "appEntrance": {"web"}, "isMobile": {"false"}, "lang": {"zh_CN"}, "fromSite": {"52"}, "returnUrl": {""}, "navlanguage": {"zh-CN"}, "navPlatform": {"Win32"}}, nil, "", &out)
	if err != nil {
		fail(w, 502, err)
		return
	}
	status := out.Content.Data.Status
	if status == "CONFIRMED" {
		if !a.ali115Gate.TryLock() {
			fail(w, 409, errors.New("阿里云盘秒传正在执行，请结束后重新登录"))
			return
		}
		defer a.ali115Gate.Unlock()
		delete(a.ali115QRs, key)
		raw, e := base64.StdEncoding.DecodeString(out.Content.Data.Biz)
		if e != nil || len(raw) > 64<<10 {
			fail(w, 502, errors.New("扫码登录凭据无效"))
			return
		}
		if !utf8.Valid(raw) {
			raw, e = simplifiedchinese.GBK.NewDecoder().Bytes(raw)
		}
		refresh, parseErr := aliQRRefresh(raw)
		if e != nil || parseErr != nil {
			fail(w, 502, errors.New("扫码登录凭据无效"))
			return
		}
		if _, err = a.aliLogin(r.Context(), refresh, true); err != nil {
			fail(w, 502, err)
			return
		}
	}
	if status == "EXPIRED" || status == "CANCELED" {
		delete(a.ali115QRs, key)
	}
	jsonResponse(w, 200, map[string]string{"status": status})
}

type aliFile struct {
	ID     string `json:"file_id"`
	Parent string `json:"parent_file_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	SHA1   string `json:"content_hash"`
}

func (session *aliSession) list(ctx context.Context, parent, share, token string) ([]aliFile, error) {
	items := []aliFile{}
	seen, markers := map[string]bool{}, map[string]bool{}
	marker := ""
	for page := 0; page < 1000; page++ {
		body := map[string]any{"parent_file_id": parent, "limit": 100, "order_by": "name", "order_direction": "ASC", "marker": marker, "fields": "*"}
		if share != "" {
			body["share_id"] = share
		} else {
			body["drive_id"] = session.Drive
		}
		var out struct {
			Items []aliFile `json:"items"`
			Next  string    `json:"next_marker"`
		}
		if err := aliRequest(ctx, "POST", aliAPI+"/adrive/v3/file/list", body, session, token, &out); err != nil {
			return nil, err
		}
		for _, f := range out.Items {
			if !aliIDPattern.MatchString(f.ID) || seen[f.ID] || !ali115SafeName(f.Name) || f.Size < 0 || (f.Type != "folder" && f.Type != "file") || f.Parent != "" && f.Parent != parent {
				return nil, errors.New("阿里云盘目录内容或分页身份异常")
			}
			seen[f.ID] = true
			items = append(items, f)
		}
		if len(items) > 20000 {
			return nil, errors.New("阿里云盘单目录超过20000项，已停止")
		}
		if out.Next == "" {
			return items, nil
		}
		if markers[out.Next] || len(out.Items) == 0 {
			return nil, errors.New("阿里云盘目录分页中断")
		}
		markers[out.Next] = true
		marker = out.Next
	}
	return nil, errors.New("阿里云盘分页超过保护范围")
}

func (s *aliSession) create(ctx context.Context, parent, name string) (string, error) {
	var out aliFile
	err := aliRequest(ctx, "POST", aliAPI+"/adrive/v2/file/createWithFolders", map[string]any{"drive_id": s.Drive, "parent_file_id": parent, "name": name, "type": "folder", "check_name_mode": "refuse"}, s, "", &out)
	if err == nil && (!aliIDPattern.MatchString(out.ID) || out.Name != "" && out.Name != name || out.Parent != "" && out.Parent != parent) {
		err = errors.New("阿里云盘新建目录身份未确认")
	}
	return out.ID, err
}

func aliRange(spec string, size int64) (int64, int64, error) {
	parts := strings.Split(spec, "-")
	if len(parts) != 2 {
		return 0, 0, errors.New("秒传分片范围无效")
	}
	start, e1 := strconv.ParseInt(parts[0], 10, 64)
	end, e2 := strconv.ParseInt(parts[1], 10, 64)
	if e1 != nil || e2 != nil || start < 0 || end < start || end >= size || end-start >= 1<<20 {
		return 0, 0, errors.New("秒传分片范围无效或超过1MiB")
	}
	return start, end, nil
}
