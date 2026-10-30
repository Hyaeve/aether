package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

func TestFeatureCompletionDeletesOnlyPublishedSources(t *testing.T) {
	for _, kind := range []string{"cas", "ed2k"} {
		t.Run(kind, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			s.Config["deleteMode"] = "permanent"
			if err := a.store.update(func(st *State) error { st.Storages[0] = s; return nil }); err != nil {
				t.Fatal(err)
			}
			base := s.Config["root"]
			for name, content := range map[string]string{"A/Season 1/movie.MKV": "media", "A/Season 1/movie.nfo": "<movie/>", "A/other/a.txt": "unselected", "A/kept/excluded.mp4": "keep", "outside.txt": "outside"} {
				writeTest(t, filepath.Join(base, filepath.FromSlash(name)), content)
			}
			if kind == "cas" {
				writeTest(t, filepath.Join(base, "A", "broken", "bad.cas"), "invalid")
			}
			task := Task{ID: "completion", Name: "completion", Kind: kind, StorageID: s.ID, Source: "/A", Target: "generated", Mode: "full", CompletionAction: "delete", MetadataExtensions: "nfo", ExcludeFiles: "excluded"}
			if err := a.validateTask(&task); err != nil {
				t.Fatal(err)
			}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 2 {
				t.Fatal(n, err)
			}
			for _, name := range []string{"A/Season 1/movie.MKV", "A/Season 1/movie.nfo", "A/other/a.txt", "A/Season 1", "A/other"} {
				if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(name))); !os.IsNotExist(err) {
					t.Fatalf("not removed: %s %v", name, err)
				}
			}
			for _, name := range []string{"A", "A/kept/excluded.mp4", "outside.txt"} {
				if _, err := os.Stat(filepath.Join(base, filepath.FromSlash(name))); err != nil {
					t.Fatal(name, err)
				}
			}
			if kind == "cas" {
				if _, err := os.Stat(filepath.Join(base, "A", "broken", "bad.cas")); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"Season 1/movie.MKV." + kind + ".strm", "Season 1/movie.nfo"} {
				if kind == "cas" {
					name = "A/" + name
				}
				if _, err := os.Stat(filepath.Join(a.outputDir, "generated", filepath.FromSlash(name))); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestFeatureCompletionKeepAndIncrementalSkip(t *testing.T) {
	for _, action := range []string{"", "keep", "delete"} {
		t.Run(action, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			writeTest(t, filepath.Join(s.Config["root"], "A", "movie.mkv"), "media")
			writeTest(t, filepath.Join(a.outputDir, "generated", "A", "movie.mkv.cas.strm"), "unrelated existing output")
			task := Task{Kind: "cas", StorageID: s.ID, Source: "/A", Target: "generated", Mode: "incremental", CompletionAction: action}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			if _, err := os.Stat(filepath.Join(s.Config["root"], "A", "movie.mkv")); err != nil {
				t.Fatal("skipped source lost", err)
			}
		})
	}
}

func TestFeatureCompletionRejectsUnsafeScope(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "A", "movie.mkv"), "media")
	for _, tc := range []struct{ source, target string }{{"/", "generated"}, {"/A", filepath.Join(s.Config["root"], "A", "generated")}, {"/A", s.Config["root"]}} {
		task := Task{Name: "unsafe", Kind: "cas", StorageID: s.ID, Source: tc.source, Target: tc.target, Mode: "full", CompletionAction: "delete"}
		if err := a.validateTask(&task); err == nil {
			t.Fatal("unsafe cleanup saved", tc)
		}
		if _, err := a.executeTask(context.Background(), task, s); err == nil {
			t.Fatal("unsafe cleanup executed", tc)
		}
	}
}

func TestFeatureCleanupKeepsLateChangedAndMissingOutputs(t *testing.T) {
	for _, mode := range []string{"late", "changed", "replaced-output", "cancelled", "replaced-parent", "changed-storage"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			s.Config["deleteMode"] = "permanent"
			_ = a.store.update(func(st *State) error { st.Storages[0] = s; return nil })
			file := filepath.Join(s.Config["root"], "A", "nested", "movie.mkv")
			writeTest(t, file, "media")
			task := Task{Kind: "cas", StorageID: s.ID, Source: "/A", Target: "generated", Mode: "full", CompletionAction: "delete"}
			c, err := a.newFeatureCleanup(task, s)
			if err != nil {
				t.Fatal(err)
			}
			defer c.close()
			dirs, _ := a.rawList(context.Background(), s, "/A")
			if err := c.observe(context.Background(), a, "/A", dirs[0]); err != nil {
				t.Fatal(err)
			}
			files, _ := a.rawList(context.Background(), s, "/A/nested")
			f := files[0]
			if err := c.observe(context.Background(), a, "/A/nested", f); err != nil {
				t.Fatal(err)
			}
			c.directory("/A", dirs[0])
			writeTest(t, filepath.Join(a.outputDir, "generated", "movie.mkv.cas.strm"), "published")
			out, err := os.OpenRoot(filepath.Join(a.outputDir, "generated"))
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			if err := c.published(context.Background(), out, f, "movie.mkv.cas.strm", false); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			switch mode {
			case "changed-storage":
				if err := a.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil }); err != nil {
					t.Fatal(err)
				}
			case "late":
				writeTest(t, filepath.Join(s.Config["root"], "A", "nested", "late.mp4"), "new media")
			case "changed":
				writeTest(t, file, "changed media")
			case "replaced-output":
				writeTest(t, filepath.Join(a.outputDir, "generated", "movie.mkv.cas.strm"), "other")
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "replaced-parent":
				if err := os.Rename(filepath.Dir(file), filepath.Join(s.Config["root"], "A", "moved")); err != nil {
					t.Fatal(err)
				}
				writeTest(t, file, "replacement")
			}
			err = c.finish(ctx, a, out)
			if mode == "late" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(s.Config["root"], "A", "nested", "late.mp4")); err != nil {
					t.Fatal("late media lost", err)
				}
			} else {
				if err == nil {
					t.Fatal("unsafe cleanup accepted")
				}
				if _, err := os.Stat(file); err != nil {
					t.Fatal("source lost", err)
				}
			}
		})
	}
}

func TestFeatureCompletionRemoteFilesKeepEmptyDirectories(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	for name, content := range map[string]string{"A/Season 1/movie.mkv": "media", "A/Season 1/movie.nfo": "<movie/>", "A/other/a.txt": "unused", "A/keep/excluded.mp4": "excluded"} {
		writeTest(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	h := &webdav.Handler{Prefix: "/", FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}
	var deletes []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			mu.Lock()
			deletes = append(deletes, r.URL.Path)
			mu.Unlock()
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	s := Storage{ID: "cleanup-dav", Name: "DAV", Type: "webdav", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/", "deleteMode": "permanent"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "remote-cleanup", Kind: "cas", StorageID: s.ID, Source: "/A", Target: "generated", Mode: "full", CompletionAction: "delete", MetadataExtensions: "nfo", ExcludeFiles: "excluded"}
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	for _, name := range []string{"A/Season 1/movie.mkv", "A/Season 1/movie.nfo", "A/other/a.txt"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatal("remote file retained", name, err)
		}
	}
	for _, name := range []string{"A", "A/Season 1", "A/other", "A/keep/excluded.mp4"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Fatal("remote directory or protected file lost", name, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deletes) != 3 {
		t.Fatal("unexpected delete requests", deletes)
	}
	for _, name := range deletes {
		if !strings.HasSuffix(name, ".mkv") && !strings.HasSuffix(name, ".nfo") && !strings.HasSuffix(name, ".txt") {
			t.Fatal("directory delete requested", name)
		}
	}
	data, err := os.ReadFile(filepath.Join(a.outputDir, "generated", "A", "Season 1", "movie.mkv.cas.strm"))
	if err != nil || generatedCASClaim(t, data).SHA256 == "" {
		t.Fatal("missing CAS output", err)
	}
	data, err = os.ReadFile(filepath.Join(a.outputDir, "generated", "A", "Season 1", "movie.nfo"))
	if err != nil || string(data) != "<movie/>" {
		t.Fatal("missing metadata output", err)
	}
}

func TestFeatureCompletionTruncatedRemoteReadNeverDeletes(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	writeTest(t, filepath.Join(dir, "A", "other.txt"), "complete source content")
	h := &webdav.Handler{Prefix: "/", FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletes.Add(1)
		}
		if r.Method == "GET" {
			_, _ = w.Write([]byte("short"))
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	s := Storage{ID: "short-cleanup", Type: "webdav", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/", "deleteMode": "permanent"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	task := Task{Kind: "cas", StorageID: s.ID, Source: "/A", Target: "generated", Mode: "full", CompletionAction: "delete"}
	if _, err := a.executeTask(context.Background(), task, s); err == nil {
		t.Fatal("truncated source accepted")
	}
	if deletes.Load() != 0 {
		t.Fatal("truncated source deleted")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "A", "other.txt")); err != nil || string(data) != "complete source content" {
		t.Fatal("source changed", err)
	}
}

func TestRemoteCASDownloadBoundAndTemporaryCleanup(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	for _, name := range []string{"one.mkv", "two.mkv", "three.mkv", "four.mkv"} {
		writeTest(t, filepath.Join(dir, name), strings.Repeat(name, 8192))
	}
	var active, peak atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var released atomic.Bool
	h := &webdav.Handler{Prefix: "/", FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			n := active.Add(1)
			defer active.Add(-1)
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
			if !released.Load() {
				entered <- struct{}{}
				<-release
			}
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	s := Storage{ID: "remote-cas", Name: "DAV", Type: "webdav", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
	temp := filepath.Join(t.TempDir(), "custom-temp")
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, s)
		cfg := normalizedTransferSettings(st.TransferSettings)
		cfg.TempDirectory = temp
		st.TransferSettings = cfg
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, target := range []string{"first", "second"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := a.executeTask(context.Background(), Task{Kind: "cas", StorageID: s.ID, Source: "/", Target: target, Mode: "full"}, s)
			if err == nil && n != 4 {
				err = errors.New("incomplete generation")
			}
			errs <- err
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			released.Store(true)
			close(release)
			wg.Wait()
			t.Fatal("two downloads did not start")
		}
	}
	if peak.Load() != 2 {
		t.Errorf("peak before release: %d", peak.Load())
	}
	released.Store(true)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 2 {
		t.Fatal("remote concurrency exceeded two", peak.Load())
	}
	for _, target := range []string{"first", "second"} {
		for _, name := range []string{"one.mkv", "two.mkv", "three.mkv", "four.mkv"} {
			data, err := os.ReadFile(filepath.Join(a.outputDir, target, name+".cas.strm"))
			if err != nil {
				t.Fatal(err)
			}
			info := generatedCASClaim(t, data)
			sha := sha256.Sum256([]byte(strings.Repeat(name, 8192)))
			if info.SHA256 != hex.EncodeToString(sha[:]) || info.MD5 == "" {
				t.Fatal(info)
			}
		}
	}
	items, err := os.ReadDir(temp)
	if err != nil || len(items) != 0 {
		t.Fatal("temporary files not cleaned", len(items), err)
	}
}

func TestRemoteCASFailureAndCancellationCleanTemporaryFiles(t *testing.T) {
	for _, mode := range []string{"truncated", "cancelled", "unwritable"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			temp := filepath.Join(t.TempDir(), "temp")
			if mode == "unwritable" {
				writeTest(t, temp, "not a directory")
			}
			_ = a.store.update(func(st *State) error {
				cfg := normalizedTransferSettings(st.TransferSettings)
				cfg.TempDirectory = temp
				st.TransferSettings = cfg
				return nil
			})
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "cancelled" {
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte("x"))
					w.(http.Flusher).Flush()
					close(started)
					<-r.Context().Done()
					return
				}
				_, _ = w.Write([]byte("short"))
			}))
			defer server.Close()
			s := Storage{ID: "failure", Type: "webdav", Config: map[string]string{"address": server.URL, "root": "/"}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				go func() {
					select {
					case <-started:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			if _, err := a.generateCASInfo(ctx, s, File{ID: "/movie.mkv", Name: "movie.mkv", Size: 100}); err == nil {
				t.Fatal("failed read generated feature")
			}
			if mode != "unwritable" {
				items, err := os.ReadDir(temp)
				if err != nil || len(items) != 0 {
					t.Fatal("failed temporary file retained", len(items), err)
				}
			}
			a.resources.mu.Lock()
			active := a.resources.active["feature-hash"]
			a.resources.mu.Unlock()
			if active != 0 {
				t.Fatal("hash slot leaked", active)
			}
		})
	}
}

func TestFeatureCompletionMetadataSkipAndCopyFailureKeepSources(t *testing.T) {
	for _, mode := range []string{"incremental", "failed"} {
		t.Run(mode, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			writeTest(t, filepath.Join(s.Config["root"], "A", "movie.mkv"), "media")
			writeTest(t, filepath.Join(s.Config["root"], "A", "movie.nfo"), "<movie/>")
			task := Task{ID: "copy", Kind: "cas", StorageID: s.ID, Source: "/A", Target: "generated", Mode: "full", CompletionAction: "delete", MetadataExtensions: "nfo"}
			if mode == "incremental" {
				task.Mode = "incremental"
				writeTest(t, filepath.Join(a.outputDir, "generated", "A", "movie.nfo"), "old metadata")
			} else {
				if err := os.MkdirAll(filepath.Join(a.outputDir, "generated", "A", "movie.nfo"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := a.executeTask(context.Background(), task, s)
			if mode == "failed" && err == nil {
				t.Fatal("metadata failure ignored")
			}
			if _, err := os.Stat(filepath.Join(s.Config["root"], "A", "movie.nfo")); err != nil {
				t.Fatal("uncopied metadata removed", err)
			}
			if mode == "failed" {
				if _, err := os.Stat(filepath.Join(s.Config["root"], "A", "movie.mkv")); err != nil {
					t.Fatal("failed task deleted source", err)
				}
			}
		})
	}
}
