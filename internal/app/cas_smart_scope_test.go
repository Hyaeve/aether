package app

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestCASFeatureInputsIgnoreCalculationMode(t *testing.T) {
	for _, source := range []string{"local", "webdav", "mobile"} {
		t.Run(source, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			hash, md, slice := strings.Repeat("a", 64), strings.Repeat("b", 32), strings.Repeat("C", 32)
			features := []CASInfo{
				{Name: "sha.mkv", Size: 42, SHA256: hash},
				{Name: "pair.mkv", Size: 43, MD5: md, SliceMD5: slice, SliceSize: 20 << 20},
				{Name: "full.mkv", Size: 44, SHA256: hash, MD5: md, SliceMD5: slice, SliceSize: 5 << 20},
				{Name: "pointer.mkv", Size: 45, SHA256: hash, MD5: md, SliceMD5: slice, SliceSize: 10 << 20},
			}
			files := []File{}
			for i, feature := range features {
				raw, err := marshalCASContent(feature)
				if err != nil {
					t.Fatal(err)
				}
				name := feature.Name + ".cas"
				if i == 3 {
					name += ".strm"
					raw = []byte(a.generatedStreamURL(streamClaim{CAS: &feature, Redirect: true}))
				}
				writeTest(t, filepath.Join(s.Config["root"], name), string(raw))
				files = append(files, File{ID: "/" + name, Name: name, Size: int64(len(raw)), SHA256: strings.Repeat("d", 64)})
			}
			server := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(s.Config["root"]), LockSystem: webdav.NewMemLS()})
			defer server.Close()
			if source == "webdav" {
				s.Type, s.Config = "webdav", map[string]string{"address": server.URL, "root": "/"}
			}
			if source == "mobile" {
				s.Type, s.Config = "mobile", map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}
				old := apiClient
				defer func() { apiClient = old }()
				apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
					if !strings.HasSuffix(r.URL.Hostname(), ".yun.139.com") {
						return http.DefaultTransport.RoundTrip(r)
					}
					var result any
					switch r.URL.Path {
					case "/user/route/qryRoutePolicy":
						result = map[string]any{"routePolicyList": []any{map[string]string{"modName": "personal", "httpsUrl": "https://personal.yun.139.com"}}}
					case "/file/getDownloadUrl":
						var input map[string]string
						if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
							t.Fatal(err)
						}
						result = map[string]string{"url": server.URL + input["fileId"]}
					default:
						t.Fatalf("unexpected mobile request %s %s", r.Method, r.URL.Path)
					}
					raw, _ := json.Marshal(map[string]any{"success": true, "data": result})
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
				})}
			}
			if err := a.store.update(func(st *State) error {
				st.Storages = []Storage{s}
				st.Settings.CacheEnabled, st.Settings.CacheMemoryMB, st.Settings.CacheMaxItems = true, 128, 10000
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			a.cache.put(s.ID+":"+rootOf(s), files, 30, a.store.snapshot().Settings)
			for _, mode := range []string{"all", "mobile", "tianyi", "auto"} {
				t.Run(mode, func(t *testing.T) {
					task := Task{Name: "preserve features", Kind: "cas", CASOperation: "generate", CASInfoMode: mode, StorageID: s.ID, Source: rootOf(s), Target: mode}
					if n, err := a.executeTask(context.Background(), task, s); err != nil || n != len(features) {
						t.Fatalf("feature input was filtered by %s: count=%d error=%v", mode, n, err)
					}
					for _, want := range features {
						raw, err := os.ReadFile(filepath.Join(a.outputDir, mode, want.Name+".cas.strm"))
						if err != nil {
							t.Fatal(err)
						}
						got := generatedCASClaim(t, raw)
						if got.Name != want.Name || got.Size != want.Size || got.SHA256 != want.SHA256 || got.MD5 != want.MD5 || got.SliceMD5 != want.SliceMD5 || got.SliceSize != want.SliceSize {
							t.Fatalf("input fields changed: got=%+v want=%+v", got, want)
						}
					}
				})
			}
		})
	}
}

func TestCASAutoDefaultAndAvailableFeatures(t *testing.T) {
	a := testApp(t)
	local := addLocal(t, a)
	mobile := Storage{ID: "native-mobile", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native", "authorization": "test"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, mobile); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, s := range []Storage{local, mobile} {
		task := Task{Name: "auto-default", Kind: "cas", StorageID: s.ID, Source: "/", Target: "out"}
		if err := a.validateTask(&task); err != nil || task.CASInfoMode != "auto" {
			t.Fatalf("default not persisted as auto: %+v %v", task, err)
		}
		task.CASInfoMode = "all"
		if err := a.validateTask(&task); err != nil || task.CASInfoMode != "all" {
			t.Fatal("explicit calculation changed", task, err)
		}
	}
	old := apiClient
	defer func() { apiClient = old }()
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("auto mobile metadata must not download or query: %s", r.URL)
		return nil, nil
	})}
	sha := strings.Repeat("a", 64)
	info, err := a.generateCASInfo(context.Background(), mobile, File{ID: "file", Name: "movie.mkv", Size: 42, SHA256: sha, MD5: strings.Repeat("b", 32)}, "auto")
	if err != nil || info.SHA256 != sha || info.MD5 != "" || info.SliceMD5 != "" {
		t.Fatal("auto mobile feature selection failed", info, err)
	}
	if _, err := a.generateCASInfo(context.Background(), mobile, File{Name: "missing.mkv", Size: 42}, "auto"); err == nil {
		t.Fatal("missing native SHA256 fabricated")
	}
}

func TestCASAutoConfigurationPersistsAcrossRestart(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, mode := range []string{"", "auto", "all", "mobile", "tianyi"} {
		input := Task{Name: "persistent-" + mode, Kind: "cas", StorageID: s.ID, Source: "/", Target: "out-" + mode, CASInfoMode: mode, Mode: "full"}
		w := request(t, h, "POST", "/api/tasks", input, cookie)
		if w.Code != 201 {
			t.Fatal("smart mode rejected by API", w.Code, w.Body.String())
		}
	}
	state := a.store.snapshotWithLogLimit(0)
	if err := validateBundle(configBundle{State: state}); err != nil {
		t.Fatal("smart mode rejected by configuration import", err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range []State{state, reopened.snapshotWithLogLimit(0)} {
		if len(snapshot.Tasks) != 5 {
			t.Fatal("tasks lost across restart", snapshot.Tasks)
		}
		for i, want := range []string{"auto", "auto", "all", "mobile", "tianyi"} {
			if got := snapshot.Tasks[i]; got.CASInfoMode != want || got.Mode != "incremental" {
				t.Fatal("calculation choice lost", got, want)
			}
		}
	}
}

func TestCASAutoLocalAndRemoteCalculateCompleteFeatures(t *testing.T) {
	for _, source := range []string{"local", "webdav", "tianyi"} {
		t.Run(source, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			content := "full-feature-content"
			writeTest(t, filepath.Join(s.Config["root"], "movie.mkv"), content)
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				w.Write([]byte(content))
			}))
			defer server.Close()
			if source == "webdav" {
				s.Type, s.Config = "webdav", map[string]string{"address": server.URL, "root": "/"}
			}
			if source == "tianyi" {
				s.Type, s.Config = "tianyi", map[string]string{"username": "account", "password": "secret"}
				a.tianyiSessions[s.ID] = tianyiSession{Key: "session", Secret: "secret", Credentials: tianyiCredentials(s), Expires: time.Now().Add(time.Hour)}
				old := apiClient
				defer func() { apiClient = old }()
				apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
					if r.URL.Hostname() == "127.0.0.1" {
						return http.DefaultTransport.RoundTrip(r)
					}
					if r.Method != "GET" || r.URL.Path != "/getFileDownloadUrl.action" {
						t.Fatalf("unexpected Tianyi operation: %s %s", r.Method, r.URL.Path)
					}
					raw, _ := json.Marshal(map[string]string{"fileDownloadUrl": server.URL + "/movie.mkv"})
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
				})}
			}
			md := md5.Sum([]byte(content))
			info, err := a.generateCASInfo(context.Background(), s, File{ID: "/movie.mkv", Name: "movie.mkv", Size: int64(len(content)), MD5: hex.EncodeToString(md[:])}, "auto")
			sha := sha256.Sum256([]byte(content))
			if err != nil || info.SHA256 != hex.EncodeToString(sha[:]) || info.MD5 != hex.EncodeToString(md[:]) || info.SliceMD5 != strings.ToUpper(info.MD5) || info.SliceSize != 10<<20 {
				t.Fatal("auto did not compute complete verified features", info, err)
			}
			if source != "local" {
				if reads.Load() != 1 {
					t.Fatal("remote content downloaded repeatedly", reads.Load())
				}
				files, err := os.ReadDir(a.transferTempDirectory())
				if err != nil || len(files) != 0 {
					t.Fatal("auto temporary media retained", files, err)
				}
			}
		})
	}
}

func TestNamedTaskScopesProtectOnlyRelatedDirectories(t *testing.T) {
	for _, kind := range []string{"cas", "strm", "ed2k", "cache"} {
		t.Run(kind, func(t *testing.T) {
			a, s := runningCASTest(t)
			active := a.store.snapshotWithLogLimit(0).Tasks[0]
			active.Kind, active.Name = kind, "精确范围任务"
			if err := a.store.update(func(st *State) error { st.Tasks[0] = active; return nil }); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				id, child string
				blocked   bool
			}{
				{"/parent/source", "", true}, {"/parent/source/sub/episode.mkv", "", true},
				{"/parent", "", true}, {"/parent/sibling/other.mkv", "", false},
				{"/parent", "new sibling/movie.mkv", false}, {"/parent/source2", "", false},
				{"/parent/source", "new.mkv", true},
			} {
				err := a.checkTaskFileNodes(context.Background(), []taskFileNode{{s, tc.id, tc.child}})
				if (err != nil) != tc.blocked || err != nil && !strings.Contains(err.Error(), active.Name) {
					t.Fatalf("kind=%s node=%s/%s blocked=%t: %v", kind, tc.id, tc.child, tc.blocked, err)
				}
			}
			out, err := a.taskRunNodes(active, s)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "cache" {
				if err := a.checkTaskFileNodes(context.Background(), out[1:]); err == nil || !strings.Contains(err.Error(), active.Name) {
					t.Fatal("output directory not protected by named task", err)
				}
			}
			candidate := Task{ID: "next", Name: "unrelated", Kind: kind, CASOperation: "generate", StorageID: s.ID, Source: "/parent/sibling", SourceLabel: "sibling", Target: "another-output"}
			if err := a.checkTaskStartScopes(context.Background(), candidate, s); err != nil {
				t.Fatal("unrelated task blocked", err)
			}
			candidate.Source = "/parent/source/sub"
			if err := a.checkTaskStartScopes(context.Background(), candidate, s); err == nil || !strings.Contains(err.Error(), active.Name) {
				t.Fatal("overlapping task not identified", err)
			}
			candidate.Source, candidate.Mode = "/parent/sibling", "incremental"
			if err := a.store.update(func(st *State) error { st.Tasks = append(st.Tasks, candidate); return nil }); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			if err := a.startTaskContext(ctx, candidate.ID, done); err != nil {
				t.Fatal("unrelated task rejected by execution entry", err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal("unrelated task failed during execution", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if got := a.store.snapshotWithLogLimit(0).Tasks[1]; got.Status != "success" || got.Processed == 0 {
				t.Fatal("unrelated task did not complete real work", got)
			}
			if err := a.checkTaskFileNodes(a.ctx, []taskFileNode{{s, active.Source, ""}}); err == nil || !strings.Contains(err.Error(), active.Name) {
				t.Fatal("completing sibling released active task protection", err)
			}
		})
	}
}

func TestNamedTaskBusyResponsesIdentifyTask(t *testing.T) {
	a, s := runningCASTest(t)
	active := a.store.snapshotWithLogLimit(0).Tasks[0]
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/tasks/" + active.ID + "/run", nil},
		{"PUT", "/api/tasks/" + active.ID, active},
		{"DELETE", "/api/tasks/" + active.ID, nil},
		{"DELETE", "/api/storages/" + s.ID, nil},
	} {
		w := request(t, h, req.method, req.path, req.body, cookie)
		if w.Code != 409 || !strings.Contains(w.Body.String(), active.Name) {
			t.Fatalf("busy response omitted running task: %s %s %d %s", req.method, req.path, w.Code, w.Body.String())
		}
	}
}

func TestNamedTaskManualOperationsAndReservations(t *testing.T) {
	for _, kind := range []string{"strm", "ed2k", "cache"} {
		t.Run(kind, func(t *testing.T) {
			a, s := runningCASTest(t)
			if err := a.store.update(func(st *State) error { st.Tasks[0].Kind = kind; return nil }); err != nil {
				t.Fatal(err)
			}
			active := a.store.snapshotWithLogLimit(0).Tasks[0]
			h := a.Handler(t.TempDir())
			cookie := setupTest(t, a, h)
			for _, operation := range []fileActionRequest{
				{Action: "mkdir", Source: "/parent", Name: "new sibling"},
				{Action: "rename", Source: "/parent/sibling", IDs: []string{"/parent/sibling/other.mkv"}, Name: "renamed.mkv"},
				{Action: "copy", Source: "/parent/sibling", IDs: []string{"/parent/sibling/renamed.mkv"}, Target: "/parent"},
			} {
				operation.StorageID, operation.TargetStorage = s.ID, s.ID
				w := request(t, h, "POST", "/api/files/action", operation, cookie)
				if w.Code != 200 {
					t.Fatal("unrelated manual operation blocked", operation.Action, w.Code, w.Body.String())
				}
			}
			w := request(t, h, "POST", "/api/files/action", fileActionRequest{Action: "delete", StorageID: s.ID, Source: "/parent", IDs: []string{"/parent/source"}}, cookie)
			if w.Code != 409 || !strings.Contains(w.Body.String(), active.Name) {
				t.Fatal("protected operation lost task name", w.Code, w.Body.String())
			}
			staged := filepath.Join(t.TempDir(), "upload")
			writeTest(t, staged, "new media")
			if err := a.storeUploadedFile(a.ctx, s, "/parent/sibling", "uploaded.mkv", staged); err != nil {
				t.Fatal("unrelated upload blocked", err)
			}
			a.runMu.Lock()
			release, err := a.reserveTaskFileNodes(a.ctx, []taskFileNode{{s, "/parent/sibling", ""}})
			delete(a.running, active.ID)
			a.runMu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			candidate := Task{ID: "new", Name: "next task", Kind: kind, StorageID: s.ID, Source: "/parent/sibling", SourceLabel: "sibling", Target: "next-output"}
			if err := a.checkTaskStartScopes(a.ctx, candidate, s); err == nil || !strings.Contains(err.Error(), candidate.Name) {
				t.Fatal("new task ignored reserved transfer scope", err)
			}
			candidate.Source, candidate.SourceLabel = "/parent/source", "source"
			if err := a.checkTaskStartScopes(a.ctx, candidate, s); err != nil {
				t.Fatal("unrelated reservation blocked task", err)
			}
		})
	}
}
