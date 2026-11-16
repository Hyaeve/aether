package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestShareLiveMkdirWaitsForReceiptDirectory(t *testing.T) {
	for _, scenario := range []string{"delayed", "wrong-id", "wrong-type"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			link.DirectoryID = ""
			if err := a.store.update(func(st *State) error { st.ShareLiveLinks[0] = link; return nil }); err != nil {
				t.Fatal(err)
			}
			writes, readsAfter := 0, 0
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/1/clouddrive/file":
					writes++
					return `{"code":0,"data":{"fid":"created-dir"}}`
				case "/1/clouddrive/file/sort":
					if writes == 0 {
						return `{"code":0,"data":{"list":[]}}`
					}
					readsAfter++
					if scenario == "delayed" && readsAfter < 3 {
						if readsAfter == 1 {
							return `{"code":500}`
						}
						return `{"code":0,"data":{"list":[]}}`
					}
					if scenario == "wrong-id" {
						return `{"code":0,"data":{"list":[{"fid":"other-dir","file_name":"Saved","dir":true}]}}`
					}
					return fmt.Sprintf(`{"code":0,"data":{"list":[{"fid":"created-dir","file_name":"Saved","dir":%t,"file_type":1}]}}`, scenario != "wrong-type")
				}
				t.Errorf("unexpected request %s", r.URL.Path)
				return `{"code":500}`
			})
			err := a.initializeShareLiveDirectory(a.ctx, s, &link)
			current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
			if writes != 1 {
				t.Fatal("mkdir was not submitted once", writes)
			}
			if scenario == "delayed" {
				if err != nil || current.DirectoryID != "created-dir" || current.Intent != "" || current.PendingDirectoryID != "" || readsAfter != 3 {
					t.Fatal("delayed directory was not confirmed", err, readsAfter, current.DirectoryID, current.Intent, current.PendingDirectoryID)
				}
			} else if err == nil || current.DirectoryID != "" || current.Intent != "mkdir:Saved" || current.PendingDirectoryID != "created-dir" {
				t.Fatal("unrelated item was adopted or its receipt was lost", err, current.DirectoryID, current.Intent, current.PendingDirectoryID)
			}
		})
	}
}

func TestShareLiveMkdirCanChooseUnusedName(t *testing.T) {
	for _, scenario := range []string{"fresh", "known-id", "same", "occupied", "read-error", "different-target", "different-source"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			link.DirectoryID, link.Intent, link.Status = "", "mkdir:Saved", "failed"
			if scenario == "known-id" {
				link.PendingDirectoryID = "known-dir"
			}
			if err := a.store.update(func(st *State) error {
				st.ShareLiveLinks[0] = link
				st.ShareTransfers = []shareTransferRecord{{ID: "old", LiveID: link.ID, DirectoryName: link.DirectoryName}}
				st.Notifications.Targets = append(st.Notifications.Targets, telegramTarget{ID: "other-target", StorageID: s.ID, Parent: "other-parent", Provider: s.Type})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			reads := 0
			shareMock(t, func(r *http.Request) string {
				if r.Method != http.MethodGet || r.URL.Path != "/1/clouddrive/file/sort" || r.URL.Query().Get("pdir_fid") != link.Parent {
					t.Error("legacy recovery made an unrelated cloud request", r.Method, r.URL.Path)
				}
				reads++
				if scenario == "read-error" {
					return `{"code":500}`
				}
				if scenario == "occupied" {
					return `{"code":0,"data":{"list":[{"fid":"foreign","file_name":"NEW NAME","dir":true}]}}`
				}
				return `{"code":0,"data":{"list":[{"fid":"unproven","file_name":"Saved","dir":true}]}}`
			})
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			list := request(t, h, http.MethodGet, "/api/share-live", nil, cookie)
			var links []struct {
				DirectoryCreationPending bool `json:"directoryCreationPending"`
			}
			if err := json.Unmarshal(list.Body.Bytes(), &links); err != nil || len(links) != 1 || !links[0].DirectoryCreationPending {
				t.Fatal("pending mkdir not identified by the list API", list.Code, err)
			}
			input := shareLiveInput{URL: link.Link, DirectoryName: "New Name", TargetID: link.TargetID}
			switch scenario {
			case "same":
				input.DirectoryName = " " + strings.ToUpper(link.DirectoryName) + " "
			case "different-target":
				input.TargetID = "other-target"
			case "different-source":
				input.URL = "https://pan.quark.cn/s/other"
			}
			w := request(t, h, http.MethodPut, "/api/share-live/"+link.ID, input, cookie)
			state := a.store.snapshotWithLogLimit(0)
			current := state.ShareLiveLinks[0]
			if scenario == "fresh" || scenario == "known-id" {
				if w.Code != http.StatusOK || reads != 1 || current.Intent != "" || current.DirectoryID != "" || current.PendingDirectoryID != "" || current.DirectoryName != input.DirectoryName || current.Status != "idle" || state.ShareTransfers[0].DirectoryName != link.DirectoryName {
					t.Fatal("unconfirmed record could not safely select a new destination", w.Code, w.Body.String(), reads, current.Intent, current.Status)
				}
				return
			}
			if w.Code != http.StatusConflict || current.Intent != link.Intent || current.DirectoryName != link.DirectoryName {
				t.Fatal("unverified edit cleared the old marker", w.Code, w.Body.String())
			}
		})
	}
}

func TestShareLiveMkdirReceiptResumesAfterRestart(t *testing.T) {
	a, _, link := liveTestSetup(t)
	link.DirectoryID = ""
	if err := a.store.update(func(st *State) error { st.ShareLiveLinks[0] = link; return nil }); err != nil {
		t.Fatal(err)
	}
	writes, saves := 0, 0
	visible := false
	mockShareLiveQuarkRun(t, nil, func() string {
		saves++
		return `{"code":0,"data":{"task_id":"transfer"}}`
	}, func() string { return `{"code":0,"data":{"status":2}}` })
	next := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost && r.URL.Path == "/1/clouddrive/file" {
			writes++
			return jsonHTTPResponse(`{"code":0,"data":{"fid":"saved-root"}}`), nil
		}
		if r.URL.Path == "/1/clouddrive/file/sort" {
			parent := r.URL.Query().Get("pdir_fid")
			if parent == "parent" && !visible || parent == "dest-one" || parent == "dest-two" {
				return jsonHTTPResponse(`{"code":0,"data":{"list":[]}}`), nil
			}
		}
		return next.RoundTrip(r)
	})
	a.executeShareLive(a.ctx, link, shareTransferRecord{ID: "first", Time: time.Now()})
	current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
	if writes != 1 || saves != 0 || current.Intent != "mkdir:Saved" || current.PendingDirectoryID != "saved-root" {
		t.Fatal("first run did not hold unconfirmed mkdir", writes, saves)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	visible = true
	if err := a.startShareLive(link.ID, "manual"); err != nil {
		t.Fatal("manual run could not resume the directory receipt", err)
	}
	a.wg.Wait()
	current = a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
	if current.Status != "completed" || current.Intent != "" || current.PendingDirectoryID != "" || current.DirectoryID != "saved-root" || writes != 1 || saves != 2 {
		t.Fatal("recovery did not verify and continue the first transfer", current.Status, current.Message, current.Intent, current.DirectoryID, writes, saves)
	}
}

func TestShareLiveMkdirMobileUsesReturnedDirectoryID(t *testing.T) {
	a, s, link := liveTestSetup(t)
	s.Type = "mobile"
	s.Config = map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}
	link.Provider, link.DirectoryID = s.Type, ""
	link.Link = "https://yun.139.com/shareweb/#/w/i/code"
	link.Stamp = telegramDigest(shareConfig(s))
	if err := a.store.update(func(st *State) error {
		st.Storages[0], st.ShareLiveLinks[0] = s, link
		st.Notifications.Targets[0].Provider = s.Type
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writes, readsAfter := 0, 0
	shareMock(t, func(r *http.Request) string {
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			return `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
		case "/file/create":
			writes++
			return `{"success":true,"data":{"fileId":"mobile-dir"}}`
		case "/file/list":
			if writes == 0 {
				return `{"success":true,"data":{"items":[]}}`
			}
			readsAfter++
			current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
			if current.PendingDirectoryID != "mobile-dir" {
				t.Error("directory list was requested before the native receipt had been saved")
			}
			if readsAfter == 1 {
				return `{"success":true,"data":{"items":[]}}`
			}
			return `{"success":true,"data":{"items":[{"fileId":"mobile-dir","name":"Saved","type":"folder"}]}}`
		}
		t.Error("unexpected request", r.Method, r.URL.Path)
		return `{"success":false}`
	})
	err := a.initializeShareLiveDirectory(a.ctx, s, &link)
	current := a.store.snapshotWithLogLimit(0).ShareLiveLinks[0]
	if err != nil || writes != 1 || readsAfter != 2 || current.DirectoryID != "mobile-dir" || current.PendingDirectoryID != "" || current.Intent != "" {
		t.Fatal("mobile mkdir receipt was not verified", err, writes, readsAfter, current.DirectoryID, current.Intent)
	}
}
