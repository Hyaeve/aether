package app

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigBackup(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if request(t, h, "POST", "/api/config/backup", nil, nil).Code != 401 {
		t.Fatal("backup requires auth")
	}
	setup := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil)
	cookie := setup.Result().Cookies()[0]
	if request(t, h, "GET", "/api/config/backup", nil, cookie).Code != 405 {
		t.Fatal("backup method")
	}
	if err := os.MkdirAll(filepath.Join(a.store.dir, "organize"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.store.dir, "organize", "rules.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	res := request(t, h, "POST", "/api/config/backup", nil, cookie)
	if res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(res.Code)
	}
	reader, err := zip.NewReader(bytes.NewReader(res.Body.Bytes()), int64(res.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, entry := range reader.File {
		found[entry.Name] = true
	}
	if !found["state.enc"] || !found["master.key"] || !found["organize/rules.json"] {
		t.Fatal(found)
	}
	if found["log/system.json"] {
		t.Fatal("logs included")
	}
}
