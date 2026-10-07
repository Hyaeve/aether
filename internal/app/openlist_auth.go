package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var openlistSessions = struct {
	sync.Mutex
	items map[[32]byte]openlistSession
}{items: map[[32]byte]openlistSession{}}

type openlistSession struct {
	token string
	until time.Time
}

type downloadUAKey struct{}

func openlistSessionKey(s Storage) [32]byte {
	return sha256.Sum256([]byte(s.ID + "\x00" + s.Config["address"] + "\x00" + s.Config["username"] + "\x00" + s.Config["password"]))
}

func openlistHeaders(ctx context.Context, s Storage) (http.Header, error) {
	if s.Config["authMode"] != "account" {
		return http.Header{"Authorization": {s.Config["token"]}}, nil
	}
	key := openlistSessionKey(s)
	openlistSessions.Lock()
	defer openlistSessions.Unlock()
	if cached, ok := openlistSessions.items[key]; ok && time.Now().Before(cached.until) {
		return http.Header{"Authorization": {cached.token}}, nil
	}
	var response struct {
		Code int `json:"code"`
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	// Login credentials must not follow a redirect to a different service.
	client := *apiClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	body, _ := json.Marshal(map[string]string{"username": s.Config["username"], "password": s.Config["password"]})
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(s.Config["address"], "/")+"/api/auth/login", strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.New("OpenList 登录地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, errors.New("OpenList 登录连接失败")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response) != nil {
		return nil, errors.New("OpenList 登录响应无效")
	}
	if response.Code == 402 {
		return nil, errors.New("OpenList 账号启用了双重验证，请改用 API 令牌")
	}
	if response.Code != 200 || response.Data.Token == "" {
		return nil, errors.New("OpenList 账号或密码错误，或登录被限制")
	}
	for k, v := range openlistSessions.items {
		if time.Now().After(v.until) {
			delete(openlistSessions.items, k)
		}
	}
	if len(openlistSessions.items) >= 256 {
		clear(openlistSessions.items)
	}
	openlistSessions.items[key] = openlistSession{response.Data.Token, time.Now().Add(30 * time.Minute)}
	return http.Header{"Authorization": {response.Data.Token}}, nil
}

func openlistJSON(ctx context.Context, s Storage, endpoint string, body, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		headers, err := openlistHeaders(ctx, s)
		if err != nil {
			return err
		}
		if endpoint == "get" && s.Config["passUA"] != "false" {
			if ua, ok := ctx.Value(downloadUAKey{}).(string); ok {
				headers.Set("User-Agent", ua)
			}
		}
		var raw json.RawMessage
		if err = requestJSON(ctx, "POST", strings.TrimRight(s.Config["address"], "/")+"/api/fs/"+endpoint, headers, body, &raw); err != nil {
			if attempt == 0 && s.Config["authMode"] == "account" && strings.Contains(err.Error(), "HTTP 401") {
				openlistSessions.Lock()
				delete(openlistSessions.items, openlistSessionKey(s))
				openlistSessions.Unlock()
				continue
			}
			return err
		}
		var status struct {
			Code int `json:"code"`
		}
		if err = json.Unmarshal(raw, &status); err != nil {
			return err
		}
		if status.Code == 401 && s.Config["authMode"] == "account" && attempt == 0 {
			openlistSessions.Lock()
			delete(openlistSessions.items, openlistSessionKey(s))
			openlistSessions.Unlock()
			continue
		}
		return json.Unmarshal(raw, out)
	}
	return errors.New("OpenList 登录已失效")
}

func openlistDirectoryPassword(s Storage) string {
	if s.Config["authMode"] == "account" {
		return ""
	}
	return s.Config["password"] // Preserve existing directory-password configurations.
}
