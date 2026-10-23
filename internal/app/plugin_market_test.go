package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const marketFixture = `{"Demo":{"name":"演示插件","subtitle":"文件服务","description":"目录整理","version":"1.0","author":"author","icon":"demo.png","labels":["目录"],"aether":{"api":1}}}`

func marketTransport(t *testing.T, handler func(*http.Request) (int, string)) {
	t.Helper()
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" || r.URL.Scheme != "https" || r.URL.Host != "raw.githubusercontent.com" || r.Header.Get("Authorization") != "" || !strings.HasSuffix(r.URL.Path, ".json") {
			t.Errorf("unexpected repository access: %s %s", r.Method, r.URL.Redacted())
		}
		code, body := handler(r)
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
}

func TestPluginMarketRepositoryAndCatalog(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	var calls atomic.Int32
	marketTransport(t, func(r *http.Request) (int, string) { calls.Add(1); return 200, marketFixture })
	for _, route := range []string{"/api/plugin-market", "/api/plugin-market/repositories"} {
		if w := request(t, h, "GET", route, nil, nil); w.Code != 401 {
			t.Fatal("unauthorized", w.Code)
		}
	}
	w := request(t, h, "GET", "/api/plugin-market/repositories", nil, cookie)
	var settings struct {
		Repositories []pluginRepository `json:"repositories"`
		Revision     string             `json:"revision"`
	}
	if json.Unmarshal(w.Body.Bytes(), &settings) != nil || w.Code != 200 || settings.Revision == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	repo := pluginRepository{Name: "测试仓库", URL: "https://github.com/example/plugins.git", Enabled: true}
	input := map[string]any{"repositories": []pluginRepository{repo}, "revision": settings.Revision}
	w = request(t, h, "PUT", "/api/plugin-market/repositories", input, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if json.Unmarshal(w.Body.Bytes(), &settings) != nil || len(settings.Repositories) != 1 || settings.Repositories[0].ID == "" || settings.Repositories[0].Ref != "main" {
		t.Fatal(w.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatal("write must validate only catalog", calls.Load())
	}
	w = request(t, h, "GET", "/api/plugin-market?q=目录&count=1", nil, cookie)
	var catalog struct {
		Plugins          []marketPlugin
		Total            int
		RuntimeAvailable bool
	}
	if json.Unmarshal(w.Body.Bytes(), &catalog) != nil || w.Code != 200 || catalog.Total != 1 || len(catalog.Plugins) != 1 || catalog.RuntimeAvailable || catalog.Plugins[0].Repository != settings.Repositories[0].ID || !catalog.Plugins[0].Aether {
		t.Fatal(w.Code, w.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatal("cache miss", calls.Load())
	}
	for _, query := range []string{"q=不匹配", "page=2&count=1"} {
		w = request(t, h, "GET", "/api/plugin-market?"+query, nil, cookie)
		if !strings.Contains(w.Body.String(), `"plugins":[]`) {
			t.Fatal(w.Body.String())
		}
	}
	if w := request(t, h, "PUT", "/api/plugin-market/repositories", input, cookie); w.Code != 409 {
		t.Fatal("stale revision", w.Code)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || len(reopened.snapshot().PluginRepositories) != 1 || reopened.snapshot().PluginRepositories[0] != settings.Repositories[0] {
		t.Fatal("configuration persistence", err)
	}
	settings.Repositories[0].Enabled = false
	w = request(t, h, "PUT", "/api/plugin-market/repositories", settings, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/plugin-market?refresh=1", nil, cookie)
	if calls.Load() != 1 || !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("disabled source fetched", calls.Load(), w.Body.String())
	}
}

func TestPluginMarketValidation(t *testing.T) {
	if marketRevision(nil) != marketRevision([]pluginRepository{}) {
		t.Fatal("empty source revisions differ after restart")
	}
	for _, address := range []string{"http://github.com/x/y", "https://127.0.0.1/x/y", "https://user:secret@github.com/x/y", "https://github.com/x/y?token=secret", "https://github.com/x/y/blob/main/package.json", "https://github.com/x/..", "https://github.com.evil.test/x/y"} {
		if _, err := normalizePluginRepository(pluginRepository{URL: address}); err == nil {
			t.Error("accepted", address)
		}
	}
	repo, err := normalizePluginRepository(pluginRepository{ID: "one", URL: "https://github.com/x/y/tree/dev", Enabled: true})
	if err != nil || repo.Ref != "dev" {
		t.Fatal(repo, err)
	}
	if err := validatePluginRepositories([]pluginRepository{repo, repo}); err == nil {
		t.Fatal("duplicate repository")
	}
	for _, raw := range []string{`[]`, `{} {}`, `{"A":{}}`, `{"A":{"name":"A","version":"1"},"a":{"name":"B","version":"1"}}`, `{"../x":{"name":"A","version":"1"}}`, strings.Repeat(" ", (1<<20)+1), `{"A":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`} {
		if _, err := decodeMarketIndex([]byte(raw), repo); err == nil {
			t.Error("accepted invalid index", len(raw))
		}
	}
	for _, icon := range []string{"https://evil.test/x.png", "../secret.png", "icons/../secret.png", "//evil.test/x.png", "icons/x.png?token=secret"} {
		if marketIcon(repo, icon) != "" {
			t.Error("accepted unsafe icon", icon)
		}
	}
}

func TestPluginMarketStaleFallbackBoundedAndShared(t *testing.T) {
	a := testApp(t)
	repo, _ := normalizePluginRepository(pluginRepository{ID: "one", URL: "https://github.com/x/y", Enabled: true})
	var calls atomic.Int32
	var failed atomic.Bool
	marketTransport(t, func(r *http.Request) (int, string) {
		calls.Add(1)
		if failed.Load() {
			return 503, "private upstream response"
		}
		time.Sleep(10 * time.Millisecond)
		return 200, marketFixture
	})
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := a.marketIndex(context.Background(), repo, false)
			if len(result.Plugins) != 1 || result.Error != "" {
				t.Error(result.Error)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("duplicate fetch", calls.Load())
	}
	failed.Store(true)
	result := a.marketIndex(context.Background(), repo, true)
	if len(result.Plugins) != 1 || result.Error == "" || strings.Contains(result.Error, "private") {
		t.Fatal("lost last success or leaked response", result.Error)
	}
	a.marketIndex(context.Background(), repo, false)
	if calls.Load() != 2 {
		t.Fatal("failure not backed off", calls.Load())
	}
	for i := range 70 {
		other := repo
		other.ID = fmt.Sprint("source", i)
		a.marketIndex(context.Background(), other, false)
	}
	if len(a.marketCache) > 64 {
		t.Fatal("unbounded cache", len(a.marketCache))
	}
}

func TestPluginMarketIndex404Fallback(t *testing.T) {
	repo, _ := normalizePluginRepository(pluginRepository{URL: "https://github.com/x/y"})
	var paths []string
	marketTransport(t, func(r *http.Request) (int, string) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/package.json") {
			return 404, ""
		}
		return 200, marketFixture
	})
	if plugins, err := fetchMarketIndex(context.Background(), repo); err != nil || len(plugins) != 1 || len(paths) != 2 {
		t.Fatal(err, paths)
	}
}

func TestPluginMarketGlobalConcurrency(t *testing.T) {
	a := testApp(t)
	var active, peak atomic.Int32
	marketTransport(t, func(r *http.Request) (int, string) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old; old = peak.Load() {
			if peak.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		return 200, marketFixture
	})
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			repo, _ := normalizePluginRepository(pluginRepository{ID: fmt.Sprint("pool", i), URL: "https://github.com/x/y"})
			if result := a.marketIndex(context.Background(), repo, false); result.Error != "" {
				t.Error(result.Error)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > 4 || peak.Load() < 2 {
		t.Fatal("global fetch gate", peak.Load())
	}
}

func TestPluginMarketRejectRedirectAndInvalidWrite(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	marketTransport(t, func(r *http.Request) (int, string) { return 302, "private upstream body" })
	w := request(t, h, "PUT", "/api/plugin-market/repositories", map[string]any{"revision": marketRevision(nil), "repositories": []pluginRepository{{URL: "https://github.com/x/y", Enabled: true}}}, cookie)
	if w.Code != 400 || strings.Contains(w.Body.String(), "private") || len(a.store.snapshot().PluginRepositories) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
}
