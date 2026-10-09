package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
					if r.Header.Get("User-Agent") != pan115ShareUA {
						t.Error("share request missing full browser UA")
					}
					if r.Method != "GET" || r.URL.Host != "115cdn.com" || r.URL.Path != "/webapi/share/snap" || r.URL.Query().Get("format") != "json" || r.Header.Get("Referer") != "https://115cdn.com/s/code?password=1234&" {
						t.Error("incorrect 115 share preview protocol", r.Method, r.URL.Host, r.URL.Path)
					}
					return `{"state":true,"data":{"count":1,"list":[{"fid":"f","n":"Film.mkv"}]}}`
				case strings.HasSuffix(r.URL.Path, "/getOutLinkInfoV6"):
					return `{"code":"0","data":{"nodNum":1,"coLst":[{"coId":"f","coName":"Film.mkv","coPath":"root/f"}]}}`
				default:
					saves++
					b, _ := io.ReadAll(r.Body)
					switch provider {
					case "115":
						if r.Method != "POST" || r.URL.Host != "webapi.115.com" || r.URL.Path != "/share/receive" || r.Header.Get("Referer") != "https://115cdn.com/s/code?password=1234&" {
							t.Error("incorrect 115 share receive protocol")
						}
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

func Test115SharePreview405Fallback(t *testing.T) {
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	for _, status := range []int{405, 302, 403, 500, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			calls := 0
			query := url.Values{"share_code": {"private-code"}, "receive_code": {"private-pass"}, "cid": {"0"}, "offset": {"100"}, "limit": {"100"}, "format": {"json"}}
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				wantHost, wantPath := "115cdn.com", "/webapi/share/snap"
				if calls == 2 {
					wantHost, wantPath = "webapi.115.com", "/share/snap"
				}
				if r.Method != http.MethodGet || r.URL.Host != wantHost || r.URL.Path != wantPath || r.URL.RawQuery != query.Encode() || r.Header.Get("Cookie") != "private-cookie" || r.Header.Get("User-Agent") != pan115ShareUA || r.Header.Get("Referer") != "https://115cdn.com/s/private-code?password=private-pass&" {
					t.Error("unexpected preview request protocol")
				}
				code, body := status, "private upstream body"
				if calls == 2 {
					code, body = 200, `{"state":true,"data":{"count":1}}`
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Location": {"https://attacker.example/"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			var result struct {
				Data struct {
					Count int `json:"count"`
				} `json:"data"`
			}
			err := read115Share(context.Background(), Storage{Type: "115", Config: map[string]string{"cookie": "private-cookie"}}, query, &result)
			wantCalls := 1
			if status == 405 {
				wantCalls = 2
			}
			if calls != wantCalls || status == 405 && (err != nil || result.Data.Count != 1) || status != 405 && err == nil {
				t.Fatal(calls, result, err)
			}
			if err != nil {
				for _, secret := range []string{"private-cookie", "private-code", "private-pass", "attacker", "private upstream body"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatal("secret in error")
					}
				}
			}
		})
	}
}

func Test115SharePreviewFallbackStopsAfterSecondFailureOrCancellation(t *testing.T) {
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	for _, cancelRequest := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if cancelRequest {
				cancel()
			}
			return &http.Response{StatusCode: 405, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})
		err := read115Share(ctx, Storage{Type: "115"}, url.Values{}, &map[string]any{})
		cancel()
		wantCalls := 2
		if cancelRequest {
			wantCalls = 1
		}
		if err == nil || calls != wantCalls {
			t.Fatal(cancelRequest, calls, err)
		}
	}
}

func Test115ShareHTTPFailureDoesNotRetryOrLeakSecrets(t *testing.T) {
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	for _, status := range []int{405, 302, 500} {
		calls := 0
		apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://attacker.example/?secret"}}, Body: io.NopCloser(strings.NewReader("private upstream body")), Request: r}, nil
		})
		s := Storage{Type: "115", Config: map[string]string{"cookie": "private-cookie"}}
		_, err := submitShare(context.Background(), s, sharePreview{Code: "private-code", Pass: "private-pass"}, "target", []shareEntry{{ID: "file", Name: "test"}})
		if err == nil || calls != 1 || !strings.Contains(err.Error(), "分享转存失败") {
			t.Fatal(calls, err)
		}
		for _, secret := range []string{"private-cookie", "private-code", "private-pass", "attacker", "private upstream body"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatal("secret in error")
			}
		}
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
