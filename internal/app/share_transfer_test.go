package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseShareLink(t *testing.T) {
	for _, tc := range []struct{ raw, provider, code, pass string }{
		{"https://115.com/s/abc?password=1234", "115", "abc", "1234"},
		{"分享 https://pan.quark.cn/s/abc 提取码：abcd", "quark", "abc", "abcd"},
		{"https://yun.139.com/shareweb/#abc", "", "", ""},
		{"https://yun.139.com/shareweb/abc", "mobile", "abc", ""},
		{"https://yun.139.com/shareweb/#/w/i/abc", "mobile", "abc", ""},
		{"https://yun.139.com/w/r?linkID=abc&passwd=XD7G", "mobile", "abc", "XD7G"},
		{"https://caiyun.139.com/m/i?abc", "mobile", "abc", ""},
		{"https://pan.quark.cn.evil.test/s/abc", "", "", ""},
		{"https://user:pass@115.com/s/abc", "", "", ""},
		{"https://115.com:8443/s/abc", "", "", ""},
		{"https://115.com/s/abc https://115.com/s/def", "", "", ""},
	} {
		provider, code, pass, err := parseShareLink(tc.raw, "")
		if tc.provider == "" {
			if err == nil {
				t.Fatal("accepted", tc.raw)
			}
			continue
		}
		if err != nil || provider != tc.provider || code != tc.code || pass != tc.pass {
			t.Fatal(tc, provider, code, pass, err)
		}
	}
}

func shareMock(t *testing.T, fn func(*http.Request) string) {
	t.Helper()
	old := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fn(r))), Request: r}, nil
	})
	t.Cleanup(func() { apiClient.Transport = old })
}

func TestShareProtocols(t *testing.T) {
	for _, provider := range []string{"115", "quark", "mobile"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: provider, Type: provider, Config: map[string]string{"cookie": "secret", "mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
			saves := 0
			shareMock(t, func(r *http.Request) string {
				if provider == "mobile" && r.Header.Get("Authorization") == "" {
					t.Error("missing auth")
				}
				if provider != "mobile" && r.Header.Get("Cookie") != "secret" {
					t.Error("missing cookie")
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/token"):
					return `{"code":0,"data":{"stoken":"private-token"}}`
				case strings.HasSuffix(r.URL.Path, "/detail"):
					return `{"code":0,"data":{"list":[{"fid":"f","file_name":"Film.mkv","share_fid_token":"file-token"}]},"metadata":{"_total":1}}`
				case strings.HasSuffix(r.URL.Path, "/snap"):
					return `{"state":true,"data":{"count":1,"list":[{"fid":"f","n":"Film.mkv"}]}}`
				case strings.HasSuffix(r.URL.Path, "/getOutLinkInfoV6"):
					return `{"code":"0","data":{"nodNum":1,"coLst":[{"coId":"f","coName":"Film.mkv","coPath":"root/f"}]}}`
				default:
					saves++
					b, _ := io.ReadAll(r.Body)
					switch provider {
					case "115":
						if !strings.Contains(string(b), "file_id=f") || !strings.Contains(string(b), "cid=target") {
							t.Error(string(b))
						}
						return `{"state":true}`
					case "quark":
						if !strings.Contains(string(b), `"fid_token_list":["file-token"]`) || !strings.Contains(string(b), `"to_pdir_fid":"target"`) {
							t.Error(string(b))
						}
						return `{"code":0,"data":{"task_id":"job"}}`
					default:
						if !strings.Contains(string(b), `"contentInfoList":["root/f"]`) || !strings.Contains(string(b), `"newCatalogID":"target"`) {
							t.Error(string(b))
						}
						return `{"code":"0","data":{"taskID":"job"}}`
					}
				}
			})
			token, items, err := a.readShare(context.Background(), s, "code", "1234")
			if err != nil || len(items) != 1 {
				t.Fatal(items, err)
			}
			b, _ := json.Marshal(items)
			if strings.Contains(string(b), "token") || strings.Contains(string(b), "root/f") {
				t.Fatal("leaked share capability", string(b))
			}
			task, err := submitShare(context.Background(), s, sharePreview{Code: "code", Pass: "1234", Token: token}, "target", items)
			if err != nil || saves != 1 || provider != "115" && task != "job" {
				t.Fatal(task, saves, err)
			}
		})
	}
}

func TestShareAPIReplayConflictAndStatus(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Name: "Quark", Enabled: true, Config: map[string]string{"cookie": "secret"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	saves, conflict := 0, false
	shareMock(t, func(r *http.Request) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			return `{"code":0,"data":{"stoken":"private-token"}}`
		case strings.HasSuffix(r.URL.Path, "/detail"):
			return `{"code":0,"data":{"list":[{"fid":"f","file_name":"Film.mkv","share_fid_token":"file-token"}]},"metadata":{"_total":1}}`
		case strings.HasSuffix(r.URL.Path, "/sort"):
			if conflict {
				return `{"code":0,"data":{"list":[{"fid":"old","file_name":"Film.mkv"}]}}`
			}
			return `{"code":0,"data":{"list":[]}}`
		case strings.HasSuffix(r.URL.Path, "/task"):
			return `{"code":0,"data":{"status":2}}`
		default:
			saves++
			return `{"code":0,"data":{"task_id":"job"}}`
		}
	})
	h := a.Handler(t.TempDir())
	input := map[string]any{"storageId": "q", "url": "https://pan.quark.cn/s/abc"}
	if w := request(t, h, "POST", "/api/files/share/preview", input, nil); w.Code != 401 {
		t.Fatal("unprotected")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "POST", "/api/files/share/preview", input, cookie)
	var preview struct {
		Preview string `json:"preview"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Preview == "" || strings.Contains(w.Body.String(), "private-token") {
		t.Fatal(w.Code, w.Body.String())
	}
	input = map[string]any{"storageId": "q", "preview": preview.Preview, "parent": "0", "ids": []string{"f"}}
	conflict = true
	if w = request(t, h, "POST", "/api/files/share/save", input, cookie); w.Code != 409 || saves != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	conflict = false
	if w = request(t, h, "POST", "/api/files/share/save", input, cookie); w.Code != 200 || saves != 1 || !strings.Contains(w.Body.String(), "submitted") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(t, h, "POST", "/api/files/share/save", input, cookie); w.Code != 409 || saves != 1 {
		t.Fatal("replay", w.Code)
	}
	if w = request(t, h, "POST", "/api/files/share/status", input, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "completed") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestShareRefusesCredentialRedirect(t *testing.T) {
	old := apiClient.Transport
	calls := 0
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://attacker.example/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	defer func() { apiClient.Transport = old }()
	err := shareRequest(context.Background(), Storage{Type: "quark", Config: map[string]string{"cookie": "secret"}}, "GET", "https://drive.quark.cn/test", nil, &map[string]any{})
	if err == nil || calls != 1 {
		t.Fatal("redirect followed", calls, err)
	}
}

func TestSharePreviewRejectsChangedCredentials(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "new"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	r, _ := http.NewRequest("POST", "/", nil)
	r.AddCookie(cookie)
	a.sharePreviews = map[string]*sharePreview{"p": {Storage: "q", Owner: authorizationOwner(r), Expires: time.Now().Add(time.Minute), Config: shareConfig(Storage{Config: map[string]string{"cookie": "old"}})}}
	w := request(t, h, "POST", "/api/files/share/save", map[string]any{"storageId": "q", "preview": "p", "ids": []string{"f"}}, cookie)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
}
