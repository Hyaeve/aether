package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestShareLiveSchedulesAndTargetValidation(t *testing.T) {
	at := time.Date(2026, 10, 29, 13, 19, 0, 0, time.FixedZone("CST", 8*3600))
	want := time.Date(2026, 11, 5, 13, 0, 0, 0, at.Location())
	if got := shareLiveNext(shareLiveLink{IntervalDays: 7}, at); !got.Equal(want) {
		t.Fatal(got, want)
	}
	if !shareLiveNext(shareLiveLink{}, at).IsZero() {
		t.Fatal("zero days scheduled")
	}
	for _, tc := range []struct {
		cron  string
		valid bool
	}{
		{"", true}, {"0 */12 * * *", true}, {"0 13 * * *", true}, {"0 */11 * * *", false},
		{"0 0,13 * * *", false}, {"*/14 * * * *", false}, {"invalid", false},
	} {
		if err := validateShareLiveCron(tc.cron); (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
	}
	target := telegramTarget{ID: "target", Provider: "quark", StorageID: "q", Parent: "parent", Path: "/save"}
	if target.updateDays() != 7 {
		t.Fatal("missing default")
	}
	for _, days := range []int{-1, 0, 1, 7, 365, 366} {
		target.UpdateDays = &days
		err := validateNotifications(notificationConfig{Targets: []telegramTarget{target}}, State{Storages: []Storage{{ID: "q", Type: "quark"}}})
		if (err == nil) != (days >= 0 && days <= 365) {
			t.Fatal(days, err)
		}
	}
}

func liveTestSetup(t *testing.T) (*App, Storage, shareLiveLink) {
	t.Helper()
	a := testApp(t)
	s := Storage{ID: "q-live", Name: "夸克测试", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private-cookie"}}
	link := shareLiveLink{ID: "live", TargetID: "target", StorageID: s.ID, Provider: s.Type, Parent: "parent", DirectoryID: "saved-root", DirectoryName: "Saved",
		Name: "Series", Link: "https://pan.quark.cn/s/code?pwd=private-pass", Stamp: telegramDigest(shareConfig(s)), IntervalDays: 7}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Notifications.Targets = []telegramTarget{{ID: link.TargetID, Provider: s.Type, StorageID: s.ID, Parent: link.Parent, Path: "/Target"}}
		st.ShareLiveLinks = []shareLiveLink{link}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return a, s, link
}

func TestShareLiveRecordsPersistPrivatelyAndRegisterOnlyAcceptedDirectories(t *testing.T) {
	a, s, link := liveTestSetup(t)
	if err := a.store.update(func(st *State) error { st.ShareLiveLinks = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	p := sharePreview{Parent: link.Parent, Destination: link.DirectoryID, Config: shareConfig(s), Items: []shareEntry{{ID: "source", Name: "Original", IsDir: true}}}
	record := shareTransferRecord{ID: "record", StorageID: s.ID, Provider: s.Type, Name: "Series", Link: link.Link, DirectoryName: "Saved", Source: "bot", Status: "running"}
	if err := a.recordShareTransfer(record, p); err != nil {
		t.Fatal(err)
	}
	if len(a.store.snapshot().ShareLiveLinks) != 0 {
		t.Fatal("registered before acceptance")
	}
	record.Status = "completed"
	if err := a.recordShareTransfer(record, p); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	if len(st.ShareLiveLinks) != 1 || st.ShareLiveLinks[0].IntervalDays != 7 || st.ShareTransfers[0].LiveID == "" {
		t.Fatal(st.ShareLiveLinks, st.ShareTransfers)
	}
	if err := a.recordShareTransfer(record, p); err != nil {
		t.Fatal(err)
	}
	if len(a.store.snapshot().ShareLiveLinks) != 1 || len(a.store.snapshot().ShareTransfers) != 1 {
		t.Fatal("duplicate records")
	}
	raw, err := os.ReadFile(filepath.Join(a.store.runtimeDir, "state.json"))
	if err != nil || strings.Contains(string(raw), "private-pass") {
		t.Fatal("secret not encrypted", err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if w := request(t, h, "GET", "/api/share-transfers", nil, nil); w.Code != 401 {
		t.Fatal("history not protected")
	}
	w := request(t, h, "GET", "/api/share-transfers", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"bot"`) || strings.Contains(w.Body.String(), "private-pass") {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if result := reopened.snapshot(); len(result.ShareLiveLinks) != 1 || result.ShareLiveLinks[0].Link != link.Link || len(result.ShareTransfers) != 1 {
		t.Fatal("runtime lost")
	}
	if err := a.store.update(func(st *State) error {
		st.ShareLiveLinks[0].Status = "running"
		st.ShareLiveLinks[0].Intent = "batch:pending"
		st.ShareTransfers[0].Status = "running"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reopened, err = NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if current := reopened.snapshot(); current.ShareLiveLinks[0].Status != "interrupted" || !current.ShareLiveLinks[0].NextRun.IsZero() || current.ShareLiveLinks[0].Intent == "" || current.ShareTransfers[0].Status != "interrupted" {
		t.Fatal("restart made uncertain writes replayable")
	}
}

func TestShareLiveMobileUpdatesUseSharedProtocolAndHonestSubmittedStatus(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile-live", Name: "移动", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	link := shareLiveLink{ID: "mobile-live", Provider: s.Type, StorageID: s.ID, TargetID: "target", Parent: "parent", DirectoryID: "saved", DirectoryName: "Saved", Name: "Original", Link: "https://yun.139.com/shareweb/#/w/i/code", Stamp: telegramDigest(shareConfig(s)), IntervalDays: 7}
	a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Notifications.Targets = []telegramTarget{{ID: "target", Provider: s.Type, StorageID: s.ID, Parent: "parent", Path: "/Target"}}
		st.ShareLiveLinks = []shareLiveLink{link}
		return nil
	})
	saves := 0
	shareMock(t, func(r *http.Request) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/qryRoutePolicy"):
			return `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
		case strings.HasSuffix(r.URL.Path, "/file/list"):
			var body struct {
				Parent string `json:"parentFileId"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Parent == "parent" {
				return `{"success":true,"data":{"items":[{"fileId":"saved","name":"Saved","type":"folder"}]}}`
			}
			if body.Parent == "saved" {
				return `{"success":true,"data":{"items":[{"fileId":"old","name":"old.mkv","size":42}]}}`
			}
		case strings.HasSuffix(r.URL.Path, "/getOutLinkInfoV6"):
			var body struct {
				Info struct {
					Parent string `json:"pCaID"`
				} `json:"getOutLinkInfoReq"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if body.Info.Parent == "root" {
				return `{"resultCode":"0","data":{"caLst":[{"caID":"source","caName":"Original","caPath":"root/source"}]}}`
			}
			return `{"resultCode":"0","data":{"coLst":[{"coID":"old","coName":"old.mkv","coPath":"root/source/old"},{"coID":"new","coName":"new.mkv","coPath":"root/source/new"}]}}`
		case strings.HasSuffix(r.URL.Path, "/createOuterLinkBatchOprTask"):
			saves++
			var body map[string]map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			info := body["createOuterLinkBatchOprTaskReq"]["taskInfo"].(map[string]any)
			if info["newCatalogID"] != "saved" || len(info["contentInfoList"].([]any)) != 1 {
				t.Fatal(info)
			}
			return `{"resultCode":"0","data":{"taskID":"submitted-job"}}`
		}
		t.Fatal("unexpected mobile write/read", r.URL.Path)
		return ""
	})
	a.executeShareLive(a.ctx, link, shareTransferRecord{ID: "run-mobile", Provider: s.Type, StorageID: s.ID, Source: "cron", Time: time.Now()})
	record := a.store.snapshot().ShareTransfers[0]
	if saves != 1 || record.Submitted != 1 || record.Skipped != 1 || record.Status != "submitted" {
		t.Fatal(saves, record)
	}
}

func TestShareLiveIncrementalQuarkUpdatesPreserveTreeAndUnknownWrites(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			var mu sync.Mutex
			submissions := 0
			shareMock(t, func(r *http.Request) string {
				mu.Lock()
				defer mu.Unlock()
				switch r.URL.Path {
				case "/1/clouddrive/file/sort":
					switch r.URL.Query().Get("pdir_fid") {
					case "parent":
						return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
					case "saved-root":
						return `{"code":0,"data":{"list":[{"fid":"season","file_name":"Season 1","dir":true}]}}`
					case "season":
						return `{"code":0,"data":{"list":[{"fid":"old","file_name":"old.mkv","size":42}]}}`
					}
				case "/1/clouddrive/share/sharepage/token":
					return `{"code":0,"data":{"stoken":"share-token"}}`
				case "/1/clouddrive/share/sharepage/detail":
					switch r.URL.Query().Get("pdir_fid") {
					case "0":
						return `{"code":0,"data":{"list":[{"fid":"source-root","file_name":"Original","dir":true}]}}`
					case "source-root":
						return `{"code":0,"data":{"list":[{"fid":"source-season","file_name":"Season 1","dir":true}]}}`
					case "source-season":
						return `{"code":0,"data":{"list":[{"fid":"source-old","file_name":"old.mkv","share_fid_token":"old-token"},{"fid":"source-new","file_name":"new.mkv","share_fid_token":"new-token"}]}}`
					}
				case "/1/clouddrive/share/sharepage/save":
					var body struct {
						IDs    []string `json:"fid_list"`
						Parent string   `json:"to_pdir_fid"`
						Source string   `json:"pdir_fid"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if len(body.IDs) != 1 || body.IDs[0] != "source-new" || body.Parent != "season" || body.Source != "source-season" {
						t.Fatal("incremental/tree broken", body)
					}
					submissions++
					if uncertain {
						return `{"code":500}`
					}
					return `{"code":0,"data":{"task_id":"task"}}`
				case "/1/clouddrive/task":
					return `{"code":0,"data":{"status":2}}`
				}
				t.Error("unexpected request", r.Method, r.URL.Path, r.URL.RawQuery)
				return `{"code":500}`
			})
			record := shareTransferRecord{ID: "run", LiveID: link.ID, StorageID: s.ID, Provider: s.Type, Name: link.Name, Link: link.Link, Source: "cron", Time: time.Now()}
			a.executeShareLive(context.Background(), link, record)
			state := a.store.snapshot()
			result := state.ShareTransfers[len(state.ShareTransfers)-1]
			if submissions != 1 || result.Skipped != 1 {
				t.Fatal("unexpected results", submissions, result)
			}
			if uncertain {
				if result.Status != "failed" || state.ShareLiveLinks[0].Intent == "" || !state.ShareLiveLinks[0].NextRun.IsZero() {
					t.Fatal("unknown write replayable", result, state.ShareLiveLinks)
				}
				if err := a.startShareLive(link.ID, "manual"); err == nil {
					t.Fatal("unknown write restarted")
				}
			} else if result.Status != "completed" || result.Submitted != 1 || state.ShareLiveLinks[0].Intent != "" || state.ShareLiveLinks[0].NextRun.IsZero() {
				t.Fatal(result, state.ShareLiveLinks)
			}
		})
	}
}

func TestShareLiveRenameRemoveAndDeleteAreDistinctAndGuarded(t *testing.T) {
	for _, action := range []string{"rename", "remove", "delete", "changed", "unknown"} {
		t.Run(action, func(t *testing.T) {
			a, _, link := liveTestSetup(t)
			a.store.update(func(st *State) error {
				st.ShareTransfers = []shareTransferRecord{{ID: "old", LiveID: link.ID, Name: link.Name, DirectoryName: link.DirectoryName}}
				if action == "changed" {
					st.Storages[0].Config["cookie"] = "other"
				}
				if action == "unknown" {
					st.ShareLiveLinks[0].Intent = "delete"
				}
				return nil
			})
			name, exists, writes := "Saved", true, 0
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/1/clouddrive/file/sort":
					if !exists {
						return `{"code":0,"data":{"list":[]}}`
					}
					return fmt.Sprintf(`{"code":0,"data":{"list":[{"fid":"saved-root","file_name":%q,"dir":true}]}}`, name)
				case "/1/clouddrive/file/rename":
					var body struct {
						Name string `json:"file_name"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					name = body.Name
					writes++
					return `{"code":0}`
				case "/1/clouddrive/file/delete":
					writes++
					exists = false
					return `{"code":0}`
				default:
					t.Fatal("unexpected operation", r.URL.Path)
					return ""
				}
			})
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			url := "/api/share-live/" + link.ID + "/" + action
			method := "POST"
			body := any(map[string]bool{"confirm": true})
			if action == "rename" {
				url = "/api/share-live/" + link.ID
				method = "PUT"
				body = map[string]string{"url": link.Link, "name": "New Series", "directoryName": "Renamed", "cron": ""}
			}
			if action == "changed" || action == "unknown" {
				url = "/api/share-live/" + link.ID + "/delete"
			}
			w := request(t, h, method, url, body, cookie)
			if action == "changed" || action == "unknown" {
				if w.Code != 409 || writes != 0 {
					t.Fatal(w.Code, w.Body.String(), writes)
				}
				return
			}
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			state := a.store.snapshot()
			if action == "rename" {
				if name != "Renamed" || state.ShareLiveLinks[0].DirectoryName != "Renamed" || state.ShareTransfers[0].DirectoryName != "Renamed" || !state.ShareLiveLinks[0].NextRun.IsZero() {
					t.Fatal("rename not synchronized")
				}
			}
			if action == "remove" && (writes != 0 || !exists || len(state.ShareLiveLinks) != 0) {
				t.Fatal("remove deleted cloud")
			}
			if action == "delete" && (writes != 1 || exists || len(state.ShareLiveLinks) != 0) {
				t.Fatal("delete did not remove directory")
			}
		})
	}
}

func TestTaskProgressCountsRealPlanAndCompletedSkips(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Series", "one.mkv"), "one")
	writeTest(t, filepath.Join(s.Config["root"], "Series", "two.mkv"), "two")
	writeTest(t, filepath.Join(s.Config["root"], "Series", "ignore", "three.mkv"), "skip")
	writeTest(t, filepath.Join(a.outputDir, "library", "Series", "one.strm"), "existing")
	task := Task{ID: "task", Kind: "strm", StorageID: s.ID, Source: "/Series", Target: "library", ExcludeDirs: "ignore", Status: "running"}
	a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil })
	n, err := a.executeTask(context.Background(), task, s)
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	result := a.store.snapshot().Tasks[0]
	if !result.ProgressKnown || result.ProgressTotal != 2 || result.ProgressDone != 2 {
		t.Fatal(result)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/tasks/panel", nil, cookie)
	var value struct{ Items []taskPanelEntry }
	if json.Unmarshal(w.Body.Bytes(), &value) != nil || len(value.Items) != 1 || value.Items[0].Total != 2 || value.Items[0].Processed != 2 {
		t.Fatal(w.Body.String())
	}
}

func TestTaskHashProgressReportsActualBytes(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	content := strings.Repeat("v", 3<<20+17)
	writeTest(t, filepath.Join(s.Config["root"], "movie.mkv"), content)
	var previous int64
	calls := 0
	ctx := context.WithValue(context.Background(), taskHashProgressKey{}, func(done, total int64) {
		if done <= previous || done > total || total != int64(len(content)) {
			t.Fatal("invented hash progress", done, total, previous)
		}
		previous = done
		calls++
	})
	if _, err := a.hashLocalCASInfo(ctx, s, File{ID: "/movie.mkv", Name: "movie.mkv"}, true, true); err != nil {
		t.Fatal(err)
	}
	if calls < 2 || previous != int64(len(content)) {
		t.Fatal("missing byte progress", calls, previous)
	}
}

func TestTaskProgressLimitDoesNotRejectTheTask(t *testing.T) {
	a := testApp(t)
	s := addCASBindings(t, a)[0]
	cfg := a.store.snapshot().Settings
	cfg.CacheEnabled, cfg.CacheMemoryMB, cfg.CacheMaxItems = true, 256, 1000000
	task := Task{ID: "large", Kind: "cas", StorageID: s.ID, Source: rootOf(s), Status: "running"}
	if err := a.store.update(func(st *State) error { st.Settings = cfg; st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	// Invalid names still count toward preview memory, but are never published.
	a.cache.put(s.ID+":"+rootOf(s), make([]File, 100001), 30, cfg)
	n, err := a.executeTask(a.ctx, task, s)
	if err != nil || n != 0 || a.store.snapshot().Tasks[0].ProgressKnown {
		t.Fatal("preview limit rejected work or invented a total", n, err)
	}
}

func TestShareLiveDestinationUsesOnlyMatchingTargetAndCurrentSession(t *testing.T) {
	a, s, link := liveTestSetup(t)
	session := s
	session.Config = map[string]string{"cookie": "rotated-session"}
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if r.Header.Get("Cookie") != "rotated-session" || !strings.HasSuffix(r.URL.Path, "/sort") {
			t.Fatal("stale session or extra request", r.URL.Path)
		}
		return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
	})
	p := sharePreview{Parent: link.Parent, Items: []shareEntry{{Name: "Saved", IsDir: true}}}
	a.resolveShareDestination(a.ctx, session, &p)
	if calls != 1 || p.Destination != "saved-root" {
		t.Fatal(calls, p.Destination)
	}
	p.Parent, p.Destination = "unrelated", ""
	a.resolveShareDestination(a.ctx, session, &p)
	if calls != 1 {
		t.Fatal("unconfigured target caused extra reads")
	}
	if stored, err := a.store.storage(s.ID); err != nil || stored.Config["cookie"] != "private-cookie" {
		t.Fatal("session overwrote configured credentials", err)
	}
}

func TestShareLiveRunOutlivesTheRequest(t *testing.T) {
	a, _, link := liveTestSetup(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	shareMock(t, func(r *http.Request) string {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sort"):
			return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
		case strings.HasSuffix(r.URL.Path, "/token"):
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				t.Error("HTTP request cancellation reached the background task")
			}
			return `{"code":0,"data":{"stoken":"token"}}`
		case strings.HasSuffix(r.URL.Path, "/detail"):
			if r.URL.Query().Get("pdir_fid") == "0" {
				return `{"code":0,"data":{"list":[{"fid":"source","file_name":"Original","dir":true}]}}`
			}
			return `{"code":0,"data":{"list":[]}}`
		}
		t.Error("unexpected background write", r.URL.Path)
		return `{"code":500}`
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/api/share-live/"+link.ID+"/run", nil).WithContext(ctx)
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != 202 {
		t.Fatal(response.Code, response.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("background update did not begin")
	}
	cancel()
	once.Do(func() { close(release) })
	a.wg.Wait()
	current := a.store.snapshot()
	if current.ShareLiveLinks[0].Status != "completed" || len(current.ShareTransfers) != 1 || current.ShareTransfers[0].Source != "manual" {
		t.Fatal("request cancellation interrupted background update", current.ShareLiveLinks)
	}
}
