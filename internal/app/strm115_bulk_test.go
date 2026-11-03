package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		case "/app/chrome/downfolders":
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
