package app

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/webdav"
)

func TestCASTaskKeepsSelectedSourceDirectory(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		name := "relative"
		if absolute {
			name = "absolute"
		}
		t.Run(name, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			for name, content := range map[string]string{
				"139/剧集/movie.mkv":               "movie",
				"139/剧集/Season 1/episode.mp4":    "episode",
				"139/剧集/Season 1/episode.nfo":    "<episode/>",
				"139/剧集/excluded/ignored.mkv":    "excluded",
				"139/剧集/Season 1/import.mkv.cas": `{"name":"import.mkv","size":42,"sha256":"` + strings.Repeat("a", 64) + `"}`,
			} {
				writeTest(t, filepath.Join(s.Config["root"], filepath.FromSlash(name)), content)
			}
			target := "家庭影音"
			base := filepath.Join(a.outputDir, target)
			if absolute {
				target = filepath.Join(t.TempDir(), target)
				base = target
			}
			old := filepath.Join(base, "movie.mkv.cas.strm")
			writeTest(t, old, "old flat output")
			task := Task{Kind: "cas", StorageID: s.ID, Source: "/139/剧集/", SourceLabel: "stale label", Target: target, MetadataExtensions: "nfo", ExcludeDirs: "excluded"}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 4 {
				t.Fatal(n, err)
			}
			var files []string
			if err := filepath.WalkDir(base, func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					rel, err := filepath.Rel(base, name)
					if err != nil {
						return err
					}
					files = append(files, filepath.ToSlash(rel))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			want := []string{"movie.mkv.cas.strm", "剧集/movie.mkv.cas.strm", "剧集/Season 1/episode.mp4.cas.strm", "剧集/Season 1/episode.nfo", "剧集/Season 1/import.mkv.cas.strm"}
			slices.Sort(files)
			slices.Sort(want)
			if !reflect.DeepEqual(files, want) {
				t.Fatalf("output tree: got %v, want %v", files, want)
			}
			for _, name := range []string{"movie.mkv", "Season 1/episode.mp4", "Season 1/import.mkv"} {
				raw, err := os.ReadFile(filepath.Join(base, "剧集", filepath.FromSlash(name+".cas.strm")))
				if err != nil {
					t.Fatal(err)
				}
				if info := generatedCASClaim(t, raw); info.Name != filepath.Base(name) {
					t.Fatal("directory wrapper changed media identity", info)
				}
			}
			if raw, err := os.ReadFile(filepath.Join(base, "剧集", "Season 1", "episode.nfo")); err != nil || string(raw) != "<episode/>" {
				t.Fatal("metadata not copied with media", string(raw), err)
			}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 0 {
				t.Fatal("incremental rerun", n, err)
			}
			if raw, err := os.ReadFile(old); err != nil || string(raw) != "old flat output" {
				t.Fatal("old flat output changed", string(raw), err)
			}
		})
	}
}

func TestCASTaskCloudSourceDirectoryLabelAndRoots(t *testing.T) {
	for _, tc := range []struct{ name, source, root, label, prefix string }{
		{"selected", "opaque-selected", "opaque-root", "剧集", "剧集"},
		{"configured-root", "opaque-root", "opaque-root", "剧集", ""},
		{"root", "/", "", "根目录", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			s := addCASBindings(t, a)[0]
			s.Config["root"] = tc.root
			cfg := a.store.snapshot().Settings
			cfg.CacheEnabled, cfg.CacheMemoryMB, cfg.CacheMaxItems = true, 128, 10000
			if err := a.store.update(func(st *State) error { st.Settings = cfg; return nil }); err != nil {
				t.Fatal(err)
			}
			sha := strings.Repeat("a", 64)
			a.cache.put(s.ID+":"+tc.source, []File{{ID: "cloud-movie", Name: "movie.mkv", Size: 42, SHA256: sha}, {ID: "opaque-child", Name: "Season 1", IsDir: true}}, 30, cfg)
			a.cache.put(s.ID+":opaque-child", []File{{ID: "cloud-episode", Name: "episode.mp4", Size: 42, SHA256: sha}}, 30, cfg)
			task := Task{Kind: "cas", StorageID: s.ID, Source: tc.source, SourceLabel: tc.label, Target: "library", CacheTTL: 30}
			if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 2 {
				t.Fatal(n, err)
			}
			for _, name := range []string{"movie.mkv", "Season 1/episode.mp4"} {
				raw, err := os.ReadFile(filepath.Join(a.outputDir, "library", tc.prefix, filepath.FromSlash(name+".cas.strm")))
				if err != nil {
					t.Fatal(err)
				}
				if info := generatedCASClaim(t, raw); info.SHA256 != sha || info.Name != filepath.Base(name) {
					t.Fatal(info)
				}
			}
			if tc.prefix != "" {
				if _, err := os.Stat(filepath.Join(a.outputDir, "library", "movie.mkv.cas.strm")); !os.IsNotExist(err) {
					t.Fatal("selected cloud directory flattened", err)
				}
			}
		})
	}
}

func TestCASTaskSelectedDirectorySkipsRemoteReadsAndCleanup(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	for name, content := range map[string]string{"new.mkv": "new media", "skip.mkv": "keep media", "skip.nfo": "<keep/>"} {
		writeTest(t, filepath.Join(dir, "139", "series", name), content)
	}
	h := &webdav.Handler{Prefix: "/", FileSystem: webdav.Dir(dir), LockSystem: webdav.NewMemLS()}
	var skippedReads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/skip.mkv") {
			skippedReads.Add(1)
			http.Error(w, "skipped media must not be read", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer server.Close()
	s := Storage{ID: "source-directory-dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": server.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"skip.mkv.cas.strm": "existing output", "skip.nfo": "existing metadata"} {
		writeTest(t, filepath.Join(a.outputDir, "library", "series", name), content)
	}
	task := Task{Kind: "cas", StorageID: s.ID, Source: "/139/series", SourceLabel: "stale label", Target: "library", CompletionAction: "delete", MetadataExtensions: "nfo"}
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if skippedReads.Load() != 0 {
		t.Fatal("existing nested output still read source", skippedReads.Load())
	}
	for _, name := range []string{"skip.mkv", "skip.nfo"} {
		if _, err := os.Stat(filepath.Join(dir, "139", "series", name)); err != nil {
			t.Fatal("skipped source removed", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "139", "series", "new.mkv")); !os.IsNotExist(err) {
		t.Fatal("published source not cleaned up", err)
	}
	raw, err := os.ReadFile(filepath.Join(a.outputDir, "library", "series", "new.mkv.cas.strm"))
	if err != nil || generatedCASClaim(t, raw).Name != "new.mkv" {
		t.Fatal("missing nested output", err)
	}
	for name, content := range map[string]string{"skip.mkv.cas.strm": "existing output", "skip.nfo": "existing metadata"} {
		if raw, err := os.ReadFile(filepath.Join(a.outputDir, "library", "series", name)); err != nil || string(raw) != content {
			t.Fatal("skipped output changed", name, string(raw), err)
		}
	}
}

func TestCASTaskSelectedDirectoryRejectsInvalidCloudLabel(t *testing.T) {
	for _, label := range []string{"", ".", "..", "../escape", "A/B", `A\B`, "A:B"} {
		t.Run(label, func(t *testing.T) {
			a := testApp(t)
			s := addCASBindings(t, a)[0]
			cfg := a.store.snapshot().Settings
			cfg.CacheEnabled, cfg.CacheMemoryMB, cfg.CacheMaxItems = true, 128, 10000
			if err := a.store.update(func(st *State) error { st.Settings = cfg; return nil }); err != nil {
				t.Fatal(err)
			}
			a.cache.put(s.ID+":opaque-selected", []File{{ID: "cloud-movie", Name: "movie.mkv", Size: 42, SHA256: strings.Repeat("a", 64)}}, 30, cfg)
			task := Task{Kind: "cas", StorageID: s.ID, Source: "opaque-selected", SourceLabel: label, Target: "library"}
			if _, err := a.executeTask(context.Background(), task, s); err == nil || !strings.Contains(err.Error(), "源目录名称") {
				t.Fatal("invalid cloud source label accepted or used for upstream access", err)
			}
			if _, err := os.Stat(a.outputDir); !os.IsNotExist(err) {
				t.Fatal("invalid source created output", err)
			}
		})
	}
}

func TestCASTaskSelectedDirectoryOutputConflictKeepsSource(t *testing.T) {
	for _, conflict := range []string{"series", "series/movie.mkv.cas.strm"} {
		t.Run(conflict, func(t *testing.T) {
			a := testApp(t)
			s := addLocal(t, a)
			source := filepath.Join(s.Config["root"], "139", "series", "movie.mkv")
			writeTest(t, source, "keep source")
			output := filepath.Join(a.outputDir, "library", filepath.FromSlash(conflict))
			if conflict == "series" {
				writeTest(t, output, "existing file")
			} else if err := os.MkdirAll(output, 0755); err != nil {
				t.Fatal(err)
			}
			task := Task{Kind: "cas", StorageID: s.ID, Source: "/139/series", Target: "library", CompletionAction: "delete"}
			if _, err := a.executeTask(context.Background(), task, s); err == nil {
				t.Fatal("output conflict ignored")
			}
			if raw, err := os.ReadFile(source); err != nil || string(raw) != "keep source" {
				t.Fatal("unpublished source changed", string(raw), err)
			}
		})
	}
}
