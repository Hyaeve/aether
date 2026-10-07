package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/webdav"
)

func TestFileCreateUploadAndDownload(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	create := fileActionRequest{StorageID: s.ID, Source: "/", Action: "mkdir", Name: "new"}
	if w := request(t, h, "POST", "/api/files/action", create, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, name := range []string{"new", "../escape", "", "."} {
		create.Name = name
		if w := request(t, h, "POST", "/api/files/action", create, cookie); w.Code == 200 {
			t.Fatal("invalid or duplicate directory accepted", name)
		}
	}
	upload := func(name, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/files/upload?"+url.Values{"storage": {s.ID}, "parent": {"/new"}, "name": {name}}.Encode(), strings.NewReader(body))
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if upload("private.txt", "no", false).Code != 401 {
		t.Fatal("upload unprotected")
	}
	if w := upload("folder/child.txt", "hello", true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := upload("folder/child.txt", "overwrite", true); w.Code == 200 {
		t.Fatal("overwritten")
	}
	for _, name := range []string{"../escape.txt", "/absolute.txt", "a\\b", "x/../y"} {
		if w := upload(name, "bad", true); w.Code == 200 {
			t.Fatal("unsafe upload path", name)
		}
	}
	content, err := os.ReadFile(filepath.Join(s.Config["root"], "new", "folder", "child.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatal(string(content), err)
	}
	var list []File
	json.Unmarshal(request(t, h, "GET", "/api/files?storage="+s.ID+"&path=/new", nil, cookie).Body.Bytes(), &list)
	for _, file := range list {
		if file.Modified.IsZero() {
			t.Fatal("missing modification time", file)
		}
	}
}

func TestFileTimestampFormats(t *testing.T) {
	for _, input := range []string{`1720000000`, `"1720000000000"`, `"2026-10-07T12:13:14+08:00"`, `"2026-10-07 12:13:14"`, `"20261007121314"`} {
		var stamp fileTimestamp
		if err := json.Unmarshal([]byte(input), &stamp); err != nil || stamp.IsZero() || stamp.Year() > 2100 {
			t.Fatal(input, stamp, err)
		}
	}
	if !parseFileTime("not-a-date").IsZero() {
		t.Fatal("invented modification time")
	}
	file, err := file115(map[string]json.RawMessage{"file_category": json.RawMessage(`0`), "file_id": json.RawMessage(`123`), "file_name": json.RawMessage(`"folder"`), "user_utime": json.RawMessage(`1720000000`)})
	if err != nil || !file.IsDir || file.Modified.IsZero() {
		t.Fatal(file, err)
	}
}

func TestMountParentSiblingAllowed(t *testing.T) {
	a := testApp(t)
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	if err := os.Mkdir(source, 0755); err != nil {
		t.Fatal(err)
	}
	a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "sibling", Type: "local", Enabled: true, Config: map[string]string{"root": source}})
		return nil
	})
	mount := MountConfig{Name: "sibling", MountPoint: parent, Mode: 0755}
	if err := a.validateMount(&mount); err != nil {
		t.Fatal("sibling incorrectly blocked", err)
	}
	mount.MountPoint = source
	mount.StorageID = "sibling"
	if err := a.validateMount(&mount); err == nil {
		t.Fatal("recursive mount accepted")
	}
}

func TestFileUploadCloudDAVAndIncomplete(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "upload-dav", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	if err := a.receiveFile(context.Background(), s, "/", "books/chapter.txt", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "books", "chapter.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatal(string(data), err)
	}
	if err := a.receiveFile(context.Background(), s, "/", "books/chapter.txt", strings.NewReader("changed"), 7); err == nil {
		t.Fatal("overwritten existing cloud file")
	}
	if err := a.receiveFile(context.Background(), s, "/", "partial.txt", strings.NewReader("short"), 50); err == nil {
		t.Fatal("partial accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.receiveFile(ctx, s, "/", "cancel.txt", strings.NewReader("hello"), 5); err == nil {
		t.Fatal("cancelled upload accepted")
	}
	pending, _ := filepath.Glob(filepath.Join(a.dataDir, "cache", "file-upload", "*"))
	if len(pending) != 0 {
		t.Fatal("upload temporary files leaked", pending)
	}
}
