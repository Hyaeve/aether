package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStrmReplaceAPI(t *testing.T) {
	a := testApp(t)
	t.Cleanup(func() { strmReplaceManagers.Delete(a) })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	mux := http.NewServeMux()
	a.RegisterStrmReplace(mux)
	for _, method := range []string{"GET", "POST"} {
		if w := request(t, mux, method, "/api/strm-replace", nil, nil); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one.strm"), []byte("old/old"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, in := range []strmReplaceRequest{
		{Directory: dir, Find: "old"},
		{Directory: dir, Confirmed: true},
		{Directory: "../escape", Find: "old", Confirmed: true},
		{Directory: dir, Find: strings.Repeat("x", 16385), Confirmed: true},
	} {
		if w := request(t, mux, "POST", "/api/strm-replace", in, cookie); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	in := strmReplaceRequest{Directory: dir, Find: "old", Replace: "new", Confirmed: true}
	w := request(t, mux, "POST", "/api/strm-replace", in, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var task strmReplaceTask
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		w = request(t, mux, "GET", "/api/strm-replace?id="+task.ID, nil, cookie)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if task.Status != "completed" || task.Processed != 1 || task.Changed != 1 || task.FinishedAt == nil {
		t.Fatalf("%+v", task)
	}
	if w := request(t, mux, "GET", "/api/strm-replace?id=missing", nil, cookie); w.Code != 404 {
		t.Fatal(w.Code)
	}
	m := a.strmReplacements()
	m.mu.Lock()
	m.tasks[0].Status = "running"
	m.mu.Unlock()
	if w := request(t, mux, "POST", "/api/strm-replace", in, cookie); w.Code != 409 {
		t.Fatal(w.Code)
	}
	other := &App{}
	t.Cleanup(func() { strmReplaceManagers.Delete(other) })
	if len(other.strmReplacements().tasks) != 0 {
		t.Fatal("cross-app history")
	}
	m.mu.Lock()
	m.tasks = make([]strmReplaceTask, strmReplaceHistory)
	for i := range m.tasks {
		m.tasks[i].Status = "completed"
	}
	m.mu.Unlock()
	if w := request(t, mux, "POST", "/api/strm-replace", in, cookie); w.Code != 202 {
		t.Fatal(w.Code)
	}
	a.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tasks) != strmReplaceHistory || m.tasks[0].Status != "completed" {
		t.Fatal("unbounded history or unfinished task")
	}
}

func TestStrmReplaceFiles(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"movie.STRM": "\xef\xbb\xbfhttps://old/x\r\nold", "other.txt": "old", "none.strm": "none", "large.strm": strings.Repeat("x", strmReplaceMaxBytes+1)} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	task := strmReplaceTask{}
	err := scanStrmReplace(context.Background(), strmReplaceRequest{Directory: dir, Find: "old", Replace: "new"}, &task, func() {})
	if err != nil || task.Processed != 2 || task.Changed != 1 || task.Skipped != 1 {
		t.Fatal(task, err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "movie.STRM"))
	if string(data) != "\xef\xbb\xbfhttps://new/x\r\nnew" {
		t.Fatalf("%q", data)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "other.txt"))
	if string(data) != "old" {
		t.Fatal("changed non-STRM")
	}
}

func TestStrmReplaceConflictAndLimits(t *testing.T) {
	for _, mutation := range []string{"in-place", "replaced", "deleted", "same-size-time"} {
		t.Run(mutation, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "a.strm")
			if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			info, _ := os.Stat(file)
			changed, err := replaceStrmFile(root, "a.strm", "old", "new", func() {
				if mutation == "replaced" || mutation == "deleted" {
					if err := os.Remove(file); err != nil {
						t.Fatal(err)
					}
				}
				if mutation != "deleted" {
					if err := os.WriteFile(file, []byte("EXT"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if mutation == "same-size-time" {
					if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err == nil || changed {
				t.Fatal("overwrote concurrent edit")
			}
			if mutation != "deleted" {
				data, _ := os.ReadFile(file)
				if string(data) != "EXT" {
					t.Fatal(string(data))
				}
			}
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".aether-") {
					t.Fatal("leaked temporary file")
				}
			}
		})
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.strm"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if changed, err := replaceStrmFile(root, "a.strm", "o", strings.Repeat("x", strmReplaceMaxBytes), nil); err == nil || changed {
		t.Fatal("output limit bypass")
	}
	task := strmReplaceTask{Scanned: strmReplaceMaxEntries}
	if err := scanStrmReplace(context.Background(), strmReplaceRequest{Directory: dir, Find: "old"}, &task, func() {}); err == nil {
		t.Fatal("scan limit bypass")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scanStrmReplace(ctx, strmReplaceRequest{Directory: dir, Find: "old"}, &strmReplaceTask{}, func() {}); err == nil {
		t.Fatal("ignored cancellation")
	}
	deep := dir
	for i := 0; i <= strmReplaceMaxDepth; i++ {
		deep = filepath.Join(deep, "d")
		if err := os.Mkdir(deep, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := scanStrmReplace(context.Background(), strmReplaceRequest{Directory: dir, Find: "old"}, &strmReplaceTask{}, func() {}); err == nil {
		t.Fatal("depth limit bypass")
	}
}

func TestStrmReplaceSymlinks(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	file := filepath.Join(outside, "a.strm")
	if err := os.WriteFile(file, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink(file, filepath.Join(dir, "link.strm")); err != nil {
		t.Fatal(err)
	}
	task := strmReplaceTask{}
	if err := scanStrmReplace(context.Background(), strmReplaceRequest{Directory: dir, Find: "old", Replace: "new"}, &task, func() {}); err != nil || task.Skipped != 2 {
		t.Fatal(task, err)
	}
	if root, err := openStrmReplaceRoot(filepath.Join(dir, "linked")); err == nil {
		root.Close()
		t.Fatal("accepted symlink root")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := replaceStrmFile(root, "../a.strm", "old", "new", nil); err == nil {
		t.Fatal("escaped root")
	}
	if _, err := replaceStrmFile(root, "link.strm", "old", "new", nil); err == nil {
		t.Fatal("followed symlink")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "old" {
		t.Fatal("modified outside root")
	}
}
