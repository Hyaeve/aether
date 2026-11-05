package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLibraryScrapeOneShareIsOneWork(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"01.mkv.share.strm", "02.mkv.share.strm", "Season 2/Show.S02E01.mkv.share.strm"} {
		writeTest(t, filepath.Join(dir, filepath.FromSlash(name)), "https://example.test/file")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	items, err := scanSTRM(context.Background(), root, scrapeIndex{}, scrapeSettings{ShareOnly: true, LibraryName: "Show (2026)"})
	if err != nil {
		t.Fatal(err)
	}
	works := scrapeWorks(items)
	if len(works) != 1 || works[0].Count != 3 || works[0].Title != "Show" || works[0].Kind != "tv" {
		t.Fatal("share was flattened into separate works", works)
	}
	for _, item := range items {
		if item.LibraryWork != "Show (2026)" || item.Title != "Show" || strings.HasPrefix(item.Path, "Show/") {
			t.Fatal("logical grouping changed disk paths", item)
		}
	}
	plain, err := scanSTRM(context.Background(), root, scrapeIndex{}, scrapeSettings{})
	if err != nil || len(scrapeWorks(plain)) <= 1 {
		t.Fatal("ordinary STRM grouping was changed", err)
	}
}

func TestLibraryMetadataOptionsSaveDoesNotGenerate(t *testing.T) {
	for _, provider := range []string{"quark", "mobile", "cas", "115"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			old := quarkLibraryCard{ID: "options", Secret: "https://pan.quark.cn/s/code", Name: "Show", ShareName: "Show", Directory: t.TempDir(), DirectoryName: "Show", MediaExtensions: "mkv", CascadeCleanup: "off", Enabled: true}
			if provider == "mobile" || provider == "cas" {
				old.Secret = mobileLibrarySecret("code", "")
			}
			if provider == "115" {
				old.Secret = "https://115.com/s/code"
				old.StorageID = "pan"
			}
			if err := a.store.update(func(st *State) error {
				switch provider {
				case "quark":
					initQuarkLibraryRuntime(st)
					st.QuarkLibraryCards = []quarkLibraryCard{old}
				case "mobile":
					initMobileLibrary(st)
					st.MobileLibraryCards = []quarkLibraryCard{old}
				case "cas":
					initCASLibrary(st)
					st.CASLibraryCards = []quarkLibraryCard{old}
				case "115":
					initLibrary115(st)
					st.Library115Cards = []quarkLibraryCard{old}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			previous := apiClient.Transport
			defer func() { apiClient.Transport = previous }()
			apiClient.Transport = casTransport(func(*http.Request) (*http.Response, error) {
				t.Error("configuration save contacted cloud")
				return nil, errors.New("unexpected request")
			})
			next := old
			next.MediaExtensions = "mkv;mp4"
			next.ExcludeFiles = "sample"
			next.ScrapeEnabled = true
			next.CascadeCleanup = "jpg;nfo"
			ok, err := a.saveShareLibraryMetadata(a.ctx, provider, old, next)
			if !ok || err != nil {
				t.Fatal("options should only be persisted", ok, err)
			}
			var got quarkLibraryCard
			st := a.store.snapshot()
			switch provider {
			case "quark":
				got = st.QuarkLibraryCards[0]
			case "mobile":
				got = st.MobileLibraryCards[0]
			case "cas":
				got = st.CASLibraryCards[0]
			case "115":
				got = st.Library115Cards[0]
			}
			if got.MediaExtensions != next.MediaExtensions || got.ExcludeFiles != "sample" || !got.ScrapeEnabled || got.CascadeCleanup != "jpg;nfo" {
				t.Fatal("options not saved", got)
			}
			if _, err := os.Stat(quarkLibraryRoot(next)); !os.IsNotExist(err) {
				t.Fatal("saving options generated a library", err)
			}
		})
	}
}

func TestScrapeParentScopeAndSingleFileStaySeparate(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	task := Task{ID: "scope", Kind: "strm", StorageID: s.ID, Target: t.TempDir()}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	root, err := a.scrapeRoot(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	items := []scrapeItem{{Path: "Media/Show/a.strm", Kind: "tv", Title: "Show", TMDB: 1}, {Path: "Media/Show/b.strm", Kind: "tv", Title: "Show", TMDB: 1}, {Path: "Other/c.strm", Kind: "movie", Title: "Other", TMDB: 9}}
	for _, item := range items {
		writeTest(t, filepath.Join(root, filepath.FromSlash(item.Path)), "stream")
	}
	if err := a.saveScrapeIndex(task.ID, scrapeIndex{Root: root, Items: items}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": task.ID, "scope": "Media", "kind": "tv", "tmdb": 2}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	updated := a.loadScrapeIndex(task.ID, root).Items
	if updated[0].TMDB != 2 || updated[1].TMDB != 2 || updated[2].TMDB != 9 {
		t.Fatal("parent scope crossed into unrelated work", updated)
	}
	w = request(t, h, "POST", "/api/strm-scrape/reset", map[string]any{"taskId": task.ID, "path": items[0].Path, "group": false, "confirmed": true}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	updated = a.loadScrapeIndex(task.ID, root).Items
	if updated[0].TMDB != 0 || updated[1].TMDB != 2 || updated[2].TMDB != 9 {
		t.Fatal("single file reset expanded to sibling files", updated)
	}
	for _, item := range items {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(item.Path))); err != nil {
			t.Fatal("reset removed media", err)
		}
	}
}

func TestDirectoryScrapeFolderMatchAndResetAreScoped(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	root := s.Config["root"]
	writeTest(t, filepath.Join(root, "Show", "Arrival.2016.mkv"), "media")
	writeTest(t, filepath.Join(root, "Other", "keep.mkv"), "other")
	tmdb, _ := workspaceTMDB(t)
	in := directoryScrapeInput{StorageID: s.ID, Directory: "/", IDs: []string{"/Show"}, Group: true, TMDB: 1, Kind: "movie"}
	if err := a.runDirectoryScrape(a.ctx, s, in, "match", directoryScrapeKey(s, "/"), tmdb, PluginConfig{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Show", "Arrival.2016.nfo")); err != nil {
		t.Fatal("explicit folder match did not write metadata", err)
	}
	index := a.loadDirectoryScrapeIndex(directoryScrapeKey(s, ""))
	index.Items = append(index.Items, scrapeItem{SourceID: "/Other/keep.mkv", SourceName: "keep.mkv", SourceParent: "/Other", TMDB: 9, Kind: "movie", Status: "ok"})
	if err := a.saveScrapeIndex(directoryScrapeKey(s, ""), index); err != nil {
		t.Fatal(err)
	}
	if err := a.runDirectoryScrape(a.ctx, s, in, "reset", directoryScrapeKey(s, "/"), PluginConfig{}, PluginConfig{}); err != nil {
		t.Fatal(err)
	}
	index = a.loadDirectoryScrapeIndex(directoryScrapeKey(s, ""))
	if len(index.Items) != 1 || index.Items[0].TMDB != 9 {
		t.Fatal("reset crossed selected folder", index.Items)
	}
	for _, name := range []string{"Show/Arrival.2016.nfo", "Show/Arrival.2016.mkv", "Other/keep.mkv"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal("metadata reset deleted files", err)
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Second)
	cancel()
	if err := a.runDirectoryScrape(ctx, s, in, "reset", directoryScrapeKey(s, "/"), PluginConfig{}, PluginConfig{}); err == nil {
		t.Fatal("cancelled reset changed index")
	}
}
