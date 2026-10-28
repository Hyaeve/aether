package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFolderCleanupAPITransportPersistenceAndUnknownReceipts(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unknown"}[unknown], func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "pool", Name: "115", Type: "115", Enabled: true, Config: map[string]string{"root": "0", "cookie": "UID=123_A1;CID=test;SEID=secret"}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			present, writes := true, 0
			old := apiClient
			defer func() { apiClient = old }()
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				payload := `{"state":true}`
				if r.Method == "GET" && r.URL.Path == "/files" {
					key := r.URL.Query().Get("cid")
					if key != "10" {
						t.Fatal("read outside selected directory", key)
					}
					rows := []map[string]any{}
					if present {
						rows = append(rows, map[string]any{"fid": "123", "n": "film.mkv", "s": 5})
					}
					raw, _ := json.Marshal(map[string]any{"state": true, "cid": "10", "count": len(rows), "data": rows, "path": []map[string]string{{"cid": "0", "name": "根目录"}, {"cid": "10", "name": "incoming"}}})
					payload = string(raw)
				} else if r.Method == "POST" && strings.Contains(r.URL.Path, "delete") {
					if err := r.ParseForm(); err != nil || r.Form.Get("fid[0]") != "123" {
						t.Fatal("incorrect delete range", r.Form, err)
					}
					writes++
					if unknown {
						return nil, errors.New("connection closed")
					}
					present = false
				} else {
					t.Fatal("unexpected upstream call", r.Method, r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})}
			h := a.Handler(t.TempDir())
			if request(t, h, "GET", "/api/115-folder-cleanup", nil, nil).Code != 401 {
				t.Fatal("missing authentication")
			}
			setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "secret"}, nil)
			cookie := setup.Result().Cookies()[0]
			body := map[string]any{"enabled": true, "rules": []folderCleanupRule{{StorageID: s.ID, Directory: "10", Cron: "*/15 * * * *", Days: 0}}}
			if w := request(t, h, "PUT", "/api/115-folder-cleanup", body, cookie); w.Code != 400 || writes != 0 {
				t.Fatal("saved destructive range without confirmation", w.Code, w.Body.String())
			}
			body["confirmed"] = true
			if w := request(t, h, "PUT", "/api/115-folder-cleanup", body, cookie); w.Code != 200 || writes != 0 {
				t.Fatal("save wrote cloud files or failed", w.Code, w.Body.String())
			}
			rule := a.store.snapshot().FolderCleanup.Rules[0]
			if rule.Label != "/incoming" || len(rule.Trail) != 2 || rule.ID == "" {
				t.Fatal("did not resolve cloud identity", rule)
			}
			// A stale or forged scheduler snapshot is not an executable rule.
			changed := rule
			changed.Days = 5
			if _, err := a.runFolderCleanup(context.Background(), changed); err == nil || writes != 0 {
				t.Fatal("stale rule executed", err, writes)
			}
			count, err := a.runFolderCleanup(context.Background(), rule)
			if writes != 1 || unknown && (count != 0 || err == nil) || !unknown && (count != 1 || err != nil) {
				t.Fatal("unexpected deletion outcome", count, err, writes)
			}
			reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
			if err != nil {
				t.Fatal(err)
			}
			st := reopened.store.snapshot()
			if !reflect.DeepEqual(st.FolderCleanup.Rules[0], rule) || len(st.FolderCleanupReceipts) != 1 {
				t.Fatal("settings or receipts lost after restart")
			}
			for _, receipt := range st.FolderCleanupReceipts {
				if receipt.Done == unknown {
					t.Fatal("unknown result reported confirmed")
				}
			}
			if _, err := reopened.runFolderCleanup(context.Background(), rule); unknown && err == nil || writes != 1 {
				t.Fatal("unknown destructive request replayed", err, writes)
			}
		})
	}
}

func TestFolderCleanupUnknownEpochAndUnsafeConfiguration(t *testing.T) {
	writes := 0
	ops := folderCleanupOps{
		Check: func(context.Context, string) error { return nil },
		List: func(context.Context, string) ([]File, error) {
			return []File{{ID: "f", Name: "unknown.mkv", Created: time.Unix(0, 0)}}, nil
		},
		Delete: func(context.Context, folderCleanupEntry) error { writes++; return nil },
	}
	if _, err := cleanFolderEntries(context.Background(), folderCleanupRule{Directory: "10", Days: 5}, time.Now(), ops); err != nil || writes != 0 {
		t.Fatal("missing upstream timestamp was treated as old", err, writes)
	}
	rule := folderCleanupRule{ID: "a", StorageID: "p", Directory: "10", Cron: "*/15 * * * *", Trail: []searchCrumb{{ID: "0", Name: "根目录"}, {ID: "10", Name: "incoming"}}}
	st := State{Storages: []Storage{{ID: "p", Type: "115", Config: map[string]string{"root": "0"}}}}
	if err := validateFolderCleanup(folderCleanupSettings{Rules: []folderCleanupRule{rule}}, st, false); err != nil {
		t.Fatal(err)
	}
	child := rule
	child.ID, child.Directory = "b", "20"
	child.Trail = append(append([]searchCrumb{}, rule.Trail...), searchCrumb{ID: "20", Name: "nested"})
	if validateFolderCleanup(folderCleanupSettings{Rules: []folderCleanupRule{rule, child}}, st, false) == nil {
		t.Fatal("overlapping cleanup policies accepted")
	}
	st.Storages[0].Config["deleteMode"] = "permanent"
	if validateFolderCleanup(folderCleanupSettings{Rules: []folderCleanupRule{rule}}, st, false) == nil {
		t.Fatal("unsupported permanent deletion accepted")
	}
}

func TestFolderCleanupFinalEmptyDirectoryGuard(t *testing.T) {
	for _, hasNewFile := range []bool{true, false} {
		t.Run(map[bool]string{true: "new-arrival", false: "still-empty"}[hasNewFile], func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "p", Type: "115", Enabled: true, Config: map[string]string{"root": "0", "cookie": "UID=123_A1;CID=test;SEID=secret"}}
			old, writes, childReads := apiClient, 0, 0
			defer func() { apiClient = old }()
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				payload := `{"state":true}`
				if r.Method == "GET" {
					key := r.URL.Query().Get("cid")
					switch key {
					case "10":
						payload = `{"state":true,"cid":"10","count":1,"data":[{"cid":"20","pid":"10","n":"nested"}]}`
					case "20":
						childReads++
						payload = `{"state":true,"cid":"20","count":0,"data":[]}`
						if hasNewFile {
							payload = `{"state":true,"cid":"20","count":1,"data":[{"fid":"30","n":"just-arrived.mkv","s":5}]}`
						}
					default:
						t.Fatal("outside cleanup range", key)
					}
				} else {
					writes++
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})}
			err := a.cloudFileAction(a.ctx, s, s, fileActionRequest{Source: "10", IDs: []string{"20"}, Action: "delete", emptyOnly: true})
			if hasNewFile {
				if err == nil || writes != 0 {
					t.Fatal("nonempty directory deleted", writes, err)
				}
			} else if err != nil || writes != 1 {
				t.Fatal("empty directory not deleted once", writes, err)
			}
			if childReads != 1 {
				t.Fatal("missing last-minute directory read", childReads)
			}
		})
	}
}

func TestFolderCleanupChangedStoragePausesWithoutPreventingStartup(t *testing.T) {
	for _, changed := range []string{"root", "deleteMode"} {
		t.Run(changed, func(t *testing.T) {
			a := testApp(t)
			rule := folderCleanupRule{ID: "rule", StorageID: "p", Directory: "10", Label: "/incoming", Cron: "*/15 * * * *", Trail: []searchCrumb{{ID: "0", Name: "根目录"}, {ID: "10", Name: "incoming"}}}
			if err := a.store.update(func(st *State) error {
				pool := Storage{ID: "p", Type: "115", Enabled: true, Config: map[string]string{"root": "0", "cookie": "UID=123_A1;CID=test;SEID=secret"}}
				if changed == "root" {
					pool.Config["root"] = "10"
				} else {
					pool.Config["deleteMode"] = "permanent"
				}
				st.Storages = []Storage{pool}
				st.FolderCleanup = folderCleanupSettings{Enabled: true, Rules: []folderCleanupRule{rule}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
			if err != nil {
				t.Fatal("storage edit caused restart failure", err)
			}
			if _, err := reopened.runFolderCleanup(a.ctx, rule); err == nil {
				t.Fatal("unsafe changed storage still executed")
			}
		})
	}
}
