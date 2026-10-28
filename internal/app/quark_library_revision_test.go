package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitQuarkLibrary(t *testing.T, a *App, sid string) quarkLibraryResult {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		result := a.store.snapshotWithLogLimit(0).QuarkLibraryRuntime.Results[sid]
		if !result.LastRun.IsZero() && result.Status != "running" {
			return result
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("accepted background generation did not finish")
	return quarkLibraryResult{}
}

func TestQuarkLibraryRevisionBackgroundSurvivesRequestCancellation(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/api/virtual-library/quark/card/run", nil).WithContext(ctx)
	r.SetPathValue("id", card.ID)
	w := httptest.NewRecorder()
	a.quarkLibraryRunAPI(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("generation must be accepted independently of the HTTP context: %d %s", w.Code, w.Body.String())
	}
	if result := waitQuarkLibrary(t, a, card.ID); result.Status != "completed" || result.Generated != 1 {
		t.Fatal(result)
	}
}

func TestQuarkLibraryRevisionAcknowledgesBeforeCloudReadFinishes(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	next := apiClient.Transport
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return next.RoundTrip(r)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("POST", "/api/virtual-library/quark/card/run", nil).WithContext(ctx)
	r.SetPathValue("id", card.ID)
	w := httptest.NewRecorder()
	go func() { a.quarkLibraryRunAPI(w, r); close(returned) }()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		close(release)
		cancel()
		<-returned
		t.Fatal("HTTP acceptance waited for cloud generation")
	}
	if w.Code != http.StatusAccepted {
		close(release)
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("background cloud read did not start")
	}
	cancel()
	if result := a.store.snapshot().QuarkLibraryRuntime.Results[card.ID]; result.Status != "running" {
		close(release)
		t.Fatal("background state is not visible during generation", result)
	}
	second := httptest.NewRecorder()
	a.quarkLibraryRunAPI(second, r)
	if second.Code != http.StatusConflict {
		close(release)
		t.Fatal("overlapping execution was accepted", second.Code)
	}
	close(release)
	if result := waitQuarkLibrary(t, a, card.ID); result.Status != "completed" {
		t.Fatal("disconnect cancelled accepted execution", result)
	}
}

func TestQuarkLibraryRevisionPortableRoute(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "Movie.mkv.strm"))
	if err != nil || !strings.Contains(string(body), "/quark-share/sample-aB12/Movie.mkv") {
		t.Fatalf("share/path URL not generated: %q %v", body, err)
	}
	h := a.Handler(t.TempDir())
	for _, method := range []string{"HEAD", "GET"} {
		w := request(t, h, method, "/quark-share/sample-aB12/Movie.mkv", nil, nil)
		if w.Code != 200 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
	for _, address := range []string{"/quark-share/sample/Movie.mkv", "/quark-share/sample-wrong/Movie.mkv", "/quark-share/sample-aB12/elsewhere/Movie.mkv", "/quark-share/unknown/Movie.mkv"} {
		if w := request(t, h, "GET", address, nil, nil); w.Code != 404 {
			t.Fatal("unregistered path accepted", address, w.Code)
		}
	}
	if m.saves != 1 {
		t.Fatal("unregistered request triggered transfer", m.saves)
	}
}

func TestQuarkLibraryRevisionSnapshotAndCascade(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	root := quarkLibraryRoot(card)
	writeTest(t, filepath.Join(root, "Movie.nfo"), "scraped")
	writeTest(t, filepath.Join(root, "Movie-poster.jpg"), "poster")
	writeTest(t, filepath.Join(root, "abandoned", "poster.jpg"), "old scraper image")
	writeTest(t, filepath.Join(root, "user.txt"), "preserve")
	m.files = []shareEntry{{ID: "next", Name: "Next.mkv", Size: 10, Token: "file-token"}}
	result, err := a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Generated != 1 {
		t.Fatal(result, err)
	}
	for _, name := range []string{"Movie.mkv.strm", "Movie.nfo", "Movie-poster.jpg", "abandoned"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatal("stale output not reconciled", name, err)
		}
	}
	if body, err := os.ReadFile(filepath.Join(root, "user.txt")); err != nil || string(body) != "preserve" {
		t.Fatal("unrelated file changed", err)
	}
	if len(a.store.snapshot().QuarkLibraryRuntime.Files) != 1 {
		t.Fatal("stale playback grant retained")
	}
}

func TestQuarkLibraryRevisionExpiryRemovesGeneratedRoot(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "Movie.nfo"), "scraped")
	if err := a.store.update(func(st *State) error {
		st.QuarkLibraryRuntime.Results[card.ID] = quarkLibraryResult{InvalidSince: time.Now().Add(-16 * 24 * time.Hour)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.invalid = "41006"
	if _, err := a.runQuarkLibrary(context.Background(), card); err == nil {
		t.Fatal("invalid share not reported")
	}
	if _, err := os.Stat(quarkLibraryRoot(card)); !os.IsNotExist(err) {
		t.Fatal("expired card directory retained", err)
	}
	if len(a.store.snapshot().QuarkLibraryCards) != 0 {
		t.Fatal("expired card retained after confirmed cleanup")
	}
}

func TestQuarkLibraryRevisionPlaybackListRotationAndVisibility(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	b := quarkTVDefaults(QuarkTVBinding{Enabled: true, AccessToken: "access", Device: "device"})
	b.CookieHash = quarkCookieHash(a.store.snapshot().Storages[0])
	if err := a.store.update(func(st *State) error { st.QuarkTV = map[string]QuarkTVBinding{"q": b}; return nil }); err != nil {
		t.Fatal(err)
	}
	next := apiClient.Transport
	delayed, rotated := false, false
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/sort") && m.saves > 0 {
			if !rotated {
				rotated, delayed = true, true
				return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": {"__puus=rotated-list; Path=/; Secure"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"list":[]}}`)), Request: r}, nil
			}
			if !strings.Contains(r.Header.Get("Cookie"), "__puus=rotated-list") {
				return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
			}
		}
		return next.RoundTrip(r)
	})
	var grant quarkLibraryFile
	for _, file := range a.store.snapshot().QuarkLibraryRuntime.Files {
		grant = file
	}
	w := request(t, a.Handler(t.TempDir()), "GET", "/q/"+grant.Key+"/Movie.mkv", nil, nil)
	if w.Code != 302 || w.Header().Get("Location") != "https://cdn.test/movie.mp4" || !delayed || m.saves != 1 {
		t.Fatal("transfer/list/TV handoff failed", w.Code, w.Body.String(), m.saves)
	}
	st := a.store.snapshot()
	if st.QuarkTV["q"].CookieHash != quarkCookieHash(st.Storages[0]) {
		t.Fatal("trusted list Cookie rotation invalidated TV binding")
	}
}

func TestQuarkLibraryRevisionCleanupSafety(t *testing.T) {
	for _, mode := range []string{"cascade-off", "network", "modified", "marker", "expiry-user-file", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			if mode == "cascade-off" {
				card.CascadeCleanup = "off"
				if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
				t.Fatal(err)
			}
			root := quarkLibraryRoot(card)
			writeTest(t, filepath.Join(root, "Movie.nfo"), "scraped")
			writeTest(t, filepath.Join(root, "orphan", "poster.jpg"), "scraped")
			ctx := context.Background()
			switch mode {
			case "network":
				m.fail = true
			case "modified":
				writeTest(t, filepath.Join(root, "Movie.mkv.strm"), "user edited stream")
			case "marker":
				writeTest(t, filepath.Join(root, quarkLibraryMarker), "wrong-owner")
			case "expiry-user-file":
				writeTest(t, filepath.Join(root, "user.txt"), "unrelated")
				m.invalid = "41007"
				if err := a.store.update(func(st *State) error {
					st.QuarkLibraryRuntime.Results[card.ID] = quarkLibraryResult{InvalidSince: time.Now().Add(-16 * 24 * time.Hour)}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			m.files = []shareEntry{{ID: "next", Name: "Next.mkv", Size: 10, Token: "file-token"}}
			_, err := a.runQuarkLibrary(ctx, card)
			success := mode == "cascade-off" || mode == "modified"
			if (err == nil) != success {
				t.Fatal(mode, err)
			}
			for _, name := range []string{"Movie.nfo", "orphan/poster.jpg"} {
				_, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
				if mode == "modified" && name == "orphan/poster.jpg" {
					if !os.IsNotExist(err) {
						t.Fatal("unrelated obsolete scraper directory retained", err)
					}
				} else if err != nil {
					t.Fatal("protected scraper file removed", mode, name, err)
				}
			}
			_, existsErr := os.Stat(filepath.Join(root, "Movie.mkv.strm"))
			if mode == "cascade-off" && !os.IsNotExist(existsErr) || mode != "cascade-off" && existsErr != nil {
				t.Fatal("incorrect STRM preservation", mode, existsErr)
			}
			if mode == "expiry-user-file" && len(a.store.snapshot().QuarkLibraryCards) != 1 {
				t.Fatal("card removed despite blocked expiry cleanup")
			}
		})
	}
}

func TestQuarkLibraryRevisionDoesNotAdoptUnrelatedDirectory(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	root := quarkLibraryRoot(card)
	writeTest(t, filepath.Join(root, "Photos", "family.jpg"), "unrelated user photograph")
	if _, err := a.runQuarkLibrary(context.Background(), card); err == nil {
		t.Fatal("pre-existing unrelated directory was adopted for automatic cleanup")
	}
	if body, err := os.ReadFile(filepath.Join(root, "Photos", "family.jpg")); err != nil || string(body) != "unrelated user photograph" {
		t.Fatal("unrelated image deleted", err)
	}
}

func TestQuarkLibraryRevisionMissingSTRMStillCleansCompanions(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	root := quarkLibraryRoot(card)
	if err := os.Remove(filepath.Join(root, "Movie.mkv.strm")); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(root, "Movie.nfo"), "scraped")
	m.files = nil
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "Movie.nfo")); !os.IsNotExist(err) {
		t.Fatal("missing obsolete STRM left its companion behind", err)
	}
}

func TestQuarkLibraryRevisionCleanupRechecksContent(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	root, err := openStrmReplaceRoot(quarkLibraryRoot(card))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	checks := 0
	ctx := context.WithValue(context.Background(), quarkLibraryGuardKey{}, func() error {
		checks++
		if checks == 2 {
			writeTest(t, filepath.Join(quarkLibraryRoot(card), "Movie.mkv.strm"), "changed before delete")
		}
		return nil
	})
	if _, err := a.reconcileQuarkLibrary(ctx, card, root, map[string]bool{}, false); err == nil {
		t.Fatal("cleanup did not stop on content change")
	}
	if body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "Movie.mkv.strm")); err != nil || string(body) != "changed before delete" {
		t.Fatal("changed file removed", err)
	}
}

func TestQuarkLibraryRevisionNestedNamesAndAuthorization(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	next := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/detail") {
			var list []map[string]any
			switch r.URL.Query().Get("pdir_fid") {
			case "0":
				list = []map[string]any{{"fid": "root-folder", "file_name": "Series", "dir": true}}
			case "root-folder":
				list = []map[string]any{{"fid": "s1", "file_name": "第一 季", "dir": true}, {"fid": "s2", "file_name": "第二季", "dir": true}, {"fid": "images-only", "file_name": "无媒体目录", "dir": true}}
			case "s1", "s2":
				name := "第1集 #1.mkv"
				if r.URL.Query().Get("pdir_fid") == "s2" {
					name = "第2集 #2.mkv"
				}
				list = []map[string]any{{"fid": "episode-" + r.URL.Query().Get("pdir_fid"), "file_name": name, "size": 10, "share_fid_token": "file-token"}}
			case "images-only":
				list = []map[string]any{{"fid": "orphan-nfo", "file_name": "movie.nfo", "size": 10, "share_fid_token": "file-token"}}
			}
			b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": list}, "metadata": map[string]any{"_total": len(list)}})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
		}
		if strings.HasSuffix(r.URL.Path, "/save") {
			var in struct {
				Parent string   `json:"to_pdir_fid"`
				Source string   `json:"pdir_fid"`
				IDs    []string `json:"fid_list"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatal(err)
			}
			if len(in.IDs) != 1 || in.IDs[0] != "episode-"+in.Source || in.Source != "s1" && in.Source != "s2" {
				t.Fatal("filename-only lookup lost its registered source directory", in)
			}
			name := "第1集 #1.mkv"
			if in.Source == "s2" {
				name = "第2集 #2.mkv"
			}
			m.saves++
			m.folders[in.Parent] = []File{{ID: "copied-" + in.IDs[0], Name: name, Size: 10}}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"task_id":"job"}}`)), Request: r}, nil
		}
		return next.RoundTrip(r)
	})
	if result, err := a.runQuarkLibrary(context.Background(), card); err != nil || result.Generated != 2 || result.Metadata != 0 || m.saves != 0 {
		t.Fatal(result, err)
	}
	binding := quarkTVDefaults(QuarkTVBinding{Enabled: true, AccessToken: "access", Device: "device"})
	binding.CookieHash = quarkCookieHash(a.store.snapshot().Storages[0])
	if err := a.store.update(func(st *State) error { st.QuarkTV = map[string]QuarkTVBinding{"q": binding}; return nil }); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	items := []struct{ directory, name string }{{"第一 季", "第1集 #1.mkv"}, {"第二季", "第2集 #2.mkv"}}
	for _, item := range items {
		body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), item.directory, item.name+".strm"))
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(strings.TrimSpace(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/quark-share/sample-aB12/"+item.name {
			t.Fatal("generated URL contains the local directory", u.Path)
		}
		if w := request(t, h, "HEAD", u.EscapedPath(), nil, nil); w.Code != 200 {
			t.Fatal("filename-only URL cannot be resolved", w.Code, w.Body.String())
		}
		if w := request(t, h, "GET", u.EscapedPath(), nil, nil); w.Code != 302 || w.Header().Get("Location") != "https://cdn.test/movie.mp4" {
			t.Fatal("nested source did not transfer through the registered directory to TV", w.Code, w.Body.String())
		}
	}
	if m.saves != 2 {
		t.Fatal("nested files did not retain independent source identities", m.saves)
	}
	for _, item := range items {
		relative := item.directory + "/" + item.name + ".strm"
		legacy := strings.TrimRight(a.currentSTRMSettings().Address, "/") + "/quark-share/sample-aB12/" + url.PathEscape(item.directory) + "/" + url.PathEscape(item.name) + "\n"
		writeTest(t, filepath.Join(quarkLibraryRoot(card), filepath.FromSlash(relative)), legacy)
		sum := sha256.Sum256([]byte(legacy))
		if err := a.store.update(func(st *State) error {
			for key, file := range st.QuarkLibraryRuntime.Files {
				if file.CardID == card.ID && file.Relative == relative {
					file.Digest = hex.EncodeToString(sum[:])
					st.QuarkLibraryRuntime.Files[key] = file
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if w := request(t, h, "GET", "/quark-share/sample-aB12/"+url.PathEscape(item.directory)+"/"+url.PathEscape(item.name), nil, nil); w.Code != 404 {
			t.Fatal("directory-bearing URL was accepted", w.Code, w.Body.String())
		}
	}
	if result, err := a.runQuarkLibrary(context.Background(), card); err != nil || result.Generated != 2 || m.saves != 2 {
		t.Fatal("tracked previous-format pointers were not upgraded without retransferring media", result, err, m.saves)
	}
	for _, item := range items {
		body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), item.directory, item.name+".strm"))
		want := quarkLibraryURL(card, item.directory+"/"+item.name+".strm", a.currentSTRMSettings().Address)
		if err != nil || string(body) != want {
			t.Fatal("previous-format pointer retained a directory", string(body), err)
		}
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "无媒体目录")); !os.IsNotExist(err) {
		t.Fatal("metadata-only subtree was generated", err)
	}
}

func TestQuarkLibraryFilenameOnlyRejectsAmbiguousNames(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		for _, grant := range st.QuarkLibraryRuntime.Files {
			grant.Key = id()
			grant.Relative, grant.SourceID, grant.SourceParent = "Season2/Movie.mkv.strm", "another-video", "another-directory"
			st.QuarkLibraryRuntime.Files[grant.Key] = grant
			break
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	for _, method := range []string{"HEAD", "GET"} {
		w := request(t, h, method, "/quark-share/sample-aB12/Movie.mkv", nil, nil)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "同名") {
			t.Fatal("ambiguous name selected an arbitrary file", method, w.Code, w.Body.String())
		}
	}
	if m.saves != 0 {
		t.Fatal("ambiguous URL triggered cloud transfer", m.saves)
	}
}

func TestQuarkLibraryRevisionSharedCompanionAndCascadePersistence(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv;mp4")
	m.files = append(m.files, shareEntry{ID: "mp4", Name: "Movie.mp4", Size: 10, Token: "file-token"})
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(quarkLibraryRoot(card), "Movie.nfo"), "shared metadata")
	m.files = m.files[1:]
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "Movie.nfo")); err != nil || string(body) != "shared metadata" {
		t.Fatal("metadata still used by another media file was removed", err)
	}
	if err := a.store.initModules(); err != nil {
		t.Fatal(err)
	}
	if err := a.store.initRuntime(filepath.Join(a.dataDir, "runtime")); err != nil {
		t.Fatal(err)
	}
	card.CascadeCleanup = "off"
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initModules()
	}
	if err == nil {
		err = reopened.initRuntime(filepath.Join(a.dataDir, "runtime"))
	}
	if err != nil {
		t.Fatal(err)
	}
	st := reopened.snapshot()
	if st.QuarkLibraryCards[0].CascadeCleanup != "off" || st.QuarkLibraryRuntime.Roots[card.ID].Token == "" {
		t.Fatal("cascade preference or directory ownership did not persist")
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	body := map[string]any{"url": card.Secret, "name": card.Name, "directory": card.Directory, "directoryName": card.DirectoryName, "enabled": false}
	w := request(t, h, "PUT", "/api/virtual-library/quark/"+card.ID, body, cookie)
	if w.Code != 200 || a.store.snapshot().QuarkLibraryCards[0].CascadeCleanup != "off" {
		t.Fatal("old client silently re-enabled cascade cleanup", w.Code, w.Body.String())
	}
}

func TestQuarkLibraryRevisionCleanupRejectsReplacedDirectory(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	directory := quarkLibraryRoot(card)
	root, err := openStrmReplaceRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	checks := 0
	ctx := context.WithValue(context.Background(), quarkLibraryGuardKey{}, func() error {
		checks++
		if checks == 2 {
			if err := os.Rename(directory, directory+"-original"); err != nil {
				return err
			}
			writeTest(t, filepath.Join(directory, "user.txt"), "new unrelated directory")
			owner := a.store.snapshot().QuarkLibraryRuntime.Roots[card.ID]
			writeTest(t, filepath.Join(directory, quarkLibraryMarker), owner.Token)
		}
		return nil
	})
	if _, err := a.reconcileQuarkLibrary(ctx, card, root, map[string]bool{}, false); err == nil {
		t.Fatal("copied marker authorized a different directory identity")
	}
	for _, name := range []string{filepath.Join(directory, "user.txt"), filepath.Join(directory+"-original", "Movie.mkv.strm")} {
		if _, err := os.Stat(name); err != nil {
			t.Fatal("directory replacement caused deletion", name, err)
		}
	}
}
