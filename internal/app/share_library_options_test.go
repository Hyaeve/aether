package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShareLibraryNewSuffixQuark(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	result, err := a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Generated != 1 {
		t.Fatal(result, err)
	}
	body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "Movie.mkv.share.strm"))
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(body)), "/Movie.mkv") {
		t.Fatal("share suffix or original playback filename missing", string(body), err)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "Movie.mkv.strm")); !os.IsNotExist(err) {
		t.Fatal("old share format was generated", err)
	}
}

func TestShareLibraryNewSuffix115(t *testing.T) {
	f := newVirtual115NameFixture(t, "aB12")
	directory := t.TempDir()
	if w := f.generate(t, directory); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body, err := os.ReadFile(filepath.Join(directory, "Show", "第一季", "第1集 #1.mkv.share.strm"))
	if err != nil || !strings.Contains(string(body), "/第1集 %231.mkv") {
		t.Fatal("115 output format or readable body missing", string(body), err)
	}
}

func TestShareLibraryQuarkOldOutputIsNotReused(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	dir := quarkLibraryRoot(card)
	if err := os.Rename(filepath.Join(dir, "Movie.mkv.share.strm"), filepath.Join(dir, "Movie.mkv.strm")); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(dir, "Movie.nfo"), "shared metadata")
	if err := a.store.update(func(st *State) error {
		for key, file := range st.QuarkLibraryRuntime.Files {
			file.Relative = "Movie.mkv.strm"
			st.QuarkLibraryRuntime.Files[key] = file
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Generated != 1 || result.Removed != 1 {
		t.Fatal("legacy output counted as current-format output", result, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Movie.mkv.strm")); !os.IsNotExist(err) {
		t.Fatal("registered legacy output not reconciled", err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "Movie.nfo")); err != nil || string(body) != "shared metadata" {
		t.Fatal("metadata shared by the new output removed", string(body), err)
	}
}

func TestShareLibrary115NewOutputNeverOverwrites(t *testing.T) {
	f := newVirtual115NameFixture(t, "")
	directory := t.TempDir()
	if w := f.generate(t, directory); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	name := filepath.Join(directory, "Show", "第一季", "第1集 #1.mkv.share.strm")
	writeTest(t, name, "external edit")
	w := f.generate(t, directory)
	var result struct{ Generated, Skipped int }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || result.Generated != 0 || result.Skipped != 2 {
		t.Fatal("existing new-format files overwritten", w.Code, w.Body.String(), err)
	}
	if body, err := os.ReadFile(name); err != nil || string(body) != "external edit" {
		t.Fatal("external change lost", string(body), err)
	}
}

func TestShareLibraryOptionsAPIAndPersistence(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	if err := a.store.initModules(); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	body := map[string]any{"url": "https://pan.quark.cn/s/sample", "name": "Share", "directoryName": "Share", "directory": t.TempDir(), "enabled": false}
	w := request(t, h, "POST", "/api/virtual-library/quark", body, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	card := a.store.snapshot().QuarkLibraryCards[0]
	if card.ScrapeEnabled {
		t.Fatal("shares must default to excluded from scraping")
	}
	body["scrapeEnabled"], body["excludeDirs"], body["excludeFiles"], body["excludeTypes"] = true, "Extras;Trailer", "Sample", ".ISO;Mp4"
	address := "/api/virtual-library/quark/" + card.ID
	if w = request(t, h, "PUT", address, body, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initModules()
	}
	if err != nil {
		t.Fatal(err)
	}
	saved := reopened.snapshot().QuarkLibraryCards[0]
	if !saved.ScrapeEnabled || saved.ExcludeDirs != "Extras;Trailer" || saved.ExcludeFiles != "Sample" || saved.ExcludeTypes != "iso;mp4" {
		t.Fatal("options lost on restart", saved)
	}
	if err := validateQuarkLibrary(reopened.snapshot()); err != nil {
		t.Fatal("persisted/importable card invalid", err)
	}
	for _, field := range []string{"scrapeEnabled", "excludeDirs", "excludeFiles", "excludeTypes"} {
		delete(body, field)
	}
	if w = request(t, h, "PUT", address, body, cookie); w.Code != 200 || !a.store.snapshot().QuarkLibraryCards[0].ScrapeEnabled {
		t.Fatal("omitted fields erased saved options", w.Code, w.Body.String())
	}
	before := a.store.snapshot().QuarkLibraryCards[0]
	for _, invalid := range []struct{ field, value string }{{"excludeTypes", "mkv;bad/ext"}, {"excludeDirs", strings.Repeat("x", 4097)}, {"excludeFiles", "bad\nvalue"}} {
		body[invalid.field] = invalid.value
		if w = request(t, h, "PUT", address, body, cookie); w.Code != 400 || a.store.snapshot().QuarkLibraryCards[0] != before {
			t.Fatal("invalid exclusions saved", invalid.field, w.Code, w.Body.String())
		}
		delete(body, invalid.field)
	}
	body["scrapeEnabled"], body["excludeDirs"], body["excludeFiles"], body["excludeTypes"] = false, "", "", ""
	if w = request(t, h, "PUT", address, body, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cleared := a.store.snapshot().QuarkLibraryCards[0]
	if cleared.ScrapeEnabled || cleared.ExcludeDirs != "" || cleared.ExcludeFiles != "" || cleared.ExcludeTypes != "" {
		t.Fatal("explicit clearing not saved", cleared)
	}
}

func TestShareLibraryExclusionsBeforeMetadataTransfer(t *testing.T) {
	a := testApp(t)
	mock := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv;mp4")
	card.ExcludeDirs, card.ExcludeFiles, card.ExcludeTypes = "extras", "sample", "mp4;nfo"
	mock.files = append(mock.files, shareEntry{ID: "s", Name: "Sample.mkv", Size: 10}, shareEntry{ID: "p", Name: "Other.mp4", Size: 10}, shareEntry{ID: "n", Name: "Movie.nfo", Size: 10})
	transport := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		parent := r.URL.Query().Get("pdir_fid")
		if strings.HasSuffix(r.URL.Path, "/detail") && (parent == "root-folder" || parent == "extras") {
			list := []map[string]any{}
			if parent == "root-folder" {
				for _, f := range mock.files {
					list = append(list, map[string]any{"fid": f.ID, "file_name": f.Name, "size": f.Size, "share_fid_token": "source-token"})
				}
				list = append(list, map[string]any{"fid": "extras", "file_name": "Extras", "dir": true})
			} else {
				list = append(list, map[string]any{"fid": "hidden", "file_name": "Hidden.mkv", "size": 10, "share_fid_token": "source-token"}, map[string]any{"fid": "sidecar", "file_name": "Hidden.nfo", "size": 10, "share_fid_token": "source-token"})
			}
			raw, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": list}, "metadata": map[string]any{"_total": len(list)}})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
		}
		return transport.RoundTrip(r)
	})
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Generated != 1 || result.Metadata != 0 || mock.saves != 0 {
		t.Fatal("excluded items reached output/metadata transfer", result, err, mock.saves)
	}
	files := a.store.snapshot().QuarkLibraryRuntime.Files
	if len(files) != 1 {
		t.Fatal(files)
	}
	for _, file := range files {
		if file.Name != "Movie.mkv" || file.Relative != "Movie.mkv.share.strm" {
			t.Fatal(file)
		}
	}
}

func TestShareLibraryExclusionsReconcileOnlyOwnedOutput(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	dir := quarkLibraryRoot(card)
	writeTest(t, filepath.Join(dir, "keep.txt"), "untracked")
	card.ExcludeFiles = "Movie"
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Removed != 1 || result.Generated != 0 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Movie.mkv.share.strm")); !os.IsNotExist(err) {
		t.Fatal("excluded registered output retained", err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "keep.txt")); err != nil || string(body) != "untracked" {
		t.Fatal("untracked file changed", err)
	}
}

func TestShareLibraryScrapeOptInAndNewFormatOnly(t *testing.T) {
	a := testApp(t)
	card := newQuarkLibraryCard(t, a, "mkv")
	taskID := quarkScrapePrefix + card.ID
	if _, err := a.scrapeRoot(taskID); err == nil || len(shareScrapeSources(a.store.snapshot())) != 0 {
		t.Fatal("default card participates in scrape")
	}
	card.ScrapeEnabled, card.ExcludeDirs, card.ExcludeFiles, card.ExcludeTypes = true, "Extras", "Sample", "mp4"
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	rootName := quarkLibraryRoot(card)
	for _, name := range []string{"Movie.2024.mkv.share.strm", "Movie.2024.mkv.strm", "Sample.mkv.share.strm", "Other.mp4.share.strm", "Extras/Hidden.mkv.share.strm", "Show/01.mkv.share.strm", "Show/02.mkv.share.strm"} {
		filename := filepath.Join(rootName, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		writeTest(t, filename, "http://media.test/file\n")
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/state", nil, cookie)
	var state struct {
		Libraries []map[string]any `json:"scrapeLibraries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || len(state.Libraries) != 1 || len(state.Libraries[0]) != 5 || state.Libraries[0]["id"] != taskID || state.Libraries[0]["provider"] != "quark" {
		t.Fatal("share source missing or private fields exposed", w.Body.String(), err)
	}
	if strings.Contains(w.Body.String(), "pan.quark.cn") || strings.Contains(w.Body.String(), "aB12") || strings.Contains(w.Body.String(), card.Directory) {
		t.Fatal("share source leaked private configuration", w.Body.String())
	}
	w = request(t, h, "GET", "/api/strm-scrape/items?taskId="+taskID, nil, cookie)
	var items []scrapeItem
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 200 || len(items) != 3 {
		t.Fatal("scrape includes excluded or old-format files", w.Code, w.Body.String(), err)
	}
	for _, item := range items {
		if !isShareSTRM(item.Path) || strings.Contains(item.Title, "share") || strings.Contains(item.Title, "mkv") {
			t.Fatal("share suffix entered title recognition", item)
		}
		if strings.HasPrefix(item.Path, "Show/") && (item.Kind != "tv" || item.Episode < 1) {
			t.Fatal("share numeric episodes not grouped", item)
		}
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		t.Fatal(err)
	}
	record, err := a.newScrapeArchive(taskID, rootName, "identify", root, items[0], "")
	root.Close()
	if err != nil || record.TaskName != card.Name {
		t.Fatal("share archive missing source name", record, err)
	}
	// A changed exclusion invalidates a fresh cached index.
	card.ExcludeFiles = "Movie;Sample"
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/api/strm-scrape/items?taskId="+taskID+"&cached=true", nil, cookie)
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil || w.Code != 200 || len(items) != 2 {
		t.Fatal("stale exclusions reused from cache", w.Code, w.Body.String(), err)
	}
	for _, name := range []string{"Movie.2024.mkv.strm", "Sample.mkv.share.strm"} {
		if w := request(t, h, "GET", "/api/strm-scrape/files?taskId="+taskID+"&path="+name, nil, cookie); w.Code != 400 {
			t.Fatal("excluded file editor accepted", name, w.Code)
		}
	}
	writeTest(t, filepath.Join(rootName, "Extras.mkv.share.strm"), "http://media.test/extras\n")
	if w := request(t, h, "GET", "/api/strm-scrape/files?taskId="+taskID+"&path=Extras.mkv.share.strm", nil, cookie); w.Code != 200 {
		t.Fatal("directory exclusion incorrectly matched a filename", w.Code, w.Body.String())
	}
	card.ScrapeEnabled = false
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "GET", "/api/strm-scrape/items?taskId="+taskID, nil, cookie); w.Code != 400 {
		t.Fatal("opted-out source still accessible", w.Code)
	}
}

func TestShareLibraryScrapeGateAndOldGrant(t *testing.T) {
	a := testApp(t)
	mock := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	card.ScrapeEnabled = true
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	unlock, err := a.lockShareScrape(quarkScrapePrefix + card.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/api/virtual-library/quark/card/run", "/api/strm-scrape/scan"} {
		w := request(t, h, "POST", endpoint, map[string]string{"taskId": quarkScrapePrefix + card.ID}, cookie)
		if w.Code != 409 {
			t.Fatal("concurrent share writer accepted", endpoint, w.Code)
		}
	}
	unlock()
	w := request(t, h, "POST", "/api/strm-scrape/scan", map[string]string{"taskId": quarkScrapePrefix + card.ID}, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.scrapeMu.Lock()
		running := a.scrapeProgress.Running
		a.scrapeMu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("share scan never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !a.quarkLibraryMu.TryLock() {
		t.Fatal("share scrape leaked its generation gate")
	}
	a.quarkLibraryMu.Unlock()
	if err := a.store.update(func(st *State) error {
		for key, file := range st.QuarkLibraryRuntime.Files {
			file.Relative = "Movie.mkv.strm"
			st.QuarkLibraryRuntime.Files[key] = file
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"HEAD", "GET"} {
		w := request(t, h, method, "/quark-share/sample-aB12/Movie.mkv", nil, nil)
		if w.Code != 404 || mock.saves != 0 {
			t.Fatal("legacy grant accepted or triggered transfer", method, w.Code, mock.saves)
		}
	}
}

func TestShareLibraryCompanionsKeepNewSuffixOutOfMediaStem(t *testing.T) {
	for _, name := range []string{"Movie.nfo", "Movie.mkv.nfo", "Movie.mkv.share.nfo", "Movie-poster.jpg", "Movie.mkv-poster.jpg", "Movie.mkv.share-poster.jpg"} {
		if !quarkLibraryCompanion("dir/"+name, "dir/Movie.mkv.share.strm") {
			t.Fatal("new share companion not recognized", name)
		}
	}
	for _, name := range []string{"Other.nfo", "elsewhere/Movie.nfo", "Movie.mkv.share.strm"} {
		if quarkLibraryCompanion(name, "dir/Movie.mkv.share.strm") {
			t.Fatal("unrelated file matched as companion", name)
		}
	}
	if title := recognizeSTRM("Arrival.2016.mkv.share.strm"); title.Title != "Arrival" || title.Year != "2016" {
		t.Fatal(title)
	}
}

func TestShareLibraryScrapeWritesMetadataAndArchives(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	card.ScrapeEnabled = true
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/movie/1" || r.Header.Get("Authorization") != "Bearer key" {
			t.Error("unexpected TMDB request", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		jsonResponse(w, 200, map[string]any{"id": 1, "title": "Movie", "release_date": "2024-01-01", "overview": "Share library metadata"})
	}))
	defer up.Close()
	if err := a.store.update(func(st *State) error {
		st.QuarkLibraryCards[0] = card
		st.Plugins = map[string]PluginConfig{"tmdb": {Enabled: true, APIKey: "key", APIURL: up.URL, ImageURL: up.URL, Language: "en-US"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	transport := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == strings.TrimPrefix(up.URL, "http://") {
			return http.DefaultTransport.RoundTrip(r)
		}
		return transport.RoundTrip(r)
	})
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	taskID := quarkScrapePrefix + card.ID
	w := request(t, h, "GET", "/api/strm-scrape/items?taskId="+taskID, nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/strm-scrape/match", map[string]any{"taskId": taskID, "path": "Movie.mkv.share.strm", "tmdb": 1, "kind": "movie", "scrape": true}, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	waitRevisionScrape(t, a)
	rootName := quarkLibraryRoot(card)
	body, err := os.ReadFile(filepath.Join(rootName, "Movie.mkv.share.nfo"))
	if err != nil || !strings.Contains(string(body), "Share library metadata") {
		t.Fatal("share metadata not written", string(body), err)
	}
	root, err := os.OpenRoot(rootName)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyQuarkLibraryOwner(root, a.store.snapshot().QuarkLibraryRuntime.Roots[card.ID]); err != nil {
		t.Fatal("scraping changed library ownership", err)
	}
	root.Close()
	w = request(t, h, "GET", "/api/archives/scrape", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"taskName":"Series"`) || !strings.Contains(w.Body.String(), `"status":"success"`) {
		t.Fatal("share scrape execution missing from archives", w.Code, w.Body.String())
	}
}
