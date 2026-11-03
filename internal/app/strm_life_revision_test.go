package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCASExportEncodingSelection(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "media")
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	for _, format := range []string{"encoded", "plain", "invalid"} {
		w := request(t, h, "POST", "/api/files/cas-export", map[string]any{"storageId": s.ID, "source": "/", "ids": []string{"/film.mkv"}, "confirm": true, "format": format}, cookie)
		if format == "invalid" {
			if w.Code != 400 {
				t.Fatal("invalid format accepted", w.Code)
			}
			continue
		}
		if w.Code != 200 {
			t.Fatal(format, w.Code, w.Body.String())
		}
		raw := w.Body.Bytes()
		if format == "encoded" {
			var err error
			raw, err = base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(raw)))
			if err != nil {
				t.Fatal("not portable encoded CAS", err, w.Body.String())
			}
		}
		var info CASInfo
		if json.Unmarshal(raw, &info) != nil || validateCAS(info) != nil {
			t.Fatal("invalid exported CAS", string(raw))
		}
		if _, err := decodeCAS(w.Body.Bytes(), "film.mkv.cas"); err != nil {
			t.Fatal("cannot re-import", err)
		}
	}
}

func TestShareLibraryNamesManualRejectAndBotTimestamp(t *testing.T) {
	root := t.TempDir()
	first := quarkLibraryCard{ID: "first", Directory: root, DirectoryName: "剧集"}
	next := quarkLibraryCard{ID: "second", Directory: root, DirectoryName: "剧集"}
	st := State{QuarkLibraryCards: []quarkLibraryCard{first}}
	if err := shareLibraryDirectoryAvailable(st, next, quarkLibraryCard{}); err == nil {
		t.Fatal("manual duplicate accepted")
	}
	draft := quarkLibraryCard{ID: "draft", DirectoryName: "Draft"}
	if err := shareLibraryNamesAvailable(State{MobileLibraryCards: []quarkLibraryCard{{ID: "mobile-draft", DirectoryName: "Draft"}}}, draft); !errors.Is(err, errShareLibraryDirectory) {
		t.Fatal("late cross-provider draft collision accepted", err)
	}
	at := time.Date(2026, 10, 31, 22, 23, 0, 0, time.FixedZone("CST", 8*3600))
	got, err := telegramLibraryDirectory(st, next, at)
	if err != nil || got != "剧集（10／31 22：23）" {
		t.Fatal(got, err)
	}
	st.MobileLibraryCards = []quarkLibraryCard{{ID: "third", Directory: root, DirectoryName: got}}
	got, err = telegramLibraryDirectory(st, next, at)
	if err != nil || !strings.HasSuffix(got, "）-2") {
		t.Fatal(got, err)
	}
	if err := os.Mkdir(filepath.Join(root, "已有目录"), 0755); err != nil {
		t.Fatal(err)
	}
	next.DirectoryName = "已有目录"
	if shareLibraryDirectoryAvailable(st, next, quarkLibraryCard{}) == nil {
		t.Fatal("existing disk directory adopted")
	}
}

func TestSTRMLifeValidationScheduleAndCascadeOptions(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	task := Task{Kind: "strm", Name: "生活事件", StorageID: s.ID, Source: "10", SourceLabel: "Show", Mode: strmUpdate, STRMSource: "life", Enabled: true}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if delta := nextRun(task, now).Sub(now); delta != 5*time.Second {
		t.Fatal(delta)
	}
	task.STRMSource = "tree"
	if err := a.validateTask(&task); err != nil || task.STRMSource != "scan" || task.Mode != strmUpdate || !nextRun(task, now).IsZero() {
		t.Fatal("old tree task was not mapped to ordinary update", task, err)
	}
	task.STRMSource = "life"
	task.Cron = "*/15 * * * *"
	task.CascadeDelete = true
	task.Mode = strmMissing
	if err := a.validateTask(&task); err != nil || task.CascadeExtensions != defaultSTRMCascadeExtensions || task.Mode != strmUpdate {
		t.Fatal(task, err)
	}
	task.Cron = "*/14 * * * *"
	if a.validateTask(&task) == nil {
		t.Fatal("short cron accepted")
	}
	task.Cron = ""
	task.STRMSource = "life"
	task.CascadeExtensions = "strm"
	if a.validateTask(&task) == nil {
		t.Fatal("pointer cascade accepted")
	}
}

func TestSTRMLifeAPIDefaultActivationAndSyncConfirmation(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	input := Task{Name: "115事件", Kind: "strm", StorageID: s.ID, Source: "10", SourceLabel: "Show", Target: t.TempDir(), Enabled: true}
	before := time.Now()
	w := request(t, h, "POST", "/api/tasks", input, cookie)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	task := a.store.snapshot().Tasks[0]
	if task.STRMSource != "life" || task.STRMLifeSince.Before(before) || task.NextRun.IsZero() {
		t.Fatal("native115 new task did not activate life monitor", task)
	}
	started := task.STRMLifeSince
	task.STRMLifeSince = time.Unix(1, 0)
	w = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
	if w.Code != 200 || !a.store.snapshot().Tasks[0].STRMLifeSince.Equal(started) {
		t.Fatal("ordinary client save rewound activation", w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/tasks/"+task.ID+"/sync", map[string]bool{"confirm": false}, cookie)
	if w.Code != 400 || len(a.archives.records) != 0 {
		t.Fatal("full sync started without explicit confirmation", w.Code)
	}
	task.STRMSource, task.Cron = "tree", ""
	w = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
	if w.Code != 200 || a.store.snapshot().Tasks[0].STRMSource != "scan" {
		t.Fatal("old tree task did not become a normal task", w.Code, w.Body.String())
	}
	task.Cron = "*/15 * * * *"
	w = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func Test115STRMExplicitScanModesPersistWithoutLifeScheduling(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, mode := range []string{strmMissing, strmUpdate} {
		input := Task{Name: mode, Kind: "strm", StorageID: s.ID, Source: "10", SourceLabel: "Show", Target: t.TempDir(), Enabled: true, STRMSource: "scan", Mode: mode}
		w := request(t, h, "POST", "/api/tasks", input, cookie)
		if w.Code != 201 {
			t.Fatal(w.Code, w.Body.String())
		}
		tasks := a.store.snapshot().Tasks
		task := tasks[len(tasks)-1]
		if task.Mode != mode || task.STRMSource != "scan" || !task.NextRun.IsZero() || !task.STRMLifeSince.IsZero() {
			t.Fatal("normal mode became a life monitor", task)
		}
		w = request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		saved := a.store.snapshot().Tasks[len(tasks)-1]
		if saved.Mode != mode || saved.STRMSource != "scan" || !saved.NextRun.IsZero() {
			t.Fatal("save changed normal strategy", saved)
		}
		other := saved
		other.Mode = strmUpdate
		if mode == strmMissing && strmTaskConfig(saved) == strmTaskConfig(other) {
			t.Fatal("mode change did not invalidate the running plan")
		}
	}
}

func Test115STRMScanMissingKeepsExistingWhileUpdateRefreshes(t *testing.T) {
	for _, mode := range []string{strmMissing, strmUpdate} {
		t.Run(mode, func(t *testing.T) {
			a, s, file := strm115Fixture(t)
			task := Task{ID: "native-scan", Name: mode, Kind: "strm", StorageID: s.ID, Source: "/", Target: t.TempDir(), STRMSource: "scan", Mode: mode, MediaExtensions: "mkv"}
			if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
				t.Fatal(err)
			}
			shareMock(t, func(r *http.Request) string {
				if r.Method != "GET" || r.URL.Path != "/files" {
					t.Fatal("normal scan used life events or modified source", r.Method, r.URL.Path)
				}
				return `{"state":true,"cid":"0","count":1,"path":[{"cid":"0","name":"根目录"}],"data":[{"fid":"file","cid":"0","n":"film.mkv","s":42,"pc":"` + file.PickCode + `"}]}`
			})
			output := filepath.Join(task.Target, "film.strm")
			writeTest(t, output, "existing link\n")
			n, err := a.executeTask(a.ctx, task, s)
			if err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if mode == strmMissing {
				if n != 0 || string(body) != "existing link\n" {
					t.Fatal("missing mode overwrote existing STRM", n, string(body))
				}
			} else if n != 1 || !strings.Contains(string(body), "/"+file.PickCode+"/film.mkv") {
				t.Fatal("update mode failed to refresh STRM", n, string(body))
			}
		})
	}
}

func TestSTRMCascadeKeepsSeriesArtUntilWholeSeriesGone(t *testing.T) {
	a, s, task := strmModeFixture(t)
	task.CascadeDelete = true
	task.CascadeExtensions = "jpg;nfo;srt;png"
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	for _, name := range []string{"Series/Season 1/E01.mkv", "Series/Season 1/E02.mkv"} {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), "video")
	}
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Series/tvshow.nfo", "Series/poster.jpg", "Series/season01-poster.jpg", "Series/Season 1/season.nfo", "Series/Season 1/E01.nfo", "Series/Season 1/E01.zh.srt", "Series/personal.txt"} {
		writeTest(t, filepath.Join(task.Target, filepath.FromSlash(name)), "art")
	}
	os.Remove(filepath.Join(s.Config["root"], "Series", "Season 1", "E01.mkv"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"E01.nfo", "E01.zh.srt"} {
		if _, err := os.Stat(filepath.Join(task.Target, "Series", "Season 1", name)); !os.IsNotExist(err) {
			t.Fatal("episode sidecar retained", name, err)
		}
	}
	for _, name := range []string{"Series/tvshow.nfo", "Series/poster.jpg", "Series/season01-poster.jpg", "Series/Season 1/season.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, filepath.FromSlash(name))); err != nil {
			t.Fatal("series art removed too soon", name, err)
		}
	}
	os.Remove(filepath.Join(s.Config["root"], "Series", "Season 1", "E02.mkv"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Series/tvshow.nfo", "Series/poster.jpg", "Series/season01-poster.jpg", "Series/Season 1/season.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatal("expired series art retained", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(task.Target, "Series", "personal.txt")); err != nil {
		t.Fatal("unrelated file deleted", err)
	}
}

func TestSTRMLifeIncrementalScopeCursorAndDeletion(t *testing.T) {
	a, s, file := strm115Fixture(t)
	task := Task{ID: "life", Name: "生活事件", Kind: "strm", StorageID: s.ID, Source: "10", SourceLabel: "Show", Target: t.TempDir(), Mode: strmUpdate, STRMSource: "life",
		STRMLifeSince: time.Now().Add(-time.Minute), Enabled: true, MediaExtensions: "mkv", CascadeDelete: true, CascadeExtensions: "jpg;nfo"}
	a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil })
	phase, calls := 0, 0
	shareMock(t, func(r *http.Request) string {
		if r.Method != "GET" {
			t.Fatal("life attempted cloud write", r.URL.Path)
		}
		switch r.URL.Path {
		case "/behavior/detail":
			if phase == 2 {
				return `{"state":true,"data":{"count":1,"list":[{"id":3040163688862324737,"type":22,"file_id":"file","parent_id":"10","file_name":"Episode01.mkv","file_category":1,"update_time":` + timeNowUnix() + `}]}}`
			}
			return `{"state":true,"data":{"count":1,"list":[{"id":3040163688862324736,"type":2,"file_id":"file","parent_id":"10","file_name":"Episode01.mkv","file_category":1,"update_time":` + timeNowUnix() + `}]}}`
		case "/files":
			calls++
			if r.URL.Query().Get("cid") != "10" {
				t.Fatal("expanded unrelated directory", r.URL.RawQuery)
			}
			if phase == 2 {
				return `{"state":true,"cid":"10","count":0,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[]}`
			}
			return `{"state":true,"cid":"10","count":1,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[{"fid":"file","cid":"10","n":"Episode01.mkv","s":42,"pc":"` + file.PickCode + `"}]}`
		}
		t.Fatal("unexpected endpoint", r.URL.Path)
		return ""
	})
	n, err := a.executeSTRMLife(a.ctx, task, s)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	output := filepath.Join(task.Target, "Show", "Episode01.strm")
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, "Show", "poster.jpg"), "art")
	before := calls
	phase = 1
	if n, err := a.executeSTRMLife(a.ctx, task, s); err != nil || n != 0 || calls != before {
		t.Fatal("cursor replayed processed event", n, calls, before, err)
	}
	base, prefix, _ := a.outputLocation(task.Target)
	h, err := a.beginSTRMSync(task, s, base, filepath.ToSlash(filepath.Join(prefix, "Show")))
	if err != nil || h.manifest.Life.ID != "3040163688862324736" {
		t.Fatal("big event ID lost", h, err)
	}
	phase = 2
	if _, err := a.executeSTRMLife(context.Background(), task, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("delete event retained owned STRM", err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "Show", "poster.jpg")); !os.IsNotExist(err) {
		t.Fatal("series artwork retained after whole work gone", err)
	}
}

func timeNowUnix() string { return strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10) }

func lifeRevisionTask(t *testing.T, a *App, s Storage) Task {
	t.Helper()
	task := Task{ID: "life", Name: "生活事件", Kind: "strm", StorageID: s.ID, Source: "10", SourceLabel: "Show", Target: t.TempDir(), Mode: strmUpdate, STRMSource: "life",
		STRMLifeSince: time.Now().Add(-time.Minute), Enabled: true, MediaExtensions: "mkv", CascadeDelete: true}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestSTRMLifePaginationFailureKeepsCursor(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	cursor := strmLifeCursor{ID: "300", Since: time.Now().Add(-time.Hour).Unix()}
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if calls == 1 {
			return `{"state":true,"data":{"count":"2","list":[{"id":"302","type":2,"file_id":"file","parent_id":"10","file_name":"film.mkv","update_time":` + timeNowUnix() + `}]}}`
		}
		return `{"state":false,"error":"pagination unavailable"}`
	})
	events, next, err := a.pullSTRMLife(a.ctx, s, cursor, time.Now())
	if err == nil || len(events) != 0 || next != cursor || calls != 2 {
		t.Fatal("partial page advanced cursor", events, next, calls, err)
	}
}

func TestSTRMLifeFutureEventsWaitForNextPoll(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	until := time.Now()
	cursor := strmLifeCursor{Since: until.Add(-time.Hour).Unix()}
	shareMock(t, func(r *http.Request) string {
		return fmt.Sprintf(`{"state":true,"data":{"count":1,"list":[{"id":"302","type":2,"file_id":"file","parent_id":"10","file_name":"film.mkv","update_time":%d}]}}`, until.Add(time.Second).Unix())
	})
	events, next, err := a.pullSTRMLife(a.ctx, s, cursor, until)
	if err != nil || len(events) != 0 || next != cursor {
		t.Fatal("future event consumed early", events, next, err)
	}
	events, next, err = a.pullSTRMLife(a.ctx, s, cursor, until.Add(2*time.Second))
	if err != nil || len(events) != 1 || next.ID != "302" {
		t.Fatal("future event lost", events, next, err)
	}
}

func TestSTRMLifeFolderDeletionSupersedesChildChanges(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	task := lifeRevisionTask(t, a, s)
	root := filepath.Join(task.Target, "Show")
	writeTest(t, filepath.Join(root, "Season", "film.strm"), "old")
	h, err := a.beginSTRMSync(task, s, task.Target, "Show")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ensure(a.ctx, nil); err != nil {
		t.Fatal(err)
	}
	digest, _, err := strmOutputDigest(h.root, "Season/film.strm")
	if err != nil {
		t.Fatal(err)
	}
	h.manifest.Files["Season/film.strm"] = digest
	h.manifest.Sources = map[string]strmLifeSource{
		"season": {Relative: "Season", Parent: "10", Name: "Season", Directory: true},
		"film":   {Relative: "Season/film.mkv", Parent: "season", Name: "film.mkv"},
	}
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	h.root.Close()
	calls, stillPresent := 0, true
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/behavior/detail":
			return `{"state":true,"data":{"count":3,"list":[{"id":"303","type":22,"file_id":"season","parent_id":"10","file_name":"Season","file_category":0,"update_time":` + timeNowUnix() + `},{"id":"302","type":22,"file_id":"film","parent_id":"season","file_name":"film.mkv","update_time":` + timeNowUnix() + `},{"id":"301","type":22,"file_id":"unrelated","parent_id":"missing-other-root","file_name":"other.mkv","update_time":` + timeNowUnix() + `}]}}`
		case "/files":
			calls++
			if r.URL.Query().Get("cid") != "10" {
				t.Fatal("read deleted or unrelated directory", r.URL.RawQuery)
			}
			if stillPresent {
				return `{"state":true,"cid":"10","count":1,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[{"cid":"season","pid":"10","n":"Season"}]}`
			}
			return `{"state":true,"cid":"10","count":0,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[]}`
		}
		t.Fatal(r.URL.Path)
		return ""
	})
	if _, err := a.executeSTRMLife(a.ctx, task, s); err == nil {
		t.Fatal("stale deletion event removed a live directory")
	}
	if _, err := os.Stat(filepath.Join(root, "Season", "film.strm")); err != nil {
		t.Fatal("live source output removed", err)
	}
	stillPresent, calls = false, 0
	if _, err := a.executeSTRMLife(a.ctx, task, s); err != nil || calls != 1 {
		t.Fatal("folder deletion blocked by child/foreign event", calls, err)
	}
	if _, err := os.Stat(filepath.Join(root, "Season", "film.strm")); !os.IsNotExist(err) {
		t.Fatal("folder deletion did not remove owned output", err)
	}
}

func TestSTRMLifeFailedPublishRetriesWithoutAdvancing(t *testing.T) {
	a, s, file := strm115Fixture(t)
	task := lifeRevisionTask(t, a, s)
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/behavior/detail":
			return `{"state":true,"data":{"count":1,"list":[{"id":"302","type":2,"file_id":"file","parent_id":"10","file_name":"Episode01.mkv","update_time":` + timeNowUnix() + `}]}}`
		case "/files":
			return `{"state":true,"cid":"10","count":1,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[{"fid":"file","cid":"10","n":"Episode01.mkv","s":42,"pc":"` + file.PickCode + `"}]}`
		}
		t.Fatal(r.URL.Path)
		return ""
	})
	ctx := context.WithValue(a.ctx, strmLifeWorkKey{}, func() error { return errors.New("archive unavailable") })
	if _, err := a.executeSTRMLife(ctx, task, s); err == nil {
		t.Fatal("publish continued without archive")
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "Show")
	if err != nil || h.manifest.Life.ID != "" || len(h.manifest.Sources) != 0 {
		t.Fatal("failed batch committed cursor/index", h, err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "Show", "Episode01.strm")); !os.IsNotExist(err) {
		t.Fatal("failed archive allowed output publication", err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "Show", strmOwnerName(task.ID))); !os.IsNotExist(err) {
		t.Fatal("failed archive allowed ownership mutation", err)
	}
	if count, err := a.executeSTRMLife(a.ctx, task, s); err != nil || count != 1 {
		t.Fatal("uncommitted batch cannot retry", count, err)
	}
}

func TestSTRMLifeQuietPollDoesNotArchiveEmptySuccess(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	task := lifeRevisionTask(t, a, s)
	shareMock(t, func(r *http.Request) string {
		if r.URL.Path != "/behavior/detail" {
			t.Fatal("empty monitor scanned unrelated cloud directory", r.URL.Path)
		}
		return `{"state":true,"data":{"count":0,"list":[]}}`
	})
	done := make(chan error, 1)
	if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "life-monitor"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(a.archives.records) != 0 {
		t.Fatal("empty poll created successful archive")
	}
	for _, event := range a.store.snapshot().Logs {
		if event.Module == "tasks" && strings.Contains(event.Message, "执行完成") {
			t.Fatal("empty poll logged fake completion", event.Message)
		}
	}
	if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "manual"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || len(a.archives.records) != 1 || a.archives.records[0].Status != "success" {
		t.Fatal("explicit manual run lost result archive", err, a.archives.records)
	}
}

func TestSTRMLifePendingCascadeResumesWithoutNewEvents(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	task := lifeRevisionTask(t, a, s)
	root := filepath.Join(task.Target, "Show")
	writeTest(t, filepath.Join(root, "film.nfo"), "art")
	h, err := a.beginSTRMSync(task, s, task.Target, "Show")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ensure(a.ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.manifest.PendingCascade = []string{"film.strm"}
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	h.root.Close()
	shareMock(t, func(r *http.Request) string { return `{"state":true,"data":{"count":0,"list":[]}}` })
	marked := 0
	ctx := context.WithValue(a.ctx, strmLifeWorkKey{}, func() error { marked++; return nil })
	if _, err := a.executeSTRMLife(ctx, task, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "film.nfo")); !os.IsNotExist(err) || marked != 1 {
		t.Fatal("pending cleanup did not run", marked, err)
	}
}

func TestSTRMContentURLsPreserveIdentityInBothModes(t *testing.T) {
	a, s, file := strm115Fixture(t)
	file.Name = "剧集 [A] #1%?\t.mkv "
	for _, encoded := range []bool{false, true} {
		content := "original"
		if encoded {
			content = "encoded"
		}
		if err := a.store.update(func(st *State) error { st.Settings.STRMContent = content; return nil }); err != nil {
			t.Fatal(err)
		}
		address, err := a.publicSTRMURL(s, file)
		if err != nil {
			t.Fatal(err)
		}
		addresses := []string{address,
			virtual115FilenameURL("http://example.test", "code", "pass", file.Name, encoded),
			mobileLibraryURL(quarkLibraryCard{Secret: mobileLibrarySecret("code", "pass")}, file.Name, "http://example.test", encoded)}
		quark := quarkLibraryURL(quarkLibraryCard{ID: "library", Secret: "https://pan.quark.cn/s/code?pwd=pass"}, file.Name, "http://example.test", encoded)
		addresses = append(addresses, quark)
		for _, address := range addresses {
			parsed, err := url.Parse(strings.TrimSpace(address))
			if err != nil || !strings.HasSuffix(parsed.Path, "/"+file.Name) || parsed.RawQuery != "" || parsed.Fragment != "" {
				t.Fatal("name changed URL identity", address, parsed, err)
			}
			if strings.Contains(address, "剧集") == encoded {
				t.Fatal("content setting not applied", encoded, address)
			}
		}
	}
}

func TestShareLiveManualCreateIsReadOnlyAndNameImmutable(t *testing.T) {
	a, _, link := liveTestSetup(t)
	a.store.update(func(st *State) error { st.ShareLiveLinks = nil; return nil })
	writes := 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/1/clouddrive/share/sharepage/token":
			return `{"code":0,"data":{"stoken":"token"}}`
		case "/1/clouddrive/share/sharepage/detail":
			return `{"code":0,"data":{"list":[{"fid":"original","file_name":"Original","dir":true}]}}`
		}
		writes++
		t.Error("create submitted cloud write", r.Method, r.URL.Path)
		return `{"code":0}`
	})
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/share-live", map[string]any{"url": link.Link, "targetId": link.TargetID, "name": "Spoofed", "cron": ""}, cookie)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := a.store.snapshot().ShareLiveLinks
	if len(saved) != 1 || saved[0].Name != "Original" || saved[0].DirectoryName != "Original" || saved[0].DirectoryID != "" || !saved[0].NextRun.IsZero() || writes != 0 {
		t.Fatal(saved, writes)
	}
	w = request(t, h, "PUT", "/api/share-live/"+saved[0].ID, map[string]any{"url": link.Link, "name": "Spoofed", "directoryName": "Original", "cron": ""}, cookie)
	if w.Code != 400 || writes != 0 {
		t.Fatal("share name editable", w.Code, writes)
	}
}

func TestShareLiveRelocationConfirmsAndPreservesDirectoryIdentity(t *testing.T) {
	a, s, link := liveTestSetup(t)
	a.store.update(func(st *State) error {
		st.Notifications.Targets = append(st.Notifications.Targets, telegramTarget{ID: "next", StorageID: s.ID, Provider: s.Type, Parent: "new-parent", Path: "/New"})
		st.ShareTransfers = []shareTransferRecord{{ID: "old", LiveID: link.ID, DirectoryName: link.DirectoryName}}
		return nil
	})
	name, parent, writes := "Saved", "parent", 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			if r.URL.Query().Get("pdir_fid") == parent {
				return fmt.Sprintf(`{"code":0,"data":{"list":[{"fid":"saved-root","file_name":%q,"dir":true}]}}`, name)
			}
			return `{"code":0,"data":{"list":[]}}`
		case "/1/clouddrive/file/rename":
			var body struct {
				Name string `json:"file_name"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			name = body.Name
			writes++
			return `{"code":0}`
		case "/1/clouddrive/file/move":
			var body struct {
				Parent string `json:"to_pdir_fid"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			parent = body.Parent
			writes++
			return `{"code":0}`
		}
		t.Fatal("unexpected migration call", r.URL.Path)
		return ""
	})
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	input := map[string]any{"url": link.Link, "name": link.Name, "directoryName": "Renamed", "cron": "", "targetId": "next", "confirm": false}
	w := request(t, h, "PUT", "/api/share-live/"+link.ID, input, cookie)
	if w.Code != 400 || writes != 0 {
		t.Fatal("migration without confirmation", w.Code, writes, w.Body.String())
	}
	input["confirm"] = true
	a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "busy", Kind: "strm", Name: "正在同步的目录", StorageID: s.ID, Source: link.DirectoryID, SourceLabel: link.DirectoryName, Target: t.TempDir()}}
		return nil
	})
	a.runMu.Lock()
	a.running["busy"] = func() {}
	a.runMu.Unlock()
	w = request(t, h, "PUT", "/api/share-live/"+link.ID, input, cookie)
	if w.Code != 409 || writes != 0 || !strings.Contains(w.Body.String(), "正在同步的目录") {
		t.Fatal("live migration bypassed running task scope", w.Code, writes, w.Body.String())
	}
	a.runMu.Lock()
	delete(a.running, "busy")
	a.runMu.Unlock()
	w = request(t, h, "PUT", "/api/share-live/"+link.ID, input, cookie)
	st := a.store.snapshot()
	if w.Code != 200 || writes != 2 || st.ShareLiveLinks[0].DirectoryID != "saved-root" || st.ShareLiveLinks[0].Parent != "new-parent" || st.ShareLiveLinks[0].TargetID != "next" || st.ShareLiveLinks[0].Intent != "" || st.ShareTransfers[0].DirectoryName != "Renamed" {
		t.Fatal(w.Code, w.Body.String(), writes, st.ShareLiveLinks)
	}
}

func TestShareLiveFileManagerRenameReturnsToRecord(t *testing.T) {
	a, s, link := liveTestSetup(t)
	a.store.update(func(st *State) error {
		st.ShareTransfers = []shareTransferRecord{{ID: "old", LiveID: link.ID, DirectoryName: link.DirectoryName}}
		return nil
	})
	name, writes := "Saved", 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			return fmt.Sprintf(`{"code":0,"data":{"list":[{"fid":"saved-root","file_name":%q,"dir":true}]}}`, name)
		case "/1/clouddrive/file/rename":
			var body struct {
				Name string `json:"file_name"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			name = body.Name
			writes++
			return `{"code":0}`
		}
		t.Fatal("unexpected call", r.URL.Path)
		return ""
	})
	if err := a.cloudFileAction(a.ctx, s, s, fileActionRequest{Action: "rename", Source: "parent", IDs: []string{"saved-root"}, Name: "New"}); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	if writes != 1 || st.ShareLiveLinks[0].DirectoryName != "New" || st.ShareTransfers[0].DirectoryName != "New" {
		t.Fatal(writes, st.ShareLiveLinks, st.ShareTransfers)
	}
	stored, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if stored.snapshot().ShareLiveLinks[0].DirectoryName != "New" {
		t.Fatal("rename not persisted")
	}
}

func TestShareLiveFileManagerUnconfirmedRenameKeepsRecord(t *testing.T) {
	a, s, link := liveTestSetup(t)
	present := true
	shareMock(t, func(r *http.Request) string {
		if present {
			return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
		}
		return `{"code":0,"data":{"list":[]}}`
	})
	req := fileActionRequest{Action: "rename", Source: link.Parent, IDs: []string{link.DirectoryID}, Name: "New"}
	for _, exists := range []bool{true, false} {
		present = exists
		if err := a.syncShareLiveDirectory(a.ctx, s, req); err == nil {
			t.Fatal("unconfirmed cloud result silently updated record")
		}
		if a.store.snapshot().ShareLiveLinks[0].DirectoryName != link.DirectoryName {
			t.Fatal("unconfirmed rename changed saved directory")
		}
	}
}

func TestSTRMFullSyncAndResetRemainDistinct(t *testing.T) {
	a, s, task := strmModeFixture(t)
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "video")
	writeTest(t, filepath.Join(s.Config["root"], "gone.mkv"), "video")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, "personal.txt"), "preserve until explicit whole-library reset")
	info, _ := os.Stat(filepath.Join(task.Target, "film.strm"))
	os.Remove(filepath.Join(s.Config["root"], "gone.mkv"))
	task.Mode = strmMissing
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	done := make(chan error, 1)
	if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "full-sync"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "gone.strm")); !os.IsNotExist(err) {
		t.Fatal("full sync did not remove stale owned output", err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "personal.txt")); err != nil {
		t.Fatal("full sync reset whole directory", err)
	}
	unchanged, _ := os.Stat(filepath.Join(task.Target, "film.strm"))
	if !os.SameFile(info, unchanged) || !info.ModTime().Equal(unchanged.ModTime()) {
		t.Fatal("full sync rewrote unchanged output")
	}
	if a.store.snapshot().Tasks[0].Mode != strmMissing {
		t.Fatal("full sync changed saved mode")
	}
	manifest, err := a.beginSTRMSync(task, s, task.Target, ".")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		manifest.manifest.Files[fmt.Sprintf("old-%d.strm", i)] = strings.Repeat("0", 64)
	}
	if err := manifest.save(); err != nil {
		t.Fatal(err)
	}
	if err := a.startTaskContext(a.ctx, task.ID, done, true); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "personal.txt")); !os.IsNotExist(err) {
		t.Fatal("confirmed owned-library reset did not clear whole library", err)
	}
	if _, err := os.Stat(filepath.Join(s.Config["root"], "film.mkv")); err != nil {
		t.Fatal("reset deleted source", err)
	}
	if a.store.snapshot().Tasks[0].Mode != strmMissing {
		t.Fatal("reset changed saved mode")
	}
}

func TestSTRMCascadePendingSurvivesInterruptedCleanup(t *testing.T) {
	a, s, task := strmModeFixture(t)
	task.CascadeDelete = true
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	writeTest(t, filepath.Join(s.Config["root"], "film.mkv"), "video")
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(task.Target, "film.nfo"), "art")
	base, prefix, _ := a.outputLocation(task.Target)
	h, err := a.beginSTRMSync(task, s, base, prefix)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	cancel()
	if err := h.cascadeOutputs(ctx, []string{"film.strm"}); err == nil {
		t.Fatal("cancelled cascade proceeded")
	}
	if len(h.manifest.PendingCascade) != 1 {
		t.Fatal("pending cleanup lost")
	}
	delete(h.manifest.Files, "film.strm")
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(s.Config["root"], "film.mkv"))
	os.Remove(filepath.Join(task.Target, "film.strm"))
	a.cache.clear()
	if _, err := a.executeTask(a.ctx, task, s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(task.Target, "film.nfo")); !os.IsNotExist(err) {
		t.Fatal("pending cascade not resumed", err)
	}
	h, err = a.beginSTRMSync(task, s, base, prefix)
	if err != nil || len(h.manifest.PendingCascade) != 0 {
		t.Fatal("completed cleanup remains pending", err)
	}
}

func TestSTRMContentSettingRoundTrip(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	for _, mode := range []string{"original", "encoded"} {
		w := request(t, h, "PUT", "/api/strm/settings", map[string]string{"address": "http://localhost:15151", "format": "pickcode_name", "content": mode}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result map[string]any
		w = request(t, h, "GET", "/api/strm/settings", nil, cookie)
		json.Unmarshal(w.Body.Bytes(), &result)
		if result["content"] != mode {
			t.Fatal("content not persisted", result)
		}
	}
}

func TestRenameNumberDefaultsAfterName(t *testing.T) {
	var rule renameRule
	if err := json.Unmarshal([]byte(`{"kind":"number"}`), &rule); err != nil {
		t.Fatal(err)
	}
	compiled, err := compileRenameRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	name, err := compiled.apply("电影", 0)
	if err != nil || name != "电影1" {
		t.Fatal("number not appended by default", name, err)
	}
}
