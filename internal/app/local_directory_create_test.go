package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDirectoryCreateProtectedAndBounded(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	login := request(t, h, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "test"}, nil)
	if login.Code != 201 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	mux := http.NewServeMux()
	mux.Handle("POST /api/local-directories", a.protected(http.HandlerFunc(a.createLocalDirectory)))
	parent := t.TempDir()
	input := map[string]string{"path": parent, "name": "new folder"}
	if w := request(t, mux, "POST", "/api/local-directories", input, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(t, mux, "POST", "/api/local-directories", input, cookie); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if info, err := os.Stat(filepath.Join(parent, "new folder")); err != nil || !info.IsDir() {
		t.Fatal("directory not created", err)
	}
	if w := request(t, mux, "POST", "/api/local-directories", input, cookie); w.Code != 409 {
		t.Fatal("collision accepted", w.Code)
	}
	for _, name := range []string{"", ".", "..", "../escape", `a\b`, "a:b", "bad\x00", "bad\n", "trailing.", " padded ", "CON", "COM1.txt", "a?b"} {
		input["name"] = name
		if w := request(t, mux, "POST", "/api/local-directories", input, cookie); w.Code != 400 {
			t.Fatalf("accepted name %q: %d", name, w.Code)
		}
	}
	input["name"] = "child"
	for _, path := range []string{"", "relative", parent + string(filepath.Separator) + "..", filepath.Join(parent, "missing"), `\\server\share`, `\\?\C:\Windows`} {
		input["path"] = path
		if w := request(t, mux, "POST", "/api/local-directories", input, cookie); w.Code != 400 {
			t.Fatalf("accepted path %q: %d", path, w.Code)
		}
	}
}

func TestLocalDirectoryCreateRejectsSymlinkParent(t *testing.T) {
	parent, target := t.TempDir(), t.TempDir()
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if root, err := openLocalDirectoryParent(link); err == nil {
		root.Close()
		t.Fatal("symlink accepted")
	}
	if err := os.Mkdir(filepath.Join(target, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if root, err := openLocalDirectoryParent(filepath.Join(link, "nested")); err == nil {
		root.Close()
		t.Fatal("ancestor symlink accepted")
	}
}
