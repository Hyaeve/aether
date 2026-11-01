package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type mobileLibraryMock struct {
	list        map[string][]map[string]any
	calls       []string
	downloads   int
	fail        string
	onRead      func()
	onDownload  func()
	rejectAuth  string
	finalCursor any
}

func mockMobileLibrary(t *testing.T, a *App) (*mobileLibraryMock, quarkLibraryCard) {
	t.Helper()
	s := Storage{ID: "mobile", Type: "mobile", Name: "移动测试", Enabled: true, Config: map[string]string{
		"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:library-account:library-token")),
	}}
	card := quarkLibraryCard{ID: "mobile-card", Secret: "https://yun.139.com/shareweb/CodeAb?pwd=Ab12",
		Name: "移动影视", ShareName: "移动影视", Directory: t.TempDir(), DirectoryName: "影视", Enabled: true,
		CascadeCleanup: "off", MediaExtensions: "mkv;mp4", MetadataExtensions: "nfo"}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.MobileLibraryCards = []quarkLibraryCard{card}
		initMobileLibrary(st)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m := &mobileLibraryMock{list: map[string][]map[string]any{"root": {
		{"coID": "video", "coName": "剧集 [A] #1.mkv", "coPath": "root/video", "coSize": 10},
	}}}
	shareMock(t, func(r *http.Request) string {
		m.calls = append(m.calls, r.URL.Path)
		if m.onRead != nil {
			fn := m.onRead
			m.onRead = nil
			fn()
		}
		if m.fail != "" || m.rejectAuth != "" && r.Header.Get("Authorization") == m.rejectAuth {
			return `{"resultCode":"403","data":{}}`
		}
		method := filepath.Base(r.URL.Path)
		data := map[string]any{}
		switch method {
		case "getOutLinkGeneral":
			data["getOutLinkGeneralResp"] = map[string]any{"outLinkGeneral": []map[string]string{{"lkName": "移动影视", "passwd": "Ab12"}}}
		case "getOutLinkInfoV6":
			var in struct {
				Request struct {
					Parent string `json:"pCaID"`
					Begin  string `json:"bNum"`
					End    string `json:"eNum"`
					Pass   string `json:"passwd"`
				} `json:"getOutLinkInfoReq"`
			}
			if json.NewDecoder(r.Body).Decode(&in) != nil || in.Request.Pass != "Ab12" {
				t.Error("password or read payload missing")
			}
			start, _ := strconv.Atoi(in.Request.Begin)
			end, _ := strconv.Atoi(in.Request.End)
			if in.Request.Parent != "root" && !strings.HasPrefix(in.Request.Parent, "/") {
				t.Error("non-root catalog path must start with a slash")
			}
			items := m.list[strings.TrimPrefix(in.Request.Parent, "/")]
			files, dirs := []map[string]any{}, []map[string]any{}
			for _, entry := range items[min(max(start-1, 0), len(items)):min(end, len(items))] {
				if entry["caID"] != nil {
					dirs = append(dirs, entry)
				} else {
					files = append(files, entry)
				}
			}
			data["coLst"], data["caLst"] = files, dirs
			if end < len(items) {
				data["nextPageCursor"] = strconv.Itoa(end)
			} else if m.finalCursor != nil {
				data["nextPageCursor"] = m.finalCursor
			}
		case "dlFromOutLinkV3":
			m.downloads++
			if m.onDownload != nil {
				fn := m.onDownload
				m.onDownload = nil
				fn()
			}
			var in map[string]map[string]any
			if json.NewDecoder(r.Body).Decode(&in) != nil || in["commonAccountInfo"]["account"] != "library-account" {
				t.Error("download account not bound")
			}
			data["redrUrl"] = "https://mobile-library-cdn.test/file"
		default:
			t.Error("unexpected cloud operation", method)
		}
		raw, _ := json.Marshal(map[string]any{"resultCode": "0", "data": data})
		return string(raw)
	})
	wire := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "mobile-library-cdn.test" {
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("cloud credentials forwarded to CDN")
			}
			w := httptest.NewRecorder()
			http.ServeContent(w, r, "file", time.Time{}, strings.NewReader("0123456789"))
			return w.Result(), nil
		}
		return wire.RoundTrip(r)
	})
	return m, card
}

func mobileLibraryHandler(t *testing.T, a *App) (http.Handler, *http.Cookie) {
	t.Helper()
	h := a.Handler(t.TempDir())
	cookies := request(t, h, "POST", "/api/auth/setup", credentials{Username: "mobile-library", Password: "x"}, nil).Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("setup failed")
	}
	return h, cookies[0]
}

func TestMobileLibraryGenerateRawFilenameAndReadOnlyPlayback(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	result, err := a.runMobileLibrary(context.Background(), card)
	if err != nil || result.Generated != 1 || m.downloads != 0 {
		t.Fatal(result, err, m.downloads)
	}
	raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "剧集 [A] #1.mkv.share.strm"))
	want := "/139-share/CodeAb-Ab12/剧集 [A] %231.mkv\n"
	if err != nil || !strings.HasSuffix(string(raw), want) || strings.Contains(string(raw), "library-token") || strings.Contains(string(raw), "root/video") {
		t.Fatal("wrong portable body", string(raw), err)
	}
	noPass := card
	noPass.Secret = mobileLibrarySecret("CodeAb", "")
	if got := mobileLibraryURL(noPass, "剧集 [A] #1.mkv.share.strm", "http://example.test"); got != "http://example.test/139-share/CodeAb/剧集 [A] %231.mkv\n" {
		t.Fatal("empty password left a separator or changed the name", got)
	}
	h, _ := mobileLibraryHandler(t, a)
	before := len(m.calls)
	target := "/139-share/CodeAb-Ab12/" + url.PathEscape("剧集 [A] #1.mkv")
	if w := request(t, h, "HEAD", target, nil, nil); w.Code != 200 || len(m.calls) != before {
		t.Fatal("HEAD performed upstream operations", w.Code)
	}
	if w := request(t, h, "GET", "/139-share/CodeAb-Ab12/other.mkv", nil, nil); w.Code != 404 || len(m.calls) != before {
		t.Fatal("unregistered file reached provider", w.Code)
	}
	if w := request(t, h, "GET", target, nil, nil); w.Code != 302 || w.Header().Get("Location") != "https://mobile-library-cdn.test/file" || m.downloads != 1 {
		t.Fatal("direct link not resolved", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", target, nil, nil); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if next, err := a.runMobileLibrary(context.Background(), card); err != nil || next.Generated != 0 || next.Skipped != 1 || m.downloads != 1 {
		t.Fatal("incremental generation downloaded media", next, err)
	}
}

func TestMobileLibraryPaginationDirectoriesAndExclusions(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	m.finalCursor = "0"
	card.ExcludeDirs, card.ExcludeTypes = "排除目录", "mp4"
	m.list["root"] = []map[string]any{{"caID": "folder", "caName": "分享原目录", "caPath": "root/folder"}}
	m.list["root/folder"] = []map[string]any{{"caID": "skip", "caName": "排除目录", "caPath": "root/skip"}}
	for i := 0; i < 101; i++ {
		m.list["root/folder"] = append(m.list["root/folder"], map[string]any{"coID": fmt.Sprintf("v%d", i), "coName": fmt.Sprintf("影片%03d.mkv", i), "coPath": fmt.Sprintf("root/folder/v%d", i), "coSize": "10"})
	}
	m.list["root/folder"] = append(m.list["root/folder"], map[string]any{"coID": "skip-file", "coName": "skip.mp4", "coPath": "root/folder/skip-file", "coSize": 10})
	if err := a.store.update(func(st *State) error { st.MobileLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := a.runMobileLibrary(context.Background(), card)
	if err != nil || result.Generated != 101 || m.downloads != 0 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "影片100.mkv.share.strm")); err != nil {
		t.Fatal("last page omitted or root wrapped twice", err)
	}
	for _, grant := range a.store.snapshot().MobileLibraryRuntime.Files {
		if grant.SourceParent != "root/folder" || strings.Contains(grant.Name, "skip") {
			t.Fatal("manifest lost actual parent", grant)
		}
	}
}

func TestMobileLibraryRelayRangesAndMetadataUseShareDownloads(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	m.list["root"] = append(m.list["root"], map[string]any{"coID": "nfo", "coName": "movie.nfo", "coPath": "root/nfo", "coSize": 10})
	cfg := effectiveMobileLibrary(a.store.snapshot())
	cfg.PlaybackMode = "relay"
	a.store.update(func(st *State) error { st.MobileLibrary = &cfg; return nil })
	result, err := a.runMobileLibrary(context.Background(), card)
	if err != nil || result.Metadata != 1 || m.downloads != 1 {
		t.Fatal(result, err)
	}
	content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "movie.nfo"))
	if err != nil || string(content) != "0123456789" {
		t.Fatal("metadata body missing", string(content), err)
	}
	h, _ := mobileLibraryHandler(t, a)
	r := httptest.NewRequest("GET", "/139-share/CodeAb-Ab12/"+url.PathEscape("剧集 [A] #1.mkv"), nil)
	r.Header.Set("Range", "bytes=3-6")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "3456" || w.Header().Get("Content-Range") != "bytes 3-6/10" || m.downloads != 2 {
		t.Fatal("range proxy failed", w.Code, w.Body.String(), w.Header())
	}
	for _, call := range m.calls {
		if strings.Contains(call, "BatchOpr") || strings.Contains(call, "/file/") {
			t.Fatal("share playback wrote to cloud storage", call)
		}
	}
}

func TestMobileLibraryAmbiguousNamesAndRevocationFailClosed(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	m.list["root"] = []map[string]any{{"caID": "a", "caName": "A"}, {"caID": "b", "caName": "B"}}
	for _, p := range []string{"a", "b"} {
		m.list[p] = []map[string]any{{"coID": p + "-video", "coName": "same.mkv", "coPath": p + "/video", "coSize": 10}}
	}
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	h, cookie := mobileLibraryHandler(t, a)
	target := "/139-share/CodeAb-Ab12/same.mkv"
	before := len(m.calls)
	if w := request(t, h, "GET", target, nil, nil); w.Code != 409 || len(m.calls) != before {
		t.Fatal("ambiguous name was guessed", w.Code)
	}
	if w := request(t, h, "DELETE", "/api/virtual-library/mobile/"+card.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "GET", target, nil, nil); w.Code != 404 || len(m.calls) != before {
		t.Fatal("removed card still authorized playback", w.Code)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "A", "same.mkv.share.strm")); err != nil {
		t.Fatal("removing record deleted output", err)
	}
}

func TestMobileLibrarySyncOnlyRemovesUnchangedRegisteredOutputs(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	m.list["root"] = append(m.list["root"], map[string]any{"coID": "keep", "coName": "keep.mkv", "coPath": "root/keep", "coSize": 10})
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "keep.mkv.share.strm"), "user changed")
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "personal.txt"), "user file")
	m.list["root"] = nil
	result, err := a.runMobileLibrary(context.Background(), card)
	if err != nil || result.Removed != 1 {
		t.Fatal(result, err)
	}
	for _, name := range []string{"keep.mkv.share.strm", "personal.txt"} {
		if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), name)); err != nil {
			t.Fatal("unowned/changed file removed", name, err)
		}
	}
	h, _ := mobileLibraryHandler(t, a)
	if w := request(t, h, "GET", "/139-share/CodeAb-Ab12/keep.mkv", nil, nil); w.Code != 404 {
		t.Fatal("stale changed output still grants playback", w.Code)
	}
}

func TestMobileLibraryFailedReadAndChangedConfigDoNotPublishOrClean(t *testing.T) {
	for _, scenario := range []string{"read-failure", "config-changed", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			a := testApp(t)
			m, card := mockMobileLibrary(t, a)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "read-failure":
				m.fail = "403"
			case "config-changed":
				m.onRead = func() { a.store.update(func(st *State) error { st.MobileLibraryCards[0].Enabled = false; return nil }) }
			case "cancelled":
				cancel()
			}
			if _, err := a.runMobileLibrary(ctx, card); err == nil {
				t.Fatal("unsafe generation continued")
			}
			if _, err := os.Stat(quarkLibraryRoot(card)); !os.IsNotExist(err) {
				t.Fatal("output published before read/config validation", err)
			}
			if m.downloads != 0 {
				t.Fatal("media downloaded while generating")
			}
		})
	}
}

func TestMobileLibraryAPISettingsDraftsAndPersistence(t *testing.T) {
	a := testApp(t)
	_, _ = mockMobileLibrary(t, a)
	h, cookie := mobileLibraryHandler(t, a)
	if w := request(t, h, "GET", "/api/virtual-library/mobile", nil, nil); w.Code != 401 {
		t.Fatal("admin API not protected", w.Code)
	}
	cfg := effectiveMobileLibrary(a.store.snapshot())
	cfg.Directory = t.TempDir()
	if w := request(t, h, "PUT", "/api/virtual-library/mobile/settings", cfg, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := map[string]any{"url": "https://yun.139.com/shareweb/#/w/i/NewCode?pwd=Ab12", "name": "新分享", "directoryName": "新目录", "enabled": false}
	w := request(t, h, "POST", "/api/virtual-library/mobile", body, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := a.store.snapshot()
	saved := state.MobileLibraryCards[1]
	if saved.Secret != mobileLibrarySecret("NewCode", "Ab12") || saved.CascadeCleanup != "off" || saved.ScrapeEnabled {
		t.Fatal("canonical draft defaults lost", saved)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || len(reopened.snapshot().MobileLibraryCards) != 2 || effectiveMobileLibrary(reopened.snapshot()).PlaybackMode != "redirect" {
		t.Fatal("settings/cards did not persist", err)
	}
	raw, _ := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	if strings.Contains(string(raw), "NewCode") || strings.Contains(string(raw), "library-token") {
		t.Fatal("share password or storage authorization stored in clear text")
	}
	if w = request(t, h, "POST", "/api/virtual-library/mobile", body, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), `"field":"directoryName"`) {
		t.Fatal("duplicate share accepted", w.Code)
	}
	cfg.PlaybackMode = "hls"
	if w = request(t, h, "PUT", "/api/virtual-library/mobile/settings", cfg, cookie); w.Code != 400 {
		t.Fatal("unimplemented mode accepted", w.Code)
	}
}

func TestMobileLibraryBackgroundGenerationAndRestartRuntime(t *testing.T) {
	a := testApp(t)
	_, card := mockMobileLibrary(t, a)
	h, cookie := mobileLibraryHandler(t, a)
	w := request(t, h, "POST", "/api/virtual-library/mobile/"+card.ID+"/run", nil, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result := a.store.snapshot().MobileLibraryRuntime.Results[card.ID]
		if result.Status == "completed" {
			break
		}
		if result.Status == "failed" {
			t.Fatal(result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result := a.store.snapshot().MobileLibraryRuntime.Results[card.ID]; result.Status != "completed" {
		t.Fatal("background job did not complete", result)
	}
	if err := a.store.update(func(st *State) error {
		r := st.MobileLibraryRuntime.Results[card.ID]
		r.Status = "running"
		st.MobileLibraryRuntime.Results[card.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.initRuntime(filepath.Join(a.dataDir, "runtime")); err != nil {
		t.Fatal(err)
	}
	st := store.snapshot()
	if st.MobileLibraryRuntime.Results[card.ID].Status != "interrupted" || len(st.MobileLibraryRuntime.Files) != 1 || st.MobileLibraryRuntime.Roots[card.ID].Token == "" {
		t.Fatal("runtime grants/ownership/restart state lost")
	}
}

func TestMobileLibraryCronScrapeAndOutputIsolation(t *testing.T) {
	a := testApp(t)
	_, card := mockMobileLibrary(t, a)
	card.Cron, card.ScrapeEnabled = "0 */6 * * *", true
	a.store.update(func(st *State) error { st.MobileLibraryCards[0] = card; return nil })
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	taskID := mobileScrapePrefix + card.ID
	root, err := a.scrapeRoot(taskID)
	if err != nil || root != quarkLibraryRoot(card) || len(shareScrapeSources(a.store.snapshot())) != 1 || !a.scrapeSourceConfig(taskID).ShareOnly {
		t.Fatal("mobile share scraping not integrated", root, err)
	}
	unlock, err := a.lockShareScrape(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if a.mobileLibraryMu.TryLock() {
		a.mobileLibraryMu.Unlock()
		t.Fatal("generation and scraping use different locks")
	}
	unlock()
	st := a.store.snapshot()
	copy := card
	copy.ID, copy.Secret = "quark", "https://pan.quark.cn/s/quark"
	st.QuarkLibraryCards = []quarkLibraryCard{copy}
	if validateMobileLibrary(st) == nil || validateQuarkLibrary(st) == nil {
		t.Fatal("providers can overwrite each other's output")
	}
	if err := a.store.update(func(st *State) error {
		r := st.MobileLibraryRuntime.Results[card.ID]
		r.NextRun = time.Now().Add(-time.Minute)
		st.MobileLibraryRuntime.Results[card.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.mobileLibraryTick(time.Now())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		result := a.store.snapshot().MobileLibraryRuntime.Results[card.ID]
		if result.Status == "completed" && result.NextRun.After(time.Now()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("due cron did not run")
}

func TestMobileLibraryForeignMarkerAndBrokenPagingFailClosed(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	writeTest(t, filepath.Join(quarkLibraryRoot(card), quarkLibraryMarker), "foreign owner")
	if _, err := a.runMobileLibrary(context.Background(), card); err == nil {
		t.Fatal("foreign directory owner accepted")
	}
	if err := os.RemoveAll(quarkLibraryRoot(card)); err != nil {
		t.Fatal(err)
	}
	m.list["root"] = []map[string]any{{"coID": "duplicate", "coName": "A.mkv", "coSize": 10}, {"coID": "duplicate", "coName": "B.mkv", "coSize": 10}}
	if _, err := a.runMobileLibrary(context.Background(), card); err == nil {
		t.Fatal("duplicate source IDs published")
	}
	if _, err := os.Stat(quarkLibraryRoot(card)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed scan still touched output")
	}
}

func TestMobileLibraryResolveActivationAndSafeMigration(t *testing.T) {
	a := testApp(t)
	_, card := mockMobileLibrary(t, a)
	h, cookie := mobileLibraryHandler(t, a)
	w := request(t, h, "POST", "/api/virtual-library/mobile/resolve", map[string]string{"url": card.Secret}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "移动影视") {
		t.Fatal("share title did not resolve", w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/virtual-library/mobile/resolve", map[string]string{"url": card.Secret}, nil); w.Code != 401 {
		t.Fatal("resolve endpoint not protected")
	}
	body := map[string]any{"url": card.Secret, "name": card.Name, "directory": card.Directory, "directoryName": card.DirectoryName, "enabled": true}
	w = request(t, h, "PUT", "/api/virtual-library/mobile/"+card.ID, body, cookie)
	if w.Code != 202 {
		t.Fatal("enabled save was not accepted in background", w.Code, w.Body.String())
	}
	a.wg.Wait()
	if result := a.store.snapshot().MobileLibraryRuntime.Results[card.ID]; result.Status != "completed" {
		t.Fatal("saved card did not generate", result)
	}
	oldFile := filepath.Join(quarkLibraryRoot(card), "剧集 [A] #1.mkv.share.strm")
	newParent := t.TempDir()
	body["directory"], body["enabled"] = newParent, false
	target := filepath.Join(newParent, card.DirectoryName)
	writeTest(t, filepath.Join(target, quarkLibraryMarker), "foreign marker")
	if w := request(t, h, "PUT", "/api/virtual-library/mobile/"+card.ID, body, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), `"field":"directoryName"`) {
		t.Fatal("migration adopted foreign target", w.Code, w.Body.String())
	}
	if _, err := os.Stat(oldFile); err != nil {
		t.Fatal("rejected migration deleted original", err)
	}
	if err := os.Remove(filepath.Join(target, quarkLibraryMarker)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "PUT", "/api/virtual-library/mobile/"+card.ID, body, cookie); w.Code != 200 {
		t.Fatal("unchanged owned files did not migrate", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(target, filepath.Base(oldFile))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("unchanged old registered file remained", err)
	}
	body["enabled"] = true
	if w := request(t, h, "PUT", "/api/virtual-library/mobile/"+card.ID, body, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	a.wg.Wait()
	if result := a.store.snapshot().MobileLibraryRuntime.Results[card.ID]; result.Status != "completed" {
		t.Fatal("migrated directory could not recover ownership", result)
	}
}

func TestMobileLibraryPlaybackAccountFallbackAndLateRevocation(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	second := st.Storages[0]
	second.ID = "second-mobile"
	second.Config = maps.Clone(second.Config)
	second.Config["authorization"] = base64.StdEncoding.EncodeToString([]byte("pc:library-account:second-token"))
	cfg := effectiveMobileLibrary(st)
	cfg.StorageIDs = []string{st.Storages[0].ID, second.ID}
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, second)
		st.MobileLibrary = &cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.rejectAuth = "Basic " + st.Storages[0].Config["authorization"]
	h, _ := mobileLibraryHandler(t, a)
	target := "/139-share/CodeAb-Ab12/" + url.PathEscape("剧集 [A] #1.mkv")
	before := len(m.calls)
	if w := request(t, h, "GET", target, nil, nil); w.Code != 302 || m.downloads != 1 || len(m.calls)-before != 4 {
		t.Fatal("healthy bound account was not used after first account failed", w.Code, w.Body.String(), m.downloads, len(m.calls)-before)
	}
	m.onDownload = func() {
		if err := a.store.update(func(st *State) error { revokeMobileLibraryCard(st, card.ID); return nil }); err != nil {
			t.Error(err)
		}
	}
	if w := request(t, h, "GET", target, nil, nil); w.Code != 404 || w.Header().Get("Location") != "" {
		t.Fatal("revoked grant returned a late direct link", w.Code, w.Body.String())
	}
}

func TestMobileLibraryOwnerRecoveryAndReplacedRoot(t *testing.T) {
	a := testApp(t)
	_, card := mockMobileLibrary(t, a)
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	token := st.MobileLibraryRuntime.Roots[card.ID].Token
	marker := filepath.Join(quarkLibraryRoot(card), quarkLibraryMarker)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal("unchanged inventory did not restore marker", err)
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != token {
		t.Fatal("ownership changed during recovery", err)
	}
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.Rename(quarkLibraryRoot(card), quarkLibraryRoot(card)+"-moved"); err != nil {
		t.Fatal(err)
	}
	writeTest(t, marker, token)
	if err := a.checkMobileLibraryRoot(card, root); err == nil {
		t.Fatal("replaced root with a copied marker was accepted")
	}
}

func TestMobileLibraryConfigBackupSeparatesAndPreservesRuntime(t *testing.T) {
	a := testApp(t)
	_, card := mockMobileLibrary(t, a)
	if _, err := a.runMobileLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	h, cookie := mobileLibraryHandler(t, a)
	cfg := effectiveMobileLibrary(a.store.snapshot())
	if err := a.store.update(func(st *State) error { st.MobileLibrary = &cfg; return nil }); err != nil {
		t.Fatal(err)
	}
	res := request(t, h, "POST", "/api/config/backup", map[string]string{"password": "mobile-backup"}, cookie)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	raw, err := openBackup(res.Body.Bytes(), "mobile-backup")
	if err != nil {
		t.Fatal(err)
	}
	var bundle configBundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.State.MobileLibraryCards) != 1 || bundle.State.MobileLibrary == nil || len(bundle.State.MobileLibraryRuntime.Files) != 0 || len(bundle.State.MobileLibraryRuntime.Roots) != 0 {
		t.Fatal("configuration backup lost cards or exported local grants")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "mobile.aether")
	file.Write(res.Body.Bytes())
	form.WriteField("password", "mobile-backup")
	form.Close()
	req := httptest.NewRequest("POST", "/api/config/import", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := applyPendingConfig(a.store); err != nil {
		t.Fatal(err)
	}
	current := a.store.snapshot()
	if len(current.MobileLibraryRuntime.Files) != 1 || len(current.MobileLibraryRuntime.Roots) != 1 || len(current.MobileLibraryCards) != 1 {
		t.Fatal("configuration import erased existing playback grants")
	}
}

func TestMobileLibraryLegacyShortCronNeverExecutesStaleRuntime(t *testing.T) {
	a := testApp(t)
	m, card := mockMobileLibrary(t, a)
	card.Cron = "*/5 * * * *"
	if err := a.store.update(func(st *State) error {
		st.MobileLibraryCards[0] = card
		st.MobileLibraryRuntime.Results[card.ID] = quarkLibraryResult{Status: "idle", NextRun: time.Now().Add(-time.Minute)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.mobileLibraryTick(time.Now())
	a.wg.Wait()
	if len(m.calls) != 0 || a.store.snapshot().MobileLibraryRuntime.Results[card.ID].Status != "idle" {
		t.Fatal("legacy short Cron executed using stale nextRun")
	}
	if validateMobileLibrary(a.store.snapshot()) == nil || validateMobileLibrary(a.store.snapshot(), true) != nil {
		t.Fatal("legacy startup and new-save Cron validation diverged")
	}
}
