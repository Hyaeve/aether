package proxy

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aether/internal/linkcore/config"
	"aether/internal/linkcore/resolver"
	"aether/internal/linkcore/stats"
	"aether/internal/linkcore/strm"
	"aether/internal/linkcore/upstream"
)

func TestAetherM4BConversion(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "sample.m4b")
	if out, err := exec.Command(ffmpeg, "-nostdin", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.2", "-c:a", "aac", "-y", source).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := config.Default()
	provider, err := upstream.New(config.Upstream{Name: "test", Type: config.UpstreamAudiobookshelf, BaseURL: "http://127.0.0.1:12345", APIKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	r := resolver.New(cfg.Cache, cfg.Redirect)
	defer r.Close()
	cache := NewAudioCache(ctx, filepath.Join(dir, "cache"))
	server := NewWithAudioCache(provider, r, stats.New(20), cfg.Redirect, cache)
	resolution := &resolver.Resolution{Target: &strm.Target{Type: strm.TargetLocal, Path: source, Filename: "sample.m4b"}}
	req := httptest.NewRequest("GET", "http://proxy.test/audio", nil)
	req.Header.Set("User-Agent", "iPhone")
	if !server.shouldTranscode(req, resolution) {
		t.Fatal("M4B not adapted")
	}
	for _, hit := range []bool{false, true} {
		w := httptest.NewRecorder()
		_, cached, err := server.relayTranscoded(w, req, source, resolution)
		if err != nil || cached != hit || w.Body.Len() == 0 || w.Header().Get("Content-Type") != "audio/mp4" {
			t.Fatal("conversion/cache", cached, err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(dir, "cache", "*", "*", "*.m4a"))
	if len(files) != 1 {
		t.Fatal("cache layout", files)
	}
	if info, err := os.Stat(files[0]); err != nil || info.Size() == 0 {
		t.Fatal("missing m4a", err)
	}
}
