package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCASExportQueueSerialProgressAndDownload(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "film.mp4"), "media")
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	// Hold dispatch until both accepted requests are present to inspect FIFO order.
	a.casExportMu.Lock()
	a.casExportRunning = true
	a.casExportMu.Unlock()
	submit := func() string {
		w := request(t, h, "POST", "/api/files/cas-export/jobs", casExportRequest{StorageID: s.ID, Source: "/", IDs: []string{"/film.mp4"}, Confirm: true}, cookie)
		var job casExportJob
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &job) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return job.ID
	}
	first, second := submit(), submit()
	a.casExportMu.Lock()
	if len(a.casExportQueue) != 2 || a.casExportQueue[0].ID != first || a.casExportQueue[1].ID != second {
		t.Error("FIFO order lost")
	}
	a.casExportMu.Unlock()
	w := request(t, h, "GET", "/api/files/cas-export/jobs/"+second, nil, cookie)
	var job casExportJob
	json.Unmarshal(w.Body.Bytes(), &job)
	if job.Status != "queued" {
		t.Fatal(job)
	}
	go a.runCASExports()
	for _, key := range []string{first, second} {
		deadline := time.Now().Add(10 * time.Second)
		for {
			w = request(t, h, "GET", "/api/files/cas-export/jobs/"+key, nil, cookie)
			json.Unmarshal(w.Body.Bytes(), &job)
			if job.Status == "success" {
				break
			}
			if job.Status == "error" || time.Now().After(deadline) {
				t.Fatal(job)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if job.Total != 1 || job.Processed != 1 {
			t.Fatal("progress", job)
		}
		w = request(t, h, "GET", "/api/files/cas-export/jobs/"+key+"/download", nil, cookie)
		var info CASInfo
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &info) != nil || info.SHA256 == "" || info.MD5 == "" || info.SliceMD5 == "" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for {
		a.casExportMu.Lock()
		running := a.casExportRunning
		a.casExportMu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if request(t, h, "GET", "/api/files/cas-export/jobs/"+first, nil, nil).Code != 401 {
		t.Fatal("unprotected jobs")
	}
}

func TestCloud115ShareProtocolPreferencesAndNoWriteRetry(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "cloud115", Name: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	old := apiClient
	defer func() { apiClient = old }()
	writes := 0
	reject := false
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"state":true,"cid":"0","count":1,"data":[{"fid":"10","n":"film.mp4","s":4,"fc":0}]}`
		status := 200
		switch r.URL.Path {
		case "/share/send":
			writes++
			raw = `{"state":true,"data":{"share_code":"abc123","receive_code":"ABCD"}}`
			if reject {
				status = 403
			}
		case "/share/updateshare":
			writes++
			r.ParseForm()
			if r.Form.Get("receive_code") != "ZZ99" || r.Form.Get("share_duration") != "15" || r.Form.Get("receive_user_limit") != "3" {
				t.Fatal("share settings", r.Form)
			}
			raw = `{"state":true}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	options := cloudShareOptions{Days: 15, CodeMode: "fixed", Code: "ZZ99", Limit: 3, Autofill: true}
	w := request(t, h, "PUT", "/api/cloud/preferences?storage="+s.ID, options, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	input := map[string]any{"storageId": s.ID, "parent": "/", "ids": []string{"10"}, "options": options}
	w = request(t, h, "POST", "/api/cloud/shares", input, cookie)
	if w.Code != 200 || writes != 2 || !strings.Contains(w.Body.String(), "password=ZZ99") {
		t.Fatal(w.Code, w.Body.String(), writes)
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil || reopened.store.snapshot().CloudShareOptions["115"].Code != "ZZ99" {
		t.Fatal("preferences lost", err)
	}
	reject = true
	w = request(t, h, "POST", "/api/cloud/shares", input, cookie)
	if w.Code != 502 || writes != 3 {
		t.Fatal("write retried", w.Code, writes)
	}
	input["ids"] = []string{"foreign"}
	w = request(t, h, "POST", "/api/cloud/shares", input, cookie)
	if w.Code != 400 || writes != 3 {
		t.Fatal("foreign file shared")
	}
}

func TestRecycleCleanupEncryptedAndStrictAgeSelection(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "cleanup115", Name: "cleanup", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	rule := recycleCleanupRule{StorageID: s.ID, PIN: "123456", Cron: "0 3 * * *", Days: 10}
	value := recycleCleanupSettings{Enabled: true, Rules: []recycleCleanupRule{rule}}
	w := request(t, h, "PUT", "/api/115-recycle-cleanup", value, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	if err != nil || strings.Contains(string(raw), `"recyclePIN":"123456"`) {
		t.Fatal("PIN plaintext", err)
	}
	w = request(t, h, "GET", "/api/115-recycle-cleanup", nil, cookie)
	if strings.Contains(w.Body.String(), "123456") {
		t.Fatal("PIN exposed")
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil || reopened.store.snapshot().RecycleCleanup.Rules[0].PIN != "123456" {
		t.Fatal("PIN lost", err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	cleaned := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		raw := `{"state":true}`
		switch r.URL.Path {
		case "/rb":
			body := map[string]any{"state": true, "data": []map[string]any{{"id": "old", "file_name": "old.mp4", "file_size": "4", "dtime": time.Now().Add(-11 * 24 * time.Hour).Unix()}, {"id": "recent", "file_name": "recent.mp4", "file_size": "4", "dtime": time.Now().Add(-time.Hour).Unix()}, {"id": "unknown", "file_name": "unknown.mp4", "file_size": "4", "dtime": 0}}}
			b, _ := json.Marshal(body)
			raw = string(b)
		case "/rb/clean":
			cleaned++
			r.ParseForm()
			if r.Form.Get("rid[0]") != "old" || r.Form.Get("rid[1]") != "" || r.Form.Get("password") != "123456" {
				t.Fatal("unsafe cleanup", r.Form)
			}
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
	})}
	if count, err := a.cleanup115Recycle(context.Background(), rule); err != nil || count != 1 || cleaned != 1 {
		t.Fatal(err, cleaned)
	}
	value.Rules = append(value.Rules, rule)
	if validateRecycleCleanup(value, a.store.snapshot()) == nil {
		t.Fatal("duplicate pool accepted")
	}
	rule.Days = 11
	value.Rules = []recycleCleanupRule{rule}
	if validateRecycleCleanup(value, a.store.snapshot()) != nil {
		t.Fatal("ten was treated as maximum")
	}
}
