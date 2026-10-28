package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type quarkLibraryMock struct {
	files     []shareEntry
	invalid   string
	fail      bool
	saves     int
	deletes   int
	folders   map[string][]File
	submitted bool
	unknown   bool
}

func mockQuarkLibrary(t *testing.T, a *App) *quarkLibraryMock {
	t.Helper()
	m := &quarkLibraryMock{files: []shareEntry{{ID: "video", Name: "Movie.mkv", Size: 10, Token: "file-secret"}}, folders: map[string][]File{}}
	s := Storage{ID: "q", Name: "夸克测试", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "__puus=secret-cookie"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = old })
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			if m.fail {
				return nil, errors.New("network-private-message")
			}
			if m.invalid != "" {
				body = `{"code":` + m.invalid + `}`
			} else {
				body = `{"code":0,"data":{"stoken":"token-secret","title":"Share title"}}`
			}
		case strings.HasSuffix(r.URL.Path, "/detail"):
			files := []map[string]any{}
			if r.URL.Query().Get("pdir_fid") == "0" {
				files = append(files, map[string]any{"fid": "root-folder", "file_name": "Series", "dir": true})
			} else if r.URL.Query().Get("pdir_fid") == "root-folder" {
				for _, f := range m.files {
					files = append(files, map[string]any{"fid": f.ID, "file_name": f.Name, "size": f.Size, "share_fid_token": f.Token})
				}
			}
			b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": files}, "metadata": map[string]any{"_total": len(files)}})
			body = string(b)
		case strings.HasSuffix(r.URL.Path, "/sort"):
			parent := r.URL.Query().Get("pdir_fid")
			items := []map[string]any{}
			for _, f := range m.folders[parent] {
				items = append(items, map[string]any{"fid": f.ID, "file_name": f.Name, "size": f.Size, "dir": f.IsDir, "file_type": 1})
			}
			b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": items}})
			body = string(b)
		case r.URL.Path == "/1/clouddrive/file":
			var in struct {
				Parent string `json:"pdir_fid"`
				Name   string `json:"file_name"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			fid := "folder-" + in.Name
			m.folders[in.Parent] = append(m.folders[in.Parent], File{ID: fid, Name: in.Name, IsDir: true})
			body = `{"code":0,"data":{"fid":"` + fid + `"}}`
		case strings.HasSuffix(r.URL.Path, "/save"):
			m.saves++
			var in struct {
				Parent string   `json:"to_pdir_fid"`
				Source string   `json:"pdir_fid"`
				IDs    []string `json:"fid_list"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.Source != "root-folder" || len(in.IDs) != 1 {
				t.Fatal("share parent lost", in)
			}
			if m.unknown {
				return nil, io.ErrUnexpectedEOF
			}
			for _, f := range m.files {
				if f.ID == in.IDs[0] {
					m.folders[in.Parent] = []File{{ID: "copied-" + f.ID, Name: f.Name, Size: f.Size}}
				}
			}
			body = `{"code":0,"data":{"task_id":"job"}}`
		case strings.HasSuffix(r.URL.Path, "/task"):
			body = `{"code":0,"data":{"status":2}}`
		case strings.HasSuffix(r.URL.Path, "/download"):
			body = `{"code":0,"data":[{"download_url":"https://download.test/media"}]}`
		case strings.HasSuffix(r.URL.Path, "/delete"):
			m.deletes++
			var in struct {
				IDs []string `json:"filelist"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			for parent, files := range m.folders {
				if len(files) == 1 && len(in.IDs) == 1 && files[0].ID == in.IDs[0] {
					m.folders[parent] = nil
				}
			}
			body = `{"code":0}`
		case r.URL.Host == "download.test":
			header := http.Header{"Content-Length": {"10"}}
			return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader("0123456789")), Request: r}, nil
		case r.URL.Host == "open-api-drive.quark.cn":
			body = `{"status":200,"data":{"video_info":[{"url":"https://cdn.test/movie.mp4","accessable":1,"resolution":"super","format":"mp4"}]}}`
		default:
			t.Fatal("unexpected request", r.URL.Host, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	return m
}

func newQuarkLibraryCard(t *testing.T, a *App, media string) quarkLibraryCard {
	t.Helper()
	card := quarkLibraryCard{ID: "card", Secret: "https://pan.quark.cn/s/sample?pwd=aB12", Name: "Series", ShareName: "Series",
		Directory: t.TempDir(), DirectoryName: "Series", MediaExtensions: media, Enabled: true}
	if err := a.store.update(func(st *State) error { st.QuarkLibraryCards = []quarkLibraryCard{card}; return nil }); err != nil {
		t.Fatal(err)
	}
	return card
}

func TestQuarkLibraryGenerateIncrementalAndCustomExtensions(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv;pdf")
	m.files = append(m.files, shareEntry{ID: "comic", Name: "Comic.pdf", Size: 30, Token: "file-token"}, shareEntry{ID: "skip", Name: "ignore.txt", Size: 20, Token: "file-token"})
	result, err := a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Generated != 2 || result.Metadata != 0 || m.saves != 0 {
		t.Fatal(result, err, m.saves)
	}
	for _, name := range []string{"Movie.mkv.strm", "Comic.pdf.strm"} {
		content, err := os.ReadFile(filepath.Join(card.Directory, "Series", name))
		if err != nil || !strings.Contains(string(content), "/quark-share/sample-aB12/"+strings.TrimSuffix(name, ".strm")) || strings.Contains(string(content), "secret-cookie") {
			t.Fatal(name, err)
		}
	}
	m.files = append(m.files, shareEntry{ID: "next", Name: "Next.mkv", Size: 10, Token: "file-token"})
	result, err = a.runQuarkLibrary(context.Background(), card)
	if err != nil || result.Generated != 1 || result.Skipped != 2 || len(a.store.snapshot().QuarkLibraryRuntime.Files) != 3 {
		t.Fatal(result, err)
	}
}

func TestQuarkLibraryMigrationOnlyOwnedAndUnchangedFiles(t *testing.T) {
	for _, scenario := range []string{"commit", "rollback", "conflict", "modified", "overlap"} {
		t.Run(scenario, func(t *testing.T) {
			a := testApp(t)
			mockQuarkLibrary(t, a)
			old := newQuarkLibraryCard(t, a, "mkv")
			if _, err := a.runQuarkLibrary(context.Background(), old); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(quarkLibraryRoot(old), "Movie.mkv.strm"))
			os.WriteFile(filepath.Join(quarkLibraryRoot(old), "user.txt"), []byte("keep user data"), 0644)
			next := old
			next.Directory, next.DirectoryName = t.TempDir(), "Renamed"
			switch scenario {
			case "conflict":
				os.MkdirAll(quarkLibraryRoot(next), 0755)
				os.WriteFile(filepath.Join(quarkLibraryRoot(next), "Movie.mkv.strm"), []byte("user-file"), 0644)
			case "modified":
				os.WriteFile(filepath.Join(quarkLibraryRoot(old), "Movie.mkv.strm"), []byte("user-change"), 0644)
			case "overlap":
				next.Directory = quarkLibraryRoot(old)
			}
			finish, err := a.prepareQuarkLibraryMove(context.Background(), old, next, a.store.snapshot())
			if scenario == "conflict" || scenario == "modified" || scenario == "overlap" {
				if err == nil {
					t.Fatal("unsafe migration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			finish(scenario == "commit")
			keep, gone := old, next
			if scenario == "commit" {
				keep, gone = next, old
			}
			content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(keep), "Movie.mkv.strm"))
			if err != nil || string(content) != string(before) {
				t.Fatal("migration content lost", err)
			}
			if _, err := os.Stat(filepath.Join(quarkLibraryRoot(gone), "Movie.mkv.strm")); !os.IsNotExist(err) {
				t.Fatal("old/rolled back copy retained")
			}
			if content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(old), "user.txt")); err != nil || string(content) != "keep user data" {
				t.Fatal("user file changed", err)
			}
		})
	}
}

func TestQuarkLibraryInvalidityDoesNotConfuseNetworkOrTokenFailure(t *testing.T) {
	for _, code := range []string{"41006", "41007", "41008", "31001", "network"} {
		t.Run(code, func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			a.store.update(func(st *State) error {
				initQuarkLibraryRuntime(st)
				st.QuarkLibraryRuntime.Results[card.ID] = quarkLibraryResult{InvalidSince: time.Now().Add(-16 * 24 * time.Hour)}
				return nil
			})
			if code == "network" {
				m.fail = true
			} else {
				m.invalid = code
			}
			result, err := a.runQuarkLibrary(context.Background(), card)
			gone := code == "41006" || code == "41007"
			if err == nil || (result.Status == "invalid") != gone || (len(a.store.snapshot().QuarkLibraryCards) == 0) != gone || m.saves != 0 {
				t.Fatal(result, err, len(a.store.snapshot().QuarkLibraryCards))
			}
		})
	}
}

func TestQuarkLibraryPlaybackAndIdleCleanup(t *testing.T) {
	for _, mode := range []string{"relay", "tv", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			if _, err := a.runQuarkLibrary(context.Background(), card); err != nil {
				t.Fatal(err)
			}
			st := a.store.snapshot()
			var grant quarkLibraryFile
			for _, f := range st.QuarkLibraryRuntime.Files {
				grant = f
			}
			if mode == "tv" {
				b := quarkTVDefaults(QuarkTVBinding{Enabled: true, AccessToken: "private-access", Device: "device"})
				b.CookieHash = quarkCookieHash(st.Storages[0])
				a.store.update(func(st *State) error { st.QuarkTV = map[string]QuarkTVBinding{"q": b}; return nil })
			}
			m.unknown = mode == "unknown"
			h := a.Handler(t.TempDir())
			address := "/q/" + grant.Key + "/Movie.mkv"
			w := request(t, h, "HEAD", address, nil, nil)
			if w.Code != 200 || m.saves != 0 {
				t.Fatal("HEAD performed a transfer", w.Code, m.saves)
			}
			for range 2 {
				w = request(t, h, "GET", address, nil, nil)
				want := 200
				if mode == "tv" {
					want = 302
				} else if mode == "unknown" {
					want = 502
				}
				if w.Code != want {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			if m.saves != 1 {
				t.Fatal("transfer was replayed", m.saves)
			}
			if mode == "unknown" {
				a.cleanupQuarkLibraryTemporary(time.Now().Add(13 * time.Hour))
				request(t, h, "GET", address, nil, nil)
				if m.saves != 1 || len(a.store.snapshot().QuarkLibraryRuntime.Temporary) != 1 {
					t.Fatal("cleanup discarded an unknown write intent")
				}
				return
			}
			a.cleanupQuarkLibraryTemporary(time.Now().Add(11 * time.Hour))
			if m.deletes != 0 {
				t.Fatal("temporary file expired early")
			}
			a.cleanupQuarkLibraryTemporary(time.Now().Add(13 * time.Hour))
			if m.deletes != 1 || len(a.store.snapshot().QuarkLibraryRuntime.Temporary) != 0 {
				t.Fatal("idle file not cleaned", m.deletes)
			}
		})
	}
}

func TestQuarkLibraryAPIAndPersistence(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	if err := a.store.initModules(); err != nil {
		t.Fatal(err)
	}
	if err := a.store.initRuntime(filepath.Join(a.dataDir, "runtime")); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	body := map[string]any{"url": "https://pan.quark.cn/s/sample?pwd=aB12", "directory": t.TempDir(), "enabled": true, "mediaExtensions": "mkv", "cron": "0 */6 * * *"}
	w := request(t, h, "POST", "/api/virtual-library/quark", body, cookie)
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	st := a.store.snapshot()
	if len(st.QuarkLibraryCards) != 1 || st.QuarkLibraryCards[0].Name != "Series" || st.QuarkLibraryCards[0].DirectoryName != "Series" {
		t.Fatal("defaults missing", st.QuarkLibraryCards)
	}
	if result := waitQuarkLibrary(t, a, st.QuarkLibraryCards[0].ID); result.Status != "completed" {
		t.Fatal(result)
	}
	raw, _ := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	if strings.Contains(string(raw), "aB12") || strings.Contains(string(raw), "pan.quark.cn") || strings.Contains(string(raw), "sourceParent") {
		t.Fatal("share credentials/runtime leaked to configuration")
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initModules()
	}
	if err == nil {
		err = reopened.initRuntime(filepath.Join(a.dataDir, "runtime"))
	}
	if err != nil || len(reopened.snapshot().QuarkLibraryRuntime.Files) != 1 || reopened.snapshot().QuarkLibraryCards[0].Secret != body["url"] {
		t.Fatal("persistence failed", err)
	}
	w = request(t, h, "GET", "/api/virtual-library/quark", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"url"`) || strings.Contains(w.Body.String(), `"secret"`) || strings.Contains(w.Body.String(), "token-secret") {
		t.Fatal("API fields invalid", w.Body.String())
	}
	w = request(t, h, "GET", "/api/virtual-library/quark", nil, nil)
	if w.Code != 401 {
		t.Fatal("unprotected administration", w.Code)
	}
	for _, value := range []string{"mkv;中文", "mkv;bad/ext", "mkv;bad space"} {
		body["mediaExtensions"] = value
		w = request(t, h, "POST", "/api/virtual-library/quark", body, cookie)
		if w.Code != 400 {
			t.Fatal("invalid extensions accepted", value, w.Code)
		}
	}
	for _, log := range a.store.snapshot().Logs {
		if strings.Contains(log.Message, "aB12") || strings.Contains(log.Message, "secret") {
			t.Fatal("credentials in log")
		}
	}
}

func TestQuarkLibraryMetadataIntentSurvivesFailedDownloadAndUnknownSubmit(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(fmtBool(unknown), func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			card.MetadataExtensions = "nfo"
			if err := a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil }); err != nil {
				t.Fatal(err)
			}
			m.files = []shareEntry{{ID: "meta", Name: "movie.nfo", Size: 10, Token: "file-token"}}
			m.unknown = unknown
			for range 2 {
				result, err := a.runQuarkLibrary(context.Background(), card)
				if unknown && err == nil || !unknown && err != nil {
					t.Fatal(result, err)
				}
			}
			if m.saves != 1 {
				t.Fatal("metadata submission replayed", m.saves)
			}
			if !unknown {
				content, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "movie.nfo"))
				if err != nil || string(content) != "0123456789" {
					t.Fatal("metadata not downloaded", err)
				}
			}
		})
	}
}

func TestQuarkLibraryCleanupProtectsActiveAndChangedFiles(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmtBool(changed), func(t *testing.T) {
			a := testApp(t)
			m := mockQuarkLibrary(t, a)
			card := newQuarkLibraryCard(t, a, "mkv")
			a.runQuarkLibrary(context.Background(), card)
			st := a.store.snapshot()
			var grant quarkLibraryFile
			for _, f := range st.QuarkLibraryRuntime.Files {
				grant = f
			}
			s := st.Storages[0]
			record, _, err := a.quarkLibraryTransfer(context.Background(), card, grant, &s, "token-secret", m.files[0])
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				m.folders[record.Folder][0].Size++
			} else {
				a.quarkPlaybackActive = map[string]int{record.Key: 1}
			}
			a.cleanupQuarkLibraryTemporary(time.Now().Add(13 * time.Hour))
			if m.deletes != 0 || len(a.store.snapshot().QuarkLibraryRuntime.Temporary) != 1 {
				t.Fatal("active/changed resource deleted")
			}
		})
	}
}

func TestQuarkLibraryCronAndCardOverrides(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "pdf")
	card.Cron = "0 */6 * * *"
	cfg := effectiveQuarkLibrary(a.store.snapshot())
	cfg.MediaExtensions = "mkv"
	m.files = append(m.files, shareEntry{ID: "comic", Name: "Comic.pdf", Size: 10, Token: "file-token"})
	if err := a.store.update(func(st *State) error {
		st.QuarkLibraryCards[0] = card
		st.QuarkLibrary = &cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.quarkLibraryTick(time.Now())
	st := a.store.snapshot()
	if result := st.QuarkLibraryRuntime.Results[card.ID]; result.Generated != 1 || !result.NextRun.After(time.Now()) {
		t.Fatal("cron/default override failed", result)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "Movie.mkv.strm")); !os.IsNotExist(err) {
		t.Fatal("global media extensions overrode card")
	}
}
