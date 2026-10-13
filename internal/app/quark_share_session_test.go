package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQuarkShareSessionUsesOneTokenAndRotatedCookies(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "__puus=original; other=keep"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	tokens, saves := 0, 0
	old := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		headers, body := http.Header{}, ""
		expectCookie := func(value string) {
			if r.Header.Get("Cookie") != "__puus="+value+"; other=keep" {
				t.Fatalf("cookie not continued at %s", r.URL.Path)
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			tokens++
			expectCookie("original")
			headers.Add("Set-Cookie", "__puus=root; Path=/; Secure; HttpOnly")
			body = `{"code":0,"data":{"stoken":"one-session"}}`
		case strings.HasSuffix(r.URL.Path, "/detail"):
			if r.URL.Query().Get("stoken") != "one-session" {
				t.Fatal("share token changed")
			}
			parent := r.URL.Query().Get("pdir_fid")
			if parent == "0" {
				expectCookie("root")
				headers.Add("Set-Cookie", "__puus=child; Path=/")
				body = `{"code":0,"data":{"list":[{"fid":"dir","file_name":"Show","dir":true,"share_fid_token":"directory-token"}]},"metadata":{"_total":1}}`
			} else if parent == "dir" {
				expectCookie("child")
				headers.Add("Set-Cookie", "__puus=ready; Path=/")
				body = `{"code":0,"data":{"list":[{"fid":"f1","file_name":"Film.mkv","share_fid_token":"one-session-file"}]},"metadata":{"_total":1}}`
			} else {
				t.Fatal("unexpected source parent")
			}
		case strings.HasSuffix(r.URL.Path, "/sort"):
			expectCookie("ready")
			body = `{"code":0,"data":{"list":[{"fid":"target","file_name":"Show","dir":true}]}}`
		case strings.HasSuffix(r.URL.Path, "/save"):
			saves++
			expectCookie("ready")
			var payload struct {
				Token  string   `json:"stoken"`
				Parent string   `json:"pdir_fid"`
				Tokens []string `json:"fid_token_list"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Token != "one-session" || payload.Parent != "dir" || len(payload.Tokens) != 1 || payload.Tokens[0] != "one-session-file" {
				t.Fatal("mixed share credentials", payload)
			}
			headers.Add("Set-Cookie", "__puus=submitted; Path=/")
			body = `{"code":0,"data":{"task_id":"job"}}`
		case strings.HasSuffix(r.URL.Path, "/task"):
			expectCookie("submitted")
			body = `{"code":0,"data":{"status":2}}`
		default:
			t.Fatal("unexpected endpoint", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { apiClient.Transport = old })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/share/preview", map[string]any{"storageId": "q", "url": "https://pan.quark.cn/s/code"}, cookie)
	var preview struct{ Preview string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Preview == "" || tokens != 1 {
		t.Fatal(w.Code, w.Body.String(), tokens)
	}
	for _, secret := range []string{"one-session", "__puus", "directory-token"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("preview leaked credentials")
		}
	}
	input := map[string]any{"storageId": "q", "preview": preview.Preview, "parent": "0", "batch": 0}
	if w = request(t, h, "POST", "/api/files/share/batch", input, cookie); w.Code != 200 || saves != 1 {
		t.Fatal(w.Code, w.Body.String(), saves)
	}
	if w = request(t, h, "POST", "/api/files/share/status", input, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "completed") {
		t.Fatal(w.Code, w.Body.String())
	}
	stored, err := a.store.storage(s.ID)
	if err != nil || stored.Config["cookie"] != s.Config["cookie"] {
		t.Fatal("preview changed configured credentials", err)
	}
}

func TestQuarkShareLegacySaveContinuesRotatedCookie(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "__puus=original"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	old := apiClient.Transport
	saves := 0
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		body, header := "", http.Header{}
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			header.Add("Set-Cookie", "__puus=ready; Path=/")
			body = `{"code":0,"data":{"stoken":"token"}}`
		case strings.HasSuffix(r.URL.Path, "/detail"):
			body = `{"code":0,"data":{"list":[{"fid":"f","file_name":"Film.mkv","share_fid_token":"file-token"}]},"metadata":{"_total":1}}`
		case strings.HasSuffix(r.URL.Path, "/sort"):
			body = `{"code":0,"data":{"list":[]}}`
		case strings.HasSuffix(r.URL.Path, "/save"):
			saves++
			body = `{"code":0,"data":{"task_id":"job"}}`
		default:
			t.Fatal("unexpected endpoint")
		}
		if !strings.HasSuffix(r.URL.Path, "/token") && r.Header.Get("Cookie") != "__puus=ready" {
			t.Fatal("stale cookie", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { apiClient.Transport = old })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/share/preview", map[string]any{"storageId": "q", "url": "https://pan.quark.cn/s/code"}, cookie)
	var preview struct{ Preview string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/files/share/save", map[string]any{"storageId": "q", "preview": preview.Preview, "parent": "0", "ids": []string{"f"}}, cookie)
	if w.Code != 200 || saves != 1 {
		t.Fatal(w.Code, w.Body.String(), saves)
	}
}

func TestQuarkShareStageErrorsAndCookieOrigin(t *testing.T) {
	old := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = old })
	for endpoint, stage := range map[string]string{"/share/sharepage/token": "获取分享令牌", "/share/sharepage/detail": "读取分享目录", "/share/sharepage/save": "提交转存", "/task": "查询转存结果"} {
		calls := 0
		apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("private")), Request: r}, nil
		})
		err := shareRequest(context.Background(), Storage{Type: "quark"}, "POST", "https://drive.quark.cn/1/clouddrive"+endpoint, nil, &map[string]any{})
		if err == nil || calls != 1 || !strings.Contains(err.Error(), stage) {
			t.Fatal(stage, calls, err)
		}
	}
	s := Storage{Type: "quark", Config: map[string]string{"cookie": "__puus=original"}}
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		foreign, _ := http.NewRequest("GET", "https://attacker.example/", nil)
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": {"__puus=foreign; Path=/"}}, Body: io.NopCloser(strings.NewReader(`{"code":0}`)), Request: foreign}, nil
	})
	if err := shareSessionRequest(context.Background(), &s, "GET", "https://drive.quark.cn/1/clouddrive/task", nil, &map[string]any{}); err != nil || s.Config["cookie"] != "__puus=original" {
		t.Fatal("absorbed cross-origin cookie", err)
	}
}

func TestQuarkShare403IsStageSpecificAndNeverReplayed(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private-cookie"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	old := apiClient.Transport
	saves := 0
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		body, status := `{"code":0,"data":{"list":[]}}`, 200
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			body = `{"code":0,"data":{"stoken":"private-token"}}`
		case strings.HasSuffix(r.URL.Path, "/detail"):
			body = `{"code":0,"data":{"list":[{"fid":"f","file_name":"Film.mkv","share_fid_token":"private-file-token"}]},"metadata":{"_total":1}}`
		case strings.HasSuffix(r.URL.Path, "/save"):
			saves++
			status, body = 403, "private upstream response"
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://attacker.example/"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { apiClient.Transport = old })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/share/preview", map[string]any{"storageId": "q", "url": "https://pan.quark.cn/s/code"}, cookie)
	var preview struct{ Preview string }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	input := map[string]any{"storageId": "q", "preview": preview.Preview, "parent": "0", "batch": 0}
	w = request(t, h, "POST", "/api/files/share/batch", input, cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "提交转存") || !strings.Contains(w.Body.String(), "HTTP 403") || !strings.Contains(w.Body.String(), "勿直接重复提交") || saves != 1 {
		t.Fatal(w.Code, w.Body.String(), saves)
	}
	for _, secret := range []string{"private-cookie", "private-token", "private-file-token", "private upstream", "attacker"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("failure leaked credentials")
		}
	}
	if w = request(t, h, "POST", "/api/files/share/batch", input, cookie); w.Code != 409 || saves != 1 {
		t.Fatal("failed write replayed", w.Code, saves)
	}
}
