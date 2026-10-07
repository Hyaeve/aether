package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aether/internal/linkcore/proxy"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

const test115Cookie = "UID=123_A1; CID=cid; SEID=secret; KID=kid"

func TestOffline115CookieProtocol(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "offline115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie, "device": "web"}}
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	old := apiClient
	defer func() { apiClient = old }()
	submitted := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" || !strings.Contains(r.Header.Get("Cookie"), "SEID=secret") {
			t.Fatal("115 must use cookie, not OAuth")
		}
		raw := `{"state":true,"cid":"12","count":0,"data":[]}`
		if r.URL.Path != "/files" {
			if r.URL.Host != "lixian.115.com" || r.URL.Path != "/lixianssp/" {
				t.Fatal("unexpected native offline endpoint", r.URL)
			}
			r.ParseForm()
			if r.Form.Get("data") == "" {
				t.Fatal("missing encrypted native payload")
			}
			submitted++
			// Successful encrypted responses require 115's private RSA key.
			raw = `{"state":false,"errno":10008,"error":"quota"}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	if request(t, h, "POST", "/api/files/offline", nil, nil).Code != 401 {
		t.Fatal("offline endpoint unprotected")
	}
	input := map[string]any{"storageId": s.ID, "parent": "12", "urls": []string{"https://example.com/a", "https://example.com/b"}}
	response := request(t, h, "POST", "/api/files/offline", input, cookie)
	var results []offlineResult
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &results) != nil || len(results) != 2 || results[0].Success || results[1].Success || submitted != 2 {
		t.Fatal(response.Body.String(), submitted)
	}
	input["urls"] = []string{"file:///etc/passwd"}
	if request(t, h, "POST", "/api/files/offline", input, cookie).Code != 400 || submitted != 2 {
		t.Fatal("invalid URL submitted")
	}
	info := metainfo.Info{Name: "media.mp4", Length: 4, PieceLength: 4, Pieces: make([]byte, 20)}
	rawInfo, _ := bencode.Marshal(info)
	meta := metainfo.MetaInfo{InfoBytes: rawInfo}
	var torrent bytes.Buffer
	meta.Write(&torrent)
	if err := a.submit115Torrent(context.Background(), s, "12", bytes.NewReader(torrent.Bytes())); err == nil || submitted != 3 {
		t.Fatal("BT did not use native submission/rejection")
	}
	if err := a.submit115Torrent(context.Background(), s, "12", strings.NewReader("bad torrent")); err == nil || submitted != 3 {
		t.Fatal("invalid torrent submitted")
	}
	private := true
	info.Private = &private
	meta.InfoBytes, _ = bencode.Marshal(info)
	torrent.Reset()
	meta.Write(&torrent)
	if err := a.submit115Torrent(context.Background(), s, "12", &torrent); err == nil || submitted != 3 {
		t.Fatal("private torrent must not be silently converted")
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
