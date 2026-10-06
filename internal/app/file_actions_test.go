package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileActions(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	if request(t, h, "POST", "/api/files/action", nil, nil).Code != 401 {
		t.Fatal("unauthorized")
	}
	root := s.Config["root"]
	os.WriteFile(filepath.Join(root, "one.mp4"), []byte("contents"), 0600)
	os.Mkdir(filepath.Join(root, "dest"), 0755)
	do := func(action string, ids []string, name, target string, want int) {
		t.Helper()
		res := request(t, h, "POST", "/api/files/action", fileActionRequest{StorageID: s.ID, Action: action, IDs: ids, Name: name, TargetStorage: s.ID, Target: target}, cookie)
		if res.Code != want {
			t.Fatalf("%s: %d %s", action, res.Code, res.Body.String())
		}
	}
	do("rename", []string{"/one.mp4"}, "two.mp4", "", 200)
	do("copy", []string{"/two.mp4"}, "", "/dest", 200)
	do("copy", []string{"/two.mp4"}, "", "/dest", 409)
	do("move", []string{"/dest"}, "", "/dest", 400)
	do("delete", []string{"/"}, "", "", 400)
	do("rename", []string{"../escape"}, "other", "", 400)
	do("delete", []string{"/two.mp4"}, "", "", 200)
	if _, err := os.Stat(filepath.Join(root, "two.mp4")); !os.IsNotExist(err) {
		t.Fatal("source retained")
	}
	matches, _ := filepath.Glob(filepath.Join(root, ".aether-trash", "*", "two.mp4"))
	if len(matches) != 1 {
		t.Fatal("trash missing")
	}
	do("move", []string{"/dest/two.mp4"}, "", "/", 200)
	data, err := os.ReadFile(filepath.Join(root, "two.mp4"))
	if err != nil || string(data) != "contents" {
		t.Fatal(err, string(data))
	}
}
