package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

func tianyiCredentials(s Storage) [32]byte {
	if s.Config["accessToken"] == "" && s.Config["refreshToken"] == "" {
		return sha256.Sum256([]byte(s.Config["username"] + "\x00" + s.Config["password"]))
	}
	return sha256.Sum256([]byte(s.Config["username"] + "\x00" + s.Config["password"] + "\x00" + s.Config["accessToken"] + "\x00" + s.Config["refreshToken"]))
}

func (a *App) tianyiTokenSession(ctx context.Context, s Storage, client *http.Client) (tianyiSession, error) {
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	access, refresh := s.Config["accessToken"], s.Config["refreshToken"]
	fetch := func() (tianyiSession, error) {
		var session tianyiSession
		raw, _, err := tianyiHTTP(ctx, client, "GET", tianyiAPI+"/getSessionForPC.action",
			url.Values{"appId": {tianyiAppID}, "accessToken": {access}, "clientType": {"TELEPC"}, "returnType": {"JSON"}, "version": {"7.2.4.0"}, "channelId": {"web_cloud.189.cn"}}, http.Header{"X-Request-Id": {id()}, "Referer": {"https://cloud.189.cn/"}})
		if err == nil {
			err = tianyiDecode(raw, &session)
		}
		if err == nil && (session.Key == "" || session.Secret == "") {
			err = errors.New("天翼令牌未返回有效会话")
		}
		return session, err
	}
	if access != "" && ctx.Value(forceTianyiRefreshKey{}) != true {
		if session, err := fetch(); err == nil {
			return session, nil
		} else {
			var auth *storageAuthError
			if !errors.As(err, &auth) {
				return tianyiSession{}, err
			}
		}
	}
	if refresh == "" {
		return tianyiSession{}, authStorageError(errors.New("天翼访问令牌失效，请填写刷新令牌或重新扫码"), true)
	}
	raw, _, err := tianyiHTTP(ctx, client, "POST", tianyiAuth+"/api/oauth2/refreshToken.do",
		url.Values{"clientId": {tianyiAppID}, "refreshToken": {refresh}, "grantType": {"refresh_token"}, "format": {"json"}}, nil)
	var tokens struct {
		Access  string `json:"accessToken"`
		Refresh string `json:"refreshToken"`
	}
	if err != nil {
		return tianyiSession{}, err
	}
	if json.Unmarshal(raw, &tokens) != nil {
		return tianyiSession{}, errors.New("天翼刷新响应格式异常")
	}
	if tokens.Access == "" {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &result)
		if result.Error == "invalid_grant" || result.Error == "invalid_token" {
			return tianyiSession{}, authStorageError(errors.New("天翼刷新令牌失效，请重新扫码"), true)
		}
		return tianyiSession{}, errors.New("天翼刷新未返回有效令牌")
	}
	access = tokens.Access
	if tokens.Refresh != "" {
		refresh = tokens.Refresh
	}
	// Persist rotated credentials before requesting the session so a transient failure is recoverable.
	err = a.store.update(func(st *State) error {
		for i := range st.Storages {
			current := &st.Storages[i]
			if current.ID == s.ID {
				if tianyiCredentials(*current) != tianyiCredentials(s) {
					return errors.New("天翼存储凭据已变更，请重试")
				}
				current.Config["accessToken"], current.Config["refreshToken"] = access, refresh
				return nil
			}
		}
		return errors.New("天翼存储已删除")
	})
	if err != nil {
		return tianyiSession{}, err
	}
	session, err := fetch()
	if err == nil {
		updated := s
		updated.Config = map[string]string{"username": s.Config["username"], "password": s.Config["password"], "accessToken": access, "refreshToken": refresh}
		session.Credentials = tianyiCredentials(updated)
	}
	return session, err
}

func (a *App) tianyiQRStart(w http.ResponseWriter, r *http.Request) {
	client := tianyiClient()
	raw, final, err := tianyiHTTP(r.Context(), client, "GET", "https://cloud.189.cn/api/portal/unifyLoginForPC.action",
		url.Values{"appId": {tianyiAppID}, "clientType": {"10020"}, "returnURL": {tianyiReturn}, "timeStamp": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}, nil)
	if err != nil {
		fail(w, 502, err)
		return
	}
	values := map[string]string{}
	for _, key := range []string{"lt", "reqId", "paramId"} {
		values[key] = final.Query().Get(key)
		if values[key] == "" {
			match := regexp.MustCompile(`\b` + key + `\s*=\s*["']([^"']+)["']`).FindSubmatch(raw)
			if len(match) == 2 {
				values[key] = string(match[1])
			}
		}
	}
	values["finger"] = strconv.FormatInt(time.Now().UnixNano()%9000000000+1000000000, 10)
	headers := http.Header{"Lt": {values["lt"]}, "Reqid": {values["reqId"]}, "Referer": {final.String()}, "User-Finger": {values["finger"]}}
	if values["paramId"] == "" {
		raw, _, err = tianyiHTTP(r.Context(), client, "POST", tianyiAuth+"/api/logbox/oauth2/appConf.do", url.Values{"version": {"2.0"}, "appKey": {tianyiAppID}}, headers)
		var conf struct {
			Data struct {
				ParamID string `json:"paramId"`
			} `json:"data"`
		}
		if err == nil {
			_ = json.Unmarshal(raw, &conf)
			values["paramId"] = conf.Data.ParamID
		}
	}
	if values["lt"] == "" || values["reqId"] == "" || values["paramId"] == "" {
		fail(w, 502, errors.New("无法获取天翼扫码参数"))
		return
	}
	raw, _, err = tianyiHTTP(r.Context(), client, "POST", tianyiAuth+"/api/logbox/oauth2/getUUID.do", url.Values{"appId": {tianyiAppID}}, headers)
	var qr struct {
		UUID      string `json:"uuid"`
		Encrypted string `json:"encryuuid"`
	}
	if err != nil || json.Unmarshal(raw, &qr) != nil || qr.UUID == "" || qr.Encrypted == "" {
		fail(w, 502, errors.New("获取天翼二维码失败"))
		return
	}
	values["uuid"], values["encrypted"], values["referer"] = qr.UUID, qr.Encrypted, final.String()
	for _, host := range []string{tianyiAuth, tianyiAPI, "https://cloud.189.cn"} {
		u, _ := url.Parse(host)
		cookies, _ := json.Marshal(client.Jar.Cookies(u))
		values[host] = string(cookies)
	}
	token, err := a.sealAuthorization(authorizationSession{Provider: "tianyi", Cookies: values, Owner: authorizationOwner(r), Expires: time.Now().Add(5 * time.Minute).Unix()})
	if err != nil {
		fail(w, 500, err)
		return
	}
	png, err := qrcode.Encode(qr.UUID, qrcode.Medium, 256)
	if err != nil {
		fail(w, 502, errors.New("天翼二维码无效"))
		return
	}
	jsonResponse(w, 200, map[string]any{"token": token, "image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), "expiresIn": 300})
}

func (a *App) tianyiQRPoll(w http.ResponseWriter, r *http.Request) {
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
	v := session.Cookies
	client := tianyiClient()
	for _, host := range []string{tianyiAuth, tianyiAPI, "https://cloud.189.cn"} {
		var cookies []*http.Cookie
		_ = json.Unmarshal([]byte(v[host]), &cookies)
		u, _ := url.Parse(host)
		client.Jar.SetCookies(u, cookies)
	}
	now := time.Now()
	raw, _, err := tianyiHTTP(r.Context(), client, "POST", tianyiAuth+"/api/logbox/oauth2/qrcodeLoginState.do",
		url.Values{"appId": {tianyiAppID}, "clientType": {"1"}, "returnUrl": {tianyiReturn}, "paramId": {v["paramId"]}, "uuid": {v["uuid"]}, "encryuuid": {v["encrypted"]}, "cb_SaveName": {"3"}, "isOauth2": {"false"}, "state": {""}, "date": {now.Format("2006-01-0215:04:05.000")}, "timeStamp": {strconv.FormatInt(now.UnixMilli(), 10)}},
		http.Header{"Lt": {v["lt"]}, "Reqid": {v["reqId"]}, "Referer": {v["referer"]}, "User-Finger": {v["finger"]}})
	var result struct {
		Status   *int   `json:"status"`
		Redirect string `json:"redirectUrl"`
	}
	if err != nil || json.Unmarshal(raw, &result) != nil || result.Status == nil {
		fail(w, 502, errors.New("天翼扫码状态获取失败"))
		return
	}
	switch *result.Status {
	case -106, -11002:
		jsonResponse(w, 200, map[string]string{"status": "waiting"})
		return
	case -11001:
		jsonResponse(w, 200, map[string]string{"status": "expired"})
		return
	case 0:
	default:
		fail(w, 400, errors.New("天翼扫码未获授权，请重新扫码"))
		return
	}
	if !trustedTianyi(result.Redirect) {
		fail(w, 502, errors.New("天翼扫码返回无效授权地址"))
		return
	}
	address := tianyiAPI + "/getSessionForPC.action?" + url.Values{"redirectURL": {result.Redirect}, "returnType": {"JSON"}, "clientType": {"TELEPC"}, "version": {"7.2.4.0"}, "channelId": {"web_cloud.189.cn"}}.Encode()
	raw, _, err = tianyiHTTP(r.Context(), client, "POST", address, nil, http.Header{"X-Request-Id": {id()}, "Referer": {"https://cloud.189.cn/"}})
	var tokens tianyiSession
	if err == nil {
		err = tianyiDecode(raw, &tokens)
	}
	if err != nil || (tokens.AccessToken == "" && tokens.RefreshToken == "") {
		fail(w, 502, errors.New("天翼授权未返回令牌，请重新扫码"))
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "success", "accessToken": tokens.AccessToken, "refreshToken": tokens.RefreshToken})
}
