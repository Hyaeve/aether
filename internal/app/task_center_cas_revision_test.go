package app

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCASInfoModeAPIAndIncrementalDefault(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	input := map[string]any{"name": "CAS selection", "kind": "cas", "storageId": s.ID, "source": "/", "target": "mode-api", "mode": "full", "casInfoMode": "mobile"}
	w := request(t, h, "POST", "/api/tasks", input, cookie)
	var result map[string]any
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["mode"] != "incremental" || result["casInfoMode"] != "mobile" {
		t.Fatal("CAS mode not persisted with fixed incremental generation", w.Code, w.Body.String())
	}
	input["casInfoMode"] = "unsupported"
	if w := request(t, h, "POST", "/api/tasks", input, cookie); w.Code != 400 {
		t.Fatal("unknown feature mode accepted", w.Code)
	}
}

func TestCASTaskInfoModeSelectsOnlyRequestedHashes(t *testing.T) {
	for _, mode := range []string{"all", "mobile", "tianyi"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			writeTest(t, filepath.Join(s.Config["root"], "movie.mkv"), "mode-dependent-media")
			raw, _ := json.Marshal(map[string]any{"name": "CAS modes", "kind": "cas", "storageId": s.ID, "source": "/", "target": mode, "mode": "incremental", "casInfoMode": mode})
			var task Task
			if err := json.Unmarshal(raw, &task); err != nil {
				t.Fatal(err)
			}
			if err := a.validateTask(&task); err != nil {
				t.Fatal(err)
			}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			content, err := os.ReadFile(filepath.Join(a.outputDir, mode, "movie.mkv.cas.strm"))
			if err != nil {
				t.Fatal(err)
			}
			info := generatedCASClaim(t, content)
			if (info.SHA256 != "") != (mode != "tianyi") || (info.MD5 != "") != (mode != "mobile") ||
				(info.SliceMD5 != "") != (mode != "mobile") || (info.SliceSize > 0) != (mode != "mobile") {
				t.Fatalf("wrong hash selection for %s: %+v", mode, info)
			}
		})
	}
}

func TestCASAlwaysSkipsExistingRelativeOutputEvenForLegacyFullMode(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Season", "episode.mkv"), "new media")
	out := filepath.Join(a.outputDir, "existing", "Season", "episode.mkv.cas.strm")
	writeTest(t, out, "preserve existing")
	task := Task{Name: "existing", Kind: "cas", CASOperation: "generate", StorageID: s.ID, Source: "/", Target: "existing", Mode: "full"}
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 0 {
		t.Fatal("existing CAS was recalculated", n, err)
	}
	content, _ := os.ReadFile(out)
	if string(content) != "preserve existing" {
		t.Fatal("existing output was overwritten")
	}
}

func TestTaskPanelReadOnlySnapshotAndAuthorization(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/tasks/panel", nil, nil); w.Code != 401 {
		t.Fatal("task panel is not protected", w.Code)
	}
	cookie := setupTest(t, a, h)
	if err := a.store.update(func(st *State) error {
		st.Tasks = []Task{{ID: "running", Name: "CAS running", Kind: "cas", Status: "running", Processed: 3, Message: "正在生成"},
			{ID: "finished", Name: "finished", Kind: "strm", Status: "success"}}
		st.GenerationJobs = []GenerationJob{{ID: "queue", Kind: "cas", Name: "CAS queue", Status: "queued", Total: 10, Preview: "private-preview"}}
		st.Automations = []Automation{{ID: "flow", Name: "workflow", Status: "running", Message: "动作1"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.scrapeMu.Lock()
	a.scrapeProgress = scrapeProgress{Running: true, TaskID: "running", Done: 2, Total: 5, Message: "正在刮削"}
	a.scrapeMu.Unlock()
	w := request(t, h, "GET", "/api/tasks/panel", nil, cookie)
	var result struct {
		Items []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 4 ||
		strings.Contains(w.Body.String(), "private-preview") || strings.Contains(w.Body.String(), `"finished"`) {
		t.Fatal("panel did not return the four active sources safely", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("live panel can be cached")
	}
	if w := request(t, h, "POST", "/api/tasks/panel", nil, cookie); w.Code != 405 {
		t.Fatal("panel accepted mutation", w.Code)
	}
}

func TestRemoteCASInfoModesKeepTransferVerification(t *testing.T) {
	for _, mode := range []string{"all", "mobile", "tianyi"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			body := "remote mode-specific content"
			var reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reads.Add(1)
				w.Write([]byte(body))
			}))
			defer server.Close()
			s := Storage{ID: "remote", Type: "webdav", Config: map[string]string{"address": server.URL, "root": "/"}}
			info, err := a.generateCASInfo(a.ctx, s, File{ID: "/movie.mkv", Name: "movie.mkv", Size: int64(len(body))}, mode)
			if err != nil || reads.Load() != 1 {
				t.Fatal("remote features failed", reads.Load(), err)
			}
			sha, md := sha256.Sum256([]byte(body)), md5.Sum([]byte(body))
			if mode != "tianyi" && info.SHA256 != hex.EncodeToString(sha[:]) || mode == "tianyi" && info.SHA256 != "" ||
				mode != "mobile" && info.MD5 != hex.EncodeToString(md[:]) || mode == "mobile" && (info.MD5 != "" || info.SliceMD5 != "") {
				t.Fatal("remote mode or integrity was lost", info)
			}
			files, err := os.ReadDir(a.transferTempDirectory())
			if err != nil || len(files) != 0 {
				t.Fatal("temporary media retained", files, err)
			}
		})
	}
}

func TestCASMobileDefaultManualOverrideAndResetProtection(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "mobile", Type: "mobile", Enabled: true, Config: map[string]string{"authorization": "secret"}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "cas", Name: "CAS", Kind: "cas", StorageID: s.ID, Source: "/", Target: "out"}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(task)
	if !strings.Contains(string(raw), `"casInfoMode":"auto"`) {
		t.Fatal("mobile source did not keep the smart default")
	}
	if err := json.Unmarshal([]byte(`{"casInfoMode":"all"}`), &task); err != nil {
		t.Fatal(err)
	}
	if err := a.validateTask(&task); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(task)
	if !strings.Contains(string(raw), `"casInfoMode":"all"`) {
		t.Fatal("manual all-features override lost")
	}
	if err := a.store.update(func(st *State) error { st.Tasks = []Task{task}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.startTask(task.ID, true); err == nil || !strings.Contains(err.Error(), "仅增量生成") {
		t.Fatal("CAS destructive full reset not explicitly rejected", err)
	}
}

func TestCASInfoModeRejectsInvalidImportedConfiguration(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	setupTest(t, a, h)
	bundle := configBundle{State: a.store.snapshot()}
	bundle.State.Tasks = []Task{{ID: "invalid", Kind: "cas", CASInfoMode: "unsupported"}}
	if err := validateBundle(bundle); err == nil {
		t.Fatal("invalid CAS information selection accepted by configuration import")
	}
}

func TestCASExistingOutputCleanupDoesNotReadOrRemoveSource(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "A", "movie.mkv"), "keep source")
	task := Task{Kind: "cas", CASOperation: "generate", StorageID: s.ID, Source: "/A", Target: "out", CompletionAction: "delete"}
	files, err := a.rawList(a.ctx, s, "/A")
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	cleanup, err := a.newFeatureCleanup(task, s)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup.close()
	if err := cleanup.observe(a.ctx, a, "/A", files[0], true); err != nil {
		t.Fatal(err)
	}
	item := cleanup.items[files[0].ID]
	if !item.protected || item.digest != "" || item.output != "" {
		t.Fatal("skipped CAS source was read or marked for deletion", item)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	cancel()
	if err := cleanup.finish(ctx, a, nil); err != nil {
		t.Fatal("protected item reached source reads", err)
	}
	if data, err := os.ReadFile(filepath.Join(s.Config["root"], "A", "movie.mkv")); err != nil || string(data) != "keep source" {
		t.Fatal("protected source changed", string(data), err)
	}
}
