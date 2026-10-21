package app

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloudQuarkCreateUsesTaskAndRotatedSessionCookie(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "quark-share", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "__puus=old"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	old := apiClient
	defer func() { apiClient = old }()
	writes := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		header := http.Header{"Content-Type": {"application/json"}}
		raw := `{"code":0,"status":200,"data":{"list":[{"fid":"10","file_name":"film.mp4","file_type":1,"size":4}]}}`
		switch r.URL.Path {
		case "/1/clouddrive/share":
			writes++
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["expired_type"] != float64(3) || body["url_type"] != float64(2) || len(body["passcode"].(string)) != 4 {
				t.Fatal("incorrect share options", body)
			}
			header.Add("Set-Cookie", "__puus=new; Path=/; Secure")
			raw = `{"code":0,"status":200,"data":{"task_id":"task"}}`
		case "/1/clouddrive/task":
			if !strings.Contains(r.Header.Get("Cookie"), "__puus=new") || r.URL.Query().Get("task_id") != "task" {
				t.Fatal("task lost session", r.Header)
			}
			raw = `{"code":0,"status":200,"data":{"status":2,"share_id":"share"}}`
		case "/1/clouddrive/share/password":
			writes++
			if !strings.Contains(r.Header.Get("Cookie"), "__puus=new") {
				t.Fatal("password lost session")
			}
			raw = `{"code":0,"status":200,"data":{"share_url":"https://pan.quark.cn/s/abcdef","passcode":"ZZ99"}}`
		}
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	input := map[string]any{"storageId": s.ID, "parent": "0", "ids": []string{"10"}, "options": cloudShareOptions{Days: 7, Encrypted: true, Autofill: true}}
	w := request(t, h, "POST", "/api/cloud/shares", input, cookie)
	if w.Code != 200 || writes != 2 || !strings.Contains(w.Body.String(), "pwd=ZZ99") {
		t.Fatal(w.Code, writes, w.Body.String())
	}
	current, _ := a.store.storage(s.ID)
	if current.Config["cookie"] != "__puus=old" {
		t.Fatal("ephemeral cookie overwrote account")
	}
}

func TestCloudRecycleManualPINForeignIDsNoRetryAndHealthGate(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "manual-trash", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	old := apiClient
	defer func() { apiClient = old }()
	writes, reads := 0, 0
	reject := false
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"state":true}`
		status := 200
		if r.URL.Path == "/rb" {
			reads++
			raw = `{"state":true,"data":[{"id":"one","file_name":"film.mp4","file_size":"4","dtime":100}]}`
		} else if r.URL.Path == "/rb/clean" || r.URL.Path == "/rb/revert" {
			writes++
			r.ParseForm()
			if r.Form.Get("rid[0]") != "one" || r.Form.Get("rid[1]") != "" {
				t.Fatal("empty/full/foreign deletion", r.Form)
			}
			if r.URL.Path == "/rb/clean" && r.Form.Get("password") != "123456" {
				t.Fatal("PIN absent")
			}
			if reject {
				status = 403
			}
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	url := "/api/cloud/manage?kind=trash&storage=" + s.ID
	post := func(action, pin string, ids []string) int {
		return request(t, h, "POST", url, map[string]any{"action": action, "password": pin, "ids": ids, "confirm": true}, cookie).Code
	}
	if post("delete", "", []string{"one"}) == 200 || writes != 0 {
		t.Fatal("missing PIN accepted")
	}
	if post("delete", "123456", []string{"foreign"}) != 409 || writes != 0 {
		t.Fatal("foreign ID accepted")
	}
	if post("restore", "", []string{"one"}) != 200 || writes != 1 {
		t.Fatal("restore failed")
	}
	if post("delete", "123456", []string{"one"}) != 200 || writes != 2 {
		t.Fatal("delete failed")
	}
	reject = true
	if post("clear", "123456", nil) != 502 || writes != 3 {
		t.Fatal("ambiguous write retried", writes)
	}
	a.store.update(func(st *State) error {
		st.Storages[0].Health = &storageHealthState{Stamp: storageRevision(s), State: "expired"}
		return nil
	})
	before := reads
	if post("delete", "123456", []string{"one"}) != 409 || writes != 3 || reads != before {
		t.Fatal("health gate allowed upstream traffic")
	}
}

func TestCloudCASRecordsRuntimeRecoveryAndBusyProtection(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "cas-cloud", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	key, err := a.beginCloudCASRecord(s, "/target", CASInfo{Name: "film.mp4", Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	url := "/api/cloud/manage?kind=downloads&storage=" + s.ID
	w := request(t, h, "POST", url, map[string]any{"ids": []string{key}, "action": "delete", "confirm": true}, cookie)
	if w.Code == 200 || len(a.store.snapshot().CloudCASRecords) != 1 {
		t.Fatal("running record removed")
	}
	w = request(t, h, "POST", url, map[string]any{"ids": []string{key}, "action": "retry", "confirm": true}, cookie)
	if w.Code == 200 || len(a.store.snapshot().CloudCASRecords) != 1 {
		t.Fatal("running request replayed")
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil || reopened.store.snapshot().CloudCASRecords[0].Status != "interrupted" {
		t.Fatal("runtime recovery", err)
	}
	a.finishCloudCASRecord(key, false)
	w = request(t, h, "POST", url, map[string]any{"ids": []string{key}, "action": "delete", "confirm": true}, cookie)
	if w.Code != 200 || len(a.store.snapshot().CloudCASRecords) != 0 {
		t.Fatal("terminal record cannot be cleared", w.Body.String())
	}
}

func TestCASExportExpiredResultCleanupDoesNotTouchOtherFiles(t *testing.T) {
	a := testApp(t)
	root := filepath.Join(a.dataDir, "cache", "cas-export-results")
	os.MkdirAll(root, 0700)
	for _, name := range []string{"cas-export-expired", "cas-export-recent", "unrelated"} {
		os.WriteFile(filepath.Join(root, name), []byte("data"), 0600)
		if name != "cas-export-recent" {
			old := time.Now().Add(-25 * time.Hour)
			os.Chtimes(filepath.Join(root, name), old, old)
		}
	}
	a.cleanupCASExportResults()
	if _, err := os.Stat(filepath.Join(root, "cas-export-expired")); !os.IsNotExist(err) {
		t.Fatal("expired orphan retained", err)
	}
	for _, name := range []string{"cas-export-recent", "unrelated"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("unrelated/current result removed", err)
		}
	}
	if validateCloudRows([]cloudManagementEntry{{ID: "a"}, {ID: "a"}}) == nil || validateCloudRows([]cloudManagementEntry{{}}) == nil {
		t.Fatal("ambiguous identifiers accepted")
	}
}

func TestSimulcastGlobalToggleStopsCurrentConfiguration(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "simulcast-toggle", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	cfg := SimulcastConfig{Enabled: true, Directory: "10"}
	a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.Simulcast = map[string]SimulcastConfig{s.ID: cfg}
		return nil
	})
	p := &pan115Simulcast{app: a}
	if !p.current(s, cfg) {
		t.Fatal("legacy unset global switch disabled existing binding")
	}
	a.store.update(func(st *State) error { disabled := false; st.SimulcastEnabled = &disabled; return nil })
	if p.current(s, cfg) {
		t.Fatal("global disable allowed in-flight copy")
	}
}
