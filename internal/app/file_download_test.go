package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowser115DownloadUsesClientUAAndSignature(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "browser115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	old := apiClient
	defer func() { apiClient = old }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.UserAgent() != "Browser-Test/1" {
			t.Fatal("wrong UA", r.UserAgent())
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":10008,"error":"rejected"}`)), Request: r}, nil
	})}
	h := a.Handler(t.TempDir())
	link := a.streamURL(s.ID, "file", "pick")
	r := httptest.NewRequest("GET", link+"&download=1", nil)
	r.Header.Set("User-Agent", "Browser-Test/1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if calls != 1 || w.Code != 502 || w.Header().Get("Location") != "" {
		t.Fatal(calls, w.Code, w.Body.String())
	}
	r.URL.RawQuery = "download=1"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 || calls != 1 {
		t.Fatal("signature bypass")
	}
}

func TestDownloadRedirectResponse(t *testing.T) {
	for _, address := range []string{"https://cdn.example/file?token=abc", "javascript:alert(1)", "https://user:secret@cdn.example/file", "/relative"} {
		w := httptest.NewRecorder()
		redirectDownload(w, httptest.NewRequest("GET", "/", nil), address)
		if strings.HasPrefix(address, "https://cdn.") {
			if w.Code != 302 || w.Header().Get("Location") != address || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Header())
			}
		} else if w.Code != 502 {
			t.Fatal("unsafe redirect", address)
		}
		if w.Header().Get("Set-Cookie") != "" {
			t.Fatal("credential leaked")
		}
	}
}
