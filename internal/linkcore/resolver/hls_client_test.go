package resolver

import (
	"aether/internal/linkcore/config"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHLSPlaylistNegotiationStaysWithClient(t *testing.T) {
	probes := 0
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes++
		t.Error("playlist was pre-fetched by server", r.Method)
		http.Redirect(w, r, "/signed?ticket=server-ip", 302)
	}))
	defer cdn.Close()
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("share preparation used HEAD")
		}
		http.Redirect(w, r, cdn.URL+"/playlist.m3u8?source=share", 302)
	}))
	defer entry.Close()
	cfg := config.Default()
	r := New(cfg.Cache, cfg.Redirect)
	defer r.Close()
	final, hops, err := r.followRedirects(context.Background(), entry.URL+"/139-share/code/Movie.mkv", "Hills/1.11.0")
	if err != nil || final != cdn.URL+"/playlist.m3u8?source=share" || len(hops) != 1 || probes != 0 {
		t.Fatal(final, hops, probes, err)
	}
	final, hops, err = r.followRedirects(context.Background(), cdn.URL+"/playlist.m3u8", "Hills/1.11.0")
	if err != nil || final != cdn.URL+"/playlist.m3u8" || len(hops) != 0 || probes != 0 {
		t.Fatal(final, hops, probes, err)
	}
}
