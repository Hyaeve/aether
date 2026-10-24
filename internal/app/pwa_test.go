package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPWAStaticTypesAndMissingAssets(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "<html>app</html>", "sw.js": "self.addEventListener('fetch',()=>{})", "manifest.webmanifest": `{"name":"Aether"}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h := a.Handler(dir)
	for path, contentType := range map[string]string{"/sw.js": "application/javascript", "/manifest.webmanifest": "application/manifest+json"} {
		w := request(t, h, "GET", path, nil, nil)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), contentType) || w.Header().Get("Cache-Control") != "no-cache" {
			t.Fatal("PWA content type or stale worker", path, w.Code, w.Header())
		}
	}
	if w := request(t, h, "GET", "/pwa/missing.png", nil, nil); w.Code != 404 || strings.Contains(w.Body.String(), "<html>app") {
		t.Fatal("missing PWA asset served app document", w.Code)
	}
}
