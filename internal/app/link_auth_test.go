package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	lc "aether/internal/linkcore/config"
	"aether/internal/linkcore/upstream"
)

func TestABSActiveSessionAuthorization(t *testing.T) {
	for _, prefix := range []string{"", "/audiobookshelf"} {
		t.Run(prefix, func(t *testing.T) {
			status := http.StatusPartialContent
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != prefix+"/public/session/new-session/track/1" {
					t.Errorf("wrong authorization endpoint: %s", r.URL.Path)
				}
				if r.URL.Query().Get("token") != "player" || r.Header.Get("Range") != "bytes=0-0" {
					t.Error("missing player query or bounded probe")
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("service credential leaked")
				}
				if status == 302 {
					w.Header().Set("Location", "/login")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			provider, err := upstream.New(lc.Upstream{Name: "abs", Type: lc.UpstreamAudiobookshelf, BaseURL: server.URL + prefix, APIKey: "service-secret"})
			if err != nil {
				t.Fatal(err)
			}
			accepted := 0
			handler := authorizeLinkPlayer(provider, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				accepted++
				w.WriteHeader(204)
			}))
			for _, value := range []int{206, 200, 401, 403, 404, 302} {
				status = value
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest("GET", prefix+"/public/session/new-session/track/1?token=player", nil))
				want := value
				if value == 200 || value == 206 {
					want = 204
				}
				if w.Code != want {
					t.Fatalf("status %d: got %d", value, w.Code)
				}
			}
			if accepted != 2 || calls != 6 {
				t.Fatal("authorization bypass or duplicate requests", accepted, calls)
			}
		})
	}
}
