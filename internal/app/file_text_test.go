package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFileTextSharedEditorAuthenticatedRevisionAndTypes(t *testing.T) {
	a, _, source, _ := backupFixture(t)
	h := a.Handler(t.TempDir())
	os.WriteFile(filepath.Join(source, "a.strm"), []byte("https://example.test/media"), 0644)
	os.WriteFile(filepath.Join(source, "a.txt"), []byte("not editable"), 0644)
	endpoint := "/api/files/text?storage=source&parent=/&id=/a.strm"
	if request(t, h, "GET", endpoint, nil, nil).Code != 401 {
		t.Fatal("unprotected")
	}
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "GET", endpoint, nil, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var data map[string]string
	json.Unmarshal(w.Body.Bytes(), &data)
	stale := data["revision"]
	data["content"] = "https://example.test/new"
	if w = request(t, h, "PUT", endpoint, data, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	content, _ := os.ReadFile(filepath.Join(source, "a.strm"))
	if string(content) != data["content"] {
		t.Fatal("not saved")
	}
	if request(t, h, "PUT", endpoint, map[string]string{"content": "bad", "revision": stale}, cookie).Code != 409 {
		t.Fatal("stale revision accepted")
	}
	if request(t, h, "GET", "/api/files/text?storage=source&parent=/&id=/a.txt", nil, cookie).Code != 400 {
		t.Fatal("unrelated file accepted")
	}
	if request(t, h, "GET", "/api/files/text?storage=source&parent=/&id=/../secret.strm", nil, cookie).Code == 200 {
		t.Fatal("traversal accepted")
	}
}
