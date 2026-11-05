package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func strmBulkFixture(t *testing.T, n int, broken string) (*App, Storage, Task, *int) {
	t.Helper()
	a, s, _ := strm115Fixture(t)
	task := Task{ID: "file-bulk", Name: "批量目录", Kind: "strm", StorageID: s.ID, Source: "10", SourceLabel: "Show", Target: t.TempDir(), Mode: strmUpdate, STRMSource: "scan", MediaExtensions: "mkv", Cron: "*/15 * * * *"}
	a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil })
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if r.Method != "GET" {
			t.Fatal("bulk enumeration attempted a write", r.Method, r.URL.Path)
		}
		var response any
		switch r.URL.Path {
		case "/files/get_info":
			response = map[string]any{"state": true, "data": []any{map[string]any{"cid": "10", "pid": "0", "n": "Show", "pc": "folderpick12345"}}}
		case "/files/downfolders":
			dirs := []any{}
			for i := 0; i < n; i++ {
				parent := "10"
				if broken == "orphan" {
					parent = "999999"
				}
				dirs = append(dirs, map[string]any{"fid": strconv.Itoa(i + 20), "pid": parent, "fn": fmt.Sprintf("Season%04d", i)})
			}
			response = map[string]any{"state": true, "data": map[string]any{"list": dirs, "has_next_page": false}}
		case "/files":
			q := r.URL.Query()
			if q.Get("show_dir") == "0" {
				// Web files defaults to the current directory. Only cur=0 includes descendants.
				if q.Get("cur") != "0" {
					return `{"state":true,"cid":"10","offset":0,"count":0,"data":[]}`
				}
				offset, _ := strconv.Atoi(q.Get("offset"))
				items := []any{}
				for i := offset; i < min(offset+1150, n); i++ {
					fid := strconv.Itoa(i + 10000)
					if broken == "duplicate" {
						fid = "10000"
					}
					pick := fmt.Sprintf("pick%012d", i)
					if broken == "missing-pickcode" {
						pick = ""
					}
					items = append(items, map[string]any{"fid": fid, "cid": strconv.Itoa(i + 20), "n": "film.mkv", "pc": pick, "s": 42})
				}
				cid := "10"
				if broken == "wrong-root" {
					cid = "0"
				}
				count := n
				if broken == "changing" && offset > 0 {
					count++
				}
				if broken == "truncated" && offset > 0 {
					items = []any{}
				}
				response = map[string]any{"state": true, "cid": cid, "offset": offset, "count": count, "data": items}
			} else {
				if q.Get("cid") != "10" {
					t.Fatal("fallback left requested scope", q)
				}
				response = map[string]any{"state": true, "cid": "10", "offset": 0, "count": 1, "data": []any{map[string]any{"fid": "10001", "cid": "10", "n": "fallback.mkv", "pc": "fallbackpick123", "s": 42}}}
			}
		default:
			t.Fatal("unexpected bulk endpoint", r.URL.Path)
		}
		raw, _ := json.Marshal(response)
		return string(raw)
	})
	return a, s, task, &calls
}

func Test115STRMBulkSubtreeReducesDirectoryRequests(t *testing.T) {
	a, s, task, calls := strmBulkFixture(t, 1200, "")
	plan, total, err := a.scanTaskProgress(a.ctx, task, s, "Show", true)
	if err != nil || total != 1200 || len(plan) != 1201 || *calls != 4 {
		t.Fatal("expected one root lookup, one directory table and two file pages", len(plan), total, *calls, err)
	}
	for _, f := range plan["20"] {
		if f.PickCode != "pick000000000000" || f.Name != "film.mkv" {
			t.Fatal("bulk list lost playback identity", f)
		}
	}
}

func Test115STRMBulkUsesWebFoldersAndVersionedFallback(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(fmt.Sprint(fallback), func(t *testing.T) {
			a, s, task, _ := strmBulkFixture(t, 2, "")
			original := apiClient.Transport
			var endpoints []string
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "downfolders") {
					endpoints = append(endpoints, r.URL.Host+r.URL.Path)
					if r.URL.Path == "/files/downfolders" && fallback {
						return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("unavailable")), Request: r}, nil
					}
					if r.URL.Path == "/app/2.0/chrome/downfolders" {
						clone := r.Clone(r.Context())
						copyURL := *r.URL
						copyURL.Host = "webapi.115.com"
						copyURL.Path = "/files/downfolders"
						clone.URL = &copyURL
						return original.RoundTrip(clone)
					}
				}
				return original.RoundTrip(r)
			})
			defer func() { apiClient.Transport = original }()
			plan, err := a.strm115BulkPlan(a.ctx, s, task.Source)
			if err != nil || len(plan) != 3 {
				t.Fatal(plan, err)
			}
			expected := 1
			if fallback {
				expected = 2
			}
			if len(endpoints) != expected || endpoints[0] != "webapi.115.com/files/downfolders" {
				t.Fatal("wrong primary bulk endpoint", endpoints)
			}
			if fallback && endpoints[1] != "proapi.115.com/app/2.0/chrome/downfolders" {
				t.Fatal("fallback omitted API version", endpoints)
			}
		})
	}
}

func Test115STRMBulkRootUsesSubtreePages(t *testing.T) {
	a, s, _, _ := strmBulkFixture(t, 2, "")
	base := apiClient.Transport
	var rootReads int
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/files" && r.URL.Query().Get("cid") == "0" {
			rootReads++
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"state":true,"cid":"0","count":2,"offset":0,"data":[{"cid":"10","pid":"0","n":"Show","pc":"folderpick12345"},{"fid":"9","cid":"0","n":"root.mkv","pc":"rootpick123456","s":10}]}`)), Request: r}, nil
		}
		return base.RoundTrip(r)
	})
	defer func() { apiClient.Transport = base }()
	plan, err := a.strm115BulkPlan(a.ctx, s, "/")
	if err != nil || rootReads != 1 || len(plan) != 4 || len(plan["/"]) != 2 || plan["20"][0].PickCode != "pick000000000000" {
		t.Fatal("root fell back to full DFS or lost pickcodes", len(plan), rootReads, err)
	}
}

func Test115STRMLifeFullSyncTenThousandWithOneSecondAPI(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 1, "")
	task.STRMSource = "life"
	task.fullSyncGeneration = true
	task.APIInterval = 1000
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	a.intervals[s.ID] = 1000
	base := apiClient.Transport
	var calls int
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/behavior/detail" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"state":true,"data":{"list":[]}}`)), Request: r}, nil
		}
		if r.URL.Path == "/files" && r.URL.Query().Get("cur") == "0" {
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			list := []any{}
			for i := offset; i < min(10000, offset+1150); i++ {
				list = append(list, map[string]any{"fid": strconv.Itoa(10000 + i), "cid": "20", "n": fmt.Sprintf("Episode%05d.mkv", i), "pc": fmt.Sprintf("pick%012d", i), "s": 42})
			}
			raw, _ := json.Marshal(map[string]any{"state": true, "cid": "10", "count": 10000, "offset": offset, "data": list})
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
		}
		return base.RoundTrip(r)
	})
	defer func() { apiClient.Transport = base }()
	started := time.Now()
	n, err := a.executeTask(a.ctx, task, s)
	if err != nil || n != 10000 || calls != 12 {
		t.Fatal("life full sync used per-file cloud calls or omitted pages", n, calls, err)
	}
	for i := 0; i < 10000; i++ {
		raw, err := os.ReadFile(filepath.Join(task.Target, "Show", "Season0000", fmt.Sprintf("Episode%05d.strm", i)))
		if err != nil || !strings.Contains(string(raw), fmt.Sprintf("/pick%012d/", i)) {
			t.Fatal("output missing exact pickcode", i, err)
		}
	}
	t.Logf("10000 STRM with 1s API gate: %s, %d cloud requests; local fixture, not NAS/cloud timing", time.Since(started), calls)
}

func Test115STRMBulkWritesPickcodeOutputsLocally(t *testing.T) {
	a, s, task, calls := strmBulkFixture(t, 2, "")
	n, err := a.executeTask(a.ctx, task, s)
	if err != nil || n != 2 || *calls != 3 {
		t.Fatal(n, *calls, err)
	}
	for i := 0; i < 2; i++ {
		body, err := os.ReadFile(filepath.Join(task.Target, "Show", fmt.Sprintf("Season%04d", i), "film.strm"))
		if err != nil || !strings.Contains(string(body), fmt.Sprintf("/pick%012d/film.mkv", i)) {
			t.Fatal("wrong bulk output", string(body), err)
		}
	}
	if _, err := os.Stat(filepath.Join(task.Target, "Show", strmOwnerName(task.ID))); !os.IsNotExist(err) {
		t.Fatal("bulk generation created a visible ownership file", err)
	}
	h, err := a.beginSTRMSync(task, s, task.Target, "Show")
	if err != nil || h.manifest.RootIdentity == "" {
		t.Fatal("bulk generation lost internal directory identity", err)
	}
}

func Test115STRMBulkFullSyncCoversEveryPageAndRepairsMissingOutput(t *testing.T) {
	a, s, task, calls := strmBulkFixture(t, 1200, "")
	task.fullSyncGeneration = true
	started := time.Now()
	n, err := a.executeTask(a.ctx, task, s)
	if err != nil || n != 1200 || *calls != 4 {
		t.Fatal("full sync lost recursive pages", n, *calls, err)
	}
	for i := 0; i < 1200; i++ {
		body, err := os.ReadFile(filepath.Join(task.Target, "Show", fmt.Sprintf("Season%04d", i), "film.strm"))
		if err != nil || !strings.Contains(string(body), fmt.Sprintf("/pick%012d/film.mkv", i)) {
			t.Fatal("missing/wrong pickcode", i, err)
		}
	}
	missing := filepath.Join(task.Target, "Show", "Season1199", "film.strm")
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	n, err = a.executeTask(a.ctx, task, s)
	if err != nil || n != 1 || *calls != 8 {
		t.Fatal("second sync did not repair only missing output", n, *calls, err)
	}
	if _, err := os.Stat(missing); err != nil {
		t.Fatal(err)
	}
	t.Logf("1200 descendants / two recursive pages, full generation + repair: %s; cloud requests: %d", time.Since(started), *calls)
}

func Test115STRMBulkRejectsIncompletePlansAndFallsBack(t *testing.T) {
	for _, broken := range []string{"orphan", "duplicate", "wrong-root", "changing", "truncated", "missing-pickcode"} {
		t.Run(broken, func(t *testing.T) {
			a, s, task, _ := strmBulkFixture(t, 1200, broken)
			if plan, err := a.strm115BulkPlan(a.ctx, s, task.Source); err == nil || plan != nil {
				t.Fatal("unsafe bulk plan accepted", broken, err)
			}
			plan, n, err := a.scanTaskProgress(a.ctx, task, s, "Show", true)
			if err != nil || n != 1 || len(plan["10"]) != 1 || plan["10"][0].Name != "fallback.mkv" {
				t.Fatal("failed bulk snapshot leaked into fallback", n, err)
			}
			ctx, cancel := context.WithCancel(a.ctx)
			cancel()
			if _, _, err := a.scanTaskProgress(ctx, task, s, "Show", true); err == nil {
				t.Fatal("canceled bulk task continued")
			}
		})
	}
}

func Test115STRMBulkNormalMissingAndUpdateUseSamePickcodeList(t *testing.T) {
	for _, mode := range []string{strmMissing, strmUpdate} {
		t.Run(mode, func(t *testing.T) {
			a, s, task, calls := strmBulkFixture(t, 2, "")
			task.Mode = mode
			a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
			name := filepath.Join(task.Target, "Show", "Season0000", "film.strm")
			writeTest(t, name, "existing link\n")
			n, err := a.executeTask(a.ctx, task, s)
			body, readErr := os.ReadFile(name)
			if err != nil || readErr != nil || *calls != 3 {
				t.Fatal(n, *calls, err, readErr)
			}
			if mode == strmMissing {
				if n != 1 || string(body) != "existing link\n" {
					t.Fatal("missing mode changed an existing file", n, string(body))
				}
			} else if n != 2 || !strings.Contains(string(body), "/pick000000000000/film.mkv") {
				t.Fatal("update mode lost pickcode", n, string(body))
			}
		})
	}
}

func Test115STRMRequestIntervalGateHonorsZeroAndSharedMaximum(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 0, "")
	a.running[task.ID] = func() {}
	a.refreshTaskInterval(s.ID)
	if a.intervals[s.ID] != 0 {
		t.Fatal("zero cooldown was clamped", a.intervals[s.ID])
	}
	if err := a.waitAPI(a.ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if a.gates[s.ID].After(time.Now()) {
		t.Fatal("zero gate reserved a delay", a.gates[s.ID])
	}
	task.APIInterval = 1500
	a.store.update(func(st *State) error { st.Tasks[0] = task; return nil })
	a.refreshTaskInterval(s.ID)
	a.gates[s.ID] = time.Now()
	before := a.gates[s.ID]
	if err := a.waitAPI(a.ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if reserved := a.gates[s.ID].Sub(before); reserved < 1500*time.Millisecond {
		t.Fatal("configured cooldown ignored", reserved)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	cancel()
	if err := a.waitAPI(ctx, s.ID); err == nil {
		t.Fatal("canceled queued request proceeded")
	}
	other := task
	other.ID, other.Kind, other.APIInterval = "other", "cache", 2200
	a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, other); return nil })
	a.running[other.ID] = func() {}
	a.refreshTaskInterval(s.ID)
	if a.intervals[s.ID] != 2200 {
		t.Fatal("shared stricter cooldown ignored", a.intervals[s.ID])
	}
	delete(a.running, other.ID)
	delete(a.running, task.ID)
	a.refreshTaskInterval(s.ID)
	if a.intervals[s.ID] != 200 {
		t.Fatal("idle pool lost its original limit", a.intervals[s.ID])
	}
	other.StorageID, other.Kind, other.APIInterval = s.ID, "cache", 0
	a.store.update(func(st *State) error { st.Tasks = []Task{other}; return nil })
	a.running[other.ID] = func() {}
	a.refreshTaskInterval(s.ID)
	if a.intervals[s.ID] != 200 {
		t.Fatal("non-STRM task lost its original minimum", a.intervals[s.ID])
	}
}

func TestSTRMSettingsReadDoesNotCloneTaskRuntime(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.Settings.STRMURL = "https://strm.test"
		for i := 0; i < 2000; i++ {
			st.Tasks = append(st.Tasks, Task{ID: strconv.Itoa(i), Name: "large task runtime", Message: strings.Repeat("progress", 32)})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(20, func() {
		if got := a.currentSTRMSettings(); got.Address != "https://strm.test" || got.Content != "original" {
			t.Fatal(got)
		}
	})
	legacy := testing.AllocsPerRun(5, func() { _ = a.store.snapshotWithLogLimit(0).Settings })
	t.Logf("per-file settings allocations: %.0f; full-state snapshot: %.0f", allocs, legacy)
	if allocs > 10 {
		t.Fatal("per-file settings still clone task runtime", allocs)
	}
}

func Test115STRMBatchKeepsPublicationGuardsAndMarksWorkOnce(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 0, "")
	h, err := a.beginSTRMSync(task, s, task.Target, "Show")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.finish(a.ctx, false); err != nil {
			t.Error(err)
		}
	}()
	root, err := os.OpenRoot(task.Target)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	jobs := make([]strm115Output, 32)
	for i := range jobs {
		jobs[i] = strm115Output{file: File{ID: strconv.Itoa(i + 100), Name: fmt.Sprintf("episode%02d.mkv", i), PickCode: fmt.Sprintf("pick%012d", i)}, name: fmt.Sprintf("Show/Season/episode%02d.strm", i)}
	}
	var marks atomic.Int32
	ctx := context.WithValue(a.ctx, strmLifeWorkKey{}, func() error { marks.Add(1); return nil })
	changed, processed := 0, 0
	progress := func(wrote bool) {
		processed++
		if wrote {
			changed++
		}
	}
	if err := a.publish115Batch(ctx, h, s, root, jobs, false, progress); err != nil {
		t.Fatal(err)
	}
	if changed != 32 || processed != 32 || marks.Load() != 1 || len(h.manifest.Files) != 32 {
		t.Fatal(changed, processed, marks.Load(), len(h.manifest.Files))
	}
	for _, job := range jobs {
		ref, err := a.readSTRMReference("333554321/" + job.file.PickCode)
		if err != nil || ref.File != job.file.ID {
			t.Fatal("reference was not registered before publication", ref, err)
		}
	}
	modified := filepath.Join(task.Target, filepath.FromSlash(jobs[0].name))
	if err := os.WriteFile(modified, []byte("external edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, processed = 0, 0
	if err := a.publish115Batch(ctx, h, s, root, jobs, false, progress); err != nil {
		t.Fatal(err)
	}
	if changed != 0 || processed != 32 || marks.Load() != 1 {
		t.Fatal("unchanged/external outputs rewritten", changed, processed, marks.Load())
	}
	body, err := os.ReadFile(modified)
	if err != nil || string(body) != "external edit\n" {
		t.Fatal("external edit overwritten", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := a.publish115Batch(canceled, h, s, root, jobs, false, progress); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func Test115STRMBatchArchiveFailurePublishesNothing(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 0, "")
	h, err := a.beginSTRMSync(task, s, task.Target, "Show")
	if err != nil {
		t.Fatal(err)
	}
	defer h.finish(a.ctx, false)
	root, err := os.OpenRoot(task.Target)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	jobs := make([]strm115Output, 16)
	for i := range jobs {
		jobs[i] = strm115Output{file: File{ID: strconv.Itoa(i + 100), Name: "film.mkv", PickCode: fmt.Sprintf("pick%012d", i)}, name: fmt.Sprintf("Show/%d.strm", i)}
	}
	var marks atomic.Int32
	failure := errors.New("archive unavailable")
	ctx := context.WithValue(a.ctx, strmLifeWorkKey{}, func() error { marks.Add(1); return failure })
	completed := 0
	err = a.publish115Batch(ctx, h, s, root, jobs, false, func(bool) { completed++ })
	if !errors.Is(err, failure) || marks.Load() != 1 || completed != 0 || len(h.manifest.Files) != 0 {
		t.Fatal("failed archive authorized publication", err, marks.Load(), completed, len(h.manifest.Files))
	}
	if _, err := os.Stat(filepath.Join(task.Target, "Show")); !os.IsNotExist(err) {
		t.Fatal("outputs created despite archive failure", err)
	}
}
