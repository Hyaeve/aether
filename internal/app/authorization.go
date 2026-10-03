package app

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type authorizationSession struct {
	Provider string            `json:"provider"`
	Base     string            `json:"base,omitempty"`
	Token    string            `json:"token"`
	Cookies  map[string]string `json:"cookies"`
	Expires  int64             `json:"expires"`
	Owner    [32]byte          `json:"owner"`
}

type quarkAuthResponse struct {
	Status int `json:"status"`
	Data   struct {
		Members struct {
			Token  string `json:"token"`
			Ticket string `json:"service_ticket"`
		} `json:"members"`
	} `json:"data"`
}

func authorizationOwner(r *http.Request) [32]byte {
	c, _ := r.Cookie("aether_session")
	if c == nil {
		return sha256.Sum256(nil)
	}
	return sha256.Sum256([]byte(c.Value))
}

func (a *App) sealAuthorization(s authorizationSession) (string, error) {
	plain, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(a.store.aead.Seal(nonce, nonce, plain, []byte("storage-authorization"))), nil
}

func (a *App) openAuthorization(token string, r *http.Request) (authorizationSession, error) {
	var session authorizationSession
	raw, err := base64.RawURLEncoding.DecodeString(token)
	n := a.store.aead.NonceSize()
	if err != nil || len(raw) < n {
		return session, errors.New("授权会话无效，请重新扫码")
	}
	plain, err := a.store.aead.Open(nil, raw[:n], raw[n:], []byte("storage-authorization"))
	if err != nil || json.Unmarshal(plain, &session) != nil || session.Owner != authorizationOwner(r) || session.Provider != r.PathValue("provider") {
		return session, errors.New("授权会话无效，请重新扫码")
	}
	if session.Expires <= time.Now().Unix() {
		return session, errors.New("二维码已过期，请重新获取")
	}
	return session, nil
}

func cookieHeader(cookies map[string]string) string {
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, (&http.Cookie{Name: name, Value: cookies[name]}).String())
	}
	return strings.Join(parts, "; ")
}

func quarkAuthGet(r *http.Request, target string, cookies map[string]string) ([]byte, error) {
	request, err := http.NewRequestWithContext(r.Context(), "GET", target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 Chrome/130.0.0.0 Safari/537.36")
	request.Header.Set("Referer", "https://pan.quark.cn/")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", cookieHeader(cookies))
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("无法连接夸克授权服务，请重试")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("夸克授权服务响应异常")
	}
	for _, cookie := range response.Cookies() {
		if cookie.MaxAge < 0 {
			delete(cookies, cookie.Name)
		} else if cookie.Value != "" {
			cookies[cookie.Name] = cookie.Value
		}
	}
	return io.ReadAll(io.LimitReader(response.Body, 2<<20))
}

func (a *App) startAuthorization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("provider") == "115" {
		a.start115Authorization(w, r)
		return
	}
	if r.PathValue("provider") != "quark" {
		fail(w, 400, errors.New("此存储类型不支持扫码授权"))
		return
	}
	cookies := map[string]string{}
	query := url.Values{"client_id": {"532"}, "v": {"1.2"}, "request_id": {id()}}
	body, err := quarkAuthGet(r, "https://uop.quark.cn/cas/ajax/getTokenForQrcodeLogin?"+query.Encode(), cookies)
	var result quarkAuthResponse
	if err != nil {
		fail(w, 502, err)
		return
	}
	if json.Unmarshal(body, &result) != nil || result.Status != 2000000 || result.Data.Members.Token == "" {
		fail(w, 502, errors.New("无法获取夸克二维码"))
		return
	}
	session := authorizationSession{Provider: "quark", Token: result.Data.Members.Token, Cookies: cookies, Expires: time.Now().Add(5 * time.Minute).Unix(), Owner: authorizationOwner(r)}
	token, err := a.sealAuthorization(session)
	if err != nil {
		fail(w, 500, err)
		return
	}
	qrURL := "https://su.quark.cn/4_eMHBJ?" + url.Values{"token": {session.Token}, "client_id": {"532"}, "ssb": {"weblogin"}, "uc_param_str": {""}, "uc_biz_str": {"S:custom|OPT:SAREA@0|OPT:IMMERSIVE@1|OPT:BACK_BTN_STYLE@0"}}.Encode()
	image, err := qrcode.Encode(qrURL, qrcode.Medium, 256)
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"token": token, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(image), "expiresIn": 300})
}

func (a *App) pollAuthorization(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("provider") != "quark" && r.PathValue("provider") != "115" {
		fail(w, 400, errors.New("此存储类型不支持扫码授权"))
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &input) {
		return
	}
	session, err := a.openAuthorization(input.Token, r)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if session.Provider == "115" {
		a.poll115Authorization(w, r, session)
		return
	}
	query := url.Values{"client_id": {"532"}, "v": {"1.2"}, "token": {session.Token}, "request_id": {id()}}
	body, err := quarkAuthGet(r, "https://uop.quark.cn/cas/ajax/getServiceTicketByQrcodeToken?"+query.Encode(), session.Cookies)
	if err != nil {
		fail(w, 502, err)
		return
	}
	var result quarkAuthResponse
	if json.Unmarshal(body, &result) != nil {
		fail(w, 502, errors.New("夸克授权状态响应无效"))
		return
	}
	if result.Status == 50004002 || result.Status == 50004003 || result.Status == 50004004 {
		jsonResponse(w, 200, map[string]string{"status": "expired"})
		return
	}
	if result.Status != 2000000 || result.Data.Members.Ticket == "" {
		jsonResponse(w, 200, map[string]string{"status": "waiting"})
		return
	}
	query = url.Values{"st": {result.Data.Members.Ticket}, "lw": {"scan"}}
	if _, err := quarkAuthGet(r, "https://pan.quark.cn/account/info?"+query.Encode(), session.Cookies); err != nil {
		fail(w, 502, err)
		return
	}
	if _, err := quarkAuthGet(r, "https://drive-pc.quark.cn/1/clouddrive/file/sort?pr=ucpro&fr=pc&pdir_fid=0&_page=1&_size=1", session.Cookies); err != nil {
		fail(w, 502, err)
		return
	}
	if session.Cookies["__puus"] == "" || session.Cookies["__pus"] == "" {
		fail(w, 502, errors.New("授权未返回完整网盘 Cookie，请重新扫码"))
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "success", "cookie": cookieHeader(session.Cookies)})
}

type oauthProxyResponse struct {
	Success bool `json:"success"`
	Data    struct {
		SessionID string            `json:"session_id"`
		URL       string            `json:"oauth_url"`
		Status    string            `json:"status"`
		Tokens    map[string]string `json:"token_data"`
	} `json:"data"`
}

func oauthProxyRequest(r *http.Request, method, target string, payload any) (oauthProxyResponse, error) {
	var result oauthProxyResponse
	body, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(r.Context(), method, target, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Aether/"+Version)
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return result, errors.New("无法连接 OAuth 代理，请检查地址或稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || !result.Success {
		return result, errors.New("OAuth 代理响应异常，请确认代理支持 115 Open 和 Aether 客户端")
	}
	return result, nil
}

func validOAuthURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && u.RawQuery == ""
}

func (a *App) start115Authorization(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Base string `json:"base"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Base = strings.TrimRight(strings.TrimSpace(input.Base), "/")
	if !validOAuthURL(input.Base) {
		fail(w, 400, errors.New("请输入可信任的 HTTPS OAuth 代理地址（不含查询参数）"))
		return
	}
	result, err := oauthProxyRequest(r, "POST", input.Base+"/api/oauth/start", map[string]string{
		"driver_type": "115网盘Open", "callback_url": input.Base + "/callback-popup",
	})
	if err != nil {
		fail(w, 502, err)
		return
	}
	authorizationURL, err := url.Parse(result.Data.URL)
	if err != nil || authorizationURL.Scheme != "https" || authorizationURL.Hostname() == "" || authorizationURL.User != nil || result.Data.SessionID == "" {
		fail(w, 502, errors.New("OAuth 代理未返回有效的 HTTPS 授权地址或会话"))
		return
	}
	session := authorizationSession{Provider: "115", Base: input.Base, Token: result.Data.SessionID, Expires: time.Now().Add(5 * time.Minute).Unix(), Owner: authorizationOwner(r)}
	token, err := a.sealAuthorization(session)
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"token": token, "url": authorizationURL.String(), "expiresIn": 300})
}

func (a *App) poll115Authorization(w http.ResponseWriter, r *http.Request, session authorizationSession) {
	result, err := oauthProxyRequest(r, "GET", session.Base+"/api/oauth/status/"+url.PathEscape(session.Token), nil)
	if err != nil {
		fail(w, 502, err)
		return
	}
	switch result.Data.Status {
	case "success":
		access := strings.TrimSpace(result.Data.Tokens["access_token"])
		if access == "" {
			fail(w, 502, errors.New("OAuth 代理未返回 Access Token"))
			return
		}
		_, _ = oauthProxyRequest(r, "POST", session.Base+"/api/oauth/confirm-received/"+url.PathEscape(session.Token), nil)
		jsonResponse(w, 200, map[string]string{"status": "success", "accessToken": access, "refreshToken": strings.TrimSpace(result.Data.Tokens["refresh_token"])})
	case "error", "expired":
		jsonResponse(w, 200, map[string]string{"status": "expired"})
	default:
		jsonResponse(w, 200, map[string]string{"status": "waiting"})
	}
}
