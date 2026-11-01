package upstream

import (
	"aether/internal/linkcore/config"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestDeletionPathsCompletePagesAndRejectsUncertainScope(t *testing.T) {
	for _, mode := range []string{"complete", "repeat", "empty", "changed-total"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Query().Get("ParentId") == "" {
					_ = json.NewEncoder(w).Encode(map[string]any{"Id": "series", "Type": "Series", "Path": "/media/series"})
					return
				}
				calls++
				start, _ := strconv.Atoi(r.URL.Query().Get("StartIndex"))
				total := 501
				items := []any{}
				if mode == "changed-total" && start > 0 {
					total++
				}
				for i := start; i < min(501, start+500); i++ {
					if mode == "empty" && start > 0 {
						break
					}
					id := fmt.Sprint(i)
					if mode == "repeat" && start > 0 {
						id = "0"
					}
					items = append(items, map[string]any{"Id": id, "Type": "Episode", "Path": fmt.Sprintf("/media/series/%s.strm", id)})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": total})
			}))
			defer server.Close()
			p, err := New(config.Upstream{Name: "test", Type: config.UpstreamType("emby"), BaseURL: server.URL, APIKey: "key"})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("DELETE", "/Items/series?UserId=u", nil)
			r.Header.Set("X-Emby-Token", "token")
			paths, err := DeletionPaths(WithClientCredentials(context.Background(), r), p, "series")
			if mode == "complete" {
				if err != nil || len(paths) != 501 || calls != 2 {
					t.Fatalf("%d %d %v", len(paths), calls, err)
				}
			} else if err == nil {
				t.Fatal("uncertain scope accepted", mode)
			}
		})
	}
}

func TestDeletionPathsNeverEscalatesToServiceAccount(t *testing.T) {
	serviceCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") == "service-secret" || r.Header.Get("X-Emby-Token") == "service-secret" {
			serviceCalls++
		}
		w.WriteHeader(403)
	}))
	defer server.Close()
	p, err := New(config.Upstream{Name: "test", Type: config.UpstreamType("emby"), BaseURL: server.URL, APIKey: "service-secret"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("DELETE", "/Items/item?UserId=u", nil)
	r.Header.Set("X-Emby-Token", "denied-user")
	if _, err := DeletionPaths(WithDeletionCredentials(context.Background(), r), p, "item"); err == nil {
		t.Fatal("denied identity accepted")
	}
	if serviceCalls != 0 {
		t.Fatal("escalated to configured service account")
	}
	if _, err := DeletionPaths(context.Background(), p, "item"); err == nil {
		t.Fatal("anonymous deletion planning allowed")
	}
}
