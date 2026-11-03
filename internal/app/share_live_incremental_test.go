package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestShareLiveOnlyTransfersNewIdentitiesAfterCheckpoint(t *testing.T) {
	a, _, link := liveTestSetup(t)
	round, writes := 0, 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			if r.URL.Query().Get("pdir_fid") == "parent" {
				return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
			}
			if round == 0 {
				return `{"code":0,"data":{"list":[{"fid":"target-old","file_name":"old.mkv","file_type":1},{"fid":"kept-target","file_name":"removed-from-share.mkv","file_type":1}]}}`
			}
			return `{"code":0,"data":{"list":[{"fid":"kept-target","file_name":"removed-from-share.mkv","file_type":1}]}}`
		case "/1/clouddrive/share/sharepage/token":
			return `{"code":0,"data":{"stoken":"token"}}`
		case "/1/clouddrive/share/sharepage/detail":
			if r.URL.Query().Get("pdir_fid") == "0" {
				return `{"code":0,"data":{"list":[{"fid":"source-root","file_name":"Original","dir":true}]}}`
			}
			if round == 0 {
				return `{"code":0,"data":{"list":[{"fid":"old-source","file_name":"old.mkv","share_fid_token":"old"},{"fid":"vanished-source","file_name":"removed-from-share.mkv","share_fid_token":"vanished"}]}}`
			}
			if round == 2 {
				return `{"code":0,"data":{"list":[{"fid":"new-source","file_name":"new.mkv","share_fid_token":"new"}]}}`
			}
			return `{"code":0,"data":{"list":[{"fid":"old-source","file_name":"renamed-old.mkv","share_fid_token":"old"},{"fid":"new-source","file_name":"new.mkv","share_fid_token":"new"}]}}`
		case "/1/clouddrive/share/sharepage/save":
			var body struct {
				IDs []string `json:"fid_list"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if round != 1 || len(body.IDs) != 1 || body.IDs[0] != "new-source" {
				t.Fatal("re-copied an old source or repeated an existing addition", round, body.IDs)
			}
			writes++
			return `{"code":0,"data":{"task_id":"saved"}}`
		case "/1/clouddrive/task":
			return `{"code":0,"data":{"status":2}}`
		default:
			t.Fatal("unexpected write or request", r.URL.Path)
			return ""
		}
	})
	for round = 0; round < 3; round++ {
		if round > 0 {
			link = a.store.snapshot().ShareLiveLinks[0]
		}
		a.executeShareLive(context.Background(), link, shareTransferRecord{ID: time.Now().String(), LiveID: link.ID, StorageID: link.StorageID, Provider: link.Provider, Time: time.Now()})
		current := a.store.snapshot()
		result := current.ShareTransfers[len(current.ShareTransfers)-1]
		if result.Status != "completed" {
			t.Fatal(round, result)
		}
	}
	if writes != 1 {
		t.Fatal("new file was not copied exactly once", writes)
	}
}

func TestShareLiveCheckpointIntegrityAndLegacyBaseline(t *testing.T) {
	for _, kind := range []string{"legacy", "restart", "missing", "tampered", "source-changed"} {
		t.Run(kind, func(t *testing.T) {
			a, _, link := liveTestSetup(t)
			at := time.Now().Add(-time.Hour)
			link.LastRun = at
			batches := []shareBatch{{Source: "source", Directory: "old/season", Items: []shareEntry{{ID: "old", Name: "old.mkv"}, {ID: "new", Name: "new.mkv"}}}}
			if kind == "legacy" {
				filtered, cursor, skipped, baseline, err := a.planShareLiveIncremental(link, batches, time.Now())
				if err != nil || len(filtered) != 0 || cursor == "" || skipped != 2 || !baseline {
					t.Fatal(filtered, cursor, skipped, baseline, err)
				}
				link.IncrementalCursor = cursor
				filtered, _, _, baseline, err = a.planShareLiveIncremental(link, append(batches, shareBatch{Source: "child", Directory: "new/season", Items: []shareEntry{{ID: "next", Name: "next.mkv"}}}), time.Now())
				if err != nil || len(filtered) != 1 || filtered[0].Directory != "new/season" || filtered[0].Items[0].ID != "next" || baseline {
					t.Fatal("baseline backfilled old files", filtered, err)
				}
				return
			}
			cursor, err := a.stageShareLiveCheckpoint(link, map[string]bool{shareLiveFileKey("old"): true}, at)
			if err != nil {
				t.Fatal(err)
			}
			link.IncrementalCursor = cursor
			link.LastSuccess = at
			switch kind {
			case "missing":
				if err = os.Remove(a.shareLiveCheckpointPath(link, cursor)); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				if err = os.WriteFile(a.shareLiveCheckpointPath(link, cursor), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "source-changed":
				link.Link = "https://pan.quark.cn/s/different"
			case "restart":
				a.store.update(func(st *State) error { st.ShareLiveLinks = []shareLiveLink{link}; return nil })
				reopened, err := NewStore(a.store.dir)
				if err != nil {
					t.Fatal(err)
				}
				if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
					t.Fatal(err)
				}
				link = reopened.snapshot().ShareLiveLinks[0]
				if link.IncrementalCursor != cursor || !link.LastSuccess.Equal(at) {
					t.Fatal("checkpoint lost on restart")
				}
			}
			filtered, _, _, _, err := a.planShareLiveIncremental(link, batches, time.Now())
			if kind == "restart" {
				if err != nil || len(filtered) != 1 || len(filtered[0].Items) != 1 || filtered[0].Items[0].ID != "new" {
					t.Fatal(filtered, err)
				}
			} else if err == nil || len(filtered) != 0 {
				t.Fatal("invalid checkpoint silently reset", kind, filtered)
			}
		})
	}
}

func TestShareLiveAcceptedInitialTransferSeedsCheckpoint(t *testing.T) {
	for _, source := range []string{"browser", "bot"} {
		t.Run(source, func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			a.store.update(func(st *State) error { st.ShareLiveLinks = nil; return nil })
			at := time.Now()
			batches := []shareBatch{{Directory: "Original/Season 1", Items: []shareEntry{{ID: "old", Name: "old.mkv"}}}}
			record := shareTransferRecord{ID: "initial", StorageID: s.ID, Provider: s.Type, DirectoryName: "Saved", Name: "Original", Link: link.Link, Source: source, Time: at, Status: "completed"}
			p := sharePreview{Parent: link.Parent, Destination: link.DirectoryID, Config: shareConfig(s), Items: []shareEntry{{ID: "root", Name: "Original", IsDir: true}}, Batches: batches}
			if err := a.recordShareTransfer(record, p); err != nil {
				t.Fatal(err)
			}
			link = a.store.snapshot().ShareLiveLinks[0]
			if link.IncrementalCursor == "" || !link.LastSuccess.Equal(at) {
				t.Fatal("initial plan not registered", link)
			}
			batches[0].Items = append(batches[0].Items, shareEntry{ID: "new", Name: "new.mkv"})
			filtered, _, skipped, baseline, err := a.planShareLiveIncremental(link, batches, time.Now())
			if err != nil || baseline || skipped != 1 || len(filtered) != 1 || len(filtered[0].Items) != 1 || filtered[0].Items[0].ID != "new" {
				t.Fatal(filtered, baseline, skipped, err)
			}
		})
	}
}

func TestShareLiveCheckpointLimitDoesNotEvictHistory(t *testing.T) {
	a, _, link := liveTestSetup(t)
	seen := map[string]bool{}
	for i := 0; i < shareLiveSeenLimit; i++ {
		seen[shareLiveFileKey(strconv.Itoa(i))] = true
	}
	err := shareLiveObserved([]shareBatch{{Items: []shareEntry{{ID: "over-limit", Name: "new.mkv"}}}}, seen)
	if err == nil || !strings.Contains(err.Error(), "200000") {
		t.Fatal("history silently pruned", err)
	}
	if _, err = a.stageShareLiveCheckpoint(link, seen, time.Now()); err == nil {
		t.Fatal("oversized history published")
	}
}

func TestShareLiveFailedUpdateDoesNotAdvanceCheckpoint(t *testing.T) {
	a, _, link := liveTestSetup(t)
	at := time.Now().Add(-time.Hour)
	cursor, err := a.stageShareLiveCheckpoint(link, map[string]bool{shareLiveFileKey("old"): true}, at)
	if err != nil {
		t.Fatal(err)
	}
	link.IncrementalCursor, link.LastSuccess = cursor, at
	a.store.update(func(st *State) error { st.ShareLiveLinks = []shareLiveLink{link}; return nil })
	writes := 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/1/clouddrive/file/sort":
			if r.URL.Query().Get("pdir_fid") == "parent" {
				return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
			}
			return `{"code":0,"data":{"list":[]}}`
		case "/1/clouddrive/share/sharepage/token":
			return `{"code":0,"data":{"stoken":"token"}}`
		case "/1/clouddrive/share/sharepage/detail":
			if r.URL.Query().Get("pdir_fid") == "0" {
				return `{"code":0,"data":{"list":[{"fid":"source-root","file_name":"Original","dir":true}]}}`
			}
			return `{"code":0,"data":{"list":[{"fid":"old","file_name":"old.mkv","share_fid_token":"old"},{"fid":"new","file_name":"new.mkv","share_fid_token":"new"}]}}`
		case "/1/clouddrive/share/sharepage/save":
			writes++
			return `{"code":500}`
		default:
			t.Fatal(r.URL.Path)
			return ""
		}
	})
	a.executeShareLive(context.Background(), link, shareTransferRecord{ID: "failed-run", Provider: link.Provider, StorageID: link.StorageID, Time: time.Now()})
	current := a.store.snapshot().ShareLiveLinks[0]
	if writes != 1 || current.IncrementalCursor != cursor || !current.LastSuccess.Equal(at) || current.Intent == "" || !current.NextRun.IsZero() {
		t.Fatal("failure advanced or unlocked checkpoint", current, writes)
	}
	if _, err = os.Stat(a.shareLiveCheckpointPath(link, cursor)); err != nil {
		t.Fatal("last successful generation removed", err)
	}
	if err = a.startShareLive(link.ID, "manual"); err == nil {
		t.Fatal("unknown batch replay permitted")
	}
}
