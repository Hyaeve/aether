package app

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestFolderArchiveAndCounts(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	root := s.Config["root"]
	if err := os.MkdirAll(filepath.Join(root, "目录", "empty"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "目录", "movie.mp4"), []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	address := "/api/files/archive?" + url.Values{"storage": {s.ID}, "parent": {"/"}, "id": {"/目录"}, "name": {"../../evil"}}.Encode()
	if w := request(t, h, "GET", address, nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatal("unprotected archive")
	}
	w := request(t, h, "GET", address, nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 3 {
		t.Fatal("empty directories lost", len(reader.File))
	}
	for _, file := range reader.File {
		if file.Name == "目录/movie.mp4" {
			f, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(f)
			f.Close()
			if err != nil || string(content) != "video" {
				t.Fatal("invalid file", err)
			}
		}
	}
	w = request(t, h, "GET", "/api/files/archive?storage="+s.ID+"&parent=/&id=/missing", nil, cookie)
	if w.Code != 400 {
		t.Fatal("missing selection accepted", w.Code)
	}
	w = request(t, h, "GET", "/api/files/archive?storage="+s.ID+"&parent=/&id=/", nil, cookie)
	if w.Code != 400 {
		t.Fatal("root accepted", w.Code)
	}
}
