package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestCASExportCloudHashesWithoutMediaDownload(t *testing.T) {
	for _, kind := range []string{"mobile", "tianyi"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: kind, Type: kind, Enabled: true, Config: map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:13900000000:token")), "username": "account", "password": "secret"}}
			if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			a.tianyiSessions[s.ID] = tianyiSession{Key: "session", Secret: "0123456789abcdef", Credentials: sha256.Sum256([]byte("account\x00secret")), Expires: time.Now().Add(time.Hour)}
			old := apiClient
			defer func() { apiClient = old }()
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				switch r.URL.Path {
				case "/user/route/qryRoutePolicy":
					body = `{"success":true,"data":{"routePolicyList":[{"modName":"personal","httpsUrl":"https://personal.yun.139.com"}]}}`
				case "/file/list":
					body = `{"success":true,"data":{"items":[{"fileId":"file2","name":"z.mp4","type":"file","size":5,"contentHashAlgorithm":"SHA256","contentHash":"` + strings.Repeat("c", 64) + `"},{"fileId":"file","name":"cloud.mp4","type":"file","size":5,"contentHashAlgorithm":"SHA256","contentHash":"` + strings.Repeat("a", 64) + `"}]}}`
				case "/listFiles.action":
					body = `{"fileListAO":{"folderList":[],"fileList":[{"id":31,"name":"z.mp4","size":5,"md5":"` + strings.Repeat("d", 32) + `"},{"id":30,"name":"cloud.mp4","size":5,"md5":"` + strings.Repeat("b", 32) + `"}]}}`
				default:
					t.Fatalf("must not download media: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			h := a.Handler(t.TempDir())
			cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
			id := "file"
			if kind == "tianyi" {
				id = "30"
			}
			w := request(t, h, "POST", "/api/files/cas-export", casExportRequest{StorageID: s.ID, Source: "/", IDs: []string{id}, Confirm: true}, cookie)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var info CASInfo
			if json.Unmarshal(w.Body.Bytes(), &info) != nil || validateCAS(info) != nil {
				t.Fatal(w.Body.String())
			}
			if kind == "mobile" && info.SHA256 == "" || kind == "tianyi" && info.MD5 == "" {
				t.Fatal("missing cloud feature", info)
			}
			secondID := "file2"
			if kind == "tianyi" {
				secondID = "31"
			}
			for _, ids := range [][]string{{secondID, id}, {id, secondID}} {
				w := request(t, h, "POST", "/api/files/cas-export", casExportRequest{StorageID: s.ID, Source: "/", IDs: ids, Confirm: true}, cookie)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
				if err != nil {
					t.Fatal(err)
				}
				names := []string{}
				for _, member := range archive.File {
					names = append(names, member.Name)
				}
				if !reflect.DeepEqual(names, []string{"cloud.mp4.cas", "z.mp4.cas"}) {
					t.Fatal("ZIP order depends on upstream or selection order", names)
				}
			}
		})
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
