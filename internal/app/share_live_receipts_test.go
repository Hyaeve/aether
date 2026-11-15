package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mockShareLiveQuarkRun(t *testing.T, targetRead func(string) string, save func() string, task func() string) {
	t.Helper()
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			parent := r.URL.Query().Get("pdir_fid")
			if parent == "parent" {
				return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
			}
			if parent == "saved-root" {
				return `{"code":0,"data":{"list":[{"fid":"dest-one","file_name":"Season 1","dir":true},{"fid":"dest-two","file_name":"Season 2","dir":true}]}}`
			}
			return targetRead(parent)
		case "/1/clouddrive/share/sharepage/token":
			return `{"code":0,"data":{"stoken":"share-token"}}`
		case "/1/clouddrive/share/sharepage/detail":
			switch r.URL.Query().Get("pdir_fid") {
			case "0":
				return `{"code":0,"data":{"list":[{"fid":"source-root","file_name":"Original","dir":true}]}}`
			case "source-root":
				return `{"code":0,"data":{"list":[{"fid":"source-one","file_name":"Season 1","dir":true},{"fid":"source-two","file_name":"Season 2","dir":true}]}}`
			case "source-one":
				return `{"code":0,"data":{"list":[{"fid":"one","file_name":"one.mkv","share_fid_token":"one-token"}]}}`
			case "source-two":
				return `{"code":0,"data":{"list":[{"fid":"two","file_name":"two.mkv","share_fid_token":"two-token"}]}}`
			}
		case "/1/clouddrive/share/sharepage/save":
			return save()
		case "/1/clouddrive/task":
			return task()
		}
		t.Error("unexpected request", r.Method, r.URL.Path)
		return `{"code":500}`
	})
}

func TestShareLiveReadFailureDoesNotClaimUncertainWrite(t *testing.T) {
	a, _, link := liveTestSetup(t)
	var writes atomic.Int32
	mockShareLiveQuarkRun(t, func(string) string { return `{"code":500}` }, func() string {
		writes.Add(1)
		return `{"code":0,"data":{"task_id":"task"}}`
	}, func() string { return `{"code":0,"data":{"status":2}}` })
	a.executeShareLive(a.ctx, link, shareTransferRecord{ID: "preflight", Time: time.Now()})
	current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
	if writes.Load() != 0 || current.Status != "failed" || current.Intent != "" {
		t.Fatal("read-only preflight left an unconfirmed write", writes.Load(), current.Status, current.Intent)
	}
	if err := a.startShareLive(link.ID, "manual"); err != nil {
		t.Fatal("read-only failure blocked a new authorized run", err)
	}
	a.wg.Wait()
	if writes.Load() != 0 {
		t.Fatal("retry submitted without reading its target")
	}
}

func TestShareLiveDirectoryIntentStartsAtTheNativeWrite(t *testing.T) {
	for _, scenario := range []string{"preflight-error", "unknown-write", "confirmed"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			link.DirectoryID = ""
			if err := a.store.update(func(st *State) error { st.ShareLiveLinks[0] = link; return nil }); err != nil {
				t.Fatal(err)
			}
			var reads, writes int
			shareMock(t, func(r *http.Request) string {
				if r.Method == "GET" && r.URL.Path == "/1/clouddrive/file/sort" {
					reads++
					if reads == 2 && scenario == "preflight-error" {
						return `{"code":500}`
					}
					if writes == 1 {
						return `{"code":0,"data":{"list":[{"fid":"new-directory","file_name":"Saved","dir":true}]}}`
					}
					return `{"code":0,"data":{"list":[]}}`
				}
				if r.Method == "POST" && r.URL.Path == "/1/clouddrive/file" {
					writes++
					if current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]; current.Intent != "mkdir:Saved" {
						t.Error("native mkdir was submitted without a durable intent", current.Intent)
					}
					if scenario == "unknown-write" {
						return `{"code":500}`
					}
					return `{"code":0}`
				}
				t.Error("unexpected mkdir request", r.Method, r.URL.Path)
				return `{"code":500}`
			})
			err := a.initializeShareLiveDirectory(a.ctx, s, &link)
			current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
			switch scenario {
			case "preflight-error":
				if err == nil || writes != 0 || current.Intent != "" || current.DirectoryID != "" {
					t.Fatal("read-only mkdir preflight created an uncertain write", err, writes, current)
				}
			case "unknown-write":
				if err == nil || writes != 1 || current.Intent != "mkdir:Saved" || current.DirectoryID != "" {
					t.Fatal("unknown mkdir lost its write receipt", err, writes, current)
				}
			case "confirmed":
				if err != nil || writes != 1 || current.Intent != "" || current.DirectoryID != "new-directory" {
					t.Fatal("confirmed mkdir did not publish its identity", err, writes, current)
				}
			}
		})
	}
}

func TestShareLiveConfirmedBatchRetainedBeforeLaterReadFailure(t *testing.T) {
	a, _, link := liveTestSetup(t)
	var writes atomic.Int32
	var failSecond atomic.Bool
	failSecond.Store(true)
	mockShareLiveQuarkRun(t, func(parent string) string {
		if parent == "dest-two" && failSecond.Load() {
			return `{"code":500}`
		}
		return `{"code":0,"data":{"list":[]}}`
	}, func() string {
		writes.Add(1)
		return `{"code":0,"data":{"task_id":"task"}}`
	}, func() string { return `{"code":0,"data":{"status":2}}` })
	a.executeShareLive(a.ctx, link, shareTransferRecord{ID: "partial", Time: time.Now()})
	current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
	seen, err := a.loadShareLiveCheckpoint(current)
	if writes.Load() != 1 || err != nil || current.Intent != "" || !seen[shareLiveFileKey("one")] || seen[shareLiveFileKey("two")] {
		t.Fatal("confirmed batch lost its checkpoint or later read left a lock", writes.Load(), err, current.Intent, seen)
	}
	failSecond.Store(false)
	if err := a.startShareLive(link.ID, "manual"); err != nil {
		t.Fatal("later read failure prevented a new run", err)
	}
	a.wg.Wait()
	current = a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
	seen, err = a.loadShareLiveCheckpoint(current)
	if err != nil || writes.Load() != 2 || current.Status != "completed" || !seen[shareLiveFileKey("one")] || !seen[shareLiveFileKey("two")] {
		t.Fatal("retry replayed the confirmed first batch or lost the remaining file", err, writes.Load(), current, seen)
	}
}

func TestShareLiveQuarkTaskReceiptRecoversAcrossRestart(t *testing.T) {
	for _, outcome := range []string{"completed", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			a, _, link := liveTestSetup(t)
			var writes, queries atomic.Int32
			mockShareLiveQuarkRun(t, func(string) string {
				return `{"code":0,"data":{"list":[]}}`
			}, func() string {
				return fmt.Sprintf(`{"code":0,"data":{"task_id":"task-%d"}}`, writes.Add(1))
			}, func() string {
				query := queries.Add(1)
				if query == 1 {
					return `{"code":500}`
				}
				if query == 2 && outcome == "failed" {
					return `{"code":0,"data":{"status":3}}`
				}
				return `{"code":0,"data":{"status":2}}`
			})
			a.executeShareLive(a.ctx, link, shareTransferRecord{ID: "interrupted", Time: time.Now()})
			current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
			if current.Intent == "" || current.TransferTaskID != "task-1" || !shareLiveCursorValid(current.PendingCursor) || writes.Load() != 1 {
				t.Fatal("accepted task receipt was not retained", current.Intent, current.TransferTaskID, current.PendingCursor, writes.Load())
			}
			reopened, err := NewStore(a.store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
				t.Fatal(err)
			}
			a.store = reopened
			if err := a.startShareLive(link.ID, "manual"); err != nil {
				t.Fatal("manual run did not query the saved receipt", err)
			}
			a.wg.Wait()
			current = a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
			seen, err := a.loadShareLiveCheckpoint(current)
			wantWrites := int32(2)
			if outcome == "failed" {
				wantWrites = 3
			}
			if err != nil || current.Status != "completed" || current.Intent != "" || current.TransferTaskID != "" || current.PendingCursor != "" ||
				!seen[shareLiveFileKey("one")] || !seen[shareLiveFileKey("two")] || writes.Load() != wantWrites {
				t.Fatal("task recovery replayed accepted files or kept a stale lock", err, current, seen, writes.Load(), wantWrites)
			}
		})
	}
}

func TestShareLiveRunErrorsIdentifyTheActualLocalState(t *testing.T) {
	for _, scenario := range []struct{ state, want string }{
		{"missing", "记录已移除"}, {"batch:old:0", "缺少可查询的任务回执"},
		{"mkdir:Saved", "创建保存目录"}, {"relocate:parent:Saved", "改名或移动"}, {"delete", "删除目录"},
	} {
		t.Run(scenario.state, func(t *testing.T) {
			a, _, link := liveTestSetup(t)
			if err := a.store.update(func(st *State) error {
				if scenario.state == "missing" {
					st.ShareLiveLinks = nil
				} else {
					st.ShareLiveLinks[0].Intent = scenario.state
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			shareMock(t, func(r *http.Request) string {
				t.Error("unconfirmed operation performed an untracked write", r.Method, r.URL.Path)
				return `{"code":500}`
			})
			if err := a.startShareLive(link.ID, "manual"); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatal("incorrect local state diagnosis", err, scenario.want)
			}
		})
	}
}

func TestShareLiveQuarkReceiptProtection(t *testing.T) {
	for _, scenario := range []string{"query-error", "pending-task", "damaged-checkpoint", "changed-credentials", "changed-directory", "changed-task", "cleared-task", "cancelled", "cancelled-during-query"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			active, err := a.stageShareLiveCheckpoint(link, map[string]bool{}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			pending, err := a.stageShareLiveCheckpoint(link, map[string]bool{shareLiveFileKey("one"): true}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			link.IncrementalCursor, link.Intent, link.TransferTaskID, link.PendingCursor = active, "batch:pending:0", "task-1", pending
			if err := a.store.update(func(st *State) error { st.ShareLiveLinks[0] = link; return nil }); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(a.ctx)
			defer cancel()
			if scenario == "damaged-checkpoint" {
				if err := os.WriteFile(a.shareLiveCheckpointPath(link, pending), []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "changed-credentials" {
				if err := a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			var queries atomic.Int32
			shareMock(t, func(r *http.Request) string {
				if r.Method != "GET" || r.URL.Path != "/1/clouddrive/task" || r.URL.Query().Get("task_id") != "task-1" {
					t.Error("receipt recovery performed an unexpected request", r.Method, r.URL.Path)
					return `{"code":500}`
				}
				queries.Add(1)
				if scenario == "query-error" {
					return `{"code":500}`
				}
				if scenario == "pending-task" {
					cancel()
					return `{"code":0,"data":{"status":1}}`
				}
				if scenario == "cancelled-during-query" {
					cancel()
				}
				if err := a.store.update(func(st *State) error {
					current := &st.ShareLiveLinks[0]
					switch scenario {
					case "changed-directory":
						current.DirectoryName = "Different"
					case "changed-task":
						current.TransferTaskID = "task-2"
					case "cleared-task":
						current.Intent, current.TransferTaskID, current.PendingCursor = "", "", ""
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return `{"code":0,"data":{"status":2}}`
			})
			if scenario == "cancelled" {
				cancel()
			}
			if err := a.reconcileShareLiveTransfer(ctx, &link, s); err == nil {
				t.Fatal("unconfirmed or changed receipt was committed", scenario)
			}
			current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
			if current.IncrementalCursor != active || link.IncrementalCursor != active {
				t.Fatal("failed recovery advanced the confirmed checkpoint", current, link)
			}
			if scenario == "changed-task" && current.TransferTaskID != "task-2" || scenario != "changed-task" && scenario != "cleared-task" && current.TransferTaskID != "task-1" {
				t.Fatal("failed recovery destroyed the pending receipt", current)
			}
			if (scenario == "damaged-checkpoint" || scenario == "changed-credentials" || scenario == "cancelled") && queries.Load() != 0 {
				t.Fatal("invalid local state still queried the cloud", queries.Load())
			}
		})
	}
}

func TestShareLiveStaleRunningFlagWithNoWriteCanResume(t *testing.T) {
	a, _, link := liveTestSetup(t)
	if err := a.store.update(func(st *State) error { st.ShareLiveLinks[0].Status = "running"; return nil }); err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int32
	mockShareLiveQuarkRun(t, func(string) string { return `{"code":500}` }, func() string {
		writes.Add(1)
		return `{"code":0,"data":{"task_id":"task"}}`
	}, func() string { return `{"code":0,"data":{"status":2}}` })
	if err := a.startShareLive(link.ID, "manual"); err != nil {
		t.Fatal("stale flag without an active worker or write could not resume", err)
	}
	a.wg.Wait()
	if writes.Load() != 0 || a.store.snapshotWithLogLimit(0).ShareLiveLinks[0].Intent != "" {
		t.Fatal("read-only retry submitted a write")
	}
}
