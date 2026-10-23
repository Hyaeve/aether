package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func generationMobileStorage(t *testing.T, a *App) Storage {
	t.Helper()
	s := Storage{ID: "generation-mobile", Name: "移动临时池", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:test-token")), "root": "/"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMobileGenerationTemporaryRequiresConfirmation(t *testing.T) {
	a := testApp(t)
	generationMobileStorage(t, a)
	calls := mockMobileGeneration(t, "missing")
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/generation/preview", map[string]string{"kind": "cas", "mode": "links", "text": "https://yun.139.com/shareweb/#/w/i/one"}, cookie)
	var p struct {
		Preview   string
		Valid     bool
		Temporary *struct {
			StorageID string
			Count     int
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || !p.Valid || p.Preview == "" || p.Temporary == nil || p.Temporary.Count != 1 || p.Temporary.StorageID != "generation-mobile" {
		t.Fatal("missing hash must offer a confirmed temporary transfer", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "root/dir/file") || strings.Contains(w.Body.String(), `"Pass"`) || strings.Contains(w.Body.String(), `"Code"`) {
		t.Fatal("private source returned to browser")
	}
	if w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": p.Preview, "target": t.TempDir()}, cookie); w.Code != 400 || len(a.store.snapshot().GenerationJobs) != 0 || *calls != 4 {
		t.Fatal("unconfirmed temporary transfer was queued", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "auto") {
		t.Fatal("share password leaked")
	}
	m, err := a.readGenerationManifest(p.Preview)
	if err != nil || m.Mobile == nil || m.Entries[0].Mobile == nil || m.Entries[0].Mobile.Pass != "auto" {
		t.Fatal("encrypted transfer plan lost", err)
	}
	file, _ := a.generationManifestPath(p.Preview)
	raw, _ := os.ReadFile(file)
	if strings.Contains(string(raw), "root/dir/file") {
		t.Fatal("unencrypted source persisted")
	}
	a.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil })
	w = request(t, h, "POST", "/api/generation/jobs", map[string]any{"preview": p.Preview, "target": t.TempDir(), "confirm": true}, cookie)
	if w.Code != 400 || *calls != 4 || len(a.store.snapshot().GenerationJobs) != 0 {
		t.Fatal("confirmed stale pool started transfer", w.Code)
	}
}

func TestMobileGenerationBatchLimitAndSeparateParents(t *testing.T) {
	entries := []GenerationEntry{}
	for i := 0; i < 500; i++ {
		entries = append(entries, GenerationEntry{Path: fmt.Sprintf("Season/%d.mkv.cas.strm", i), Mobile: &generationMobileSource{Code: "share", Pass: "code", Token: fmt.Sprint(i)}})
	}
	entries = append(entries, GenerationEntry{Path: "Other/a.mkv.cas.strm", Mobile: &generationMobileSource{Code: "share", Pass: "code", Token: "other"}}, GenerationEntry{Path: "Season/b.mkv.cas.strm", Mobile: &generationMobileSource{Code: "another", Token: "another"}})
	groups := generationMobileBatches(entries)
	if len(groups) != 4 || len(groups[0]) != 499 || len(groups[1]) != 1 || len(groups[2]) != 1 || len(groups[3]) != 1 {
		t.Fatal("batch limit/share/parent isolation broken")
	}
	a := testApp(t)
	s := generationMobileStorage(t, a)
	if _, err := a.generationTransfer(context.Background(), s, entries[:500], "target"); err == nil {
		t.Fatal("500 entries reached upstream")
	}
	if _, err := a.generationTransfer(context.Background(), s, []GenerationEntry{{}}, "target"); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestMobileGenerationPoolRotationAndConfirmationSnapshot(t *testing.T) {
	a := testApp(t)
	s := generationMobileStorage(t, a)
	b := s
	b.ID = "mobile-b"
	now := time.Unix(1200, 0)
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, b)
		st.CASPool = normalizedCASPool(CASPoolConfig{Preferred: "tianyi", RotationMinutes: 10, Mobile: CASPoolGroup{StorageIDs: []string{s.ID, b.ID}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{s.ID, b.ID, s.ID} {
		selected, _, err := a.selectGenerationMobile(now.Add(time.Duration(i) * 10 * time.Minute))
		if err != nil || selected.ID != want {
			t.Fatal(selected.ID, err)
		}
	}
	selected, stamp, err := a.selectGenerationMobile(now)
	if err != nil {
		t.Fatal(err)
	}
	plan := generationMobilePlan{StorageID: selected.ID, Revision: storageRevision(selected), Pool: stamp}
	if _, err = a.generationMobileCurrent(plan); err != nil {
		t.Fatal(err)
	}
	a.store.update(func(st *State) error {
		st.Storages[0].Health = &storageHealthState{Stamp: storageRevision(st.Storages[0]), State: "failed"}
		return nil
	})
	if _, err = a.generationMobileCurrent(plan); err == nil {
		t.Fatal("confirmation survived unhealthy storage")
	}
	if selected, _, err = a.selectGenerationMobile(now); err != nil || selected.ID != b.ID {
		t.Fatal("unhealthy storage not excluded", err)
	}
	a.store.update(func(st *State) error {
		st.Storages[0].Health = nil
		st.Storages[0].Config["root"] = "changed"
		return nil
	})
	if _, err = a.generationMobileCurrent(plan); err == nil {
		t.Fatal("confirmation survived config change")
	}
}

func TestMobileGenerationTemporaryLifecycle(t *testing.T) {
	for _, variant := range []string{"success", "permanent", "root-renamed", "submit-unknown", "missing-task", "task-failed", "wrong-task", "missing-hash", "changed-size", "duplicate-name", "source-changed", "output-conflict", "output-changed", "delete-failed", "delete-unconfirmed", "pool-changed", "cancelled"} {
		t.Run(variant, func(t *testing.T) {
			a := testApp(t)
			s := generationMobileStorage(t, a)
			if variant == "permanent" {
				a.store.update(func(st *State) error { st.Storages[0].Config["deleteMode"] = "permanent"; return nil })
				s, _ = a.store.storage(s.ID)
			}
			_, stamp, _ := a.selectGenerationMobile(time.Now())
			plan := generationMobilePlan{StorageID: s.ID, Revision: storageRevision(s), Pool: stamp, Directory: "cas-import-test"}
			m := generationManifest{Kind: "cas", Mobile: &plan, Entries: []GenerationEntry{
				{Path: "Season/episode.mkv.cas.strm", Size: 12, Mobile: &generationMobileSource{Code: "private-code", Pass: "Ab12", Owner: "owner-id", ID: "source-id", Name: "episode.mkv", Token: "root/source-id"}},
			}, Created: time.Now()}
			job := GenerationJob{ID: "temporary-job", Kind: "cas", Name: "CAS 解析生成", Status: "queued", Total: 1, Preview: id()}
			if err := a.store.update(func(st *State) error { st.GenerationJobs = []GenerationJob{job}; return nil }); err != nil {
				t.Fatal(err)
			}
			base := t.TempDir()
			out := filepath.Join(base, "Season", "episode.mkv.cas.strm")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			folders := map[string][]map[string]any{"/": {{"fileId": "aether", "name": "Aether", "type": "folder"}}, "aether": {}}
			submits, deletes, reads := 0, 0, 0
			old := apiClient
			defer func() { apiClient = old }()
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "POST" || r.Header.Get("Authorization") != "Basic "+s.Config["authorization"] {
					t.Fatal("unexpected request or credentials")
				}
				body, _ := io.ReadAll(r.Body)
				payload := map[string]any{}
				share := r.URL.Host == "share-kd-njs.yun.139.com"
				if share {
					plain, err := mobileShareDecrypt(body)
					if err != nil {
						t.Fatal(err)
					}
					body = plain
				}
				if json.Unmarshal(body, &payload) != nil {
					t.Fatal("invalid request")
				}
				var data any = map[string]any{}
				status := 200
				switch filepath.Base(r.URL.Path) {
				case "qryRoutePolicy":
					data = map[string]any{"routePolicyList": []any{map[string]any{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
				case "list":
					folder := payload["parentFileId"].(string)
					if folder == "/" && variant == "root-renamed" {
						if reads > 0 {
							folders["/"][0]["name"] = "Aether(1)"
						}
						reads++
					}
					if folder == "folder-2" && submits > 0 {
						reads++
						if reads == 2 && variant == "source-changed" {
							folders[folder][0]["contentHash"] = strings.Repeat("b", 64)
						}
						if reads == 2 && variant == "output-changed" {
							if err := os.WriteFile(out, []byte("changed"), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
					data = map[string]any{"items": folders[folder]}
				case "create":
					if payload["type"] != "folder" {
						t.Fatal("media upload attempted")
					}
					name, parent := payload["name"].(string), payload["parentFileId"].(string)
					fid := "folder-1"
					if parent == "folder-1" {
						fid = "folder-2"
					}
					if !generationHasTemporary(a.store.snapshot().GenerationJobs[0]) {
						t.Fatal("mutation lacked durable intent")
					}
					folders[parent] = append(folders[parent], map[string]any{"fileId": fid, "name": name, "type": "folder"})
					folders[fid] = []map[string]any{}
					data = map[string]any{"fileId": fid}
				case "createOuterLinkBatchOprTask":
					submits++
					p := payload["createOuterLinkBatchOprTaskReq"].(map[string]any)
					info := p["taskInfo"].(map[string]any)
					if p["ownerAccount"] != "owner-id" || info["newCatalogID"] != "folder-2" || len(info["contentInfoList"].([]any)) != 1 {
						t.Fatal("transfer destination/source mismatch")
					}
					if a.store.snapshot().GenerationJobs[0].Temporary[1].Phase != "submitting" {
						t.Fatal("missing durable submit intent")
					}
					if variant == "submit-unknown" {
						status = 502
					}
					data = map[string]any{"taskID": "transfer-task"}
					if variant == "missing-task" {
						data = map[string]any{}
					}
					file := map[string]any{"fileId": "our-copy", "name": "episode.mkv", "size": 12, "type": "file", "contentHash": strings.Repeat("a", 64), "contentHashAlgorithm": "SHA256"}
					if variant == "missing-hash" {
						delete(file, "contentHash")
					}
					if variant == "changed-size" {
						file["size"] = 13
					}
					folders["folder-2"] = []map[string]any{file, {"fileId": "unrelated", "name": "keep.txt", "size": 5, "type": "file"}}
					if variant == "duplicate-name" {
						folders["folder-2"] = append(folders["folder-2"], file)
					}
				case "queryBatchOprTaskDetail":
					result, id := 1, "transfer-task"
					if variant == "task-failed" {
						result = 3
					}
					if variant == "wrong-task" {
						id = "other-task"
					}
					data = map[string]any{"queryBatchOprTaskDetailRes": map[string]any{"batchOprTask": map[string]any{"taskID": id, "taskStatus": 2, "taskResultCode": result, "resultCode": 0}}}
					if variant == "output-conflict" {
						os.MkdirAll(filepath.Dir(out), 0755)
						os.WriteFile(out, []byte("existing"), 0600)
					}
					if variant == "pool-changed" {
						a.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil })
					}
					if variant == "cancelled" {
						cancel()
					}
				case "batchTrash", "batchDelete":
					if (variant == "permanent") != (filepath.Base(r.URL.Path) == "batchDelete") {
						t.Fatal("storage deletion policy was ignored")
					}
					deletes++
					ids := payload["fileIds"].([]any)
					if len(ids) != 1 || ids[0] != "our-copy" {
						t.Fatal("removed foreign file or directory")
					}
					if _, err := os.ReadFile(out); err != nil {
						t.Fatal("delete preceded artifact publication")
					}
					if a.store.snapshot().GenerationJobs[0].Temporary[1].Files[0].Phase != "generated" {
						t.Fatal("delete preceded durable artifact receipt")
					}
					if variant == "delete-failed" {
						status = 502
					} else if variant != "delete-unconfirmed" {
						folders["folder-2"] = folders["folder-2"][1:]
					}
				default:
					t.Fatal("unverified endpoint, upload or media download", r.URL.Path)
				}
				var raw []byte
				if share {
					raw, _ = json.Marshal(map[string]any{"resultCode": "0", "data": data})
					enc, _ := mobileShareEncrypt(raw)
					raw = []byte(enc)
				} else {
					raw, _ = json.Marshal(map[string]any{"success": true, "code": "0", "data": data})
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
			})}
			a.executeGeneration(ctx, job, m, base, "")
			final := a.store.snapshot().GenerationJobs[0]
			wantSubmits := 1
			if variant == "root-renamed" {
				wantSubmits = 0
			}
			if submits != wantSubmits {
				t.Fatal("transfer replayed or skipped", submits)
			}
			if variant == "success" || variant == "permanent" {
				if final.Status != "success" || final.Success != 1 || deletes != 1 || generationHasTemporary(final) {
					t.Fatal(final, deletes)
				}
				raw, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				generatedCASClaim(t, raw)
			} else {
				if final.Status == "success" || !generationHasTemporary(final) {
					t.Fatal("uncertain work was treated as complete", final)
				}
				wantDeletes := 0
				if variant == "delete-failed" || variant == "delete-unconfirmed" {
					wantDeletes = 1
				}
				if deletes != wantDeletes {
					t.Fatal("unsafe cleanup or repeated delete", deletes)
				}
			}
			logs, _ := json.Marshal(a.store.snapshot().Logs)
			if strings.Contains(string(logs), "private-code") || strings.Contains(string(logs), "Ab12") || strings.Contains(string(logs), "test-token") {
				t.Fatal("sensitive data leaked")
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			w := request(t, h, "POST", "/api/generation/jobs", map[string]any{"preview": job.Preview, "target": base, "confirm": true}, cookie)
			if w.Code != 200 || submits != wantSubmits {
				t.Fatal("repeated job replayed a transfer", w.Code)
			}
		})
	}
}
