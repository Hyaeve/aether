package app

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestShareLiveDeleteRejectedAndUnknownReceipts(t *testing.T) {
	for _, kind := range []string{"rejected", "unknown", "task-query-failed"} {
		t.Run(kind, func(t *testing.T) {
			a, _, link := liveTestSetup(t)
			writes, present := 0, true
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/1/clouddrive/file/sort":
					if !present {
						return `{"code":0,"data":{"list":[]}}`
					}
					return `{"code":0,"data":{"list":[{"fid":"saved-root","file_name":"Saved","dir":true}]}}`
				case "/1/clouddrive/file/delete":
					writes++
					if kind == "rejected" {
						return `{"code":400,"message":"denied"}`
					}
					if kind == "unknown" {
						return `{`
					}
					return `{"code":0,"data":{"task_id":"pending-ticket"}}`
				case "/1/clouddrive/task":
					return `{"code":500,"message":"temporary failure"}`
				default:
					t.Fatal(r.URL.Path)
					return ""
				}
			})
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			call := func() int {
				return request(t, h, "POST", "/api/share-live/"+link.ID+"/delete", map[string]bool{"confirm": true}, cookie).Code
			}
			if call() != 409 || writes != 1 {
				t.Fatal("first delete", writes)
			}
			current := a.store.snapshot().ShareLiveLinks[0]
			if kind == "rejected" {
				if current.Intent != "" || current.DeleteTaskID != "" {
					t.Fatal("definite rejection held as unknown", current)
				}
				if call() != 409 || writes != 2 {
					t.Fatal("explicit retry of rejected write blocked")
				}
				return
			}
			if current.Intent != "delete" {
				t.Fatal("unknown write lost protection")
			}
			if kind == "task-query-failed" && current.DeleteTaskID != "pending-ticket" {
				t.Fatal("task receipt lost")
			}
			reopened, err := NewStore(a.store.dir)
			if err != nil {
				t.Fatal(err)
			}
			if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
				t.Fatal(err)
			}
			if saved := reopened.snapshot().ShareLiveLinks[0]; saved.Intent != current.Intent || saved.DeleteTaskID != current.DeleteTaskID || !saved.NextRun.IsZero() {
				t.Fatal("restart lost delete receipt", saved)
			}
			if call() != 409 || writes != 1 {
				t.Fatal("unknown write replayed")
			}
			present = false
			if call() != 200 || writes != 1 || len(a.store.snapshot().ShareLiveLinks) != 0 {
				t.Fatal("confirmed disappearance failed to reconcile")
			}
		})
	}
}

func TestShareLiveDeleteMobileBatchAndUnexecutedLink(t *testing.T) {
	for _, scenario := range []string{"mobile-batch", "unexecuted"} {
		t.Run(scenario, func(t *testing.T) {
			a, s, link := liveTestSetup(t)
			if scenario == "mobile-batch" {
				s.Type = "mobile"
				s.Config = map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}
				link.Provider = s.Type
				link.Stamp = telegramDigest(shareConfig(s))
				link.Intent = "batch:old:0"
			} else {
				link.DirectoryID = ""
			}
			a.store.update(func(st *State) error {
				st.Storages = []Storage{s}
				st.Notifications.Targets[0].Provider = s.Type
				st.ShareLiveLinks = []shareLiveLink{link}
				return nil
			})
			writes, present := 0, true
			shareMock(t, func(r *http.Request) string {
				switch {
				case strings.HasSuffix(r.URL.Path, "/qryRoutePolicy"):
					return `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case strings.HasSuffix(r.URL.Path, "/file/list"):
					if !present {
						return `{"success":true,"data":{"items":[]}}`
					}
					return `{"success":true,"data":{"items":[{"fileId":"saved-root","name":"Saved","type":"folder"}]}}`
				case strings.HasSuffix(r.URL.Path, "/recyclebin/batchTrash"):
					if a.store.snapshot().ShareLiveLinks[0].Intent != "delete" {
						t.Fatal("missing mobile intent")
					}
					writes++
					present = false
					return `{"success":true}`
				default:
					t.Fatal("unexpected request", r.URL.Path)
					return ""
				}
			})
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			w := request(t, h, "POST", "/api/share-live/"+link.ID+"/delete", map[string]bool{"confirm": true}, cookie)
			want := 1
			if scenario == "unexecuted" {
				want = 0
			}
			if w.Code != 200 || writes != want || len(a.store.snapshot().ShareLiveLinks) != 0 {
				t.Fatal(w.Code, w.Body.String(), writes)
			}
		})
	}
}

func TestShareLiveDeleteReconcilesIntentWithoutReplayingWrites(t *testing.T) {
	for _, scenario := range []string{"batch", "deleted", "replaced", "async", "receipt-completed", "receipt-pending", "receipt-failed", "receipt-credentials", "renamed", "list-failed", "preflight", "shared", "credentials", "changed-during-preflight"} {
		t.Run(scenario, func(t *testing.T) {
			a, _, link := liveTestSetup(t)
			intent, receipt := "", ""
			if scenario == "batch" {
				intent = "batch:old:0"
			}
			if scenario == "deleted" || scenario == "replaced" || strings.HasPrefix(scenario, "receipt-") {
				intent = "delete"
			}
			if strings.HasPrefix(scenario, "receipt-") {
				receipt = "prior-delete"
			}
			if err := a.store.update(func(st *State) error {
				st.ShareLiveLinks[0].Intent = intent
				st.ShareLiveLinks[0].DeleteTaskID = receipt
				if scenario == "preflight" {
					st.Storages[0].Config["deleteMode"] = "permanent"
				}
				if scenario == "credentials" {
					st.Storages[0].Config["cookie"] = "changed"
				}
				if scenario == "shared" {
					other := link
					other.ID = "other"
					st.ShareLiveLinks = append(st.ShareLiveLinks, other)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			exists := scenario != "deleted" && scenario != "replaced"
			writes, queries, lists := 0, 0, 0
			shareMock(t, func(r *http.Request) string {
				switch r.URL.Path {
				case "/1/clouddrive/file/sort":
					lists++
					if scenario == "changed-during-preflight" && lists == 3 {
						a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil })
					}
					if scenario == "list-failed" {
						return `{"code":500,"message":"offline"}`
					}
					if !exists {
						if scenario == "replaced" {
							return `{"code":0,"data":{"list":[{"fid":"unrelated","file_name":"Saved","dir":true}]}}`
						}
						return `{"code":0,"data":{"list":[]}}`
					}
					name := "Saved"
					if scenario == "renamed" {
						name = "Changed"
					}
					return fmt.Sprintf(`{"code":0,"data":{"list":[{"fid":"saved-root","file_name":%q,"dir":true}]}}`, name)
				case "/1/clouddrive/file/delete":
					writes++
					current := a.store.snapshot().ShareLiveLinks[0]
					if current.Intent != "delete" || current.DeleteTaskID != "" {
						t.Fatal("intent not durably installed before write", current)
					}
					if scenario == "async" {
						return `{"code":0,"data":{"task_id":"new-delete"}}`
					}
					exists = false
					return `{"code":0}`
				case "/1/clouddrive/task":
					queries++
					if scenario == "receipt-credentials" {
						a.store.update(func(st *State) error { st.Storages[0].Config["cookie"] = "changed"; return nil })
					}
					if scenario == "receipt-pending" {
						return `{"code":0,"data":{"status":1}}`
					}
					if scenario == "receipt-failed" {
						return `{"code":0,"data":{"status":3}}`
					}
					if scenario == "async" && a.store.snapshot().ShareLiveLinks[0].DeleteTaskID != "new-delete" {
						t.Fatal("missing persisted task receipt")
					}
					exists = false
					return `{"code":0,"data":{"status":2}}`
				default:
					t.Fatal("unexpected request", r.URL.Path)
					return ""
				}
			})
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			w := request(t, h, "POST", "/api/share-live/"+link.ID+"/delete", map[string]bool{"confirm": true}, cookie)
			blocked := scenario == "receipt-pending" || scenario == "receipt-credentials" || scenario == "changed-during-preflight" || scenario == "renamed" || scenario == "list-failed" || scenario == "preflight" || scenario == "shared" || scenario == "credentials"
			if blocked {
				if w.Code != 409 || writes != 0 || len(a.store.snapshot().ShareLiveLinks) == 0 {
					t.Fatal(w.Code, w.Body.String(), writes)
				}
				if a.store.snapshot().ShareLiveLinks[0].Intent != intent {
					t.Fatal("preflight changed intent")
				}
				return
			}
			wantWrites := 1
			if scenario == "deleted" || scenario == "replaced" || scenario == "receipt-completed" {
				wantWrites = 0
			}
			if w.Code != 200 || writes != wantWrites || len(a.store.snapshot().ShareLiveLinks) != 0 {
				t.Fatal(w.Code, w.Body.String(), writes, queries)
			}
			// A second click cannot re-submit the cloud delete after the record was removed.
			w = request(t, h, "POST", "/api/share-live/"+link.ID+"/delete", map[string]bool{"confirm": true}, cookie)
			if w.Code != 404 || writes != wantWrites {
				t.Fatal("replayed delete", w.Code, writes)
			}
		})
	}
}
