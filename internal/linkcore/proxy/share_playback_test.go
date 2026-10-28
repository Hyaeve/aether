package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aether/internal/linkcore/config"
)

func TestDeferredSharePlaybackFollowsGETButHEADDoesNotPrepare(t *testing.T) {
	for _, route := range []string{"/quark-share/sample/Movie.mkv", "/115-share/sample/Movie.mkv"} {
		t.Run(route, func(t *testing.T) {
			preparations := 0
			cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("media"))
			}))
			defer cdn.Close()
			share := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Length", "5")
					return
				}
				if r.Header.Get("Range") != "bytes=0-0" {
					t.Error("preparation must use a bounded GET", r.Header)
				}
				preparations++
				http.Redirect(w, r, cdn.URL+"/Movie.mkv", http.StatusFound)
			}))
			defer share.Close()
			root, pointer, ordinary := writeStrm(t, share.URL+route)
			fake := newFakeABS(t, pointer, ordinary)
			cfg := defaultRedirect()
			cfg.FollowUpstreamRedirects = true
			server, _ := newTestServer(t, fake.server.URL, root, cfg)
			head := httptest.NewRecorder()
			headRequest := httptest.NewRequest(http.MethodHead, "/api/items/book-1/file/ino-strm", nil)
			headRequest.RemoteAddr = "192.168.1.20:9000"
			server.ServeHTTP(head, headRequest)
			if preparations != 0 {
				t.Fatal("HEAD prepared a cloud file")
			}
			for range 2 {
				w := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodGet, "/api/items/book-1/file/ino-strm", nil)
				request.RemoteAddr = "192.168.1.20:9000"
				server.ServeHTTP(w, request)
				if w.Code != http.StatusFound || w.Header().Get("Location") != cdn.URL+"/Movie.mkv" {
					t.Fatal("service placeholder cached instead of CDN", w.Code, w.Header())
				}
			}
			if preparations != 1 {
				t.Fatal("share preparation not reused", preparations)
			}
		})
	}
}

func TestRejectedRelayInvalidatesTicketAndReportsActualHost(t *testing.T) {
	rejected := true
	reads, probes := 0, 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			probes++
			return
		}
		reads++
		if rejected {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("private-error-body"))
			return
		}
		w.Write([]byte("media"))
	}))
	defer origin.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			probes++
			return
		}
		http.Redirect(w, r, origin.URL+"/Movie.mkv?private-token=secret", http.StatusFound)
	}))
	defer gateway.Close()
	root, pointer, ordinary := writeStrm(t, gateway.URL+"/Movie.mkv?private-token=secret")
	fake := newFakeABS(t, pointer, ordinary)
	cfg := defaultRedirect()
	cfg.Mode, cfg.FollowUpstreamRedirects = config.RedirectNever, true
	server, collector := newTestServer(t, fake.server.URL, root, cfg)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/items/book-1/file/ino-strm", nil))
	if w.Code != http.StatusForbidden || reads != 1 || server.resolver.CacheSize() != 0 {
		t.Fatal("rejected relay was cached or automatically replayed", w.Code, reads, server.resolver.CacheSize())
	}
	events := collector.Snapshot(1).RecentEvents
	if len(events) != 1 || !strings.Contains(events[0].Error, strings.TrimPrefix(origin.URL, "http://")) ||
		strings.Contains(events[0].Error, "secret") || strings.Contains(events[0].Error, "private-error-body") {
		t.Fatal("failure did not report a safe actual responding host", events)
	}
	rejected = false
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/items/book-1/file/ino-strm", nil))
	if w.Code != http.StatusOK || w.Body.String() != "media" || reads != 2 || probes != 2 {
		t.Fatal("next playback did not refresh rejected resolution", w.Code, reads, probes)
	}
}
