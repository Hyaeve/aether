package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func runningCASTest(t *testing.T) (*App, Storage) {
	t.Helper()
	a := testApp(t)
	s := addLocal(t, a)
	for _, name := range []string{"parent/source/movie.mkv", "parent/source/sub/episode.mkv", "parent/sibling/other.mkv"} {
		writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), "media")
	}
	task := Task{ID: "active", Name: "运行中的源目录", Kind: "cas", CASOperation: "generate", CASInfoMode: "all", Mode: "incremental", StorageID: s.ID, Source: "/parent/source", SourceLabel: "source", Status: "running", Target: "generated"}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	a.running[task.ID] = func() {}
	a.runningStorage[task.ID] = s.ID
	return a, s
}

func TestCASTaskIndependentSchedulingAndAliases(t *testing.T) {
	a, s := runningCASTest(t)
	active := a.store.snapshotWithLogLimit(0).Tasks[0]
	for _, tc := range []struct {
		source, target string
		blocked        bool
	}{
		{"/parent/sibling", active.Target, false},
		{"/parent/source", "other-output", true},
		{"/parent/source/sub", "other-output", true},
		{"/parent", "other-output", true},
		{"/parent/sibling", filepath.Join(a.outputDir, active.Target, "source"), true},
		{"/parent/sibling", filepath.Join(s.Config["root"], "parent/source"), true},
	} {
		t.Run(tc.source+"/"+tc.target, func(t *testing.T) {
			candidate := Task{ID: "next", Name: "next", Kind: "cas", CASOperation: "generate", StorageID: s.ID, Source: tc.source, SourceLabel: filepath.Base(tc.source), Target: tc.target}
			err := a.checkTaskStartScopes(context.Background(), candidate, s)
			if (err != nil) != tc.blocked {
				t.Fatalf("blocked=%t, got %v", tc.blocked, err)
			}
		})
	}
	alias := Storage{ID: "alias", Type: "local", Enabled: true, Config: map[string]string{"root": filepath.Join(s.Config["root"], "parent/source")}}
	if err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{alias, "/sub/episode.mkv", ""}}); err == nil {
		t.Fatal("local alias bypassed protection")
	}
	alias.Config["root"] = filepath.Join(s.Config["root"], "parent/sibling")
	if err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{alias, "/other.mkv", ""}}); err != nil {
		t.Fatal("independent alias blocked", err)
	}
}

func TestCASTaskOpaqueAncestryFreshAndFailClosed(t *testing.T) {
	old := apiClient
	defer func() { apiClient = old }()
	for _, tc := range []struct {
		id, child     string
		blocked, fail bool
	}{
		{"sibling", "", false, false},
		{"sibling-file", "", false, false},
		{"source-file", "", true, false},
		{"ancestor", "", true, false},
		{"ancestor", "new sibling", false, false},
		{"ancestor", "source/new.mkv", true, false},
		{"source-sub", "new.mkv", true, false},
		{"sibling", "", true, true},
	} {
		t.Run(tc.id+"/"+tc.child, func(t *testing.T) {
			a := testApp(t)
			s := Storage{ID: "q", Name: "Quark", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "test", "root": "ancestor"}}
			task := Task{ID: "active", Kind: "cas", StorageID: s.ID, Source: "source", SourceLabel: "source", Target: "generated", SourceTrail: []DirectoryCrumb{{ID: "forged", Name: "forged"}}}
			if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; st.Storages = []Storage{s}; return nil }); err != nil {
				t.Fatal(err)
			}
			a.running[task.ID] = func() {}
			apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.Path != "/1/clouddrive/file/sort" {
					t.Fatalf("unexpected write or endpoint %s %s", r.Method, r.URL)
				}
				raw := `{"status":200,"code":0,"data":{"list":[]}}`
				switch r.URL.Query().Get("pdir_fid") {
				case "0":
					raw = `{"status":200,"code":0,"data":{"list":[{"fid":"ancestor","file_name":"parent","dir":true}]}}`
				case "ancestor":
					raw = `{"status":200,"code":0,"data":{"list":[{"fid":"source","file_name":"source","dir":true},{"fid":"sibling","file_name":"sibling","dir":true}]}}`
				case "source":
					raw = `{"status":200,"code":0,"data":{"list":[{"fid":"source-sub","file_name":"sub","dir":true},{"fid":"source-file","file_name":"movie.mkv","dir":false}]}}`
				case "sibling":
					raw = `{"status":200,"code":0,"data":{"list":[{"fid":"sibling-file","file_name":"other.mkv","dir":false}]}}`
				}
				status := 200
				if tc.fail {
					status = 503
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw)), Request: r}, nil
			})}
			err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{s, tc.id, tc.child}})
			if (err != nil) != tc.blocked {
				t.Fatalf("blocked=%t, got %v", tc.blocked, err)
			}
		})
	}
}

func TestCASTaskSiblingStartsWhileSourceRemainsLocked(t *testing.T) {
	a, s := runningCASTest(t)
	task := Task{ID: "next", Name: "sibling", Kind: "cas", CASOperation: "generate", CASInfoMode: "all", Mode: "incremental", StorageID: s.ID, Source: "/parent/sibling", SourceLabel: "sibling", Target: "generated", APIInterval: 1500}
	if err := a.store.update(func(st *State) error { st.Tasks[0].APIInterval = 300; st.Tasks = append(st.Tasks, task); return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	if err := a.startTaskContext(ctx, task.ID, done); err != nil {
		t.Fatal("independent task rejected", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := os.Stat(filepath.Join(a.outputDir, "generated/sibling/other.mkv.cas.strm")); err != nil {
		t.Fatal(err)
	}
	if err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{s, "/parent/source/movie.mkv", ""}}); err == nil {
		t.Fatal("finishing sibling released another task's lock")
	}
	a.gateMu.Lock()
	interval := a.intervals[s.ID]
	a.gateMu.Unlock()
	if interval != 300 {
		t.Fatal("finished task left its throttle on sibling", interval)
	}
}

func TestCASTaskCrossStorageSiblingCopyAndMove(t *testing.T) {
	for _, action := range []string{"copy", "move"} {
		t.Run(action, func(t *testing.T) {
			a, s := runningCASTest(t)
			targetDir := t.TempDir()
			server := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(targetDir), LockSystem: webdav.NewMemLS()})
			defer server.Close()
			target := Storage{ID: "dav", Name: "DAV", Type: "webdav", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
			if err := a.store.update(func(st *State) error {
				st.Storages = append(st.Storages, target)
				st.TransferSettings = normalizedTransferSettings(st.TransferSettings)
				st.TransferSettings.UploadDelay = 0
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			response := request(t, h, "POST", "/api/files/action", fileActionRequest{StorageID: s.ID, TargetStorage: target.ID, Source: "/parent", Target: "/", IDs: []string{"/parent/sibling"}, Action: action}, cookie)
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			if raw, err := os.ReadFile(filepath.Join(targetDir, "sibling/other.mkv")); err != nil || string(raw) != "media" {
				t.Fatal("copy was not verified", string(raw), err)
			}
			if _, err := os.Stat(filepath.Join(s.Config["root"], "parent/source/movie.mkv")); err != nil {
				t.Fatal("CAS source changed", err)
			}
			if len(a.taskFileUses) != 0 {
				t.Fatal("reservation leaked after transfer")
			}
		})
	}
}

func TestCASTaskTransferReservationRejectsOnlyOverlappingStarts(t *testing.T) {
	a, s := runningCASTest(t)
	a.runMu.Lock()
	release, err := a.reserveTaskFileAction(context.Background(), s, s, fileActionRequest{Action: "copy", Source: "/parent/sibling", IDs: []string{"/parent/sibling/other.mkv"}, Target: "/parent", TargetStorage: s.ID})
	a.runMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	delete(a.running, "active")
	for _, tc := range []struct {
		source  string
		blocked bool
	}{
		{"/parent/source", false}, {"/parent/sibling", true},
	} {
		task := Task{Kind: "cas", StorageID: s.ID, Source: tc.source, SourceLabel: filepath.Base(tc.source), Target: "other"}
		if err := a.checkTaskStartScopes(context.Background(), task, s); (err != nil) != tc.blocked {
			t.Fatalf("%s blocked=%t: %v", tc.source, tc.blocked, err)
		}
	}
}

func TestCASTaskTextEditsAndRunningConfiguration(t *testing.T) {
	a, s := runningCASTest(t)
	for _, dir := range []string{"source", "sibling"} {
		writeTest(t, filepath.Join(s.Config["root"], "parent", dir, "movie.nfo"), "<movie/>")
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, dir := range []string{"source", "sibling"} {
		address := "/api/files/text?storage=" + s.ID + "&parent=/parent/" + dir + "&id=/parent/" + dir + "/movie.nfo"
		get := request(t, h, "GET", address, nil, cookie)
		var content map[string]string
		if get.Code != 200 || json.Unmarshal(get.Body.Bytes(), &content) != nil {
			t.Fatal(get.Code, get.Body.String())
		}
		put := request(t, h, "PUT", address, map[string]string{"content": "<movie>changed</movie>", "revision": content["revision"]}, cookie)
		want := 200
		if dir == "source" {
			want = 409
		}
		if put.Code != want {
			t.Fatal(put.Code, put.Body.String())
		}
	}
	task := a.store.snapshotWithLogLimit(0).Tasks[0]
	if response := request(t, h, "PUT", "/api/tasks/"+task.ID, task, cookie); response.Code != 409 {
		t.Fatal("running config accepted", response.Code, response.Body.String())
	}
	if response := request(t, h, "DELETE", "/api/tasks/"+task.ID, nil, cookie); response.Code != 409 {
		t.Fatal("running task removed", response.Code, response.Body.String())
	}
}

func TestCASTaskFileActionsOnlyLockSourceTree(t *testing.T) {
	for _, tc := range []struct {
		name, action, source, item, target, newName string
		want                                        int
	}{
		{"mkdir sibling", "mkdir", "/parent", "", "", "new sibling", 200},
		{"mkdir inside", "mkdir", "/parent/source", "", "", "new child", 409},
		{"rename sibling", "rename", "/parent/sibling", "/parent/sibling/other.mkv", "", "renamed.mkv", 200},
		{"rename source", "rename", "/parent", "/parent/source", "", "renamed", 409},
		{"rename over source", "rename", "/parent", "/parent/sibling", "", "source", 409},
		{"rename child", "rename", "/parent/source/sub", "/parent/source/sub/episode.mkv", "", "renamed.mkv", 409},
		{"delete ancestor", "delete", "/", "/parent", "", "", 409},
		{"copy out", "copy", "/parent/source", "/parent/source/movie.mkv", "/parent/sibling", "", 409},
		{"copy in", "copy", "/parent/sibling", "/parent/sibling/other.mkv", "/parent/source", "", 409},
		{"copy sibling", "copy", "/parent/sibling", "/parent/sibling/other.mkv", "/parent", "", 200},
		{"move sibling", "move", "/parent/sibling", "/parent/sibling/other.mkv", "/parent", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, s := runningCASTest(t)
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			req := fileActionRequest{Action: tc.action, StorageID: s.ID, Source: tc.source, Name: tc.newName, TargetStorage: s.ID, Target: tc.target}
			if tc.item != "" {
				req.IDs = []string{tc.item}
			}
			res := request(t, h, http.MethodPost, "/api/files/action", req, cookie)
			if res.Code != tc.want {
				t.Fatalf("got %d: %s; want %d", res.Code, res.Body.String(), tc.want)
			}
			if tc.want == 409 {
				if _, err := os.Stat(filepath.Join(s.Config["root"], "parent/source/movie.mkv")); err != nil {
					t.Fatal("source changed", err)
				}
			}
		})
	}
}

func TestCASTaskUploadSiblingAndDescendant(t *testing.T) {
	for _, tc := range []struct {
		parent, name string
		blocked      bool
	}{
		{"/parent", "new-folder/new.mkv", false},
		{"/parent/sibling", "new.mkv", false},
		{"/parent", "source/sub/new.mkv", true},
		{"/parent/source", "new.mkv", true},
	} {
		t.Run(tc.parent+"/"+tc.name, func(t *testing.T) {
			a, s := runningCASTest(t)
			staged := filepath.Join(t.TempDir(), "upload")
			writeTest(t, staged, "uploaded")
			err := a.storeUploadedFile(context.Background(), s, tc.parent, tc.name, staged)
			if (err != nil) != tc.blocked || tc.blocked && !strings.Contains(err.Error(), "CAS") {
				t.Fatalf("blocked=%t, got %v", tc.blocked, err)
			}
		})
	}
}
