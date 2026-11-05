package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func discoveryFixture(t *testing.T) (*App, http.Handler, *http.Cookie, *atomic.Int32) {
	t.Helper()
	a := testApp(t)
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer discovery-test-key" || r.URL.Query().Get("language") != "zh-CN" {
			t.Error("discovery must reuse TMDB credentials and language")
		}
		if strings.HasPrefix(r.URL.Path, "/3/movie/") && r.URL.Path == "/3/movie/42" || r.URL.Path == "/3/tv/42" {
			if !strings.Contains(","+r.URL.Query().Get("append_to_response")+",", ",credits,") {
				t.Error("missing credits")
			}
			jsonResponse(w, 200, map[string]any{"id": 42, "title": "电影示例", "name": "剧集示例", "original_title": "Original", "original_name": "Series", "release_date": "2026-01-01", "first_air_date": "2025-01-01", "overview": "真实媒体详情", "poster_path": "/poster.jpg", "backdrop_path": "/backdrop.jpg", "vote_average": 8.5, "runtime": 120, "seasons": []any{map[string]any{"season_number": 1, "episode_count": 12, "name": "第一季"}}, "credits": map[string]any{"cast": []any{map[string]any{"name": "演员"}}}})
			return
		}
		kind := "movie"
		if strings.Contains(r.URL.Path, "/tv") {
			kind = "tv"
		}
		if strings.HasPrefix(r.URL.Path, "/3/discover/") && r.URL.Query().Get("with_genres") != "16" {
			t.Error("animation filter missing")
		}
		jsonResponse(w, 200, map[string]any{"total_pages": 999, "results": []any{
			map[string]any{"id": 42, "media_type": kind, "title": "电影示例", "name": "剧集示例", "release_date": "2026-01-01", "poster_path": "/poster.jpg", "vote_average": 8.5},
			map[string]any{"id": 99, "media_type": "person", "name": "不作为影视推荐"},
		}})
	}))
	t.Cleanup(up.Close)
	if err := a.store.update(func(st *State) error {
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["tmdb"] = PluginConfig{Enabled: true, APIURL: up.URL, APIKey: "discovery-test-key", Language: "zh-CN", RequestInterval: 1}
		st.Storages = []Storage{{ID: "mobile", Type: "mobile", Name: "移动影视", Enabled: true, Config: map[string]string{"mode": "native"}}, {ID: "115", Type: "115", Name: "115影视", Enabled: true, Config: map[string]string{}}}
		st.Notifications.Targets = []telegramTarget{{ID: "target", Provider: "mobile", StorageID: "mobile", Parent: "folder", Path: "/影视"}, {ID: "offline", Provider: "115", StorageID: "115", Parent: "0", Path: "/离线", ED2KOffline: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	return a, h, setupTest(t, a, h), &calls
}

func TestDiscoveryFeedsDetailsCacheAndValidation(t *testing.T) {
	a, h, cookie, calls := discoveryFixture(t)
	for _, category := range []string{"all", "tv", "movie", "anime", "rankings"} {
		w := request(t, h, "GET", "/api/discovery?category="+category, nil, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var got struct {
			Sections []struct {
				Items []discoveryMedia
				Pages int
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Sections) != len(discoveryFeeds[category]) {
			t.Fatal("missing shelves", category)
		}
		for _, s := range got.Sections {
			if len(s.Items) != 1 || s.Items[0].ID != 42 || s.Pages != 500 || !strings.HasPrefix(s.Items[0].Poster, "https://image.tmdb.org/t/p/") {
				t.Fatal("invalid feed normalization", s)
			}
		}
	}
	for _, path := range []string{"/api/discovery?q=Arrival&page=2", "/api/discovery?category=tv&group=airing&page=2", "/api/discovery/tv/42", "/api/discovery/movie/42"} {
		w := request(t, h, "GET", path, nil, cookie)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		before := calls.Load()
		if request(t, h, "GET", path, nil, cookie).Code != 200 || calls.Load() != before {
			t.Fatal("fresh cache not reused", path)
		}
	}
	for _, path := range []string{"/api/discovery?page=0", "/api/discovery?page=501", "/api/discovery?page=1.5", "/api/discovery?category=music", "/api/discovery?group=arbitrary", "/api/discovery/movie/no-id"} {
		if w := request(t, h, "GET", path, nil, cookie); w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
	if err := a.store.update(func(st *State) error { p := st.Plugins["tmdb"]; p.Enabled = false; st.Plugins["tmdb"] = p; return nil }); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "GET", "/api/discovery", nil, cookie)
	if w.Code != 502 || !strings.Contains(w.Body.String(), "配置并启用TMDB") {
		t.Fatal("disabled config must not use cached data", w.Body.String())
	}
}

func TestDiscoveryCacheBoundedAndContext(t *testing.T) {
	a, _, _, _ := discoveryFixture(t)
	c := a.tmdbContext(a.ctx).Value(tmdbCacheKey{}).(*tmdbDiskCache)
	c.maxEntries = 32
	for i := 0; i < 35; i++ {
		var out any
		if err := a.discoveryJSON(context.Background(), "search/multi", url.Values{"query": {fmt.Sprint(i)}}, &out); err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(c.dir())
	if err != nil || len(files) != 32 || c.count != 32 {
		t.Fatal("unbounded persistent metadata cache", len(files), c.count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out any
	if err := a.discoveryJSON(ctx, "search/multi", url.Values{"query": {"not-cached"}}, &out); err == nil {
		t.Fatal("cancelled lookup ran")
	}
	for _, value := range []string{"/../private", "/bad?token=secret", "https://other/poster", "/bad\npath"} {
		if discoveryImage(value, "w500") != "" {
			t.Fatal("invalid image accepted")
		}
	}
}

func TestMediaSubscriptionsPersistEditAndDuplicate(t *testing.T) {
	a, h, cookie, _ := discoveryFixture(t)
	body := map[string]any{"id": 42, "kind": "tv", "season": 1, "startEpisode": 2, "totalEpisodes": 12, "targetId": "target", "quality": "WEB-DL", "resolution": "4K", "effect": "HDR", "keyword": "关键词", "searchHours": 12, "include": "2160p", "exclude": "预告", "mediaCategory": "剧集", "customWords": "原名 => 识别名"}
	w := request(t, h, "POST", "/api/media-subscriptions", body, cookie)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var card mediaSubscription
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card.Title != "剧集示例" || card.Key == "" || card.Poster != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Fatal("not canonical metadata", card)
	}
	if w := request(t, h, "POST", "/api/media-subscriptions", body, cookie); w.Code != 409 {
		t.Fatal("duplicate accepted", w.Code)
	}
	body["season"], body["searchHours"], body["targetId"] = 2, nil, "offline"
	body["id"], body["kind"] = 999, "movie" // PUT cannot overwrite media identity.
	w = request(t, h, "PUT", "/api/media-subscriptions/"+card.Key, body, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var edited mediaSubscription
	json.Unmarshal(w.Body.Bytes(), &edited)
	if edited.Kind != "tv" || edited.ID != 42 || edited.Title != card.Title || edited.SearchHours != nil || edited.Season != 2 {
		t.Fatal("edit corrupted identity or options", edited)
	}
	// Snapshot is detached from the persisted subscription options.
	snapshot := a.store.snapshot()
	snapshot.MediaSubscriptions[0].Keyword = "discard"
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.snapshot().MediaSubscriptions; len(got) != 1 || !reflect.DeepEqual(got[0], edited) {
		t.Fatal("subscription did not survive modular store reopen", got)
	}
	w = request(t, h, "GET", "/api/media-subscriptions", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"sourceReady":false`) || strings.Contains(w.Body.String(), "discovery-test-key") {
		t.Fatal("invalid public subscription response", w.Body.String())
	}
	if w := request(t, h, "DELETE", "/api/media-subscriptions/"+card.Key, nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if len(a.store.snapshot().MediaSubscriptions) != 0 || len(a.store.snapshot().Notifications.Targets) != 2 {
		t.Fatal("unsubscribe changed unrelated configuration")
	}
	if w := request(t, h, "DELETE", "/api/media-subscriptions/"+card.Key, nil, cookie); w.Code != 404 {
		t.Fatal("missing subscription accepted")
	}
}

func TestMediaSubscriptionsValidationAndTargets(t *testing.T) {
	a, h, cookie, _ := discoveryFixture(t)
	for _, input := range []map[string]any{
		{"startEpisode": 0}, {"season": -1}, {"searchHours": 0}, {"searchHours": 1.5}, {"totalEpisodes": 2, "startEpisode": 3}, {"sourceIds": []string{"pt-not-supported"}}, {"targetId": "missing"}, {"id": -1},
	} {
		body := map[string]any{"id": 42, "kind": "tv", "season": 1, "startEpisode": 1}
		for k, v := range input {
			body[k] = v
		}
		if w := request(t, h, "POST", "/api/media-subscriptions", body, cookie); w.Code < 400 {
			t.Fatal("invalid option accepted", input)
		}
	}
	if len(a.store.snapshot().MediaSubscriptions) != 0 {
		t.Fatal("validation saved a partial card")
	}
	if err := a.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if subscriptionTargetValid(a.store.snapshot(), "target") || !subscriptionTargetValid(a.store.snapshot(), "offline") {
		t.Fatal("wrong destination eligibility")
	}
	card := mediaSubscription{discoveryMedia: discoveryMedia{ID: 42, Kind: "movie", Title: "电影"}, Key: "valid-key", mediaSubscriptionOptions: mediaSubscriptionOptions{StartEpisode: 1}}
	if err := validateMediaSubscriptions([]mediaSubscription{card}); err != nil {
		t.Fatal(err)
	}
	card.Poster = "https://untrusted.test/image.jpg"
	if err := validateResourceState(&State{MediaSubscriptions: []mediaSubscription{card}}, true); err == nil {
		t.Fatal("import bypasses subscription validation")
	}
}

func TestMediaSubscriptionConcurrentDuplicateAndAuth(t *testing.T) {
	a, h, cookie, _ := discoveryFixture(t)
	for _, endpoint := range []string{"/api/discovery", "/api/discovery/movie/42", "/api/media-subscriptions"} {
		if w := request(t, h, "GET", endpoint, nil, nil); w.Code != 401 {
			t.Fatal("read lacks authentication", endpoint, w.Code)
		}
	}
	if w := request(t, h, "POST", "/api/media-subscriptions", map[string]any{}, nil); w.Code != 401 {
		t.Fatal("write lacks authentication")
	}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- request(t, h, "POST", "/api/media-subscriptions", map[string]any{"id": 42, "kind": "movie"}, cookie).Code
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for code := range results {
		counts[code]++
	}
	if counts[201] != 1 || counts[409] != 1 || len(a.store.snapshot().MediaSubscriptions) != 1 {
		t.Fatal("concurrent subscription duplicated", counts)
	}
}
