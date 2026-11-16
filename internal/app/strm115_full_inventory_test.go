package app

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test115FullSyncCrossChecksBulkInventory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		listed int
	}{
		{"recovered", 2},
		{"source-grew", 3},
		{"still-short", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, task, _ := strmBulkFixture(t, 1, "")
			task.fullSyncGeneration = true
			base := apiClient.Transport
			t.Cleanup(func() { apiClient.Transport = base })
			var stats, fallback int
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/category/get" {
					stats++
					return jsonHTTPResponse(`{"file_category":0,"pick_code":"folderpick12345","count":"2","folder_count":"1"}`), nil
				}
				q := r.URL.Query()
				if r.URL.Path == "/files" && q.Get("show_dir") != "0" {
					fallback++
					if q.Get("cur") != "1" || q.Get("fc_mix") != "1" {
						t.Errorf("fallback did not explicitly request immediate folders and files: %s", q.Encode())
					}
					if q.Get("cid") == "10" {
						return jsonHTTPResponse(`{"state":true,"cid":"10","offset":0,"count":1,"data":[{"cid":"20","pid":"10","n":"Season0000"}]}`), nil
					}
					if q.Get("cid") == "20" {
						rows := `{"fid":"10000","cid":"20","n":"film.mkv","pc":"pick000000000000"}`
						if tc.listed >= 2 {
							rows += `,{"fid":"10001","cid":"20","n":"missed.mkv","pc":"pick000000000001"}`
						}
						if tc.listed == 3 {
							rows += `,{"fid":"10002","cid":"20","n":"new.mkv","pc":"pick000000000002"}`
						}
						return jsonHTTPResponse(fmt.Sprintf(`{"state":true,"cid":"20","offset":0,"count":%d,"data":[%s]}`, tc.listed, rows)), nil
					}
				}
				return base.RoundTrip(r)
			})
			n, err := a.executeTask(a.ctx, task, s)
			if tc.listed >= 2 {
				if n != tc.listed || err != nil || stats != 1 || fallback != 2 {
					t.Fatal("short bulk snapshot was not replaced by the complete directory listing", n, stats, fallback, err)
				}
				body, err := os.ReadFile(filepath.Join(task.Target, "Show", "Season0000", "missed.strm"))
				if err != nil || !strings.Contains(string(body), "/pick000000000001/") {
					t.Fatal("fallback did not write the missing pickcode output", string(body), err)
				}
			} else if n != 0 || err == nil || !strings.Contains(err.Error(), "独立统计") {
				t.Fatal("both short inventories were reported successful", n, err)
			}
		})
	}
}

func Test115FullSyncRejectsMalformedFallbackPage(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing-count", `{"state":true,"cid":"0","data":[]}`},
		{"missing-list", `{"state":true,"cid":"0","count":0}`},
		{"wrong-offset", `{"state":true,"cid":"0","count":0,"offset":200,"data":[]}`},
		{"wrong-parent", `{"state":true,"cid":"0","count":1,"data":[{"fid":"10001","cid":"20","n":"outside.mkv","pc":"pick000000000001"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s, task, _ := strmBulkFixture(t, 0, "")
			task.Source, task.SourceLabel, task.fullSyncGeneration = "/", "", true
			if err := a.store.update(func(st *State) error { st.Tasks[0] = task; return nil }); err != nil {
				t.Fatal(err)
			}
			base := apiClient.Transport
			t.Cleanup(func() { apiClient.Transport = base })
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/files" || r.URL.Query().Get("cid") != "0" {
					t.Fatal("unexpected inventory request", r.URL)
				}
				return jsonHTTPResponse(tc.body), nil
			})
			if n, err := a.executeTask(a.ctx, task, s); n != 0 || err == nil {
				t.Fatal("malformed listing was treated as complete", n, err)
			}
		})
	}
}

func Test115FullSyncFallbackReportsExpiredCookie(t *testing.T) {
	a, s, task, _ := strmBulkFixture(t, 0, "")
	task.fullSyncGeneration = true
	base := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = base })
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		res := jsonHTTPResponse(`{}`)
		res.StatusCode = http.StatusUnauthorized
		return res, nil
	})
	if _, err := a.listTaskDirectory(a.ctx, task, s, "10", true); err == nil || !strings.Contains(err.Error(), "CK已失效") {
		t.Fatal("fallback lost the expired credential error", err)
	}
	current, err := a.store.storage(s.ID)
	if err != nil || current.Health == nil || current.Health.State != "expired" {
		t.Fatal("fallback did not preserve storage health reporting", current.Health, err)
	}
}
