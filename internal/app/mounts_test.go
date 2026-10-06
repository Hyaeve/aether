package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMountConfigAndValidation(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	input := MountConfig{Name: "聚合挂载", MountPoint: t.TempDir(), Source: "/", Automount: true, Mode: 0755}
	if w := request(t, h, "POST", "/api/mounts", input, nil); w.Code != 401 {
		t.Fatal("missing authentication")
	}
	if w := request(t, h, "GET", "/api/local-directories", nil, nil); w.Code != 401 {
		t.Fatal("directory endpoint missing authentication")
	}
	w := request(t, h, "POST", "/api/mounts", input, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved := a.store.snapshot().Mounts[0]
	if saved.StorageID != "" || saved.ReadOnly || !saved.Automount || saved.Mode != 0755 {
		t.Fatal("defaults", saved)
	}
	if len(a.mounts.active) != 0 {
		t.Fatal("save unexpectedly mounted")
	}
	nonempty := t.TempDir()
	os.WriteFile(filepath.Join(nonempty, "keep.txt"), []byte("keep"), 0600)
	if err := a.validateMount(&MountConfig{Name: "nonempty", MountPoint: nonempty, Mode: 0755}); err != nil {
		t.Fatal("parent containing unrelated files rejected", err)
	}
	if _, err := os.Stat(mountTarget(saved)); !os.IsNotExist(err) {
		t.Fatal("saving created mount child", err)
	}
	root := t.TempDir()
	a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "local-validation", Type: "local", Enabled: true, Config: map[string]string{"root": root}})
		return nil
	})
	if err := a.validateMount(&MountConfig{Name: "recursive", MountPoint: root, Mode: 0755}); err == nil {
		t.Fatal("mount over local storage accepted")
	}
	restored, err := NewStore(a.store.dir)
	if err != nil || len(restored.snapshot().Mounts) != 1 {
		t.Fatal("persistence", err)
	}
	for _, bad := range []MountConfig{
		{Name: "root", MountPoint: string(filepath.Separator), Mode: 0755},
		{Name: "config", MountPoint: a.store.dir, Mode: 0755},
		{Name: "negative", MountPoint: t.TempDir(), UID: -1, Mode: 0755},
		{Name: "mode", MountPoint: t.TempDir(), Mode: 07777},
		{Name: "overlap", MountPoint: saved.MountPoint, Mode: 0755},
	} {
		if w := request(t, h, "POST", "/api/mounts", bad, cookie); w.Code != 400 {
			t.Fatal("accepted invalid", bad, w.Code)
		}
	}
	if runtime.GOOS != "linux" {
		if w := request(t, h, "POST", "/api/mounts/"+saved.ID+"/start", nil, cookie); w.Code != 400 {
			t.Fatal("unsupported platform reported mounted")
		}
	}
	var mounts []MountConfig
	json.Unmarshal(request(t, h, "GET", "/api/mounts", nil, cookie).Body.Bytes(), &mounts)
	if len(mounts) != 1 {
		t.Fatal(mounts)
	}
	if w := request(t, h, "DELETE", "/api/mounts/"+saved.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if _, err := os.Stat(saved.MountPoint); err != nil {
		t.Fatal("deleted mount directory", err)
	}
}

func TestDedicatedMountTarget(t *testing.T) {
	mount := MountConfig{MountPoint: t.TempDir()}
	keep := filepath.Join(mount.MountPoint, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := prepareMountTarget(mount, true); err != nil {
			t.Fatal("create or reuse empty child", err)
		}
	}
	target := mountTarget(mount)
	if target != filepath.Join(mount.MountPoint, "AetherDrive") {
		t.Fatal(target)
	}
	if err := os.WriteFile(filepath.Join(target, "occupied"), []byte("user data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, create := range []bool{false, true} {
		if err := prepareMountTarget(mount, create); err == nil {
			t.Fatal("nonempty child accepted")
		}
	}
	data, err := os.ReadFile(keep)
	if err != nil || string(data) != "keep" {
		t.Fatal("parent data changed", err)
	}
	t.Run("file", func(t *testing.T) {
		mount := MountConfig{MountPoint: t.TempDir()}
		if err := os.WriteFile(mountTarget(mount), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := prepareMountTarget(mount, true); err == nil {
			t.Fatal("file accepted as child")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		mount := MountConfig{MountPoint: t.TempDir()}
		if err := os.Symlink(t.TempDir(), mountTarget(mount)); err != nil {
			t.Skip("symlink unavailable:", err)
		}
		if err := prepareMountTarget(mount, true); err == nil {
			t.Fatal("symlink accepted as child")
		}
	})
}

func TestMountFSLocalAndAggregate(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "books"), 0755)
	os.WriteFile(filepath.Join(root, "private.txt"), []byte("outside subtree"), 0600)
	s := Storage{ID: "local", Name: "本机", Type: "local", Enabled: true, Config: map[string]string{"root": root}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	ctx := context.Background()
	fs := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/books", MountPoint: t.TempDir()}}
	f, err := fs.OpenFile(ctx, "/book.txt", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("chapter"))
	f.Close()
	if err := fs.Rename(ctx, "/book.txt", "/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	f, err = fs.OpenFile(ctx, "/renamed.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(f)
	f.Close()
	if string(data) != "chapter" {
		t.Fatal(string(data))
	}
	if _, err := fs.OpenFile(ctx, "/../private.txt", os.O_RDONLY, 0); err == nil {
		t.Fatal("traversal")
	}
	fs.config.ReadOnly = true
	if _, err := fs.OpenFile(ctx, "/denied.txt", os.O_CREATE|os.O_WRONLY, 0600); err == nil {
		t.Fatal("readonly write")
	}
	fs.config.ReadOnly = false
	if err := fs.RemoveAll(ctx, "/renamed.txt"); err != nil {
		t.Fatal(err)
	}
	trash, _ := filepath.Glob(filepath.Join(root, "books", ".aether-trash", "*", "renamed.txt"))
	if len(trash) != 1 {
		t.Fatal("trash policy", trash)
	}
	fs.config.StorageID = ""
	dir, err := fs.OpenFile(ctx, "/", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := dir.Readdir(-1)
	dir.Close()
	if len(entries) != 1 || !strings.Contains(entries[0].Name(), "本机") {
		t.Fatal("aggregate", entries)
	}
	a.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil })
	if _, err := fs.Stat(ctx, "/"+mountStorageName(s)+"/books"); err == nil {
		t.Fatal("disabled pool still readable")
	}
}

func TestMountFSCloudReader(t *testing.T) {
	a := testApp(t)
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "remote chapter")
	}))
	defer media.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fs/list":
			json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"total": 1, "content": []map[string]any{{"name": "book.mp3", "size": 14, "is_dir": false}}}})
		case "/api/fs/get":
			json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"raw_url": media.URL}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer up.Close()
	s := Storage{ID: "cloud", Name: "云端", Type: "openlist", Enabled: true, Config: map[string]string{"address": up.URL, "root": "/"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	fs := mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/"}}
	file, err := fs.OpenFile(context.Background(), "/book.mp3", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "remote chapter" {
		t.Fatal(string(data), err)
	}
	if _, err := fs.OpenFile(context.Background(), "/book.mp3", os.O_WRONLY, 0600); err == nil {
		t.Fatal("cloud write silently accepted")
	}
}

func TestLinuxFUSEMountIntegration(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("AETHER_TEST_FUSE") != "1" {
		t.Skip("requires an explicitly enabled Linux FUSE test environment")
	}
	a := testApp(t)
	source, target := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "read.txt"), []byte("FUSE read"), 0600); err != nil {
		t.Fatal(err)
	}
	s := Storage{ID: "fuse-source", Name: "FUSE", Type: "local", Enabled: true, Config: map[string]string{"root": source}}
	mount := MountConfig{ID: "fuse-integration", Name: "FUSE integration", StorageID: s.ID, Source: "/", MountPoint: target, Mode: 0755}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; st.Mounts = []MountConfig{mount}; return nil })
	defer a.mounts.close()
	if err := a.mounts.start(mount.ID); err != nil {
		t.Fatal(err)
	}
	target = mountTarget(mount)
	if !mountedAt(target) {
		t.Fatal("mount did not appear in mountinfo")
	}
	data, err := os.ReadFile(filepath.Join(target, "read.txt"))
	if err != nil || string(data) != "FUSE read" {
		t.Fatal("read", string(data), err)
	}
	if err := os.WriteFile(filepath.Join(target, "write.txt"), []byte("FUSE write"), 0600); err != nil {
		t.Fatal("write", err)
	}
	data, err = os.ReadFile(filepath.Join(source, "write.txt"))
	if err != nil || string(data) != "FUSE write" {
		t.Fatal("source write", string(data), err)
	}
	a.mounts.mu.Lock()
	err = a.mounts.stopLocked(mount.ID)
	a.mounts.mu.Unlock()
	if err != nil || mountedAt(target) {
		t.Fatal("unmount", err)
	}
}
