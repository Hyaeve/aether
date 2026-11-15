package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSTRMSyncQueuesBehindLifePollAndRunsExclusively(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 1, "")
	task.STRMSource = "life"
	task.Enabled = true
	task.Cron = ""
	task.STRMLifeSince = time.Now().Add(-time.Minute)
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	poll, release := make(chan struct{}), make(chan struct{})
	bulk, releaseBulk := make(chan struct{}), make(chan struct{})
	var pollOnce, bulkOnce, releaseOnce, releaseBulkOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	defer releaseBulkOnce.Do(func() { close(releaseBulk) })
	old := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/behavior/detail" {
			pollOnce.Do(func() { close(poll) })
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return jsonHTTPResponse(`{"state":true,"data":{"count":0,"list":[]}}`), nil
		}
		if r.URL.Path == "/files" {
			bulkOnce.Do(func() { close(bulk) })
			select {
			case <-releaseBulk:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return old.RoundTrip(r)
	})
	t.Cleanup(func() { apiClient.Transport = old })
	done := make(chan error, 1)
	if err := a.startTaskWithTrigger(a.ctx, task.ID, done, "life-monitor"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-poll:
	case <-time.After(5 * time.Second):
		t.Fatal("poll did not start")
	}
	if err := a.startTaskWithTrigger(a.ctx, task.ID, nil, "full-sync"); !errors.Is(err, errSTRMSyncQueued) {
		t.Fatal(err)
	}
	if err := a.startTaskWithTrigger(a.ctx, task.ID, nil, "full-sync"); err == nil {
		t.Fatal("duplicate queued sync accepted")
	}
	select {
	case <-bulk:
		t.Fatal("sync overlapped life poll")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-bulk:
	case <-time.After(5 * time.Second):
		t.Fatal("queued sync did not resume")
	}
	if err := a.startTaskWithTrigger(a.ctx, task.ID, nil, "full-sync"); err == nil {
		t.Fatal("active full sync accepted duplicate")
	}
	during := a.store.snapshot().Tasks[0]
	if !during.STRMFullSync || !during.NextRun.IsZero() || !during.Enabled || !during.STRMLifeSince.Equal(task.STRMLifeSince) {
		t.Fatal("full sync did not suspend scheduling in place", during)
	}
	if err := a.startTaskWithTrigger(a.ctx, task.ID, nil, "life-monitor"); err == nil {
		t.Fatal("a second life poll started during full sync")
	}
	releaseBulkOnce.Do(func() { close(releaseBulk) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a.wg.Wait()
	entries, err := os.ReadDir(filepath.Join(task.Target, task.SourceLabel))
	if err != nil || len(entries) == 0 {
		t.Fatal("full sync did not generate", err)
	}
	after := a.store.snapshot().Tasks[0]
	if after.STRMFullSync || after.NextRun.IsZero() || !after.Enabled || after.STRMSource != "life" || !after.STRMLifeSince.Equal(task.STRMLifeSince) || after.StorageID != s.ID {
		t.Fatal("life monitoring did not resume using the saved mode", after)
	}
}

func TestSTRMLifeFullSyncRestoresCronAfterStopOrRestart(t *testing.T) {
	a, _, task, _ := strmBulkFixture(t, 1, "")
	task.STRMSource, task.Enabled, task.Cron = "life", true, "*/15 * * * *"
	task.STRMLifeSince = time.Now().Add(-time.Minute)
	if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
		t.Fatal(err)
	}
	bulk := make(chan struct{}, 1)
	old := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/behavior/detail":
			return jsonHTTPResponse(`{"state":true,"data":{"count":0,"list":[]}}`), nil
		case "/files":
			select {
			case bulk <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		default:
			return old.RoundTrip(r)
		}
	})
	t.Cleanup(func() { apiClient.Transport = old })
	ctx, cancel := context.WithCancel(a.ctx)
	defer func() { cancel(); a.wg.Wait() }()
	done := make(chan error, 1)
	if err := a.startTaskWithTrigger(ctx, task.ID, done, "full-sync"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-bulk:
	case <-time.After(5 * time.Second):
		t.Fatal("full sync did not reach source enumeration")
	}
	if during := a.store.snapshot().Tasks[0]; !during.STRMFullSync || !during.NextRun.IsZero() {
		t.Fatal("scheduled life mode still has a next run during full sync", during)
	}
	restartAt := time.Now()
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if after := reopened.snapshot().Tasks[0]; after.STRMFullSync || after.Status != "interrupted" || after.NextRun.Before(nextRun(task, restartAt)) || after.NextRun.After(nextRun(task, time.Now())) {
		t.Fatal("restart retained the transient full sync pause", after)
	}
	stopAt := time.Now()
	cancel()
	<-done
	if after := a.store.snapshot().Tasks[0]; after.STRMFullSync || !after.Enabled || after.Cron != task.Cron || after.NextRun.Before(nextRun(task, stopAt)) || after.NextRun.After(nextRun(task, time.Now())) {
		t.Fatal("stopping full sync did not restore the cron schedule", after)
	}
}

func TestSTRMSyncQueueCancelsOnStopAndConfigChange(t *testing.T) {
	for _, change := range []string{"stop", "config"} {
		t.Run(change, func(t *testing.T) {
			a, _, task := strmModeFixture(t)
			task.STRMSource = "life"
			a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
			a.running[task.ID] = func() {}
			a.runningTriggers = map[string]string{task.ID: "life-monitor"}
			if err := a.startTaskWithTrigger(a.ctx, task.ID, nil, "full-sync"); !errors.Is(err, errSTRMSyncQueued) {
				t.Fatal(err)
			}
			if change == "stop" {
				r, _ := http.NewRequest("POST", "/", nil)
				r.SetPathValue("id", task.ID)
				r.SetPathValue("action", "stop")
				w := &scrapeQueueResponse{header: http.Header{}}
				a.taskAction(w, r)
			} else {
				a.store.update(func(st *State) error { st.Tasks[0].Target = t.TempDir(); return nil })
			}
			delete(a.running, task.ID)
			delete(a.runningTriggers, task.ID)
			a.drainSTRMSync(task.ID)
			if len(a.running) != 0 || len(a.pendingSTRMSync) != 0 {
				t.Fatal("cancelled queue started")
			}
		})
	}
}

func jsonHTTPResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
