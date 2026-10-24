package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"aether/internal/linkcore/config"
)

func TestRelayGuardStrictRedirectAndNetworkScope(t *testing.T) {
	for _, tc := range []struct {
		name, mode, client string
		guard              *bool
		want               int
	}{
		{"legacy fallback", "always", "8.8.8.8:1000", nil, 200},
		{"enabled fallback", "always", "8.8.8.8:1000", config.Bool(true), 200},
		{"strict public", "always", "8.8.8.8:1000", config.Bool(false), 502},
		{"strict private can redirect", "always", "192.168.1.20:1000", config.Bool(false), 302},
		{"public network strict", "public", "8.8.8.8:1000", config.Bool(false), 502},
		{"public mode private relays", "public", "192.168.1.20:1000", config.Bool(false), 200},
		{"private mode private redirects", "private", "192.168.1.20:1000", config.Bool(false), 302},
		{"private mode public relays", "private", "8.8.8.8:1000", config.Bool(false), 200},
		{"never always relays", "never", "8.8.8.8:1000", config.Bool(false), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gets := 0
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					gets++
				}
				w.Write([]byte("media"))
			}))
			defer origin.Close()
			root, pointer, ordinary := writeStrm(t, origin.URL+"/Movie.mkv")
			fake := newFakeABS(t, pointer, ordinary)
			cfg := defaultRedirect()
			cfg.Mode, cfg.RelayGuard, cfg.FollowUpstreamRedirects = config.RedirectMode(tc.mode), tc.guard, true
			server, _ := newTestServer(t, fake.server.URL, root, cfg)
			r := httptest.NewRequest("GET", "/api/items/book-1/file/ino-strm", nil)
			r.RemoteAddr = tc.client
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != tc.want || tc.want != 200 && gets != 0 {
				t.Fatal(w.Code, gets, w.Body.String())
			}
		})
	}
}

func TestRelayGuardStrictResolutionFailureDoesNotPassthrough(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		root, _, ordinary := writeStrm(t, "http://example.test/media.mkv")
		fake := newFakeABS(t, root+"/missing.strm", ordinary)
		cfg := defaultRedirect()
		cfg.RelayGuard = config.Bool(enabled)
		server, _ := newTestServer(t, fake.server.URL, root, cfg)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", "/api/items/book-1/file/ino-strm", nil))
		if !enabled && (w.Code != 502 || w.Header().Get("X-Fake-Upstream") != "") {
			t.Fatal("strict request fell back", w.Code, w.Body.String())
		}
		if enabled && w.Header().Get("X-Fake-Upstream") == "" {
			t.Fatal("legacy fallback lost", w.Code, w.Body.String())
		}
	}
}

func TestRelayGuardProbeFailureFallsBackOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "strict"}[enabled], func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			}))
			defer origin.Close()
			root, pointer, ordinary := writeStrm(t, origin.URL+"/blocked.mkv")
			fake := newFakeABS(t, pointer, ordinary)
			cfg := defaultRedirect()
			cfg.FollowUpstreamRedirects, cfg.RelayGuard = true, config.Bool(enabled)
			server, _ := newTestServer(t, fake.server.URL, root, cfg)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, httptest.NewRequest("GET", "/api/items/book-1/file/ino-strm", nil))
			if enabled && w.Header().Get("X-Fake-Upstream") == "" || !enabled && (w.Code != 502 || w.Header().Get("X-Fake-Upstream") != "") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
