package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"aether/internal/linkcore/proxy"
)

func TestCloudOfflineEndpointAndBT(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "offline115", Type: "115", Enabled: true, Config: map[string]string{"accessToken": "secret"}}
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	old := apiClient
	defer func() { apiClient = old }()
	seedName, submitted := "", 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		payload := `{"state":true,"data":{}}`
		switch r.URL.Path {
		case "/open/ufile/files":
			if r.URL.Query().Get("cid") == "seed-folder" {
				raw, _ := json.Marshal(map[string]any{"state": true, "data": []map[string]any{{"file_id": "seed", "file_name": seedName, "file_category": 1, "pick_code": "pick"}}})
				payload = string(raw)
			} else {
				payload = `{"state":true,"data":[{"file_id":"seed-folder","file_name":"Aether种子","file_category":0}]}`
			}
		case "/open/upload/init":
			r.ParseForm()
			seedName = r.Form.Get("file_name")
			if r.Form.Get("target") != "U_1_seed-folder" {
				t.Fatal("wrong seed destination", r.Form)
			}
			payload = `{"state":true,"data":{"status":2}}`
		case "/open/offline/torrent":
			r.ParseForm()
			if r.Form.Get("pick_code") != "pick" || len(r.Form.Get("torrent_sha1")) != 40 {
				t.Fatal("missing seed identifiers")
			}
			payload = `{"state":true,"data":{"info_hash":"hash","torrent_name":"Media.torrent","torrent_filelist":[{},{}]}}`
		case "/open/offline/add_task_bt":
			r.ParseForm()
			if r.Form.Get("wanted") != "0,1" || r.Form.Get("wp_path_id") != "target" || r.Form.Get("save_path") != "Media" {
				t.Fatal("wrong BT submission", r.Form)
			}
			submitted++
		case "/open/offline/add_task_urls":
			payload = `{"state":true,"data":[{"state":true,"url":"https://example.com/a","info_hash":"a"},{"state":false,"url":"https://example.com/b","message":"quota"}]}`
		default:
			t.Fatal("unexpected request", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	if request(t, h, "POST", "/api/files/offline", nil, nil).Code != 401 {
		t.Fatal("offline endpoint unprotected")
	}
	input := map[string]any{"storageId": s.ID, "parent": "target", "urls": []string{"https://example.com/a", "https://example.com/b"}}
	response := request(t, h, "POST", "/api/files/offline", input, cookie)
	var results []offlineResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &results) != nil || len(results) != 2 || !results[0].Success || results[1].Success {
		t.Fatal(response.Body.String())
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("storageId", s.ID)
	writer.WriteField("parent", "target")
	for _, name := range []string{"one.torrent", "two.torrent"} {
		part, _ := writer.CreateFormFile("torrents", name)
		part.Write([]byte("d4:infod4:name4:testee"))
	}
	writer.Close()
	req := httptest.NewRequest("POST", "/api/files/offline", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	record := httptest.NewRecorder()
	h.ServeHTTP(record, req)
	if record.Code != 200 || submitted != 2 || json.Unmarshal(record.Body.Bytes(), &results) != nil || len(results) != 2 || !results[0].Success || !results[1].Success {
		t.Fatal(record.Code, record.Body.String(), submitted)
	}
}

func TestOffline115URLProtocol(t *testing.T) {
	original := apiClient
	defer func() { apiClient = original }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/open/offline/add_task_urls" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("wrong offline request", r.URL.Path)
		}
		r.ParseForm()
		if r.Form.Get("wp_path_id") != "12" || r.Form.Get("urls") != "magnet:?xt=urn:btih:test\nhttps://example.com/media" {
			t.Fatal("wrong destination or batch", r.Form)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"state":true,"data":[{"state":true,"url":"magnet:?xt=urn:btih:test","info_hash":"hash"}]}`))}, nil
	})}
	var result []map[string]any
	s := Storage{Type: "115", Config: map[string]string{"accessToken": "secret"}}
	// The same encoded values are sent by the batched endpoint.
	err := offline115(context.Background(), s, "add_task_urls", url.Values{"wp_path_id": {"12"}, "urls": {"magnet:?xt=urn:btih:test\nhttps://example.com/media"}}, &result)
	if err != nil || len(result) != 1 {
		t.Fatal(result, err)
	}
}

func TestLinkRequestIPLocalAndExplicitProxy(t *testing.T) {
	t.Setenv("AETHER_TRUSTED_PROXIES", "172.18.0.2/32,fd00::2/128,0.0.0.0/0,invalid")
	trusted := linkTrustedProxies()
	for _, test := range []struct{ peer, header, want string }{
		{"127.0.0.1:123", "203.0.113.4", "203.0.113.4"},
		{"[::1]:123", "2001:db8::7", "2001:db8::7"},
		{"172.18.0.2:123", "2001:db8::8", "2001:db8::8"},
		{"198.51.100.1:123", "10.0.0.1", "198.51.100.1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = test.peer
		r.Header.Set("X-Forwarded-For", test.header)
		if got := proxy.ClientIP(r, trusted...); got != test.want {
			t.Fatal(test, got)
		}
	}
}
