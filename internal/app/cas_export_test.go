package app

import (
	"archive/zip"
	"bytes"
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
)

func TestCASExportPackagesStructureSingleAndSharedDirectory(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	for name, content := range map[string]string{"a.mp4": "media", "b.flac": "audio", "Show/Season 1/ep.mkv": "episode", "Show/readme.txt": "ignore"} {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), content)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	if w := request(t, h, "GET", "/api/files/cas-export", nil, nil); w.Code != 401 {
		t.Fatal("unprotected", w.Code)
	}
	output := t.TempDir()
	if w := request(t, h, "PUT", "/api/files/cas-export", map[string]string{"directory": output}, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil || reopened.store.snapshot().CASExportDirectory != output {
		t.Fatal("directory lost", err)
	}
	for _, tc := range []struct {
		ids    []string
		want   []string
		single bool
	}{{[]string{"/a.mp4"}, []string{"a.mp4"}, true}, {[]string{"/a.mp4", "/b.flac"}, []string{"a.mp4.cas", "b.flac.cas"}, false}, {[]string{"/Show"}, []string{"Show/Season 1/ep.mkv.cas"}, false}, {[]string{"/Show/Season 1/ep.mkv"}, []string{"ep.mkv"}, true}, {[]string{"/b.flac", "/Show", "/a.mp4"}, []string{"Show/Season 1/ep.mkv.cas", "a.mp4.cas", "b.flac.cas"}, false}} {
		w := request(t, h, "POST", "/api/files/cas-export", casExportRequest{StorageID: s.ID, Source: "/", IDs: tc.ids, Confirm: true}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if tc.single {
			var info CASInfo
			if json.Unmarshal(w.Body.Bytes(), &info) != nil || info.Name != tc.want[0] || info.SHA256 == "" || info.MD5 == "" || info.PlaybackURL != "" {
				t.Fatal("bad single", w.Body.String())
			}
			continue
		}
		z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		if len(z.File) != len(tc.want) {
			t.Fatal("bad archive count", len(z.File))
		}
		for i, f := range z.File {
			if f.Name != tc.want[i] {
				t.Fatal("bad structure", f.Name)
			}
			r, e := f.Open()
			if e != nil {
				t.Fatal(e)
			}
			raw, e := io.ReadAll(r)
			r.Close()
			var info CASInfo
			if e != nil || json.Unmarshal(raw, &info) != nil || validateCAS(info) != nil {
				t.Fatal("bad feature", string(raw), e)
			}
		}
	}
	w := request(t, h, "POST", "/api/files/cas-export", casExportRequest{StorageID: s.ID, Source: "/", IDs: []string{"/a.mp4"}, Directory: output, Confirm: true}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(output, "a.mp4.cas"))
	if err != nil {
		t.Fatal(err)
	}
	repeat := request(t, h, "POST", "/api/files/cas-export", casExportRequest{StorageID: s.ID, Source: "/", IDs: []string{"/a.mp4"}, Directory: output, Confirm: true}, cookie)
	if repeat.Code != 400 {
		t.Fatal("overwrite", repeat.Code)
	}
	again, _ := os.ReadFile(filepath.Join(output, "a.mp4.cas"))
	if !bytes.Equal(raw, again) {
		t.Fatal("existing changed")
	}
	for _, tc := range []casExportRequest{{StorageID: s.ID, Source: "/", IDs: []string{"/missing"}, Confirm: true}, {StorageID: s.ID, Source: "/", IDs: []string{"/a.mp4", "/a.mp4"}, Confirm: true}, {StorageID: s.ID, Source: "/", IDs: []string{"/a.mp4"}}, {StorageID: s.ID, Source: "/", IDs: []string{"/a.mp4"}, Directory: a.store.dir, Confirm: true}, {StorageID: s.ID, Source: "/", IDs: []string{"/a.mp4"}, Extensions: "txt", Confirm: true}} {
		if w := request(t, h, "POST", "/api/files/cas-export", tc, cookie); w.Code != 400 {
			t.Fatal("invalid accepted", tc, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Config["root"], "a.mp4")); err != nil {
		t.Fatal("source removed", err)
	}
	entries, _ := os.ReadDir(output)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".aether-") {
			t.Fatal("staging leaked")
		}
	}
}

func TestCASExportCancellationAndSelectionBounds(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "film.mp4"), "media")
	ctx, cancel := context.WithCancel(a.ctx)
	cancel()
	if _, _, err := a.casExportFiles(ctx, s, "/", nil, ""); err == nil {
		t.Fatal("cancel ignored")
	}
	if _, _, err := a.casExportFiles(context.Background(), s, "/", []string{"../../escape"}, ""); err == nil {
		t.Fatal("foreign selection accepted")
	}
}

func TestCASExportRejectsUnsupportedSourcesBeforeUpstreamRead(t *testing.T) {
	a := testApp(t)
	a.store.update(func(st *State) error {
		st.Storages = []Storage{
			{ID: "mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native"}},
			{ID: "tianyi", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "test", "password": "test"}},
		}
		return nil
	})
	old := apiClient
	defer func() { apiClient = old }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("cloud export contacted upstream: %s", r.URL.Path)
		return nil, nil
	})}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	for _, sid := range []string{"tianyi"} {
		for _, endpoint := range []string{"/api/files/cas-export", "/api/files/cas-export/jobs"} {
			w := request(t, h, "POST", endpoint, casExportRequest{StorageID: sid, Source: "/", IDs: []string{"file"}, Confirm: true}, cookie)
			if w.Code != 400 {
				t.Fatal("cloud export accepted", w.Code, w.Body.String())
			}
		}
	}
}

func TestCASExportMobileSHAOnlySingleArchiveAndQueue(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Name: "Mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "root": "/", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	defer func() { apiClient = old }()
	missingHash := false
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var data any
		switch r.URL.Path {
		case "/user/route/qryRoutePolicy":
			data = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
		case "/file/list":
			var body struct {
				Parent string `json:"parentFileId"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return nil, err
			}
			hash := strings.Repeat("A", 64)
			if missingHash {
				hash = ""
			}
			file := func(id, name string) map[string]any {
				return map[string]any{"fileId": id, "name": name, "type": "file", "size": 42, "contentHash": hash, "contentHashAlgorithm": "SHA256"}
			}
			items := []any{file("z", "z.mp4"), map[string]any{"fileId": "show", "name": "Show", "type": "folder"}, file("a", "a.mkv")}
			if body.Parent == "show" {
				items = []any{file("episode", "ep.mkv")}
			} else if body.Parent != "/" {
				return nil, fmt.Errorf("unexpected directory %s", body.Parent)
			}
			data = map[string]any{"items": items}
		default:
			t.Errorf("mobile CAS export must not download media or write upstream: %s", r.URL.Path)
			return nil, fmt.Errorf("unexpected upstream operation")
		}
		raw, err := json.Marshal(map[string]any{"success": true, "data": data})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}, err
	})}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	verify := func(raw []byte, name string) {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]any{"name": name, "size": float64(42), "sha256": strings.Repeat("a", 64), "md5": "", "slice_md5": "", "slice_size": float64(0)} {
			if fields[key] != want {
				t.Fatalf("%s = %v, want %v: %s", key, fields[key], want, raw)
			}
		}
		if len(fields) != 6 || fields["provider"] != nil || fields["playback_url"] != nil || fields["storageId"] != nil {
			t.Fatal("account binding leaked", fields)
		}
		if strings.Contains(string(raw), "\n  \"provider\"") || !strings.Contains(string(raw), "\n  \"sha256\"") {
			t.Fatal("standalone CAS is not the documented indented format", string(raw))
		}
	}
	v := casExportRequest{StorageID: s.ID, Source: "/", IDs: []string{"z"}, Confirm: true}
	w := request(t, h, "POST", "/api/files/cas-export", v, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	verify(w.Body.Bytes(), "z.mp4")
	v.IDs = []string{"z", "show", "a"}
	w = request(t, h, "POST", "/api/files/cas-export", v, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Show/ep.mkv.cas", "a.mkv.cas", "z.mp4.cas"}
	if len(z.File) != len(want) {
		t.Fatal("archive count", len(z.File))
	}
	for i, f := range z.File {
		if f.Name != want[i] {
			t.Fatal("archive order/structure", f.Name)
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		verify(raw, strings.TrimSuffix(filepath.Base(f.Name), ".cas"))
	}
	// Dispatch synchronously so the mock cannot outlive this test.
	a.casExportRunning = true
	v.IDs = []string{"a"}
	w = request(t, h, "POST", "/api/files/cas-export/jobs", v, cookie)
	var job casExportJob
	if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &job) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	a.runCASExports()
	w = request(t, h, "GET", "/api/files/cas-export/jobs/"+job.ID, nil, cookie)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil || job.Status != "success" || job.Processed != 1 || job.Total != 1 {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "GET", "/api/files/cas-export/jobs/"+job.ID+"/download", nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	verify(w.Body.Bytes(), "a.mkv")
	missingHash = true
	w = request(t, h, "POST", "/api/files/cas-export", v, cookie)
	if w.Code != 400 {
		t.Fatal("missing SHA256 accepted", w.Code)
	}
}

func TestRapid115FiltersPruneAndPersist(t *testing.T) {
	a, job := rapid115Fixture(t)
	job.SuccessAction = "keep"
	job.Filters = []BackupFilter{{Type: "path_prefix", Mode: "exclude", Value: "excluded", MatchFile: true, MatchDir: true}, {Type: "extension", Mode: "include", Value: ".MP4", MatchFile: true}}
	job = normalizeRapid115Job(job)
	if err := a.store.update(func(st *State) error { st.Rapid115.Manual = job; return nil }); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"kept.mp4": "media", "ignored.flac": "audio", "excluded/no.mp4": "media"} {
		writeTest(t, filepath.Join(job.Source, filepath.FromSlash(name)), content)
	}
	calls := 0
	a.rapid115Upload = func(_ context.Context, _ Storage, _ string, name string, _ *os.File, _ int64, _ bool) (bool, string, error) {
		calls++
		if name != "kept.mp4" {
			t.Fatal("filter ignored", name)
		}
		return true, "pick", nil
	}
	if err := a.runRapid115(a.ctx, job, "manual", "手动", rapid115Stamp(a.store.snapshot().Rapid115)); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	reopened, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil || len(reopened.store.snapshot().Rapid115.Manual.Filters) != 2 {
		t.Fatal("filters not persistent", err)
	}
	for _, filter := range []BackupFilter{{Type: "regex", Mode: "include", Value: "[", MatchFile: true}, {Type: "size", Mode: "include", MinSize: 10, MaxSize: 1, Unit: "MB", MatchFile: true}, {Type: "path_prefix", Mode: "exclude", Value: "../escape", MatchDir: true}} {
		job.Filters = []BackupFilter{filter}
		if validateRapid115Job(job, a.store.snapshot(), true) == nil {
			t.Fatal("invalid filter accepted", filter)
		}
	}
}
