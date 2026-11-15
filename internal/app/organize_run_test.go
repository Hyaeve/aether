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
	"sync"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func organizeFixture(t *testing.T) (*App, Storage, organizeTaskDefinition, http.Handler, *http.Cookie) {
	t.Helper()
	a := testApp(t)
	s := addLocal(t, a)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/search/movie":
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 1, "title": "Film", "release_date": "2021-01-01"}}})
		case "/3/search/tv":
			jsonResponse(w, 200, map[string]any{"results": []any{map[string]any{"id": 2, "name": "Show", "first_air_date": "2022-01-01"}}})
		case "/3/movie/1":
			jsonResponse(w, 200, map[string]any{"id": 1, "title": "Film", "original_title": "Film", "release_date": "2021-01-01"})
		case "/3/tv/2":
			jsonResponse(w, 200, map[string]any{"id": 2, "name": "Show", "original_name": "Show", "first_air_date": "2022-01-01"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)
	for _, p := range []string{"in", "out", "redundant"} {
		if err := os.MkdirAll(filepath.Join(s.Config["root"], p), 0755); err != nil {
			t.Fatal(err)
		}
	}
	task := organizeTaskDefinition{ID: "organizer", Name: "归档测试", StorageID: s.ID, Source: "/in", SourcePath: "/in", TargetStorageID: s.ID, Target: "/out", TargetPath: "/out", Method: "move", OptionsVersion: 1, ProcessConcurrency: 1, NamingRuleID: "default-naming", MediaCategoryRuleID: "default-categories", MetadataExtensions: "srt;ass;ssa"}
	if err := a.store.update(func(st *State) error {
		ensureRulePresets(st)
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["tmdb"] = PluginConfig{Enabled: true, APIURL: up.URL, APIKey: "key", Language: "zh-CN"}
		st.OrganizeTasks = []organizeTaskDefinition{task}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	return a, s, task, h, cookie
}
func waitOrganize(t *testing.T, a *App, key string) organizeRunState {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		a.runMu.Lock()
		_, active := a.running[organizeRunKey(key)]
		a.runMu.Unlock()
		run := a.store.snapshotWithLogLimit(0).OrganizeRuntime[key]
		if !active && run.Status != "" && !organizeActive(run.Status) {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("organize did not finish")
	return organizeRunState{}
}
func TestOrganizeExecutionLocalModesAndSubtitles(t *testing.T) {
	for _, method := range []string{"move", "copy", "hardlink", "symlink"} {
		t.Run(method, func(t *testing.T) {
			a, s, task, h, cookie := organizeFixture(t)
			task.Method = method
			if err := a.store.update(func(st *State) error { st.OrganizeTasks[0] = task; return nil }); err != nil {
				t.Fatal(err)
			}
			writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.mkv"), "media")
			writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.zh.srt"), "subtitle")
			if w := request(t, h, "POST", "/api/organize-tasks/organizer/run", nil, nil); w.Code != 401 {
				t.Fatal(w.Code)
			}
			w := request(t, h, "POST", "/api/organize-tasks/organizer/run", nil, cookie)
			if w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
			run := waitOrganize(t, a, task.ID)
			if run.Status != "completed" || run.Succeeded != 1 || run.Done != 1 || run.Total != 1 {
				t.Fatal(run)
			}
			var media, subtitle string
			err := filepath.WalkDir(filepath.Join(s.Config["root"], "out"), func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if strings.HasSuffix(p, ".mkv") {
					media = p
				}
				if strings.HasSuffix(p, ".srt") {
					subtitle = p
				}
				return nil
			})
			if err != nil || media == "" || subtitle == "" {
				t.Fatal(media, subtitle, err)
			}
			body, err := os.ReadFile(media)
			if err != nil || string(body) != "media" {
				t.Fatal(err)
			}
			if strings.TrimSuffix(filepath.Base(subtitle), ".zh.srt") != strings.TrimSuffix(filepath.Base(media), ".mkv") {
				t.Fatal("subtitle did not follow renamed media", subtitle, media)
			}
			_, err = os.Stat(filepath.Join(s.Config["root"], "in", "Film.2021.mkv"))
			if (method == "move") != os.IsNotExist(err) {
				t.Fatal("source retention", err)
			}
			if len(a.archives.records) != 1 || a.archives.records[0].Category != "organize" || a.archives.records[0].TMDB != 1 {
				t.Fatal(a.archives.records)
			}
			if method == "hardlink" {
				src, _ := os.Stat(filepath.Join(s.Config["root"], "in", "Film.2021.mkv"))
				dst, _ := os.Stat(media)
				if !os.SameFile(src, dst) {
					t.Fatal("not a hardlink")
				}
			}
			if method == "symlink" {
				info, _ := os.Lstat(media)
				if info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("not a symlink")
				}
			}
		})
	}
}
func TestOrganizeDuplicatesExclusionsAndNoWash(t *testing.T) {
	a, s, task, h, cookie := organizeFixture(t)
	task.FailureDirectory = "/redundant"
	task.FailurePath = "/redundant"
	if err := a.store.update(func(st *State) error { st.OrganizeTasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.mkv"), "same")
	if w := request(t, h, "POST", "/api/organize-tasks/organizer/run", nil, cookie); w.Code != 202 {
		t.Fatal(w.Code)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
		t.Fatal(run)
	}
	writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.mkv"), "same")
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Skipped != 1 || run.Failed != 0 {
		t.Fatal(run)
	}
	if body, err := os.ReadFile(filepath.Join(s.Config["root"], "redundant", "Film.2021.mkv")); err != nil || string(body) != "same" {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.mkv"), "different")
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
		t.Fatal(run)
	}
	count := 0
	filepath.WalkDir(filepath.Join(s.Config["root"], "out"), func(p string, d os.DirEntry, err error) error {
		if strings.HasSuffix(p, ".mkv") {
			count++
		}
		return nil
	})
	if count != 2 {
		t.Fatal("no-wash lost a version", count)
	}
	task.ExcludeList = "/exclude;.mp4"
	if err := a.store.update(func(st *State) error { st.OrganizeTasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(s.Config["root"], "in", "exclude", "Film.2021.mkv"), "keep")
	writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.mp4"), "keep")
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Total != 0 {
		t.Fatal(run)
	}
}
func TestOrganizeBackgroundStopAndScopeProtection(t *testing.T) {
	a, s, task, h, cookie := organizeFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-release:
		case <-r.Context().Done():
		}
		jsonResponse(w, 200, map[string]any{"results": []any{}})
	}))
	defer up.Close()
	defer close(release)
	a.store.update(func(st *State) error { p := st.Plugins["tmdb"]; p.APIURL = up.URL; st.Plugins["tmdb"] = p; return nil })
	writeTest(t, filepath.Join(s.Config["root"], "in", "Film.2021.mkv"), "keep")
	if w := request(t, h, "POST", "/api/organize-tasks/organizer/run", nil, cookie); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("not started")
	}
	if w := request(t, h, "POST", "/api/organize-tasks/organizer/run", nil, cookie); w.Code != 409 {
		t.Fatal("duplicate start", w.Code)
	}
	if w := request(t, h, "PUT", "/api/organize-tasks/organizer", task, cookie); w.Code != 409 {
		t.Fatal("edited active task", w.Code)
	}
	if w := request(t, h, "DELETE", "/api/organize-tasks/organizer", nil, cookie); w.Code != 409 {
		t.Fatal("deleted active task", w.Code)
	}
	a.runMu.Lock()
	err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{storage: s, id: "/in/Film.2021.mkv"}})
	other := a.checkTaskFileNodes(context.Background(), []taskFileNode{{storage: s, id: "/elsewhere"}})
	a.runMu.Unlock()
	if err == nil || other != nil {
		t.Fatal("scope lock", err, other)
	}
	if w := request(t, h, "GET", "/api/tasks/panel", nil, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"organize"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/organize-tasks/organizer/stop", nil, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if run := waitOrganize(t, a, task.ID); run.Status != "stopped" {
		t.Fatal(run)
	}
	if raw, err := os.ReadFile(filepath.Join(s.Config["root"], "in", "Film.2021.mkv")); err != nil || string(raw) != "keep" {
		t.Fatal("cancelled task changed source", err)
	}
}
func TestOrganizeRestartAndRuntimeIsolation(t *testing.T) {
	a, _, task, _, _ := organizeFixture(t)
	if err := a.organizeUpdate(task.ID, func(run *organizeRunState) { run.Status = "running"; run.Done = 2 }); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if run := reopened.snapshotWithLogLimit(0).OrganizeRuntime[task.ID]; run.Status != "interrupted" || run.Done != 2 {
		t.Fatal(run)
	}
	raw, err := json.Marshal(a.store.snapshotWithLogLimit(0))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	json.Unmarshal(raw, &object)
	values := map[string]any{"state": object}
	stripRuntimeConfig(values)
	body, _ := json.Marshal(values)
	if strings.Contains(string(body), "organizeRuntime") {
		t.Fatal("execution state exported as configuration")
	}
}

func TestOrganizeWashPublishesBeforeRetiringExisting(t *testing.T) {
	a, s, task, _, _ := organizeFixture(t)
	task.FailureDirectory, task.UpgradeRuleID = "/redundant", "default-upgrade"
	if err := a.store.update(func(st *State) error { st.OrganizeTasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	root := s.Config["root"]
	writeTest(t, filepath.Join(root, "in", "Film.2021.720p.mkv"), "original")
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
		t.Fatal(run)
	}
	work := filepath.Join(root, "out", "电影", "Film (2021) -{tmdbid=1}")
	old, err := filepath.Glob(filepath.Join(work, "*.mkv"))
	if err != nil || len(old) != 1 {
		t.Fatal(old, err)
	}
	writeTest(t, filepath.Join(root, "in", "Film.2021.2160p.mkv"), "better release")
	writeTest(t, filepath.Join(root, "in", "Film.2021.2160p.zh.srt"), "subtitle")
	writeTest(t, filepath.Join(work, "Film - Film.2021.2160p.zh.srt"), "existing subtitle")
	if err = a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Failed != 1 {
		t.Fatal(run)
	}
	if raw, err := os.ReadFile(old[0]); err != nil || string(raw) != "original" {
		t.Fatal("previous version moved before new publication", err)
	}
	if err = os.Remove(filepath.Join(work, "Film - Film.2021.2160p.zh.srt")); err != nil {
		t.Fatal(err)
	}
	if err = a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
		t.Fatal(run)
	}
	if _, err = os.Stat(old[0]); !os.IsNotExist(err) {
		t.Fatal("old version still published", err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "redundant", filepath.Base(old[0]))); err != nil || string(raw) != "original" {
		t.Fatal("old version not preserved in redundant directory", err)
	}
}

func TestOrganizeCleanupAndBlacklistKeepUnrelatedEntries(t *testing.T) {
	a, s, task, _, _ := organizeFixture(t)
	task.MetadataExtensions = ""
	task.SmartCleanup = true
	task.ProcessConcurrency = 3
	task.ExcludeList = "/excluded"
	if err := a.store.update(func(st *State) error {
		st.OrganizeTasks[0] = task
		st.RecognitionRules = &recognitionSettings{Blacklist: []string{"/blocked"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root := s.Config["root"]
	writeTest(t, filepath.Join(root, "in", "release", "Film.2021.mkv"), "media")
	writeTest(t, filepath.Join(root, "in", "release", "Film.2021.zh.srt"), "subtitle")
	writeTest(t, filepath.Join(root, "in", "blocked", "notes.txt"), "blocked")
	writeTest(t, filepath.Join(root, "in", "excluded", "Film.2021.mkv"), "excluded")
	if err := os.Mkdir(filepath.Join(root, "in", "unrelated"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 || run.Skipped != 1 || run.Failed != 0 {
		t.Fatal(run)
	}
	for _, name := range []string{"release", "blocked"} {
		if _, err := os.Stat(filepath.Join(root, "in", name)); !os.IsNotExist(err) {
			t.Fatal(name, err)
		}
	}
	for _, name := range []string{"excluded", "unrelated"} {
		if _, err := os.Stat(filepath.Join(root, "in", name)); err != nil {
			t.Fatal(name, err)
		}
	}
	for _, pattern := range []string{"*/Film.2021.zh.srt", "*/blocked/notes.txt"} {
		matches, err := filepath.Glob(filepath.Join(root, ".aether-trash", pattern))
		if err != nil || len(matches) != 1 {
			t.Fatal(pattern, matches, err)
		}
	}
}

func TestOrganizeWebDAVMoveAndCopy(t *testing.T) {
	for _, method := range []string{"move", "copy"} {
		t.Run(method, func(t *testing.T) {
			a, s, task, _, _ := organizeFixture(t)
			root := s.Config["root"]
			remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
			defer remote.Close()
			task.Method = method
			if err := a.store.update(func(st *State) error {
				st.Storages[0].Type = "webdav"
				st.Storages[0].Config = map[string]string{"address": remote.URL, "root": "/", "deleteMode": "trash"}
				st.OrganizeTasks[0] = task
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			writeTest(t, filepath.Join(root, "in", "Film.2021.mkv"), "remote media")
			if err := a.startOrganize(task.ID); err != nil {
				t.Fatal(err)
			}
			if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
				t.Fatal(run, a.archives.records)
			}
			target := filepath.Join(root, "out", "电影", "Film (2021) -{tmdbid=1}", "Film - Film.2021.mkv")
			if raw, err := os.ReadFile(target); err != nil || string(raw) != "remote media" {
				t.Fatal(target, err)
			}
			if _, err := os.Stat(filepath.Join(root, "in", "Film.2021.mkv")); (method == "move") != os.IsNotExist(err) {
				t.Fatal("source retention", err)
			}
		})
	}
}

func TestOrganizeScrapeUsesRecognizedMediaAndPreservesExistingMetadata(t *testing.T) {
	a, s, task, _, _ := organizeFixture(t)
	task.TMDBScrape = true
	if err := a.store.update(func(st *State) error { st.OrganizeTasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	root := s.Config["root"]
	writeTest(t, filepath.Join(root, "in", "Film.2021.mkv"), "media")
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
		t.Fatal(run)
	}
	nfo := filepath.Join(root, "out", "电影", "Film (2021) -{tmdbid=1}", "Film - Film.2021.nfo")
	if raw, err := os.ReadFile(nfo); err != nil || !strings.Contains(string(raw), "<tmdbid>1</tmdbid>") {
		t.Fatal(string(raw), err)
	}
	writeTest(t, nfo, "existing metadata")
	writeTest(t, filepath.Join(root, "in", "Film.2021.1080p.mkv"), "second version")
	if err := a.startOrganize(task.ID); err != nil {
		t.Fatal(err)
	}
	if run := waitOrganize(t, a, task.ID); run.Succeeded != 1 {
		t.Fatal(run)
	}
	if raw, err := os.ReadFile(nfo); err != nil || string(raw) != "existing metadata" {
		t.Fatal(string(raw), err)
	}
}

func TestOrganizeSTRMProbeResolvesSignedAndLocalReferences(t *testing.T) {
	a, s, _, _, _ := organizeFixture(t)
	name := filepath.Join(s.Config["root"], "in", "Film.2021.mkv")
	writeTest(t, name, "actual media")
	runner := organizeRunner{a: a, st: a.store.snapshotWithLogLimit(0)}
	for _, ref := range []string{name, a.signedStreamURL(streamClaim{Storage: s.ID, File: "/in/Film.2021.mkv"})} {
		_, f, trusted, err := runner.resolveProbePointer(context.Background(), ref)
		if err != nil || f == nil || !trusted {
			t.Fatal(trusted, err)
		}
		raw, err := io.ReadAll(f)
		f.Close()
		if err != nil || string(raw) != "actual media" {
			t.Fatal(string(raw), err)
		}
	}
	if _, f, _, err := runner.resolveProbePointer(context.Background(), "http://127.0.0.1/stream/fake?sign=fake"); err == nil || f != nil {
		t.Fatal("accepted forged file grant")
	}
	if conn, err := organizeProbePublicDial(context.Background(), "tcp", "127.0.0.1:1"); conn != nil || err == nil || !strings.Contains(err.Error(), "内部网络") {
		t.Fatal("public STRM accessed loopback", err)
	}
}
