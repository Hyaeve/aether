package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestTransferRetentionAndClear(t *testing.T) {
	a := testApp(t)
	now := time.Now()
	a.transfers.items = map[string]transferEntry{
		"running": {ID: "running", Status: "running", Updated: now.Add(-100 * time.Hour)},
		"done":    {ID: "done", Status: "completed", Updated: now},
		"failed":  {ID: "failed", Status: "failed", Updated: now},
		"expired": {ID: "expired", Status: "failed", Updated: now.Add(-73 * time.Hour)},
	}
	a.transfers.file = filepath.Join(t.TempDir(), "transfers.json")
	w := httptest.NewRecorder()
	a.transferList(w, httptest.NewRequest("GET", "/", nil))
	var items []transferEntry
	if json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) != 3 {
		t.Fatal(w.Body.String())
	}
	var restored transferLog
	restored.restore(a.transfers.file)
	if len(restored.items) != 1 || restored.items["failed"].Status != "failed" {
		t.Fatal(restored.items)
	}
	w = httptest.NewRecorder()
	a.transferList(w, httptest.NewRequest("DELETE", "/", nil))
	if json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) != 2 {
		t.Fatal(w.Body.String())
	}
	restored.restore(a.transfers.file)
	if len(restored.items) != 0 {
		t.Fatal(restored.items)
	}
	if w := request(t, a.Handler(t.TempDir()), "DELETE", "/api/transfers", nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestTMDBIntervalAndProxyCredentials(t *testing.T) {
	cfg := pluginDefaults("tmdb", PluginConfig{Language: "zh-TW"})
	if cfg.RequestInterval != 250 || validatePlugin("tmdb", cfg) != nil {
		t.Fatal(cfg)
	}
	cfg.RequestInterval = 30
	if err := waitTMDB(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := waitTMDB(context.Background(), cfg); err != nil || time.Since(start) < 25*time.Millisecond {
		t.Fatal("interval not applied", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitTMDB(ctx, PluginConfig{RequestInterval: 60000}); err == nil {
		t.Fatal("ignored cancellation")
	}
	c, closeIdle, err := pluginClient(PluginConfig{Enabled: true, Address: "http://127.0.0.1:7890", Username: "user@name", Password: "p:/word"})
	if err != nil {
		t.Fatal(err)
	}
	defer closeIdle()
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	u, err := c.Transport.(*http.Transport).Proxy(req)
	pass, _ := u.User.Password()
	if err != nil || u.User.Username() != "user@name" || pass != "p:/word" {
		t.Fatal("proxy auth not set")
	}
}

func TestProxyPasswordRedactionAndPreservation(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	p := PluginConfig{Address: "http://localhost:7890", Username: "u", Password: "secret"}
	if w := request(t, h, "PUT", "/api/plugins/proxy", p, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request(t, h, "GET", "/api/plugins/proxy", nil, cookie)
	if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Password != "********" {
		t.Fatal(w.Body.String())
	}
	if w := request(t, h, "PUT", "/api/plugins/proxy", p, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if a.store.plugin("proxy").Password != "secret" {
		t.Fatal("mask saved as password")
	}
}
