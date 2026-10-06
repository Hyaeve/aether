package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAudiobookshelfLink302(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := t.TempDir()
	mediaRoot := t.TempDir()
	pointer := filepath.Join(mediaRoot, "chapter.strm")
	if err := os.WriteFile(pointer, []byte("https://cdn.example.test/chapter.m4b"), 0600); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token != "Bearer player" && token != "Bearer service" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "book", "libraryFiles": []any{map[string]any{"ino": "file", "metadata": map[string]any{"path": "/audiobooks/chapter.strm", "filename": "chapter.strm"}}}})
	}))
	defer up.Close()
	a, err := newWithDirectories(ctx, t.TempDir(), data, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.startLinks()
	defer a.closeLinks()
	link := MediaLink{ID: "abs-test", Name: "Audio", Type: "audiobookshelf", Address: up.URL, Port: freeLinkPort(t), APIKey: "service", Mode: "always", Enabled: true, MediaRoot: mediaRoot, UpstreamRoot: "/audiobooks"}
	service, err := a.buildLink(link)
	if err != nil {
		t.Fatal(err)
	}
	a.links.services[link.ID] = service
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for _, token := range []string{"", "player"} {
		req, _ := http.NewRequest("GET", "http://127.0.0.1:"+fmtPort(link.Port)+"/api/items/book/file/file", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if token == "" && res.StatusCode != 401 {
			t.Fatal("unauthorized", res.StatusCode)
		}
		if token != "" && (res.StatusCode != 302 || res.Header.Get("Location") != "https://cdn.example.test/chapter.m4b") {
			t.Fatal("abs redirect", res.StatusCode, res.Header)
		}
	}
}

func TestLink302AndPlayerAuthorization(t *testing.T) {
	for _, kind := range []string{"emby", "fnos"} {
		t.Run(kind, func(t *testing.T) {
			source := map[string]any{"Id": "source", "Path": "https://cdn.example.test/movie.mp4", "Protocol": "Http", "Container": "strm"}
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/PlaybackInfo") {
					if r.Header.Get("X-Emby-Token") != "player-token" {
						w.WriteHeader(401)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"MediaSources": []any{source}})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/Items") {
					json.NewEncoder(w).Encode(map[string]any{"Items": []any{map[string]any{"Id": "movie", "MediaSources": []any{source}}}})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"Id": "movie", "MediaSources": []any{source}})
			}))
			defer up.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a, err := newWithDirectories(ctx, t.TempDir(), t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a.startLinks()
			defer a.closeLinks()
			link := MediaLink{ID: "test", Name: "Test", Type: kind, Address: up.URL, APIKey: "service-key", Port: freeLinkPort(t), Mode: "always", Enabled: true}
			service, err := a.buildLink(link)
			if err != nil {
				t.Fatal(err)
			}
			a.links.services[link.ID] = service
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
			address := "http://127.0.0.1:" + fmtPort(link.Port) + "/Videos/movie/stream"
			for _, token := range []string{"", "player-token", "player-token", "invalid"} {
				req, _ := http.NewRequest("GET", address, nil)
				req.Header.Set("X-Emby-Token", token)
				res, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()
				if token == "player-token" {
					if res.StatusCode != 302 || res.Header.Get("Location") != "https://cdn.example.test/movie.mp4" {
						t.Fatalf("redirect: %d %s", res.StatusCode, res.Header.Get("Location"))
					}
				} else if res.StatusCode != 401 {
					t.Fatal("authorization bypass", res.StatusCode)
				}
			}
			events := a.links.stats.Snapshot(10).RecentEvents
			if len(events) != 2 || !events[0].CacheHit {
				t.Fatalf("cache events: %+v", events)
			}
		})
	}
}
