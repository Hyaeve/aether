package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func Test115AccountUniquenessAndEdit(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	s := Storage{Name: "First", Type: "115", Enabled: false, Config: map[string]string{"cookie": test115Cookie}}
	if w := request(t, h, "POST", "/api/storages", s, cookie); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	first := a.store.snapshot().Storages[0]
	s.Name = "Second"
	s.Enabled = true
	s.Config = map[string]string{"cookie": "UID=00123_B2;CID=new;SEID=another", "device": "android"}
	if w := request(t, h, "POST", "/api/storages", s, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "不能重复") {
		t.Fatal(w.Code, w.Body.String())
	}
	first.Config = s.Config
	if w := request(t, h, "PUT", "/api/storages/"+first.ID, first, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, uid := range []string{"", "abc_A1", "0_A1", "123x_A1"} {
		bad := Storage{Name: "Bad", Type: "115", Config: map[string]string{"cookie": "UID=" + uid + ";CID=c;SEID=s"}}
		if validateStorage(&bad) == nil {
			t.Fatal(uid)
		}
	}
	s.Config = map[string]string{"cookie": "UID=456_A1;CID=c;SEID=s"}
	if w := request(t, h, "POST", "/api/storages", s, cookie); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	second := a.store.snapshot().Storages[1]
	second.Config = first.Config
	if w := request(t, h, "PUT", "/api/storages/"+second.ID, second, cookie); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	st := a.store.snapshot()
	st.Storages[1].Config = first.Config
	if validateBundle(configBundle{State: st}) == nil {
		t.Fatal("duplicate imported")
	}
}

func TestHealthDependenciesRemainPausedUntilAllRecover(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	s := Storage{ID: "source", Name: "Source", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
	target := Storage{ID: "target", Name: "Target", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
	a.store.update(func(st *State) error {
		st.Storages = []Storage{s, target}
		st.Tasks = []Task{{ID: "task", Enabled: true, StorageID: s.ID, ED2KBindingID: target.ID}, {ID: "unrelated", StorageID: "another", Enabled: true}}
		st.BackupRules = []BackupRule{{ID: "backup", Enabled: true, Sources: []BackupLocation{{StorageID: s.ID}}, Targets: []BackupLocation{{StorageID: target.ID}}}}
		return nil
	})
	if err := a.recordStorageHealthResult(s, errors.New("network"), true); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	if taskHealthPause(st, st.Tasks[0]) == "" || backupHealthPause(st, st.BackupRules[0]) == "" || taskHealthPause(st, st.Tasks[1]) != "" {
		t.Fatal("dependencies")
	}
	// Passing a cooldown deadline never grants work permission before a successful check.
	a.store.update(func(st *State) error { st.Storages[0].Health.NextCheck = time.Now().Add(-time.Hour); return nil })
	if err := a.startTask("task"); err == nil {
		t.Fatal("task started")
	}
	if err := a.startBackup("backup"); err == nil {
		t.Fatal("backup started")
	}
	w := request(t, h, "GET", "/api/state", nil, cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "healthPause") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "GET", "/api/backup-rules", nil, cookie)
	if !strings.Contains(w.Body.String(), "healthPause") {
		t.Fatal(w.Body.String())
	}
	a.recordStorageHealthResult(target, errors.New("network"), true)
	a.recordStorageHealthResult(s, nil, true)
	st = a.store.snapshot()
	if taskHealthPause(st, st.Tasks[0]) == "" || backupHealthPause(st, st.BackupRules[0]) == "" {
		t.Fatal("other unhealthy dependency ignored")
	}
	a.recordStorageHealthResult(target, nil, true)
	st = a.store.snapshot()
	if taskHealthPause(st, st.Tasks[0]) != "" || backupHealthPause(st, st.BackupRules[0]) != "" || !st.Tasks[0].Enabled || !st.BackupRules[0].Enabled {
		t.Fatal("did not recover")
	}
	if st.Tasks[0].LastRun.IsZero() == false {
		t.Fatal("manual task automatically ran")
	}
}

func TestHealthCancellationHasSeparateCause(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "s", Name: "Source", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
	a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Tasks = []Task{{ID: "task", StorageID: s.ID}}
		st.BackupRules = []BackupRule{{ID: "backup", SourceID: s.ID}}
		return nil
	})
	taskCtx, tc := context.WithCancelCause(a.ctx)
	backupCtx, bc := context.WithCancelCause(a.ctx)
	a.taskHealthCancels = map[string]func(){"task": func() { tc(errStorageHealthPaused) }}
	a.backupHealthCancels = map[string]func(){"backup": func() { bc(errStorageHealthPaused) }}
	if err := a.recordStorageHealthResult(s, errors.New("network"), true); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(context.Cause(taskCtx), errStorageHealthPaused) || !errors.Is(context.Cause(backupCtx), errStorageHealthPaused) {
		t.Fatal("active work not cancelled")
	}
	a.store.update(func(st *State) error {
		st.Tasks[0].Status = "health-paused"
		st.BackupRules[0].Status = "health-paused"
		return nil
	})
	a.recordStorageHealthResult(s, nil, true)
	st := a.store.snapshot()
	if st.Tasks[0].Status != "idle" || st.BackupRules[0].Status != "idle" {
		t.Fatal("paused state remained")
	}
}

func Test115STRMAccountNamespaceRejectsOldAndChangedAccount(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	s := Storage{ID: "s", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	second := Storage{ID: "other", Type: "115", Enabled: true, Config: map[string]string{"cookie": "UID=456_A1;CID=c;SEID=s"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s, second}; return nil })
	f := File{ID: "file", Name: "movie.mkv", PickCode: "abcdef123456"}
	first, err := a.publicSTRMURL(s, f)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.publicSTRMURL(second, f)
	if err != nil || first == other {
		t.Fatal(other, err)
	}
	for _, route := range []string{"/d/abcdef123456.mkv", "/d/789/abcdef123456.mkv", "/d/abc/abcdef123456.mkv"} {
		if w := request(t, h, "GET", route, nil, nil); w.Code != 404 {
			t.Fatal(route, w.Code)
		}
	}
	a.store.update(func(st *State) error {
		st.Storages[0].Config = map[string]string{"cookie": "UID=789_A1;CID=c;SEID=s"}
		return nil
	})
	if w := request(t, h, "GET", first, nil, nil); w.Code != 404 {
		t.Fatal("account switched", w.Code)
	}
}

func TestHealthProbeCancelsActualRunningTaskAndBackup(t *testing.T) {
	for _, kind := range []string{"task", "backup"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			source := Storage{ID: "s", Name: "Cloud", Type: "openlist", Enabled: true, Config: map[string]string{"address": "http://example.test", "token": "test", "root": "/"}}
			target := Storage{ID: "target", Name: "Local", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}}
			rule := BackupRule{ID: "backup", Name: "Copy", Enabled: true, SourceID: source.ID, Source: "/", TargetID: target.ID, Target: "/", Replace: "skip"}
			task := Task{ID: "task", Name: "Cache", Kind: "cache", Enabled: true, StorageID: source.ID, Source: "/", Interval: 60}
			a.store.update(func(st *State) error {
				st.Storages = []Storage{source, target}
				st.Tasks = []Task{task}
				st.BackupRules = []BackupRule{rule}
				return nil
			})
			old := apiClient
			defer func() { apiClient = old }()
			started := make(chan struct{})
			var once sync.Once
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/fs/list" {
					once.Do(func() { close(started) })
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":200,"data":{}}`)), Request: r}, nil
			})}
			var err error
			if kind == "task" {
				err = a.startTask(task.ID)
			} else {
				err = a.startBackup(rule.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(4 * time.Second):
				t.Fatal("execution did not start")
			}
			if err = a.recordStorageHealthResult(source, errors.New("probe failed"), true); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { a.wg.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("work not cancelled")
			}
			st := a.store.snapshot()
			status := st.Tasks[0].Status
			if kind == "backup" {
				status = st.BackupRules[0].Status
			}
			if status != "health-paused" {
				t.Fatal(status)
			}
			if err = a.recordStorageHealthResult(source, nil, true); err != nil {
				t.Fatal(err)
			}
			st = a.store.snapshot()
			status = st.Tasks[0].Status
			if kind == "backup" {
				status = st.BackupRules[0].Status
			}
			if status != "idle" {
				t.Fatal("recovery", status)
			}
		})
	}
}
