package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPluginsConfigurationAndRealRequests(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-api" {
			t.Error("missing API credential")
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "local-model" {
				t.Error("wrong model")
			}
			jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "标题：Arrival；年份：2016"}}}})
		case "/3/configuration":
			jsonResponse(w, 200, map[string]any{"images": map[string]string{"secure_base_url": "https://image.tmdb.org/"}})
		case "/3/search/multi":
			if r.URL.Query().Get("language") != "zh-CN" || r.URL.Query().Get("query") != "Arrival" {
				t.Error("missing search parameters")
			}
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 329865, "title": "降临"}}})
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	for _, kind := range []string{"ai", "tmdb"} {
		p := PluginConfig{Enabled: true, APIURL: upstream.URL, ImageURL: upstream.URL, Model: "local-model", APIKey: "secret-api", Language: "zh-CN"}
		if kind == "ai" {
			p.APIURL += "/v1"
		}
		w := request(t, h, "PUT", "/api/plugins/"+kind, p, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = request(t, h, "GET", "/api/plugins/"+kind, nil, cookie)
		if strings.Contains(w.Body.String(), "secret-api") {
			t.Fatal("secret leaked")
		}
		p.APIKey = "********"
		w = request(t, h, "POST", "/api/plugins/"+kind+"/test", map[string]any{"config": p}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		action := "search"
		if kind == "ai" {
			action = "recognize"
		}
		w = request(t, h, "POST", "/api/plugins/"+kind+"/"+action, map[string]string{"text": "Arrival"}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = request(t, h, "POST", "/api/plugins/"+kind+"/secret", map[string]string{"field": "apiKey"}, cookie)
		if !strings.Contains(w.Body.String(), "secret-api") {
			t.Fatal("explicit reveal failed")
		}
	}
	proxyCalls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls++
		// A local HTTP TMDB base lets the fixture exercise an actual forward proxy.
		if r.URL.Host != "metadata.example" {
			t.Error("wrong proxy target", r.URL.Host)
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}))
	defer proxy.Close()
	w := request(t, h, "PUT", "/api/plugins/proxy", PluginConfig{Enabled: true, Address: proxy.URL}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p := PluginConfig{APIURL: "http://metadata.example", ImageURL: "https://image.tmdb.org", Language: "zh-CN"}
	w = request(t, h, "POST", "/api/plugins/tmdb/test", map[string]any{"config": p}, cookie)
	if w.Code != 200 || proxyCalls != 1 {
		t.Fatal("proxy not used", w.Code, proxyCalls)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initTools(a.store.toolsDir)
	}
	if err != nil || reopened.snapshot().Plugins["ai"].APIKey != "secret-api" {
		t.Fatal("plugin persistence failed", err)
	}
	for _, endpoint := range []string{"/api/plugins/ai", "/api/plugins/ai/secret"} {
		method := "GET"
		if strings.HasSuffix(endpoint, "/secret") {
			method = "POST"
		}
		if w := request(t, h, method, endpoint, nil, nil); w.Code != 401 {
			t.Fatal("unprotected plugin", endpoint, w.Code)
		}
	}
}

func TestEmbyWebhookAuthenticationAndNotifications(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	w := request(t, h, "PUT", "/api/plugins/emby", PluginConfig{Enabled: true}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	token := a.store.snapshot().Plugins["emby"].Token
	if token != "aether" {
		t.Fatal("unexpected default token")
	}
	body := map[string]any{"Event": "library.new", "Item": map[string]string{"Id": "1", "Name": "测试影片"}}
	send := func(token string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/emby/webhook?token="+token, strings.NewReader(string(payload)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w = request(t, h, "POST", "/api/emby/webhook?token=incorrect", body, nil)
	if w.Code != 403 {
		t.Fatal("bad token accepted")
	}
	for range 2 {
		w = send(token)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(a.store.snapshot().LibraryNotices) != 1 {
		t.Fatal("duplicate webhook not suppressed")
	}
	w = request(t, h, "GET", "/api/state", nil, cookie)
	if !strings.Contains(w.Body.String(), "测试影片") || strings.Contains(w.Body.String(), token) {
		t.Fatal("notification state invalid")
	}
	request(t, h, "PUT", "/api/plugins/emby", PluginConfig{Enabled: false, Token: token}, cookie)
	w = request(t, h, "POST", "/api/emby/webhook?token="+token, body, nil)
	if w.Code != 403 {
		t.Fatal("disabled webhook accepted")
	}
}

func TestTianyiXMLDirectoryAndTokenRefresh(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "token189", Name: "天翼", Type: "tianyi", Enabled: true, Config: map[string]string{"authMode": "token", "accessToken": "expired", "refreshToken": "refresh", "root": "-11"}}
	if err := validateStorage(&s); err != nil {
		t.Fatal(err)
	}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	original := apiClient
	defer func() { apiClient = original }()
	listBody := `<fileList><fileListAO><folderList><folder><id>123</id><name>Movies</name><lastOpTime>2026-10-07 10:20:30</lastOpTime></folder></folderList><fileList><file><id>456</id><name>Arrival.mkv</name><size>99</size></file></fileList></fileListAO></fileList>`
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/getSessionForPC.action":
			if r.URL.Query().Get("accessToken") == "expired" {
				body = `{"errorCode":"UserInvalidOpenToken"}`
			} else {
				body = `{"sessionKey":"session","sessionSecret":"secret"}`
			}
		case "/api/oauth2/refreshToken.do":
			r.ParseForm()
			if r.Form.Get("refreshToken") != "refresh" {
				t.Error("wrong refresh token")
			}
			body = `{"accessToken":"new-access","refreshToken":"new-refresh","result":0}`
		case "/listFiles.action":
			body = listBody
		default:
			t.Fatal(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	files, err := a.tianyiList(context.Background(), s, "/")
	if err != nil || len(files) != 2 || files[0].Name != "Movies" || !files[0].IsDir || files[0].Modified.IsZero() {
		t.Fatal(files, err)
	}
	s = a.store.snapshot().Storages[0]
	if s.Config["accessToken"] != "new-access" || s.Config["refreshToken"] != "new-refresh" {
		t.Fatal("rotated tokens not persisted")
	}
	listBody = `<listFiles><fileList><count>1</count><folder><id>321</id><name>Books</name></folder></fileList></listFiles>`
	files, err = a.tianyiList(context.Background(), s, "/")
	if err != nil || len(files) != 1 || files[0].Name != "Books" {
		t.Fatal("alternate XML envelope", files, err)
	}
	listBody = `{"code":"SUCCESS"}`
	if _, err := a.tianyiList(context.Background(), s, "/"); err == nil {
		t.Fatal("invalid list silently accepted")
	}
}

func TestTianyiQRCodeReturnsTokens(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/api/portal/unifyLoginForPC.action":
			if r.URL.Query().Get("timeStamp") == "" {
				t.Fatal("missing QR login timestamp")
			}
			body = `lt="lt"; reqId="req"; paramId="param";`
		case "/api/logbox/oauth2/getUUID.do":
			if len(r.Header.Get("User-Finger")) != 10 {
				t.Fatal("missing QR device fingerprint")
			}
			body = `{"uuid":"https://open.e.189.cn/qr/test","encryuuid":"encrypted"}`
		case "/api/logbox/oauth2/qrcodeLoginState.do":
			if len(r.Header.Get("User-Finger")) != 10 {
				t.Fatal("poll lost device fingerprint")
			}
			body = `{"status":0,"redirectUrl":"https://cloud.189.cn/return"}`
		case "/getSessionForPC.action":
			body = `{"accessToken":"access","refreshToken":"refresh"}`
		default:
			t.Fatal(r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	w := request(t, h, "POST", "/api/authorization/tianyi/start", nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var qr map[string]any
	json.Unmarshal(w.Body.Bytes(), &qr)
	w = request(t, h, "POST", "/api/authorization/tianyi/poll", map[string]any{"token": qr["token"]}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"accessToken":"access"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
