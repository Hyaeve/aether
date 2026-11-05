package app

import (
	"aether/internal/linkcore/upstream"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTMDBCacheSlidingRetentionSharedByDiscoveryAndScrape(t *testing.T) {
	a, h, cookie, calls := discoveryFixture(t)
	ctx := a.tmdbContext(a.ctx)
	c := ctx.Value(tmdbCacheKey{}).(*tmdbDiskCache)
	base := time.Now().UTC()
	var clock atomic.Int64
	clock.Store(base.Unix())
	c.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	if _, err := a.discoveryMediaDetail(ctx, "movie", 42); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	cfg := pluginDefaults("tmdb", a.store.snapshot().Plugins["tmdb"])
	item := scrapeItem{Kind: "movie", TMDB: 42}
	if _, err := resolveScrapeMedia(ctx, apiClient, cfg, scrapeSettings{}, &item); err != nil || calls.Load() != before {
		t.Fatal("scraper did not reuse canonical media details", err, calls.Load(), before)
	}
	clock.Store(base.Add(12 * time.Hour).Unix())
	if _, err := a.discoveryMediaDetail(ctx, "movie", 42); err != nil || calls.Load() != before {
		t.Fatal("fresh details were refetched", err)
	}
	disk, _ := os.ReadDir(c.dir())
	if len(disk) != 1 {
		t.Fatal(len(disk))
	}
	saved, err := c.read(disk[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "GET", "/api/archives/cache", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "电影示例") {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ := c.read(disk[0].Name())
	if !after.Accessed.Equal(saved.Accessed) {
		t.Fatal("viewing archive extended retention")
	}
	clock.Store(base.Add(36 * time.Hour).Unix())
	if _, err := a.discoveryMediaDetail(ctx, "movie", 42); err != nil || calls.Load() != before+1 {
		t.Fatal("old active details were not refreshed", err)
	}
	clock.Store(base.Add(10 * 24 * time.Hour).Unix())
	if _, err := a.discoveryMediaDetail(ctx, "movie", 42); err != nil || calls.Load() != before+2 {
		t.Fatal("idle cache was not refetched", err, calls.Load())
	}
	raw, _ := os.ReadFile(filepath.Join(c.dir(), disk[0].Name()))
	if strings.Contains(string(raw), cfg.APIKey) {
		t.Fatal("credential persisted in cache")
	}
	bctx, cancel := context.WithCancel(context.Background())
	b, err := New(bctx, a.dataDir, a.outputDir)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() { cancel(); b.wg.Wait() }()
	b.tmdbContext(bctx).Value(tmdbCacheKey{}).(*tmdbDiskCache).now = c.now
	if _, err := b.discoveryMediaDetail(bctx, "movie", 42); err != nil || calls.Load() != before+2 {
		t.Fatal("cache did not survive reopening", err)
	}
}

func TestTMDBCacheFeedRefreshDoesNotFreezeRankings(t *testing.T) {
	a, _, _, calls := discoveryFixture(t)
	ctx := a.tmdbContext(a.ctx)
	c := ctx.Value(tmdbCacheKey{}).(*tmdbDiskCache)
	base := time.Now().UTC()
	var clock atomic.Int64
	clock.Store(base.Unix())
	c.now = func() time.Time { return time.Unix(clock.Load(), 0) }
	var out any
	read := func() {
		t.Helper()
		if err := a.discoveryJSON(ctx, "search/multi", url.Values{"query": {"active"}}, &out); err != nil {
			t.Fatal(err)
		}
	}
	read()
	before := calls.Load()
	clock.Store(base.Add(14 * time.Minute).Unix())
	read()
	if calls.Load() != before {
		t.Fatal("fresh feed missed cache")
	}
	clock.Store(base.Add(16 * time.Minute).Unix())
	read()
	if calls.Load() != before+1 {
		t.Fatal("active feed stayed stale")
	}
	files, _ := os.ReadDir(c.dir())
	entry, err := c.read(files[0].Name())
	if err != nil || !entry.Accessed.Equal(c.now()) || !entry.Fetched.Equal(c.now()) {
		t.Fatal("refresh timestamps missing", err)
	}
	clock.Store(base.Add(7*24*time.Hour + 17*time.Minute).Unix())
	c.prune()
	files, _ = os.ReadDir(c.dir())
	if len(files) != 0 {
		t.Fatal("idle feed file was not removed")
	}
}

func TestTMDBCacheArchiveExpiryAndDirectoryResults(t *testing.T) {
	a, h, cookie, _ := discoveryFixture(t)
	ctx := a.tmdbContext(a.ctx)
	c := ctx.Value(tmdbCacheKey{}).(*tmdbDiskCache)
	if _, err := a.discoveryMediaDetail(ctx, "tv", 42); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(c.dir())
	entry, err := c.read(files[0].Name())
	if err != nil {
		t.Fatal(err)
	}
	entry.Accessed = time.Now().Add(-8 * 24 * time.Hour)
	if err := c.write(files[0].Name(), entry); err != nil {
		t.Fatal(err)
	}
	cfg := a.store.snapshot().Settings
	cfg.CacheEnabled = true
	cfg.CacheMemoryMB = 8
	cfg.CacheMaxItems = 100
	a.cache.put("mobile:folder", []File{{ID: "episode", Name: "Episode.mkv", Size: 99}}, 60, cfg)
	w := request(t, h, "GET", "/api/archives/cache?source=directory", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Episode.mkv") || strings.Contains(w.Body.String(), "剧集示例") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(c.dir(), files[0].Name())); !os.IsNotExist(err) {
		t.Fatal("archive did not prune idle file", err)
	}
	if w := request(t, h, "GET", "/api/archives/cache", nil, nil); w.Code != 401 {
		t.Fatal("cache archive was public", w.Code)
	}
	a.cache.clear()
	w = request(t, h, "GET", "/api/archives/cache?source=directory", nil, cookie)
	if !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatal("cleared directory cache stayed in archive", w.Body.String())
	}
}

func TestTMDBCacheRejectsCorruptionAndChangedCredentials(t *testing.T) {
	a, _, _, calls := discoveryFixture(t)
	ctx := a.tmdbContext(a.ctx)
	c := ctx.Value(tmdbCacheKey{}).(*tmdbDiskCache)
	var out any
	if err := a.discoveryJSON(ctx, "search/multi", url.Values{"query": {"same"}}, &out); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(c.dir())
	if err := os.WriteFile(filepath.Join(c.dir(), files[0].Name()), []byte(`{"data":{"id":999}}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	if err := a.discoveryJSON(ctx, "search/multi", url.Values{"query": {"same"}}, &out); err != nil || calls.Load() != before+1 {
		t.Fatal("corrupt cache was trusted", err)
	}
	// A credential change must miss the old scope, even for identical endpoint/query.
	cfg := pluginDefaults("tmdb", a.store.snapshot().Plugins["tmdb"])
	cfg.APIKey = "different-key"
	fetches := 0
	client := &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		fetches++
		if r.Header.Get("Authorization") != "Bearer different-key" {
			t.Error("new credential was not used")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"results":[]}`)), Request: r}, nil
	})}
	err := tmdbGet(ctx, client, cfg, "search/multi", url.Values{"query": {"same"}}, &out)
	if err != nil || fetches != 1 {
		t.Fatal("changed scope reused old data", err)
	}
}

func Test115ShortPagesDoNotSkipFullSyncFiles(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 0, "")
	task.STRMSource = "life"
	task.fullSyncGeneration = true
	if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	original := apiClient.Transport
	offsets := []string{}
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/behavior/detail" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"state":true,"data":{"list":[]}}`)), Request: r}, nil
		}
		if r.URL.Path != "/files" {
			return original.RoundTrip(r)
		}
		q := r.URL.Query()
		if q.Get("show_dir") == "0" {
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		offsets = append(offsets, q.Get("offset"))
		start := 0
		switch q.Get("offset") {
		case "2":
			start = 2
		case "4":
			start = 4
		case "0":
		default:
			t.Fatalf("offset jumped over a short page: %s", q.Get("offset"))
		}
		values := []any{}
		for i := start; i < min(start+2, 5); i++ {
			values = append(values, map[string]any{"fid": strconv.Itoa(10000 + i), "cid": "10", "n": string(rune('A'+i)) + ".mkv", "pc": "pick00000000000" + string(rune('0'+i)), "s": 42})
		}
		raw, _ := json.Marshal(map[string]any{"state": true, "cid": "10", "count": 5, "data": values})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})
	count, err := a.executeTask(a.ctx, task, s)
	if err != nil || count != 5 {
		t.Fatal(count, err)
	}
	if strings.Join(offsets, ",") != "0,2,4" {
		t.Fatal("full sync skipped or repeated pages", offsets)
	}
	for i := 0; i < 5; i++ {
		name := string(rune('A' + i))
		b, err := os.ReadFile(filepath.Join(task.Target, "Show", name+".strm"))
		if err != nil || !strings.Contains(string(b), "pick00000000000"+string(rune('0'+i))) {
			t.Fatal("missing generated file", name, err)
		}
	}
}

func TestManualSTRMScrapeOverwritesOnlySelectedFile(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{ID: "manual-overwrite", Kind: "strm", StorageID: s.ID, Target: t.TempDir()}
	tmdb, _ := workspaceTMDB(t)
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{task}
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["tmdb"] = tmdb
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root, err := a.scrapeRoot(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(root, "Arrival.strm"), "https://media.test/arrival")
	writeTest(t, filepath.Join(root, "Arrival.nfo"), "old metadata")
	writeTest(t, filepath.Join(root, "Other.nfo"), "untouched")
	if err := a.saveScrapeIndex(task.ID, scrapeIndex{Root: root, Items: []scrapeItem{{Path: "Arrival.strm", Title: "Arrival", Kind: "movie", TMDB: 1}}}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": task.ID, "path": "Arrival.strm", "kind": "movie", "tmdb": 1, "scrape": true, "group": false}, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	a.wg.Wait()
	b, err := os.ReadFile(filepath.Join(root, "Arrival.nfo"))
	other, _ := os.ReadFile(filepath.Join(root, "Other.nfo"))
	if err != nil || !strings.Contains(string(b), "<tmdbid>1</tmdbid>") || strings.Contains(string(b), "old metadata") || string(other) != "untouched" {
		t.Fatal("manual overwrite expanded or skipped", string(b), string(other), err)
	}
}

func TestMyMediaFeaturedSeriesAndMissingDetails(t *testing.T) {
	a, _, _, _ := mediaFixture(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/Items" && (r.URL.Query().Get("IncludeItemTypes") == "Movie,Series" || r.URL.Query().Get("IncludeItemTypes") == "Series") {
			jsonResponse(w, 200, map[string]any{"Items": []any{map[string]any{"Id": "show", "Name": "剧集示例", "Type": "Series", "Path": "/media/Series/Show", "Overview": "作品简介", "DateCreated": "2026-11-06", "ProviderIds": map[string]string{"Tmdb": "42"}}}, "TotalRecordCount": 1})
			return true
		}
		if r.URL.Path == "/Shows/show/Episodes" && r.URL.Query().Get("Limit") == "1" {
			jsonResponse(w, 200, map[string]any{"Items": []any{}, "TotalRecordCount": 23})
			return true
		}
		return false
	})
	if err := a.store.update(func(st *State) error {
		st.MediaDisplay.Libraries = map[string][]upstream.Library{"emby": {{ID: "lib", Name: "剧集库"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	l, _ := mediaLink(st, "emby")
	c, err := a.dashboardClient(st, l)
	if err != nil {
		t.Fatal(err)
	}
	data := mediaDashboardData{}
	if err := a.loadVideoDashboard(a.ctx, st, l, c, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Featured) != 1 || data.Featured[0].Episodes == nil || *data.Featured[0].Episodes != 23 || data.Featured[0].Overview != "作品简介" || data.Featured[0].LibraryName != "剧集库" || !strings.HasSuffix(data.Featured[0].WebURL, "#!/item?id=show") {
		t.Fatal("featured series missing details", data.Featured)
	}
	result, err := a.scanMediaMissing(a.ctx, st, l, c, &mediaScan{})
	if err != nil || len(result.Items) != 1 {
		t.Fatal(err, result)
	}
	item := result.Items[0].Item
	if item.TMDB != 42 || item.Path != "/media/Series/Show" || item.LibraryName != "剧集库" {
		t.Fatal("missing detail lost source identity", item)
	}
}

func TestManualDirectoryScrapeOverwritesExistingMetadata(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	file := filepath.Join(s.Config["root"], "Arrival.2016.mkv")
	writeTest(t, file, "media")
	writeTest(t, strings.TrimSuffix(file, ".mkv")+".nfo", "old metadata")
	tmdb, _ := workspaceTMDB(t)
	in := directoryScrapeInput{StorageID: s.ID, Directory: "/", IDs: []string{"/Arrival.2016.mkv"}, TMDB: 1, Kind: "movie"}
	if err := a.runDirectoryScrape(a.ctx, s, in, "match", directoryScrapeKey(s, "/"), tmdb, PluginConfig{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(strings.TrimSuffix(file, ".mkv") + ".nfo")
	if err != nil || strings.Contains(string(b), "old metadata") || !strings.Contains(string(b), "<tmdbid>1</tmdbid>") {
		t.Fatal("manual match did not overwrite", string(b), err)
	}
}
