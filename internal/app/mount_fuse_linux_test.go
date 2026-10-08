package app

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/net/webdav"
)

// Exercise the actual inode callbacks without requiring a kernel FUSE mount.
func TestNativeFuseLocalCallbacks(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Movies", "film.mp4"), "video")
	ctx := context.Background()
	node := &nativeFuseNode{backend: mountFS{app: a, config: MountConfig{ID: "test", StorageID: s.ID, Source: "/", Mode: 0755, UID: 123, GID: 456}}}
	fs.NewNodeFS(node, &fs.Options{})
	var space fuse.StatfsOut
	if errno := node.Statfs(ctx, &space); errno != 0 || space.Blocks == 0 || space.Bsize == 0 || space.Bavail == 0 {
		t.Fatal("missing real filesystem capacity", space, errno)
	}
	var out fuse.EntryOut
	child, handle, _, errno := node.Create(ctx, "native.txt", syscall.O_RDWR, 0600, &out)
	if errno != 0 {
		t.Fatal(errno)
	}
	h := handle.(*nativeFuseHandle)
	if _, errno := h.Write(ctx, []byte("hello"), 0); errno != 0 {
		t.Fatal(errno)
	}
	if errno := h.Flush(ctx); errno != 0 {
		t.Fatal(errno)
	}
	var attr fuse.AttrOut
	if errno := child.Operations().(*nativeFuseNode).Getattr(ctx, handle, &attr); errno != 0 || attr.Size != 5 || attr.Uid != 123 {
		t.Fatal(attr, errno)
	}
	if errno := node.Unlink(ctx, "native.txt"); errno != syscall.EBUSY {
		t.Fatal("active write deleted", errno)
	}
	if errno := h.Release(ctx); errno != 0 {
		t.Fatal(errno)
	}
	data, err := os.ReadFile(filepath.Join(s.Config["root"], "native.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
	if errno := node.Rename(ctx, "native.txt", node, "renamed.txt", 0); errno != 0 {
		t.Fatal(errno)
	}
	if errno := node.Rmdir(ctx, "Movies"); errno != syscall.ENOTEMPTY {
		t.Fatal("recursive rmdir", errno)
	}
	if errno := node.Unlink(ctx, "renamed.txt"); errno != 0 {
		t.Fatal(errno)
	}
	node.backend.config.ReadOnly = true
	if errno := node.Access(ctx, 2); errno != syscall.EROFS {
		t.Fatal("readonly access", errno)
	}
	if _, _, _, errno := node.Create(ctx, "denied", syscall.O_RDWR, 0600, &out); errno != syscall.EROFS {
		t.Fatal(errno)
	}
}

func TestNativeFuseCloudFlushAndRandomWrite(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "dav", Name: "dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	node := &nativeFuseNode{backend: mountFS{app: a, config: MountConfig{StorageID: s.ID, Source: "/", Mode: 0755}}}
	fs.NewNodeFS(node, &fs.Options{})
	ctx := context.Background()
	var out fuse.EntryOut
	_, handle, _, errno := node.Create(ctx, "book.txt", syscall.O_RDWR, 0600, &out)
	if errno != 0 {
		t.Fatal(errno)
	}
	h := handle.(*nativeFuseHandle)
	h.Write(ctx, []byte("world"), 5)
	h.Write(ctx, []byte("hello"), 0)
	if errno := h.Flush(ctx); errno != 0 {
		t.Fatal(errno)
	}
	h.Write(ctx, []byte("HELLO"), 0)
	if errno := h.Fsync(ctx, 0); errno != 0 {
		t.Fatal(errno)
	}
	pending := h.staged.File.Name()
	h.Release(ctx)
	data, err := os.ReadFile(filepath.Join(root, "book.txt"))
	if err != nil || string(data) != "HELLOworld" {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatal("successful staging not removed", err)
	}
	child, errno := node.Lookup(ctx, "book.txt", &out)
	if errno != 0 {
		t.Fatal(errno)
	}
	node.AddChild("book.txt", child, true)
	readHandle, _, errno := child.Operations().(*nativeFuseNode).Open(ctx, syscall.O_RDONLY)
	if errno != 0 {
		t.Fatal("open existing cloud file", errno)
	}
	reader := readHandle.(*nativeFuseHandle)
	result, errno := reader.Read(ctx, make([]byte, 5), 5)
	if errno != 0 {
		t.Fatal("read existing cloud file", errno)
	}
	bytes, status := result.Bytes(nil)
	if status != fuse.OK || string(bytes) != "world" {
		t.Fatal(string(bytes), status)
	}
	reader.Release(ctx)
	handle, _, errno = child.Operations().(*nativeFuseNode).Open(ctx, syscall.O_RDWR)
	if errno != 0 {
		t.Fatal(errno)
	}
	h = handle.(*nativeFuseHandle)
	h.Write(ctx, []byte("!"), 9)
	if errno := h.Flush(ctx); errno != 0 {
		t.Fatal(errno)
	}
	h.Release(ctx)
	data, _ = os.ReadFile(filepath.Join(root, "book.txt"))
	if string(data) != "HELLOworl!" {
		t.Fatal("partial write lost existing bytes", string(data))
	}
}
