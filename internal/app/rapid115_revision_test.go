package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRapid115DeleteAfterRetriesPreservesUnsafeSources(t *testing.T) {
	for _, mode := range []string{"delete", "changed", "same-metadata", "changed-between-retries", "settings", "cancel", "unknown", "prewrite"} {
		t.Run(mode, func(t *testing.T) {
			a, job := rapid115Fixture(t)
			job.FailureAction, job.SuccessAction = "delete", "keep"
			now := time.Now()
			a.rapid115Now = func() time.Time { return now }
			file := filepath.Join(job.Source, "film.mkv")
			rapid115Put(t, file, "media")
			originalTime := now.Add(-time.Minute)
			_ = os.Chtimes(file, originalTime, originalTime)
			if err := a.store.update(func(st *State) error {
				st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "delete", Mode: "cron", Cron: "0 */3 * * *", Enabled: true}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(a.ctx)
			defer cancel()
			calls := 0
			a.rapid115Upload = func(_ context.Context, _ Storage, _, _ string, _ *os.File, _ int64, allow bool) (bool, string, error) {
				calls++
				if allow {
					t.Error("delete policy must never upload media")
				}
				if calls == 4 {
					switch mode {
					case "changed":
						rapid115Put(t, file, "changed media")
					case "same-metadata":
						rapid115Put(t, file, "other")
						_ = os.Chtimes(file, originalTime, originalTime)
					case "settings":
						_ = a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil })
					case "cancel":
						cancel()
						return false, "", ctx.Err()
					case "unknown":
						return false, "", errors.New("private unknown cloud result")
					case "prewrite":
						return false, "", &rapid115KnownFailure{errors.New("private authorization unavailable")}
					}
				}
				return false, "", nil
			}
			revision := rapid115Stamp(a.store.snapshot().Rapid115)
			for retry := 0; retry < 4; retry++ {
				err := a.runRapid115(ctx, job, "watch", "监控 · delete", revision)
				if err != nil && !(mode == "cancel" && errors.Is(err, context.Canceled)) {
					t.Fatal(err)
				}
				record := a.store.snapshot().Rapid115Records[0]
				if retry < 3 {
					delay := []time.Duration{10 * time.Minute, 30 * time.Minute, 60 * time.Minute}[retry]
					if record.Status != "retrying" || !record.NextRetry.Equal(now.Add(delay)) {
						t.Fatal("delete policy lost retry schedule", record)
					}
					if _, err := os.Stat(file); err != nil {
						t.Fatal("deleted before retry exhaustion", err)
					}
					now = record.NextRetry
					if mode == "changed-between-retries" && retry == 2 {
						rapid115Put(t, file, "other")
						_ = os.Chtimes(file, originalTime, originalTime)
					}
				} else {
					_, statErr := os.Stat(file)
					if (mode == "delete") != os.IsNotExist(statErr) {
						t.Fatal("unsafe terminal deletion", mode, record, statErr)
					}
					if mode == "delete" && (record.Status != "deleted" || !strings.Contains(record.Message, "重试3次")) {
						t.Fatal("missing delete result", record)
					}
					if strings.Contains(record.Message, "private") || len(a.store.snapshot().Rapid115Retries) != 0 {
						t.Fatal("unsafe result or retained retry", record)
					}
				}
			}
			wantCalls := 4
			if mode == "changed-between-retries" {
				wantCalls = 3
			}
			if calls != wantCalls {
				t.Fatal("incorrect number of attempts", calls)
			}
		})
	}
}

func TestRapid115RetryCronStrictlyExceeds120Minutes(t *testing.T) {
	a, job := rapid115Fixture(t)
	for _, policy := range []string{"upload", "delete"} {
		for _, item := range []struct {
			cron string
			ok   bool
		}{
			{"*/30 * * * *", false},
			{"0 */2 * * *", false},
			{"0 0,2,6 * * *", false},
			{"0 0,23 * * *", false},
			{"0 */3 * * *", true},
			{"1 0,3,6,9,12,15,18,21 * * *", true},
			{"0 0,23 * * 1", true},
			{"0 0 29 2 *", true},
			{"TZ=America/New_York 0 0,3 * * *", false},
			{"TZ=UTC 0 */3 * * *", true},
		} {
			t.Run(policy+"/"+item.cron, func(t *testing.T) {
				value := Rapid115Settings{Manual: job, Watches: []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "cron", Mode: "cron", Cron: item.cron, Enabled: true}}}
				value.Watches[0].FailureAction = policy
				err := validateRapid115(value, a.store.snapshot())
				if (err == nil) != item.ok || !item.ok && !strings.Contains(err.Error(), "120") {
					t.Fatal("incorrect minimum Cron interval", err)
				}
			})
		}
	}
	value := Rapid115Settings{Manual: job, Watches: []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "skip", Mode: "cron", Cron: "* * * * *", Enabled: true}}}
	if err := validateRapid115(value, a.store.snapshot()); err != nil {
		t.Fatal("skip policy acquired a retry restriction", err)
	}
	value.Manual.FailureAction = "delete"
	if err := validateRapid115(value, a.store.snapshot()); err == nil {
		t.Fatal("monitor-only deletion enabled in manual mode")
	}
}

func TestRapid115RecordsPageBounds(t *testing.T) {
	a, _ := rapid115Fixture(t)
	if err := a.store.update(func(st *State) error {
		for i := 0; i < 65; i++ {
			st.Rapid115Records = append(st.Rapid115Records, Rapid115Record{ID: fmt.Sprint(i), Name: fmt.Sprint(i), Status: "failed"})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, item := range []struct{ requested, page, count, first int }{{1, 1, 30, 0}, {2, 2, 30, 30}, {3, 3, 5, 60}, {99999, 3, 5, 60}, {0, 1, 30, 0}} {
		response := request(t, h, "GET", fmt.Sprintf("/api/115-rapid/records?page=%d", item.requested), nil, cookie)
		var value struct {
			Items []Rapid115Record `json:"items"`
			Page  int              `json:"page"`
			Pages int              `json:"pages"`
			Total int              `json:"total"`
		}
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &value) != nil || value.Page != item.page || value.Pages != 3 || value.Total != 65 || len(value.Items) != item.count || value.Items[0].ID != fmt.Sprint(item.first) {
			t.Fatal("incorrect record page", response.Code, response.Body.String())
		}
	}
}

func TestRapid115ManualAndMonitorRunIndependently(t *testing.T) {
	a, watch := rapid115Fixture(t)
	manual := watch
	manual.Source, manual.SuccessAction, watch.SuccessAction = t.TempDir(), "keep", "keep"
	for _, job := range []Rapid115Job{manual, watch} {
		file := filepath.Join(job.Source, "film.mkv")
		rapid115Put(t, file, "media")
		old := time.Now().Add(-time.Minute)
		_ = os.Chtimes(file, old, old)
	}
	if err := a.store.update(func(st *State) error {
		st.Rapid115 = Rapid115Settings{Manual: manual, Watches: []Rapid115Watch{{Rapid115Job: watch, ID: "watch", Name: "monitor", Mode: "watch", Enabled: true}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	gate, entered := make(chan struct{}), make(chan struct{}, 2)
	defer func() { close(gate); cancel(); a.wg.Wait() }()
	a.rapid115Upload = func(ctx context.Context, _ Storage, _, _ string, _ *os.File, _ int64, _ bool) (bool, string, error) {
		entered <- struct{}{}
		select {
		case <-gate:
			return true, "pick", nil
		case <-ctx.Done():
			return false, "", ctx.Err()
		}
	}
	revision := rapid115Stamp(a.store.snapshot().Rapid115)
	if !a.startRapid115(watch, "watch", "监控 · monitor", revision) {
		t.Fatal("monitor did not start")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not reach upload")
	}
	status := httptest.NewRecorder()
	a.rapid115Records(status, httptest.NewRequest("GET", "/api/115-rapid/records", nil))
	var channels struct {
		Manual  bool `json:"manualRunning"`
		Monitor bool `json:"monitorRunning"`
	}
	if json.Unmarshal(status.Body.Bytes(), &channels) != nil || channels.Manual || !channels.Monitor {
		t.Fatal("monitor reported as manual activity", status.Body.String())
	}
	if !a.startRapid115(manual, "manual", "手动", revision) {
		t.Fatal("monitor blocked manual start")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manual did not run while monitor was active")
	}
	if a.startRapid115(manual, "manual", "手动", revision) || a.startRapid115(watch, "watch", "监控 · monitor", revision) {
		t.Fatal("same channel started twice")
	}
}

func TestRapid115ConcurrentSameFileIsNotSubmittedTwice(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.SuccessAction = "keep"
	file := filepath.Join(job.Source, "film.mkv")
	rapid115Put(t, file, "media")
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(file, old, old)
	if err := a.store.update(func(st *State) error {
		st.Rapid115 = Rapid115Settings{Manual: job, Watches: []Rapid115Watch{{Rapid115Job: job, ID: "watch", Name: "same", Mode: "watch", Enabled: true}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gate, entered := make(chan struct{}), make(chan struct{}, 2)
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	defer func() { close(gate); cancel(); a.wg.Wait() }()
	a.rapid115Upload = func(ctx context.Context, _ Storage, _, _ string, _ *os.File, _ int64, _ bool) (bool, string, error) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-gate:
			return true, "pick", nil
		case <-ctx.Done():
			return false, "", ctx.Err()
		}
	}
	revision := rapid115Stamp(a.store.snapshot().Rapid115)
	if !a.startRapid115(job, "watch", "监控 · same", revision) {
		t.Fatal("monitor did not start")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not enter")
	}
	if !a.startRapid115(job, "manual", "手动", revision) {
		t.Fatal("manual channel blocked")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.rapid115Mu.Lock()
		active := a.rapid115Runs["manual"]
		a.rapid115Mu.Unlock()
		if !active {
			if calls.Load() != 1 || len(a.store.snapshot().Rapid115Records) != 1 {
				t.Fatal("same source was submitted by both channels")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("manual did not skip the active file")
}

func TestRapid115LiveProgressUsesActualBytes(t *testing.T) {
	a, _ := rapid115Fixture(t)
	record := Rapid115Record{ID: "live", Name: "film.mkv", Status: "running", Size: 6}
	if fresh, err := a.rapid115Intent("live", record); !fresh || err != nil {
		t.Fatal(fresh, err)
	}
	ctx := context.WithValue(a.ctx, rapid115ReportKey{}, func(phase string, done, total int64) {
		a.rapid115Mu.Lock()
		defer a.rapid115Mu.Unlock()
		a.rapid115Progress = map[string]rapid115Live{record.ID: {Phase: phase, Done: done, Total: total}}
	})
	reader := rapid115ReadProgress(ctx, strings.NewReader("abcdef"), "本地传输", 0, 6)
	if n, err := reader.Read(make([]byte, 2)); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	if live := a.rapid115RecordProgress(record); live.Done != 2 || live.Total != 6 || live.Phase != "本地传输" {
		t.Fatal("progress not derived from bytes", live)
	}
	response := httptest.NewRecorder()
	a.rapid115Records(response, httptest.NewRequest("GET", "/api/115-rapid/records?page=1", nil))
	var output struct {
		Items []Rapid115Record `json:"items"`
	}
	if json.Unmarshal(response.Body.Bytes(), &output) != nil || len(output.Items) != 1 || output.Items[0].Done != 2 || output.Items[0].Phase != "本地传输" {
		t.Fatal("live progress not exposed to polling", response.Body.String())
	}
	if a.store.snapshot().Rapid115Records[0].Done != 0 {
		t.Fatal("per-chunk progress caused persistent writes")
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	rapid115Report(ctx, "提交秒传", 0, 0)
	if live := a.rapid115RecordProgress(record); live.Done != 0 || live.Total != 0 || live.Phase != "提交秒传" {
		t.Fatal("unknown API phase invented progress", live)
	}
	record.Status, record.Message = "success", "秒传成功"
	if err := a.rapid115Finish(record); err != nil {
		t.Fatal(err)
	}
	if got := a.store.snapshot().Rapid115Records[0]; got.Done != 6 || got.Total != 6 {
		t.Fatal("confirmed completion lost real size", got)
	}
}

func TestRapid115LegacyShortCronDoesNotPreventStartup(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.FailureAction = "upload"
	if err := a.store.update(func(st *State) error {
		st.Rapid115.Watches = []Rapid115Watch{{Rapid115Job: job, ID: "old", Name: "legacy", Mode: "cron", Cron: "0 */2 * * *", Enabled: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal("legacy interval prevented container startup", err)
	}
	if err := validateRapid115(reopened.store.snapshot().Rapid115, reopened.store.snapshot()); err == nil {
		t.Fatal("editing legacy interval bypassed new constraint")
	}
}

func TestRapid115LateProgressCannotRestoreFinishedRuntime(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.SuccessAction = "keep"
	rapid115Put(t, filepath.Join(job.Source, "film.mkv"), "media")
	if err := a.store.update(func(st *State) error { st.Rapid115.Manual = job; return nil }); err != nil {
		t.Fatal(err)
	}
	var previous context.Context
	a.rapid115Upload = func(ctx context.Context, _ Storage, _, _ string, _ *os.File, _ int64, _ bool) (bool, string, error) {
		previous = ctx
		return false, "", nil
	}
	if err := a.runRapid115(a.ctx, job, "manual", "手动", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	rapid115Report(previous, "本地传输", 5, 5)
	a.rapid115Mu.Lock()
	defer a.rapid115Mu.Unlock()
	if len(a.rapid115Progress) != 0 {
		t.Fatal("late callback restored a finished record's runtime")
	}
}
