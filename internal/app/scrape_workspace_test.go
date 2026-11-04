package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"golang.org/x/net/webdav"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workspaceTMDB(t *testing.T) (PluginConfig, *httptest.Server) {
	t.Helper()
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jG1sAAAAASUVORK5CYII=")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/search/movie":
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 1, "title": "Arrival", "release_date": "2016-01-01"}}})
		case "/3/movie/1":
			jsonResponse(w, 200, map[string]any{"id": 1, "title": "Arrival", "poster_path": "/one.png", "overview": "workspace plot"})
		case "/t/p/w780/one.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return PluginConfig{Enabled: true, APIURL: server.URL, ImageURL: server.URL, APIKey: "key", Language: "en-US"}, server
}
func TestDirectoryScrapeLocalAndCloudWriteback(t *testing.T) {
	for _, kind := range []string{"local", "webdav"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			dir := t.TempDir()
			writeTest(t, filepath.Join(dir, "Arrival.2016.mkv"), "media original")
			s := Storage{ID: "directory-pool", Name: "目录池", Type: kind, Enabled: true, Config: map[string]string{"root": dir}}
			if kind == "webdav" {
				server := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()})
				defer server.Close()
				s.Config = map[string]string{"address": server.URL}
			}
			tmdb, _ := workspaceTMDB(t)
			if err := a.store.update(func(st *State) error {
				st.Storages = append(st.Storages, s)
				if st.Plugins == nil {
					st.Plugins = map[string]PluginConfig{}
				}
				st.Plugins["tmdb"] = tmdb
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			if w := request(t, h, "POST", "/api/directory-scrape/run", map[string]any{"storageId": s.ID}, nil); w.Code != 401 {
				t.Fatal("unguarded write", w.Code)
			}
			if w := request(t, h, "GET", "/api/directory-scrape/files?storageId="+s.ID, nil, cookie); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			in := directoryScrapeInput{StorageID: s.ID, Directory: "/"}
			key := directoryScrapeKey(s, "/")
			// Identification reads TMDB and persists recognition, but creates no NFO.
			if err := a.runDirectoryScrape(context.Background(), s, in, "identify", key, tmdb, PluginConfig{}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, "Arrival.2016.nfo")); !os.IsNotExist(err) {
				t.Fatal("identify wrote metadata", err)
			}
			if err := a.runDirectoryScrape(context.Background(), s, in, "run", key, tmdb, PluginConfig{}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "Arrival.2016.nfo"))
			if err != nil || !strings.Contains(string(data), "workspace plot") {
				t.Fatal(string(data), err)
			}
			if _, err := os.Stat(filepath.Join(dir, "Arrival.2016-poster.jpg")); err != nil {
				t.Fatal(err)
			}
			original, _ := os.ReadFile(filepath.Join(dir, "Arrival.2016.mkv"))
			if string(original) != "media original" {
				t.Fatal("source mutated")
			}
			writeTest(t, filepath.Join(dir, "Arrival.2016.nfo"), "user metadata")
			if err := a.runDirectoryScrape(context.Background(), s, in, "run", key, tmdb, PluginConfig{}); err != nil {
				t.Fatal(err)
			}
			data, _ = os.ReadFile(filepath.Join(dir, "Arrival.2016.nfo"))
			if string(data) != "user metadata" {
				t.Fatal("missing mode overwrote")
			}
			cfg, _ := json.Marshal(scrapeSettings{WriteMode: "overwrite"})
			if err := os.MkdirAll(filepath.Join(a.store.dir, "organize"), 0700); err != nil {
				t.Fatal(err)
			}
			writeTest(t, filepath.Join(a.store.dir, "organize", "strm-scrape.json"), string(cfg))
			if err := a.runDirectoryScrape(context.Background(), s, in, "run", key, tmdb, PluginConfig{}); err != nil {
				t.Fatal(err)
			}
			data, _ = os.ReadFile(filepath.Join(dir, "Arrival.2016.nfo"))
			if !strings.Contains(string(data), "workspace plot") {
				t.Fatal("overwrite missing", string(data))
			}
			w := request(t, h, "GET", "/api/archives/scrape?mode=group", nil, cookie)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "目录刮削") {
				t.Fatal(w.Code, w.Body.String())
			}
			if leftovers, _ := filepath.Glob(filepath.Join(a.transferTempDirectory(), "scrape", "metadata-*")); len(leftovers) != 0 {
				t.Fatal("temporary files leaked", leftovers)
			}
		})
	}
}

func TestDirectoryScrapeSelectionGuardsAndCancel(t *testing.T) {
	for _, name := range []string{"file.mpg", "file.mpeg", "file.rmvb", "file.mkvb", "file.rm", "file.cas.strm", "file.share.strm", "file.ed2k.strm"} {
		if !directoryScrapeMedia(name) {
			t.Fatal("missing supported media", name)
		}
	}
	a := testApp(t)
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "Arrival.2016.mkv"), "video")
	s := Storage{ID: "directory-local", Type: "local", Enabled: true, Config: map[string]string{"root": dir}}
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	in := directoryScrapeInput{Directory: "/", IDs: []string{"/missing"}}
	if _, _, err := a.scanDirectoryScrape(context.Background(), s, in); err == nil {
		t.Fatal("stale selection accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := a.scanDirectoryScrape(ctx, s, directoryScrapeInput{Directory: "/"}); err == nil {
		t.Fatal("cancel ignored")
	}
	output := a.directoryScrapeOutput(context.Background(), s, map[string]string{".": "/"}, func() error { return nil })
	if err := output.write("../outside.nfo", []byte("bad"), false); err == nil {
		t.Fatal("escaped scan")
	}
	if err := output.write("unknown/a.nfo", []byte("bad"), false); err == nil {
		t.Fatal("invented directory")
	}
	// A live source reservation prevents unrelated file operations on this scope.
	a.runMu.Lock()
	a.directoryScrapeTask = &Task{ID: "directory:test", Name: "目录刮削", Kind: "directory-scrape", StorageID: s.ID, Source: "/"}
	a.running["directory:test"] = func() {}
	err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{s, "/Arrival.2016.mkv", ""}})
	delete(a.running, "directory:test")
	a.directoryScrapeTask = nil
	a.runMu.Unlock()
	if err == nil {
		t.Fatal("active directory scrape did not protect source")
	}
}

func TestScrapeWorkspaceFourLibrariesAndCASFilter(t *testing.T) {
	a := testApp(t)
	st := a.store.snapshot()
	card := quarkLibraryCard{ID: "library", Name: "作品", Directory: t.TempDir(), DirectoryName: "库", Enabled: true, ScrapeEnabled: true}
	st.Library115Cards = []quarkLibraryCard{card}
	st.MobileLibraryCards = []quarkLibraryCard{card}
	st.QuarkLibraryCards = []quarkLibraryCard{card}
	st.CASLibraryCards = []quarkLibraryCard{card}
	sources := shareScrapeSources(st)
	if len(sources) != 4 {
		t.Fatal(sources)
	}
	for _, source := range sources {
		key := source["id"].(string)
		if _, ok := shareScrapeCard(st, key); !ok {
			t.Fatal(key)
		}
		unlock, err := a.lockShareScrape(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.lockShareScrape(key); err == nil {
			t.Fatal("gate did not exclude duplicate", key)
		}
		unlock()
	}
	cfg := scrapeSettings{ShareOnly: true, CASOnly: true}
	if !scrapeIncludes("Film.mkv.cas.strm", cfg) || scrapeIncludes("Film.mkv.share.strm", cfg) || scrapeIncludes("Film.strm", cfg) {
		t.Fatal("CAS filter failed")
	}
	cfg.ExcludeTypes = "mkv"
	if scrapeIncludes("Film.mkv.cas.strm", cfg) {
		t.Fatal("CAS excluded source extension ignored")
	}
	card.CascadeCleanup = "on"
	card.Secret = "https://yun.139.com/shareweb/abc?pwd=1234"
	if err := validateMobileLibraryCard(&card); err != nil || card.CascadeCleanup != "on" {
		t.Fatal("mobile cascade rejected", err)
	}
}

func TestOrganizeArchivesDurableGroupsAndSearch(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for i := 0; i < 2; i++ {
		if err := a.recordOrganizeArchive(Task{ID: "organize", Name: "归档任务"}, archiveEntry{Title: "Arrival", Kind: "movie", TMDB: 1, Poster: "/poster.png", SourceStorage: "原目录池", SourcePath: "/来源/Arrival.mkv", TargetStorage: "归档池", TargetPath: "/电影/Arrival.mkv", Status: "success", Method: "move", Size: 123, StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	w := request(t, h, "GET", "/api/archives/organize?mode=group", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"count":2`) || !strings.Contains(w.Body.String(), `"poster":"/poster.png"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/archives/organize?q=归档池&page=999", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"page":1`) || !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := openExecutionArchives(a.store, a.dataDir)
	if err != nil || len(reopened.records) < 2 {
		t.Fatal("archive persistence", err)
	}
}

func TestTelegramTypedLibraryPrefixes(t *testing.T) {
	for _, tc := range []struct{ in, mode, content string }{{"虚拟链库https://example.test", "library", "https://example.test"}, {"移动链库 链接", "library:mobile", "链接"}, {"夸克链库链接", "library:quark", "链接"}, {"115链库 链接", "library:115", "链接"}, {"CAS链库链接", "cas-library", "链接"}, {"cas链库 链接", "cas-library", "链接"}, {"/share@AetherBot 链接", "library", "链接"}, {"/cas 链接", "cas-library", "链接"}} {
		mode, content, ok := telegramCommand(tc.in)
		if !ok || mode != tc.mode || content != tc.content {
			t.Fatal(tc, mode, content, ok)
		}
	}
	mode, _, _ := telegramCommand("消息里面的移动链库链接")
	if mode != "" {
		t.Fatal("matched nonprefix")
	}
}

func TestTelegramTypedLibraryFiltersMixedBatch(t *testing.T) {
	for _, mode := range []string{"library:quark", "library:mobile", "library:115", "cas-library", "library"} {
		t.Run(mode, func(t *testing.T) {
			a, cfg, _, _ := telegramLibraryFixture(t)
			links := []telegramLink{{Provider: "quark", Kind: "share", Link: "https://pan.quark.cn/s/example"}, {Provider: "mobile", Kind: "share", Link: "https://yun.139.com/shareweb/example"}, {Provider: "115", Kind: "share", Link: "https://115.com/s/example"}}
			update := telegramTextUpdate(1, 123, 123, "")
			if err := a.queueTelegramLinks(context.Background(), cfg, *update.Message, links, true, mode); err != nil {
				t.Fatal(err)
			}
			jobs := a.store.snapshot().TelegramRuntime.Jobs
			count := 1
			if mode == "library" {
				count = 3
			}
			if len(jobs) != count {
				t.Fatal(mode, jobs)
			}
			for _, job := range jobs {
				if mode == "library" {
					continue
				}
				provider := strings.TrimPrefix(mode, "library:")
				wantKind := "library"
				if mode == "cas-library" {
					provider = "mobile"
					wantKind = mode
				}
				if job.Provider != provider || job.Kind != wantKind {
					t.Fatal("wrong provider or persisted transient mode", job)
				}
			}
		})
	}
}

func TestDirectoryScrapeAPIConflictCancelAndNoWrites(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "Arrival.2016.mkv"), "original")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	s := Storage{ID: "api-pool", Name: "目录", Type: "local", Enabled: true, Config: map[string]string{"root": dir}}
	a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, s)
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["tmdb"] = PluginConfig{Enabled: true, APIURL: server.URL, APIKey: "key"}
		return nil
	})
	body := map[string]any{"storageId": s.ID, "directory": "/"}
	if w := request(t, h, "POST", "/api/directory-scrape/run", body, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/directory-scrape/run", body, cookie); w.Code != 409 {
		t.Fatal("duplicate start", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/strm-scrape/stop", map[string]any{}, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.scrapeMu.Lock()
		running := a.scrapeProgress.Running
		a.scrapeMu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	list, _ := os.ReadDir(dir)
	if len(list) != 1 {
		t.Fatal("cancel wrote metadata", list)
	}
	a.runMu.Lock()
	reserved := a.directoryScrapeTask != nil || len(a.running) != 0
	a.runMu.Unlock()
	if reserved {
		t.Fatal("scope leaked after cancellation")
	}
}

func TestDirectoryScrapeChangedMediaStopsBeforeWrite(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	name := filepath.Join(dir, "Arrival.2016.mkv")
	writeTest(t, name, "original")
	s := Storage{ID: "changed-pool", Type: "local", Enabled: true, Config: map[string]string{"root": dir}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "search") {
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 1, "title": "Arrival", "release_date": "2016-01-01"}}})
			return
		}
		writeTest(t, name, "changed media size")
		jsonResponse(w, 200, map[string]any{"id": 1, "title": "Arrival"})
	}))
	defer server.Close()
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	if err := a.runDirectoryScrape(context.Background(), s, directoryScrapeInput{Directory: "/"}, "run", directoryScrapeKey(s, "/"), PluginConfig{APIKey: "key", APIURL: server.URL}, PluginConfig{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Arrival.2016.nfo")); !os.IsNotExist(err) {
		t.Fatal("changed media accepted", err)
	}
	a.scrapeMu.Lock()
	failed := a.scrapeProgress.Failed
	a.scrapeMu.Unlock()
	if failed != 1 {
		t.Fatal("change was not recorded as failure", failed)
	}
}

func TestScrapeWorkspace115AndCASActuallyWriteMetadata(t *testing.T) {
	for _, provider := range []string{"115", "cas"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			tmdb, _ := workspaceTMDB(t)
			card := quarkLibraryCard{ID: "scrape-card", Name: provider + "影视", Directory: t.TempDir(), DirectoryName: "作品", Enabled: true, ScrapeEnabled: true}
			rootName := quarkLibraryRoot(card)
			if err := os.MkdirAll(rootName, 0700); err != nil {
				t.Fatal(err)
			}
			suffix := ".share.strm"
			prefix := library115ScrapePrefix
			if provider == "cas" {
				suffix = ".cas.strm"
				prefix = casScrapePrefix
			}
			filename := "Arrival.2016.mkv" + suffix
			writeTest(t, filepath.Join(rootName, filename), "http://pointer.example/video")
			if err := a.store.update(func(st *State) error {
				if provider == "115" {
					st.Library115Cards = []quarkLibraryCard{card}
				} else {
					st.CASLibraryCards = []quarkLibraryCard{card}
				}
				st.Plugins = map[string]PluginConfig{"tmdb": tmdb}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			id := prefix + card.ID
			if provider == "cas" {
				w := request(t, h, "GET", "/api/virtual-library/cas", nil, cookie)
				var payload struct {
					Cards []quarkLibraryCard `json:"cards"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &payload) != nil || len(payload.Cards) != 1 || !payload.Cards[0].ScrapeEnabled {
					t.Fatal("CAS editor lost scraping preference", w.Code, w.Body.String())
				}
			}
			w := request(t, h, "GET", "/api/strm-scrape/items?taskId="+id, nil, cookie)
			if w.Code != 200 || !strings.Contains(w.Body.String(), filename) {
				t.Fatal(w.Code, w.Body.String())
			}
			w = request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": id, "path": filename, "tmdb": 1, "kind": "movie", "scrape": true}, cookie)
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			waitRevisionScrape(t, a)
			metadata := strings.TrimSuffix(filename, ".strm") + ".nfo"
			raw, err := os.ReadFile(filepath.Join(rootName, metadata))
			if err != nil || !strings.Contains(string(raw), "workspace plot") {
				t.Fatal("metadata was not actually written", err, string(raw))
			}
			if raw, _ := os.ReadFile(filepath.Join(rootName, filename)); string(raw) != "http://pointer.example/video" {
				t.Fatal("pointer mutated")
			}
		})
	}
}

func TestDirectoryScrapeIndexRetainsSelectionsAcrossFolders(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "Arrival.2016.mkv"), "root media")
	if err := os.Mkdir(filepath.Join(dir, "Folder"), 0700); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(dir, "Folder", "Arrival.2016.mkv"), "child media")
	s := Storage{ID: "cache-pool", Name: "本地", Type: "local", Enabled: true, Config: map[string]string{"root": dir}}
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	tmdb, _ := workspaceTMDB(t)
	if err := a.runDirectoryScrape(context.Background(), s, directoryScrapeInput{Directory: "/", IDs: []string{"/Folder"}, TMDB: 1, Kind: "movie"}, "match", directoryScrapeKey(s, "/"), tmdb, PluginConfig{}); err == nil {
		t.Fatal("directory was accepted as a single manual media match")
	}
	for _, in := range []directoryScrapeInput{{Directory: "/", IDs: []string{"/Arrival.2016.mkv"}}, {Directory: "/Folder", IDs: []string{"/Folder/Arrival.2016.mkv"}}} {
		if err := a.runDirectoryScrape(context.Background(), s, in, "identify", directoryScrapeKey(s, in.Directory), tmdb, PluginConfig{}); err != nil {
			t.Fatal(err)
		}
	}
	index := a.loadDirectoryScrapeIndex(directoryScrapeKey(s, ""))
	if len(index.Items) != 2 {
		t.Fatal("later selection discarded earlier recognition", index)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, folder := range []string{"/", "/Folder"} {
		w := request(t, h, "GET", "/api/directory-scrape/files?storageId="+s.ID+"&directory="+folder, nil, cookie)
		var result struct {
			Items []scrapeItem `json:"items"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].Path != "Arrival.2016.mkv" {
			t.Fatal("folder recognition not restored", folder, w.Code, w.Body.String())
		}
	}
	writeTest(t, filepath.Join(dir, "Arrival.2016.mkv"), "changed file content")
	w := request(t, h, "GET", "/api/directory-scrape/files?storageId="+s.ID+"&directory=/", nil, cookie)
	var result struct {
		Items []scrapeItem `json:"items"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	if len(result.Items) != 0 {
		t.Fatal("changed file kept old recognition")
	}
}

func TestMobileCascadeOptionControlsExcludedMediaCompanions(t *testing.T) {
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "Film.mkv.share.strm"), "pointer")
	writeTest(t, filepath.Join(dir, "Film.mkv.share.nfo"), "metadata")
	writeTest(t, filepath.Join(dir, "unrelated.jpg"), "keep")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	digest, err := quarkLibraryDigest(root, "Film.mkv.share.strm")
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]quarkLibraryFile{"film": {CardID: "c", Relative: "Film.mkv.share.strm", Digest: digest, Media: true}}
	card := quarkLibraryCard{ID: "c", CascadeCleanup: "off"}
	plan, err := mobileLibraryCompanions(context.Background(), root, card, files, map[string]bool{})
	if err != nil || len(plan) != 0 {
		t.Fatal("off deleted filtered companions", plan, err)
	}
	card.CascadeCleanup = "on"
	plan, err = mobileLibraryCompanions(context.Background(), root, card, files, map[string]bool{})
	if err != nil || len(plan) != 1 || plan[0].name != "Film.mkv.share.nfo" {
		t.Fatal("on did not plan only companion", plan, err)
	}
	writeTest(t, filepath.Join(dir, "Film.mkv.share.strm"), "externally changed")
	plan, err = mobileLibraryCompanions(context.Background(), root, card, files, map[string]bool{})
	if err != nil || len(plan) != 0 {
		t.Fatal("modified media authorized companion deletion", plan, err)
	}
}
