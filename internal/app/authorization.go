package app

import (
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

	driver "github.com/SheltonZhu/115driver/pkg/driver"
	qrcode "github.com/skip2/go-qrcode"
)

type authorizationSession struct {
	Provider string                `json:"provider"`
	Base     string                `json:"base,omitempty"`
	Token    string                `json:"token"`
	Cookies  map[string]string     `json:"cookies"`
	Expires  int64                 `json:"expires"`
	Owner    [32]byte              `json:"owner"`
	Device   string                `json:"device,omitempty"`
	QR115    *driver.QRCodeSession `json:"qr115,omitempty"`
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
	if r.PathValue("provider") == "tianyi" {
		a.tianyiQRStart(w, r)
		return
	}
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
	if r.PathValue("provider") == "tianyi" {
		w.Header().Set("Cache-Control", "no-store")
		a.tianyiQRPoll(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.PathValue("provider") != "quark" && r.PathValue("provider") != "115" {
		fail(w, 400, errors.New("此存储类型不支持扫码授权"))
		return
	}
	var input struct {
		Token  string `json:"token"`
		Device string `json:"device,omitempty"`
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
		if input.Device != "" && input.Device != session.Device {
			fail(w, 400, errors.New("设备类型已变化，请重新扫码"))
			return
		}
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

func (a *App) start115Authorization(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Device string `json:"device"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !valid115Device(input.Device) {
		fail(w, 400, errors.New("请选择 CK 对应的设备类型"))
		return
	}
	qr, err := new115Client(r.Context()).QRCodeStart()
	if err != nil {
		fail(w, 502, errors.New("无法获取 115 官方登录二维码，请重试"))
		return
	}
	image, err := qr.QRCode()
	if err != nil || qr.UID == "" || qr.Sign == "" {
		fail(w, 502, errors.New("115 返回了无效二维码"))
		return
	}
	session := authorizationSession{Provider: "115", Device: input.Device, QR115: qr, Expires: time.Now().Add(5 * time.Minute).Unix(), Owner: authorizationOwner(r)}
	token, err := a.sealAuthorization(session)
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"token": token, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(image), "expiresIn": 300})
}

func (a *App) poll115Authorization(w http.ResponseWriter, r *http.Request, session authorizationSession) {
	if session.QR115 == nil || !valid115Device(session.Device) {
		fail(w, 400, errors.New("请重新获取 115 二维码"))
		return
	}
	c := new115Client(r.Context())
	result, err := c.QRCodeStatus(session.QR115)
	if err != nil {
		fail(w, 502, errors.New("115 授权状态查询失败，请重试"))
		return
	}
	switch {
	case result.IsAllowed():
		cr, err := c.QRCodeLoginWithApp(session.QR115, driver.LoginApp(session.Device))
		if err != nil {
			fail(w, 502, errors.New("115 未返回有效 CK，请重新扫码"))
			return
		}
		if _, err := credential115(cr.Cookie()); err != nil {
			fail(w, 502, err)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "success", "cookie": cr.Cookie(), "device": session.Device})
	case result.IsExpired(), result.IsCanceled():
		jsonResponse(w, 200, map[string]string{"status": "expired"})
	default:
		jsonResponse(w, 200, map[string]string{"status": "waiting"})
	}
}
