package app

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TV credentials are independent from the web Cookie and persist only in state.enc.
type QuarkTVBinding struct {
	Enabled      bool      `json:"enabled"`
	Mode         string    `json:"mode"`
	Quality      string    `json:"quality"`
	AllowDolby   bool      `json:"allowDolby"`
	UA           string    `json:"ua"`
	UAListMode   string    `json:"uaListMode"`
	Device       string    `json:"device"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	Broker       string    `json:"broker"`
	Expires      time.Time `json:"expires"`
	CookieHash   string    `json:"cookieHash"`
	Nickname     string    `json:"nickname"`
}

type quarkTVLink struct {
	URL     string
	Expires time.Time
}

const quarkTVClient = "d3194e61504e493eb6222857bccfed94"
const quarkTVSign = "kw2dvtd7p4t3pjl2d9ed9yc8yej8kw2d"
const quarkTVBase = "https://open-api-drive.quark.cn"
const quarkTVBroker = "https://api.extscreen.com/quarkdrive"
const quarkTVUA = "Mozilla/5.0 (Linux; U; Android 13; zh-cn; M2004J7AC Build/UKQ1.231108.001) AppleWebKit/533.1 (KHTML, like Gecko) Mobile Safari/533.1"

type quarkTVHTTPError struct {
	Status       int
	Errno        int
	TokenInvalid bool
}

func (e *quarkTVHTTPError) Error() string {
	return fmt.Sprintf("夸克 TV 服务返回 HTTP %d（错误码 %d）", e.Status, e.Errno)
}

func quarkTVEnabled(st State) bool {
	return st.QuarkTVEnabled == nil || *st.QuarkTVEnabled
}

func (a *App) quarkTVOverview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case "GET":
		st := a.store.snapshotWithLogLimit(0)
		bindings := []map[string]any{}
		for _, s := range st.Storages {
			b := st.QuarkTV[s.ID]
			if s.Type == "quark" && b.AccessToken != "" {
				bindings = append(bindings, map[string]any{"id": s.ID, "name": s.Name, "nickname": b.Nickname, "enabled": b.Enabled, "valid": b.CookieHash == quarkCookieHash(s)})
			}
		}
		jsonResponse(w, 200, map[string]any{"enabled": quarkTVEnabled(st) && len(bindings) > 0, "bindings": bindings, "broker": quarkTVBroker})
	case "PUT":
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if !decode(w, r, &input) {
			return
		}
		err := a.store.update(func(st *State) error {
			if input.Enabled {
				bound := false
				for _, s := range st.Storages {
					b := st.QuarkTV[s.ID]
					bound = bound || (s.Type == "quark" && s.Enabled && b.Enabled && b.AccessToken != "" && b.CookieHash == quarkCookieHash(s))
				}
				if !bound {
					return errors.New("请先添加有效的夸克存储绑定")
				}
			}
			st.QuarkTVEnabled = &input.Enabled
			return nil
		})
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	default:
		w.WriteHeader(405)
	}
}

// Web and TV use different UID namespaces; match the account nickname as LitePan does.
// This rejects mismatches, but equal non-unique nicknames are not proof of identity.
func verifyQuarkTVAccount(ctx context.Context, s Storage, b *QuarkTVBinding) error {
	var tv struct {
		Errno int `json:"errno"`
		Data  struct {
			Nickname string `json:"nickname"`
			NickName string `json:"nick_name"`
			Nick     string `json:"nick"`
		} `json:"data"`
		Nickname string `json:"nickname"`
	}
	if err := quarkTVRequest(ctx, *b, "/user", url.Values{"method": {"user_info"}}, &tv); err != nil {
		return err
	}
	var web struct {
		Success bool `json:"success"`
		Data    struct {
			Nickname string `json:"nickname"`
		} `json:"data"`
	}
	headers := http.Header{"Cookie": {s.Config["cookie"]}, "Referer": {"https://pan.quark.cn/"}, "User-Agent": {"Mozilla/5.0"}, "Accept": {"application/json"}}
	if err := quarkTVJSON(ctx, "GET", "https://pan.quark.cn/account/info?platform=pc&fr=pc", headers, nil, &web); err != nil {
		return err
	}
	nickname := ""
	for _, value := range []string{tv.Data.Nickname, tv.Data.NickName, tv.Data.Nick, tv.Nickname} {
		if strings.TrimSpace(value) != "" {
			nickname = value
			break
		}
	}
	nickname = strings.TrimSpace(nickname)
	if tv.Errno != 0 || !web.Success || nickname == "" || strings.TrimSpace(web.Data.Nickname) == "" {
		return errors.New("无法获取账号昵称用于校验，请重新扫码")
	}
	if nickname != strings.TrimSpace(web.Data.Nickname) {
		return errors.New("扫码账号与所选夸克存储账号不一致，请使用同一账号扫码")
	}
	b.Nickname = nickname
	return nil
}

func quarkCookieHash(s Storage) string {
	sum := sha256.Sum256([]byte(s.Config["cookie"]))
	return hex.EncodeToString(sum[:])
}

func quarkDeviceQuery(device string) url.Values {
	sum := md5.Sum([]byte(device + strconv.FormatInt(time.Now().UnixMilli(), 10)))
	return url.Values{
		"req_id": {hex.EncodeToString(sum[:])}, "app_ver": {"1.8.2.2"},
		"device_id": {device}, "device_brand": {"Xiaomi"}, "platform": {"tv"},
		"device_name": {"M2004J7AC"}, "device_model": {"M2004J7AC"},
		"build_device": {"M2004J7AC"}, "build_product": {"M2004J7AC"},
		"device_gpu": {"Adreno (TM) 550"}, "activity_rect": {"{}"}, "channel": {"GENERAL"},
	}
}

// Never follow authentication redirects: access tokens must stay on the chosen host.
func quarkTVJSON(ctx context.Context, method, address string, headers http.Header, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
		headers.Set("Content-Type", "application/json")
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return errors.New("夸克 TV 服务地址无效")
	}
	req.Header = headers
	client := &http.Client{Timeout: 20 * time.Second, Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("夸克 TV 服务连接失败")
	}
	defer res.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if readErr != nil || len(data) > 8<<20 {
		return errors.New("夸克 TV 响应无效")
	}
	// HTTP 400 may carry the token-expired or QR-pending code. Decode it before
	// returning an HTTP error so callers can refresh credentials or keep polling.
	decodeErr := json.Unmarshal(data, out)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var env struct {
			Errno     int    `json:"errno"`
			Status    int    `json:"status"`
			ErrorInfo string `json:"error_info"`
		}
		_ = json.Unmarshal(data, &env)
		message := strings.ToLower(env.ErrorInfo)
		invalid := env.Errno == 10001 || env.Errno == 11001 || (env.Status == -1 && (strings.Contains(message, "access token") || strings.Contains(message, "access_token") || strings.Contains(message, "token无效") || strings.Contains(message, "token 无效")))
		return &quarkTVHTTPError{res.StatusCode, env.Errno, invalid}
	}
	if decodeErr != nil {
		return errors.New("夸克 TV 响应无效")
	}
	return nil
}

func quarkTVRequest(ctx context.Context, b QuarkTVBinding, endpoint string, extra url.Values, out any) error {
	q := quarkDeviceQuery(b.Device)
	q.Set("access_token", b.AccessToken)
	for k, v := range extra {
		q[k] = v
	}
	tm := strconv.FormatInt(time.Now().UnixMilli(), 10)
	requestID := md5.Sum([]byte(b.Device + tm))
	q.Set("req_id", hex.EncodeToString(requestID[:]))
	sum := sha256.Sum256([]byte("GET&" + endpoint + "&" + tm + "&" + quarkTVSign))
	headers := http.Header{}
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", quarkTVUA)
	headers.Set("x-pan-tm", tm)
	headers.Set("x-pan-token", hex.EncodeToString(sum[:]))
	headers.Set("x-pan-client-id", quarkTVClient)
	return quarkTVJSON(ctx, "GET", quarkTVBase+endpoint+"?"+q.Encode(), headers, nil, out)
}

func validateQuarkBroker(address string) error {
	if address == "" {
		return nil
	}
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("换取服务必须为无账号、查询参数的 HTTPS 地址")
	}
	return nil
}

func exchangeQuarkTV(ctx context.Context, b *QuarkTVBinding, secret string, refresh bool) error {
	if b.Broker == "" {
		return errors.New("请配置受信任的 HTTPS 换取服务，或重新填写 TV 凭据")
	}
	if err := validateQuarkBroker(b.Broker); err != nil {
		return err
	}
	body := map[string]string{}
	for k, v := range quarkDeviceQuery(b.Device) {
		body[k] = v[0]
	}
	if refresh {
		body["refresh_token"] = secret
	} else {
		body["code"] = secret
	}
	var result struct {
		Code int `json:"code"`
		Data struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
			Expires int    `json:"expires_in"`
			Errno   int    `json:"errno"`
		} `json:"data"`
	}
	if err := quarkTVJSON(ctx, "POST", strings.TrimRight(b.Broker, "/")+"/token", http.Header{"User-Agent": {quarkTVUA}, "Accept": {"application/json, text/plain, */*"}}, body, &result); err != nil {
		return err
	}
	if result.Code != 200 || result.Data.Errno != 0 || result.Data.Access == "" {
		return errors.New("TV 凭据换取失败，请检查授权或刷新凭据")
	}
	b.AccessToken = result.Data.Access
	if result.Data.Refresh != "" {
		b.RefreshToken = result.Data.Refresh
	}
	seconds := result.Data.Expires
	if seconds <= 0 {
		seconds = 604800
	}
	b.Expires = time.Now().Add(time.Duration(seconds) * time.Second)
	return nil
}

func quarkTVDefaults(b QuarkTVBinding) QuarkTVBinding {
	if b.Mode == "" {
		b.Mode = "adaptive"
	}
	if b.Quality == "" {
		b.Quality = "4k"
	}
	if b.UAListMode == "" {
		b.UAListMode = "proxy_list"
	}
	if b.Device == "" {
		device := md5.Sum([]byte(id()))
		b.Device = hex.EncodeToString(device[:])
	}
	return b
}

func (a *App) quarkTVSettings(w http.ResponseWriter, r *http.Request) {
	var s Storage
	for _, candidate := range a.store.snapshotWithLogLimit(0).Storages {
		if candidate.ID == r.PathValue("id") {
			s = candidate
			break
		}
	}
	if s.Type != "quark" {
		fail(w, 400, errors.New("请选择夸克存储池"))
		return
	}
	var err error
	a.quarkTVMu.Lock()
	defer a.quarkTVMu.Unlock()
	old := quarkTVDefaults(a.store.snapshotWithLogLimit(0).QuarkTV[s.ID])
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case "DELETE":
		err = a.store.update(func(st *State) error { delete(st.QuarkTV, s.ID); return nil })
		if err != nil {
			fail(w, 500, errors.New("解除绑定失败"))
			return
		}
		a.quarkTVCache = nil
		jsonResponse(w, 200, map[string]bool{"ok": true})
	case "GET":
		old.AccessToken, old.RefreshToken = "", ""
		jsonResponse(w, 200, map[string]any{"config": old, "authorized": a.store.snapshotWithLogLimit(0).QuarkTV[s.ID].AccessToken != ""})
	case "PUT":
		var b QuarkTVBinding
		if json.NewDecoder(io.LimitReader(r.Body, 32768)).Decode(&b) != nil {
			fail(w, 400, errors.New("配置格式无效"))
			return
		}
		b = quarkTVDefaults(b)
		if b.Mode != "direct" && b.Mode != "adaptive" && b.Mode != "split" {
			fail(w, 400, errors.New("接管模式无效"))
			return
		}
		if quarkQualityRank(b.Quality) < 1 || (b.UAListMode != "direct_list" && b.UAListMode != "proxy_list") {
			fail(w, 400, errors.New("画质或 UA 规则无效"))
			return
		}
		if err := validateQuarkBroker(b.Broker); err != nil {
			fail(w, 400, err)
			return
		}
		if b.AccessToken == "" && old.AccessToken != "" {
			b.AccessToken, b.RefreshToken, b.Expires = old.AccessToken, old.RefreshToken, old.Expires
			b.Device = old.Device
			b.Nickname = old.Nickname
			b.Broker = old.Broker
			b.CookieHash = old.CookieHash
		} else if b.AccessToken != "" {
			if old.AccessToken != "" {
				fail(w, 409, errors.New("该存储已绑定，请勿重复绑定"))
				return
			}
			if err := verifyQuarkTVAccount(r.Context(), s, &b); err != nil {
				fail(w, 400, err)
				return
			}
			b.Expires = time.Time{}
			b.CookieHash = quarkCookieHash(s)
		}
		if b.Enabled && b.AccessToken == "" {
			fail(w, 400, errors.New("请先绑定 TV 凭据"))
			return
		}
		err = a.store.update(func(st *State) error {
			if st.QuarkTV == nil {
				st.QuarkTV = map[string]QuarkTVBinding{}
			}
			st.QuarkTV[s.ID] = b
			return nil
		})
		if err != nil {
			fail(w, 500, errors.New("保存接管配置失败"))
			return
		}
		a.quarkTVCache = nil
		jsonResponse(w, 200, map[string]bool{"ok": true})
	default:
		w.WriteHeader(405)
	}
}

func (a *App) quarkTVAuthorization(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.PathValue("id"))
	if err != nil || s.Type != "quark" {
		fail(w, 400, errors.New("夸克存储池不可用"))
		return
	}
	var input struct {
		Session string `json:"session"`
		Broker  string `json:"broker"`
		Consent bool   `json:"consent"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 32768)).Decode(&input) != nil {
		fail(w, 400, errors.New("请求格式无效"))
		return
	}
	a.quarkTVMu.Lock()
	defer a.quarkTVMu.Unlock()
	b := quarkTVDefaults(a.store.snapshotWithLogLimit(0).QuarkTV[s.ID])
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("action") == "qr" {
		if !input.Consent || input.Broker == "" {
			fail(w, 400, errors.New("请确认将授权码及刷新凭据发送至指定换取服务"))
			return
		}
		if b.AccessToken != "" {
			fail(w, 409, errors.New("该存储已绑定，请勿重复绑定"))
			return
		}
		if err := validateQuarkBroker(input.Broker); err != nil {
			fail(w, 400, err)
			return
		}
		b.Broker = input.Broker
		var result struct {
			QR    string `json:"qr_data"`
			Token string `json:"query_token"`
		}
		err = quarkTVRequest(r.Context(), b, "/oauth/authorize", url.Values{"client_id": {quarkTVClient}, "auth_type": {"code"}, "scope": {"netdisk"}, "qrcode": {"1"}, "qr_width": {"460"}, "qr_height": {"460"}}, &result)
		if err != nil || result.QR == "" || result.Token == "" {
			fail(w, 502, errors.New("获取 TV 授权二维码失败"))
			return
		}
		if b.CookieHash != quarkCookieHash(s) {
			b.AccessToken, b.RefreshToken, b.Enabled = "", "", false
		}
		b.CookieHash = quarkCookieHash(s)
		if err = a.store.update(func(st *State) error {
			if st.QuarkTV == nil {
				st.QuarkTV = map[string]QuarkTVBinding{}
			}
			st.QuarkTV[s.ID] = b
			return nil
		}); err != nil {
			fail(w, 500, errors.New("保存设备信息失败"))
			return
		}
		session, err := a.sealAuthorization(authorizationSession{Provider: "quark-tv", Base: b.Broker, Token: result.Token, Device: b.Device, Cookies: map[string]string{"storage": s.ID, "cookie": b.CookieHash}, Expires: time.Now().Add(5 * time.Minute).Unix(), Owner: authorizationOwner(r)})
		if err != nil {
			fail(w, 500, err)
			return
		}
		image, err := quarkQRImage(result.QR)
		if err != nil {
			fail(w, 502, errors.New("二维码内容无效"))
			return
		}
		jsonResponse(w, 200, map[string]string{"session": session, "image": image})
		return
	}
	if r.PathValue("action") != "poll" {
		w.WriteHeader(404)
		return
	}
	r.SetPathValue("provider", "quark-tv")
	session, err := a.openAuthorization(input.Session, r)
	if err != nil || session.Cookies["storage"] != s.ID || session.Cookies["cookie"] != quarkCookieHash(s) || session.Device != b.Device || session.Base != b.Broker {
		fail(w, 400, errors.New("授权会话已失效，请重新扫码"))
		return
	}
	if b.AccessToken != "" {
		fail(w, 409, errors.New("该存储已绑定，请勿重复绑定"))
		return
	}
	var result struct {
		Code  string `json:"code"`
		Errno int    `json:"errno"`
	}
	err = quarkTVRequest(r.Context(), b, "/oauth/code", url.Values{"client_id": {quarkTVClient}, "scope": {"netdisk"}, "query_token": {session.Token}}, &result)
	if result.Errno == 11003 {
		jsonResponse(w, 200, map[string]bool{"pending": true})
		return
	}
	if err != nil {
		fail(w, 502, err)
		return
	}
	if result.Errno == 11003 {
		jsonResponse(w, 200, map[string]bool{"pending": true})
		return
	}
	if result.Code == "" || result.Errno != 0 {
		fail(w, 502, errors.New("TV 授权未完成或已过期"))
		return
	}
	if err = exchangeQuarkTV(r.Context(), &b, result.Code, false); err != nil {
		fail(w, 502, err)
		return
	}
	if err = verifyQuarkTVAccount(r.Context(), s, &b); err != nil {
		fail(w, 400, err)
		return
	}
	b.Enabled = true
	b.CookieHash = quarkCookieHash(s)
	if err = a.store.update(func(st *State) error {
		for _, current := range st.Storages {
			if current.ID == s.ID && current.Enabled && current.Type == "quark" && quarkCookieHash(current) == b.CookieHash {
				if st.QuarkTV[s.ID].AccessToken != "" {
					return errors.New("该存储已绑定")
				}
				st.QuarkTV[s.ID] = b
				return nil
			}
		}
		return errors.New("存储已变更，请重新扫码")
	}); err != nil {
		fail(w, 500, errors.New("保存 TV 凭据失败"))
		return
	}
	a.quarkTVCache = nil
	jsonResponse(w, 200, map[string]bool{"authorized": true})
}

func quarkQualityRank(quality string) int {
	switch strings.ToLower(strings.TrimSpace(quality)) {
	case "low", "360p", "360":
		return 1
	case "normal", "480p", "480":
		return 2
	case "high", "720p", "720":
		return 3
	case "super", "1080p", "1080", "fhd":
		return 4
	case "2k", "qhd", "1440p", "1440":
		return 5
	case "4k", "uhd", "2160p", "2160":
		return 6
	case "dolby_vision", "dolby-vision", "dovi":
		return 7
	}
	return 0
}

type quarkVideo struct {
	URL        string `json:"url"`
	Resolution string `json:"resolution"`
	Accessible int    `json:"accessable"`
	Format     string `json:"format"`
}

func quarkTVChoose(b QuarkTVBinding, videos []quarkVideo) string {
	best, target := -2000, ""
	for _, video := range videos {
		rank := quarkQualityRank(video.Resolution)
		if video.Accessible == 0 || rank == 0 || (rank > quarkQualityRank(b.Quality) && !(rank == 7 && b.AllowDolby)) || (rank == 7 && !b.AllowDolby) {
			continue
		}
		u, err := url.Parse(video.URL)
		if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(video.URL, "\r\n") {
			continue
		}
		hls := strings.EqualFold(strings.TrimSpace(video.Format), "hls") || strings.EqualFold(strings.TrimSpace(video.Format), "m3u8") || strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
		if hls && b.Mode == "adaptive" {
			continue
		}
		score := rank
		if hls {
			score -= 1000
		}
		if score > best {
			best, target = score, video.URL
		}
	}
	return target
}

func quarkTVBypass(b QuarkTVBinding, ua string) bool {
	if b.Mode != "split" {
		return false
	}
	match := false
	for _, keyword := range strings.FieldsFunc(b.UA, func(r rune) bool { return r == '\n' || r == ';' }) {
		keyword = strings.TrimSpace(keyword)
		if keyword != "" && strings.Contains(strings.ToLower(ua), strings.ToLower(keyword)) {
			match = true
			break
		}
	}
	if b.UAListMode == "direct_list" {
		return !match
	}
	return match
}

func (a *App) quarkTVTarget(ctx context.Context, s Storage, file, ua string) string {
	a.quarkTVMu.Lock()
	defer a.quarkTVMu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	if !quarkTVEnabled(st) {
		return ""
	}
	b := st.QuarkTV[s.ID]
	if !b.Enabled || b.CookieHash != quarkCookieHash(s) || b.AccessToken == "" || quarkTVBypass(b, ua) {
		return ""
	}
	key := s.ID + ":" + file
	if entry, ok := a.quarkTVCache[key]; ok && time.Now().Before(entry.Expires) {
		return entry.URL
	}
	if !b.Expires.IsZero() && time.Until(b.Expires) < time.Minute {
		if err := exchangeQuarkTV(ctx, &b, b.RefreshToken, true); err != nil {
			a.store.event("warn", "links", "夸克 TV 凭据续期失败，回退普通播放")
			return ""
		}
		if err := a.store.update(func(st *State) error { st.QuarkTV[s.ID] = b; return nil }); err != nil {
			return ""
		}
	}
	var result struct {
		Errno     int    `json:"errno"`
		Status    int    `json:"status"`
		ErrorInfo string `json:"error_info"`
		Data      struct {
			Videos []quarkVideo `json:"video_info"`
		} `json:"data"`
	}
	err := quarkTVRequest(ctx, b, "/file", url.Values{"method": {"streaming"}, "group_by": {"source"}, "fid": {file}, "resolution": {"low,normal,high,super,2k,4k"}, "support": {"dolby_vision"}}, &result)
	var httpErr *quarkTVHTTPError
	tokenMessage := strings.ToLower(result.ErrorInfo)
	invalid := result.Errno == 10001 || result.Errno == 11001 || (errors.As(err, &httpErr) && httpErr.TokenInvalid) || (result.Status == -1 && (strings.Contains(tokenMessage, "access token") || strings.Contains(tokenMessage, "access_token") || strings.Contains(tokenMessage, "token无效") || strings.Contains(tokenMessage, "token 无效")))
	if invalid && b.RefreshToken != "" && b.Broker != "" {
		if refreshErr := exchangeQuarkTV(ctx, &b, b.RefreshToken, true); refreshErr == nil {
			if saveErr := a.store.update(func(st *State) error { st.QuarkTV[s.ID] = b; return nil }); saveErr == nil {
				result.Errno, result.Status, result.Data.Videos, result.ErrorInfo = 0, 0, nil, ""
				err = quarkTVRequest(ctx, b, "/file", url.Values{"method": {"streaming"}, "group_by": {"source"}, "fid": {file}, "resolution": {"low,normal,high,super,2k,4k"}, "support": {"dolby_vision"}}, &result)
			}
		}
	}
	if err != nil || result.Errno != 0 || result.Status >= 400 {
		a.store.event("warn", "links", fmt.Sprintf("夸克 TV 播放地址获取失败（状态 %d，错误码 %d），回退本机代理", result.Status, result.Errno))
		return ""
	}
	target := quarkTVChoose(b, result.Data.Videos)
	if target == "" {
		a.store.event("info", "links", "夸克 TV 未选中兼容档位（检查画质、HLS 智能回退与会员权限），回退本机代理")
		return ""
	}
	if a.quarkTVCache == nil {
		a.quarkTVCache = map[string]quarkTVLink{}
	}
	for k, v := range a.quarkTVCache {
		if time.Now().After(v.Expires) {
			delete(a.quarkTVCache, k)
		}
	}
	if len(a.quarkTVCache) >= 1000 {
		a.quarkTVCache = map[string]quarkTVLink{}
	}
	a.quarkTVCache[key] = quarkTVLink{target, time.Now().Add(5 * time.Minute)}
	return target
}
