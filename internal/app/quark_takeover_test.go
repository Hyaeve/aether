package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestQuarkTVQualityAndRouting(t *testing.T) {
	videos := []quarkVideo{
		{URL: "https://cdn.example/high.m3u8", Resolution: "4k", Accessible: 1, Format: "hls"},
		{URL: "https://cdn.example/movie.mp4", Resolution: "super", Accessible: 1, Format: "mp4"},
		{URL: "https://cdn.example/locked.mp4", Resolution: "4k", Accessible: 0},
	}
	b := quarkTVDefaults(QuarkTVBinding{})
	if got := quarkTVChoose(b, videos); got != videos[1].URL {
		t.Fatal(got)
	}
	if got := quarkTVChoose(b, videos[:1]); got != "" {
		t.Fatal("adaptive should fall back on HLS", got)
	}
	b.Mode = "direct"
	if got := quarkTVChoose(b, videos[:1]); got != videos[0].URL {
		t.Fatal(got)
	}
	b.Mode, b.UA, b.UAListMode = "split", "Safari\nKomic", "proxy_list"
	if !quarkTVBypass(b, "komic-iOS") || quarkTVBypass(b, "VLC") {
		t.Fatal("proxy list mismatch")
	}
	b.UAListMode = "direct_list"
	if quarkTVBypass(b, "Komic") || !quarkTVBypass(b, "VLC") {
		t.Fatal("direct list mismatch")
	}
	for _, address := range []string{"http://broker.example", "https://u:p@broker.example", "https://broker.example/?secret=x"} {
		if validateQuarkBroker(address) == nil {
			t.Fatal("unsafe broker", address)
		}
	}
}

func TestQuarkTVSignedPlaybackCacheAndSecretIsolation(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "quark", Name: "Quark", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private-cookie"}}
	b := quarkTVDefaults(QuarkTVBinding{Enabled: true, AccessToken: "private-access", RefreshToken: "private-refresh", Device: "bound-device"})
	b.CookieHash = quarkCookieHash(s)
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.QuarkTV = map[string]QuarkTVBinding{s.ID: b}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	original := apiClient
	defer func() { apiClient = original }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "open-api-drive.quark.cn" || r.URL.Path != "/file" || r.URL.Query().Get("fid") != "file-1" || r.URL.Query().Get("access_token") != "private-access" || r.Header.Get("x-pan-token") == "" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected TV request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"status":200,"data":{"video_info":[{"url":"https://cdn.example/movie.mp4","accessable":1,"resolution":"super","format":"mp4"}]}}`))}, nil
	})}
	h := a.Handler(t.TempDir())
	w := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := w.Result().Cookies()[0]
	for range 2 {
		w = request(t, h, "GET", a.streamURL(s.ID, "file-1", ""), nil, nil)
		if w.Code != 302 || w.Header().Get("Location") != "https://cdn.example/movie.mp4" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatal("TV cache not reused", calls)
	}
	for _, endpoint := range []string{"/api/state", "/api/quark-takeover/quark"} {
		w = request(t, h, "GET", endpoint, nil, cookie)
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-access") || strings.Contains(w.Body.String(), "private-refresh") {
			t.Fatal("secret exposure", endpoint, w.Code)
		}
	}
	w = request(t, h, "GET", "/api/quark-takeover/quark", nil, nil)
	if w.Code != 401 {
		t.Fatal("unprotected plugin", w.Code)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().QuarkTV[s.ID].AccessToken != b.AccessToken {
		t.Fatal("credentials not persisted", err)
	}
	s.Config["cookie"] = "another-cookie"
	if target := a.quarkTVTarget(context.Background(), s, "file-1", ""); target != "" {
		t.Fatal("account change reused TV credentials")
	}
}

func TestQuarkTVManualCredentialAndQRConsent(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "ck"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	b := quarkTVDefaults(QuarkTVBinding{Enabled: true, Device: "user-device", AccessToken: "secret-token"})
	w := request(t, h, "PUT", "/api/quark-takeover/q", b, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	got := a.store.snapshot().QuarkTV["q"]
	if got.Device != "user-device" || got.AccessToken != "secret-token" {
		t.Fatal("manual binding lost")
	}
	b.AccessToken = ""
	w = request(t, h, "PUT", "/api/quark-takeover/q", b, cookie)
	if w.Code != 200 || a.store.snapshot().QuarkTV["q"].AccessToken != "secret-token" {
		t.Fatal("blank did not preserve secret")
	}
	w = request(t, h, "POST", "/api/quark-takeover/q/qr", map[string]any{"broker": "https://broker.example", "consent": false}, cookie)
	if w.Code != 400 {
		t.Fatal("missing consent accepted")
	}
	var response map[string]any
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"] == nil {
		t.Fatal(response)
	}
}

func TestQuarkTVQRBindingAndRefresh(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "q", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "ck"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	original := apiClient
	defer func() { apiClient = original }()
	polls, exchanges, streams := 0, 0, 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Host + r.URL.Path {
		case "open-api-drive.quark.cn/oauth/authorize":
			body = `{"qr_data":"https://quark.example/authorize","query_token":"private-query"}`
		case "open-api-drive.quark.cn/oauth/code":
			if r.URL.Query().Get("query_token") != "private-query" {
				t.Fatal("wrong query token")
			}
			polls++
			if polls == 1 {
				body = `{"errno":11003}`
			} else {
				body = `{"code":"private-code"}`
			}
		case "broker.example/token":
			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			exchanges++
			if exchanges == 1 && payload["code"] != "private-code" {
				t.Fatal("wrong code exchange")
			}
			if exchanges == 2 && payload["refresh_token"] != "refresh" {
				t.Fatal("wrong refresh")
			}
			body = `{"code":200,"data":{"access_token":"access","refresh_token":"refresh","expires_in":3600}}`
		case "open-api-drive.quark.cn/file":
			streams++
			if streams == 1 {
				body = `{"status":-1,"errno":11001}`
			} else {
				body = `{"data":{"video_info":[{"url":"https://cdn.example/test.mp4","resolution":"super","accessable":1,"format":"mp4"}]}}`
			}
		default:
			t.Fatalf("unexpected host/path: %s%s", r.URL.Host, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	w := request(t, h, "POST", "/api/quark-takeover/q/qr", map[string]any{"broker": "https://broker.example", "consent": true}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var qr map[string]string
	json.Unmarshal(w.Body.Bytes(), &qr)
	if !strings.HasPrefix(qr["image"], "data:image/png;base64,") || strings.Contains(qr["session"], "private-query") {
		t.Fatal("invalid QR envelope")
	}
	for i := 0; i < 2; i++ {
		w = request(t, h, "POST", "/api/quark-takeover/q/poll", map[string]string{"session": qr["session"]}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		want := "pending"
		if i == 1 {
			want = "authorized"
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal(w.Body.String())
		}
	}
	b := a.store.snapshot().QuarkTV["q"]
	b.Enabled = true
	a.store.update(func(st *State) error { st.QuarkTV["q"] = b; return nil })
	if target := a.quarkTVTarget(context.Background(), s, "file", "VLC"); target != "https://cdn.example/test.mp4" {
		t.Fatal("refresh failed", target)
	}
	if exchanges != 2 || streams != 2 {
		t.Fatal(exchanges, streams)
	}
	// An invalid or cross-storage authorization token cannot be replayed.
	w = request(t, h, "POST", "/api/quark-takeover/q/poll", map[string]string{"session": "invalid"}, cookie)
	if w.Code != 400 {
		t.Fatal("invalid authorization accepted")
	}
}
