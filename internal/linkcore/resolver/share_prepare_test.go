package resolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"aether/internal/linkcore/config"
)

func Test115SharePreparationHasSeparateCDNProbeBudget(t *testing.T) {
	var preparations atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow.mkv" {
			select {
			case <-time.After(600 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(200)
	}))
	defer cdn.Close()
	entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(200)
			return
		}
		preparations.Add(1)
		if r.UserAgent() != "Hills/1.11.0" || r.Header.Get("Range") != "bytes=0-0" {
			t.Error("preparation lost the player UA or bounded request")
		}
		select {
		case <-time.After(400 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		filename := "movie.mkv"
		if r.URL.Query().Get("slow") == "1" {
			filename = "slow.mkv"
		}
		http.Redirect(w, r, cdn.URL+"/"+filename, http.StatusFound)
	}))
	defer entry.Close()
	cfg := config.Default()
	cfg.Redirect.ProbeTimeout = 250 * time.Millisecond
	r := New(cfg.Cache, cfg.Redirect)
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	address := entry.URL + "/115-share/code/movie.mkv"
	final, hops, err := r.followRedirects(ctx, address, "Hills/1.11.0")
	if err != nil || final != cdn.URL+"/movie.mkv" || len(hops) != 1 || preparations.Load() != 1 {
		t.Fatal("first preparation inherited the short CDN timeout", final, hops, err, preparations.Load())
	}
	_, _, err = r.followRedirects(ctx, address+"?slow=1", "Hills/1.11.0")
	if err == nil || preparations.Load() != 2 {
		t.Fatal("CDN probe inherited the long preparation timeout", err, preparations.Load())
	}
	final, hops, err = r.followRedirects(WithRequestMethod(ctx, http.MethodHead), address, "Hills/1.11.0")
	if err != nil || final != address || len(hops) != 0 || preparations.Load() != 2 {
		t.Fatal("HEAD created a cloud copy", final, hops, err, preparations.Load())
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stop()
	if _, _, err = r.followRedirects(short, address, "Hills/1.11.0"); err == nil || short.Err() == nil {
		t.Fatal("preparation ignored the caller's cancellation", err)
	}
}
