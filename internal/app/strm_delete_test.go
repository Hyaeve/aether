package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func deleteFixture(t *testing.T, count int, cascade bool) (*App, Storage, Task, *strmTaskSync, []string) {
	t.Helper()
	a, s, task := strmModeFixture(t)
	task.CascadeDelete, task.Enabled, task.Cron = cascade, true, "*/15 * * * *"
	if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, ".")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ensure(a.ctx, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.root.Close() })
	sum := sha256.Sum256([]byte("pointer"))
	names := []string{}
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("film-%04d.strm", i)
		writeTest(t, filepath.Join(task.Target, name), "pointer")
		h.manifest.Files[name] = hex.EncodeToString(sum[:])
		names = append(names, name)
	}
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	return a, s, task, h, names
}

func waitDeleteCheck(t *testing.T, a *App, id string) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		a.runMu.Lock()
		_, running := a.running[id]
		a.runMu.Unlock()
		if !running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("health check did not complete")
}

func TestSTRMDeleteThresholdBoundary(t *testing.T) {
	for _, count := range []int{1000, 1001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			a, s, task, h, names := deleteFixture(t, count, false)
			if err := h.deleteOutputs(a.ctx, names); err != nil {
				t.Fatal(err)
			}
			current, _ := a.store.storage(s.ID)
			if (current.Health != nil) != (count > 1000) {
				t.Fatal("boundary did not use strictly greater than", current.Health)
			}
			if len(h.manifest.Files) != 0 || a.store.snapshot().Tasks[0].STRMDeleteState != nil {
				t.Fatal("healthy batch did not finish", task.ID)
			}
		})
	}
}

func TestSTRMDeleteThresholdCascadeCountsAndFailureDeletesNothing(t *testing.T) {
	a, s, task, h, names := deleteFixture(t, 1, true)
	one := 1
	task.DeleteThreshold, h.task.DeleteThreshold = &one, &one
	h.configStamp = strmTaskConfig(task)
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	writeTest(t, filepath.Join(task.Target, "film-0000.nfo"), "art")
	if err := os.Remove(s.Config["root"]); err != nil {
		t.Fatal(err)
	}
	if err := h.deleteOutputs(a.ctx, names); !errors.Is(err, errSTRMDeletePaused) {
		t.Fatal(err)
	}
	for _, name := range []string{"film-0000.strm", "film-0000.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
			t.Fatal("threshold check partially deleted its batch", name, err)
		}
	}
	current := a.store.snapshot().Tasks[0]
	if current.Status != "delete-paused" || current.STRMDeleteState.Count != 2 || current.STRMDeleteState.State != "waiting" || current.NextRun.IsZero() {
		t.Fatal(current)
	}
	for _, trigger := range []string{"manual", "full-sync"} {
		if err := a.startTaskWithTrigger(a.ctx, task.ID, nil, trigger); err == nil {
			t.Fatal("paused task could execute", trigger)
		}
	}
	if err := a.startTask(task.ID, true); err == nil {
		t.Fatal("reset bypassed hold")
	}
	if err := os.Mkdir(s.Config["root"], 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.checkStorageHealth(a.ctx, s); err != nil {
		t.Fatal(err)
	}
	if a.store.snapshot().Tasks[0].STRMDeleteState == nil {
		t.Fatal("background recovery cleared deletion hold")
	}
	if err := a.startSTRMDeleteCheck(task.ID, false); err != nil {
		t.Fatal(err)
	}
	waitDeleteCheck(t, a, task.ID)
	if a.store.snapshot().Tasks[0].STRMDeleteState != nil {
		t.Fatal("manual health check did not unlock")
	}
	if err := h.deleteOutputs(a.ctx, names); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"film-0000.strm", "film-0000.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); !os.IsNotExist(err) {
			t.Fatal("healthy combined batch was not removed", name, err)
		}
	}
}

func TestSTRMDeleteThresholdIgnoresMissingAndExternalFiles(t *testing.T) {
	a, s, task, h, names := deleteFixture(t, 3, true)
	one := 1
	task.DeleteThreshold, h.task.DeleteThreshold = &one, &one
	h.configStamp = strmTaskConfig(task)
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	os.Remove(filepath.Join(task.Target, names[0]))
	writeTest(t, filepath.Join(task.Target, names[1]), "external")
	writeTest(t, filepath.Join(task.Target, "film-0002.nfo"), "external art")
	h.manifest.Files["film-0002.nfo"] = fmt.Sprintf("%064d", 1)
	if err := h.deleteOutputs(a.ctx, names); err != nil {
		t.Fatal(err)
	}
	current, _ := a.store.storage(s.ID)
	if current.Health != nil {
		t.Fatal("absent or externally modified files counted as deletions")
	}
	for _, name := range []string{names[1], "film-0002.nfo"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
			t.Fatal("external modification removed", name, err)
		}
	}
}

func TestSTRMDeleteThresholdZeroAndValidation(t *testing.T) {
	a, s, task := strmModeFixture(t)
	if strmDeleteThreshold(task) != 1000 {
		t.Fatal("old task has wrong default")
	}
	zero, negative := 0, -1
	task.DeleteThreshold = &negative
	if a.validateTask(&task) == nil {
		t.Fatal("negative threshold accepted")
	}
	task.DeleteThreshold = &zero
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	if err := a.authorizeSTRMDeletion(a.ctx, task, s, 1, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	current, _ := a.store.storage(s.ID)
	if current.Health == nil {
		t.Fatal("zero did not require probe for deletion")
	}
	var decimal Task
	if json.Unmarshal([]byte(`{"deleteThreshold":1.5}`), &decimal) == nil {
		t.Fatal("decimal threshold accepted")
	}
}

func TestSTRMDeleteThresholdRetryCalendarAndCron(t *testing.T) {
	now := time.Date(2026, 12, 31, 23, 19, 42, 0, time.FixedZone("CST", 8*3600))
	task := Task{Kind: "strm", STRMSource: "life", Enabled: true}
	want := time.Date(2027, 1, 1, 23, 19, 0, 0, now.Location())
	if actual := strmDeleteRetry(task, now); !actual.Equal(want) {
		t.Fatal(actual, want)
	}
	task.Cron = "0 * * * *"
	want = time.Date(2027, 1, 1, 0, 0, 0, 0, now.Location())
	if actual := strmDeleteRetry(task, now); !actual.Equal(want) {
		t.Fatal(actual, want)
	}
	task.STRMDeleteState = &strmDeleteState{RetryAt: want}
	if !nextRun(task, now).Equal(want) {
		t.Fatal("life poll overwrote guard deadline")
	}
}

func TestSTRMDeleteThresholdRuntimeAndClientCannotClear(t *testing.T) {
	a, _, task, h, names := deleteFixture(t, 1, false)
	zero := 0
	task.DeleteThreshold = &zero
	guard := &strmDeleteState{ID: "hold", State: "waiting", Count: 4, RetryAt: time.Now().Add(24 * time.Hour).Truncate(time.Minute)}
	task.STRMDeleteState, task.Status = guard, "delete-paused"
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	handler := a.Handler(t.TempDir())
	cookie := setupTest(t, a, handler)
	task.STRMDeleteState = nil
	w := request(t, handler, "PUT", "/api/tasks/"+task.ID, task, cookie)
	if w.Code != 200 || a.store.snapshot().Tasks[0].STRMDeleteState == nil {
		t.Fatal(w.Code, w.Body.String())
	}
	stored, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	restored := stored.snapshot().Tasks[0]
	if restored.STRMDeleteState == nil || restored.STRMDeleteState.State != "waiting" || !restored.NextRun.Equal(guard.RetryAt) || strmDeleteThreshold(restored) != 0 {
		t.Fatal("hold or explicit zero did not survive restart", restored)
	}
	if _, err := os.Stat(filepath.Join(h.manifest.Root, names[0])); err != nil {
		t.Fatal("save or restart deleted output", err)
	}
}

func TestSTRMDeleteThresholdPlanChangedDuringProbeRetainsAll(t *testing.T) {
	for _, change := range []string{"output", "config", "storage", "scope"} {
		t.Run(change, func(t *testing.T) {
			a, s, task, h, names := deleteFixture(t, 2, false)
			zero := 0
			task.DeleteThreshold, h.task.DeleteThreshold = &zero, &zero
			var probes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				probes.Add(1)
				switch change {
				case "output":
					writeTest(t, filepath.Join(task.Target, names[1]), "external change")
				case "config":
					a.taskUpdate(task.ID, func(t *Task) { t.Target += "-changed" })
				case "storage":
					a.store.update(func(st *State) error { st.Storages[0].Config["username"] = "changed"; return nil })
				case "scope":
					a.store.update(func(st *State) error {
						st.Tasks = append(st.Tasks, Task{ID: "peer", Kind: "strm", StorageID: s.ID, Target: task.Target, Source: "/"})
						return nil
					})
				}
				w.WriteHeader(207)
			}))
			defer server.Close()
			s.Type, s.Config = "webdav", map[string]string{"address": server.URL, "root": "/"}
			h.storage, h.storageStamp = s, storageRevision(s)
			h.configStamp = strmTaskConfig(task)
			a.store.update(func(st *State) error { st.Tasks[0] = task; st.Storages[0] = s; return nil })
			if err := h.deleteOutputs(a.ctx, names); err == nil {
				t.Fatal("changed deletion plan accepted")
			}
			if probes.Load() != 1 {
				t.Fatal("test did not reach real probe", probes.Load())
			}
			for _, name := range names {
				if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
					t.Fatal("plan partly deleted before final validation", name, err)
				}
			}
		})
	}
}

func TestSTRMDeleteThresholdRecoveryDuplicateAndFailure(t *testing.T) {
	a, s, task, _, _ := deleteFixture(t, 0, false)
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(403)
	}))
	defer server.Close()
	defer close(release)
	s.Type, s.Config = "webdav", map[string]string{"address": server.URL, "root": "/"}
	task.STRMDeleteState = &strmDeleteState{ID: "hold", State: "waiting", Count: 5}
	a.store.update(func(st *State) error { st.Tasks[0] = task; st.Storages[0] = s; return nil })
	if err := a.startSTRMDeleteCheck(task.ID, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("probe not started")
	}
	if err := a.startSTRMDeleteCheck(task.ID, false); err == nil {
		t.Fatal("duplicate recovery allowed")
	}
	a.runMu.Lock()
	a.running[task.ID]()
	a.runMu.Unlock()
	waitDeleteCheck(t, a, task.ID)
	current := a.store.snapshot().Tasks[0]
	if current.STRMDeleteState == nil || current.STRMDeleteState.State != "waiting" || current.NextRun.IsZero() {
		t.Fatal("failed/cancelled probe cleared hold", current)
	}
}

func TestSTRMDeleteThresholdScheduledRecoveryResumes(t *testing.T) {
	a, s, task, _, names := deleteFixture(t, 1, false)
	task.STRMDeleteState = &strmDeleteState{ID: "hold", State: "waiting", Count: 1}
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	if err := a.startSTRMDeleteCheck(task.ID, true); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		current := a.store.snapshot().Tasks[0]
		if current.Status == "success" {
			if _, err := os.Stat(filepath.Join(task.Target, names[0])); !os.IsNotExist(err) {
				t.Fatal("scheduled recovery did not rerun task", err)
			}
			source, _ := a.store.storage(s.ID)
			if source.Health == nil || current.STRMDeleteState != nil {
				t.Fatal("recovery skipped health check")
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("scheduled recovery did not resume", a.store.snapshot().Tasks[0])
}

func TestSTRMDeleteThresholdFullResetIncludesAllFiles(t *testing.T) {
	a, s, task, h, names := deleteFixture(t, 1, false)
	one := 1
	task.DeleteThreshold = &one
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	writeTest(t, filepath.Join(task.Target, "personal.txt"), "also removed by confirmed reset")
	large, err := os.Create(filepath.Join(task.Target, "private-large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(65 << 20); err != nil {
		large.Close()
		t.Fatal(err)
	}
	large.Close()
	if err := os.Remove(s.Config["root"]); err != nil {
		t.Fatal(err)
	}
	if err := a.resetTaskOutput(task, s); !errors.Is(err, errSTRMDeletePaused) {
		t.Fatal(err)
	}
	for _, name := range []string{names[0], "personal.txt", "private-large.bin"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); err != nil {
			t.Fatal("reset partially deleted library", err)
		}
	}
	if current := a.store.snapshot().Tasks[0]; current.STRMDeleteState == nil || current.STRMDeleteState.Count != 3 {
		t.Fatal("reset omitted files or counted owner marker", current)
	}
	h.root.Close()
	if err := os.Mkdir(s.Config["root"], 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.startSTRMDeleteCheck(task.ID, false); err != nil {
		t.Fatal(err)
	}
	waitDeleteCheck(t, a, task.ID)
	done := make(chan error, 1)
	if err := a.startTaskContext(a.ctx, task.ID, done, true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("healthy reset did not finish", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("healthy reset deadlocked")
	}
	for _, name := range []string{names[0], "personal.txt", "private-large.bin"} {
		if _, err := os.Stat(filepath.Join(task.Target, name)); !os.IsNotExist(err) {
			t.Fatal("healthy reset did not clear approved library", err)
		}
	}
}

func TestSTRMDeleteThresholdNoApprovalForFutureBatch(t *testing.T) {
	a, s, task := strmModeFixture(t)
	zero := 0
	task.DeleteThreshold = &zero
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(207)
	}))
	defer server.Close()
	s.Type, s.Config = "webdav", map[string]string{"address": server.URL, "root": "/"}
	a.store.update(func(st *State) error { st.Storages[0] = s; return nil })
	for i := 0; i < 2; i++ {
		if err := a.authorizeSTRMDeletion(a.ctx, task, s, 1, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatal("old approval reused", calls.Load())
	}
}

func TestSTRMDeleteThresholdLifeHealthAndCursor(t *testing.T) {
	for _, scenario := range []string{"healthy", "unhealthy", "root", "rename"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, media := strm115Fixture(t)
			task := lifeRevisionTask(t, a, s)
			zero := 0
			task.DeleteThreshold = &zero
			a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
			root := filepath.Join(task.Target, "Show")
			writeTest(t, filepath.Join(root, "film.strm"), "old")
			writeTest(t, filepath.Join(root, "film.nfo"), "art")
			h, err := a.beginSTRMSync(task, s, task.Target, "Show")
			if err != nil {
				t.Fatal(err)
			}
			if err := h.ensure(a.ctx, nil); err != nil {
				t.Fatal(err)
			}
			digest, _, _ := strmOutputDigest(h.root, "film.strm")
			h.manifest.Files["film.strm"] = digest
			h.manifest.Sources = map[string]strmLifeSource{"film": {Relative: "film.mkv", Parent: "10", Name: "film.mkv"}}
			before := strmLifeCursor{ID: "300", Since: task.STRMLifeSince.Unix(), Started: task.STRMLifeSince.Unix()}
			h.manifest.Life = before
			if err := h.save(); err != nil {
				t.Fatal(err)
			}
			h.root.Close()
			var probes atomic.Int32
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/behavior/detail":
					eventID, parent, name, eventType := "film", "10", "film.mkv", 22
					if scenario == "root" {
						eventID, parent, name = "10", "0", "Show"
					}
					if scenario == "rename" {
						name, eventType = "renamed.mkv", 2
					}
					return fmt.Sprintf(`{"state":true,"data":{"count":1,"list":[{"id":"301","type":%d,"file_id":%q,"parent_id":%q,"file_name":%q,"update_time":%s}]}}`, eventType, eventID, parent, name, timeNowUnix())
				case "/files":
					cid := r.URL.Query().Get("cid")
					if r.URL.Query().Get("limit") == "1" && cid == rootOf(s) {
						probes.Add(1)
						if scenario == "unhealthy" {
							return `{"state":false,"errno":9999,"error":"unavailable"}`
						}
					}
					if scenario == "rename" && cid == "10" {
						return `{"state":true,"cid":"10","count":1,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[{"fid":"film","cid":"10","n":"renamed.mkv","s":42,"pc":"` + media.PickCode + `"}]}`
					}
					return fmt.Sprintf(`{"state":true,"cid":%q,"count":0,"path":[{"cid":"0","name":"根目录"},{"cid":"10","name":"Show"}],"data":[]}`, cid)
				default:
					// Space refresh is separate from the connection probe and may fail.
					return `{"state":false,"error":"usage not available"}`
				}
			})
			done := make(chan error, 1)
			if err := a.startTaskContext(a.ctx, task.ID, done); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("life run did not complete")
			}
			if probes.Load() != 1 {
				t.Fatal("fresh health probe not executed exactly once", probes.Load(), err)
			}
			h, readErr := a.beginSTRMSync(task, s, task.Target, "Show")
			if readErr != nil {
				t.Fatal(readErr)
			}
			current := a.store.snapshot().Tasks[0]
			if scenario == "unhealthy" {
				if !errors.Is(err, errSTRMDeletePaused) || current.Status != "delete-paused" || current.STRMDeleteState == nil ||
					h.manifest.Life != before || len(h.manifest.Sources) != 1 {
					t.Fatal("health cancellation overwrote hold or cursor", current, h.manifest.Life, err)
				}
				retry := current.STRMDeleteState.RetryAt
				if retry.Sub(time.Now()) < 23*time.Hour || retry.Second() != 0 || !retry.Equal(current.NextRun) {
					t.Fatal("life hold retried on ordinary five-second poll", retry)
				}
				for _, name := range []string{"film.strm", "film.nfo"} {
					if _, err := os.Stat(filepath.Join(root, name)); err != nil {
						t.Fatal("failed life probe deleted output", name, err)
					}
				}
				return
			}
			if err != nil || current.STRMDeleteState != nil || h.manifest.Life.ID != "301" {
				t.Fatal("healthy life batch did not complete", current, err)
			}
			for _, name := range []string{"film.strm", "film.nfo"} {
				if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Fatal("healthy life batch not removed", name, err)
				}
			}
			if scenario == "rename" {
				if _, err := os.Stat(filepath.Join(root, "renamed.strm")); err != nil {
					t.Fatal("rename did not preserve new output", err)
				}
			}
		})
	}
}

func TestSTRMDeleteThresholdConfigBackupExcludesRuntime(t *testing.T) {
	a, _, task := strmModeFixture(t)
	zero := 0
	task.DeleteThreshold = &zero
	task.STRMDeleteState = &strmDeleteState{ID: "guard-private", State: "waiting", Count: 1001}
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	handler := a.Handler(t.TempDir())
	cookie := setupTest(t, a, handler)
	response := request(t, handler, "POST", "/api/config/backup", map[string]string{"password": "backup-secret"}, cookie)
	plain, err := openBackup(response.Body.Bytes(), "backup-secret")
	if err != nil {
		t.Fatal(response.Code, err)
	}
	var bundle configBundle
	if err := json.Unmarshal(plain, &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.State.Tasks) != 1 || bundle.State.Tasks[0].STRMDeleteState != nil || strmDeleteThreshold(bundle.State.Tasks[0]) != 0 {
		t.Fatal("backup leaked hold or lost explicit threshold", bundle.State.Tasks)
	}
}

func TestSTRMDeleteThresholdDisabledRecoveryDoesNotExecute(t *testing.T) {
	a, _, task, _, names := deleteFixture(t, 1, false)
	task.Enabled = false
	task.STRMDeleteState = &strmDeleteState{ID: "hold", State: "waiting", Count: 1}
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	if err := a.startSTRMDeleteCheck(task.ID, true); err != nil {
		t.Fatal(err)
	}
	waitDeleteCheck(t, a, task.ID)
	current := a.store.snapshot().Tasks[0]
	if current.STRMDeleteState != nil || current.Enabled || !current.LastRun.IsZero() || !current.NextRun.IsZero() {
		t.Fatal("disabled task executed after health recovery", current)
	}
	if _, err := os.Stat(filepath.Join(task.Target, names[0])); err != nil {
		t.Fatal("disabled recovery deleted output", err)
	}
}

func TestSTRMDeleteThresholdLifeEnablePreservesPendingCursor(t *testing.T) {
	a, s, _ := strm115Fixture(t)
	task := lifeRevisionTask(t, a, s)
	task.Enabled = false
	task.STRMDeleteState = &strmDeleteState{ID: "hold", State: "waiting", Count: 1, RetryAt: time.Now().Add(24 * time.Hour)}
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	handler := a.Handler(t.TempDir())
	cookie := setupTest(t, a, handler)
	task.Enabled = true
	w := request(t, handler, "PUT", "/api/tasks/"+task.ID, task, cookie)
	current := a.store.snapshot().Tasks[0]
	if w.Code != 200 || !current.STRMLifeSince.Equal(task.STRMLifeSince) || current.STRMDeleteState == nil || !current.NextRun.Equal(task.STRMDeleteState.RetryAt) {
		t.Fatal("reenable lost held life events or retry deadline", w.Code, current)
	}
}
