package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameRulesAndCollisions(t *testing.T) {
	rules := []renameRule{{Kind: "replace", Find: "OLD", Replace: "new"}, {Kind: "replace", Find: "new", Replace: "final", FirstOnly: true}}
	name, err := applyRenameRules("old-OLD.mkv", rules)
	if err != nil || name != "final-new.mkv" {
		t.Fatal(name, err)
	}
	literal, err := applyRenameRules("a.$a", []renameRule{{Kind: "replace", Find: ".", Replace: "$1"}})
	if err != nil || literal != "a$1$a" {
		t.Fatal("replacement must be literal", literal, err)
	}
	files := []File{{ID: "a", Name: "one"}, {ID: "b", Name: "two"}}
	items, err := renamePlan(files, renameRequest{IDs: []string{"a"}, Rules: []renameRule{{Kind: "replace", Find: "one", Replace: "two"}}})
	if err != nil || items[0].Error == "" {
		t.Fatal("collision not reported", items, err)
	}
	if _, err := applyRenameRules("one", []renameRule{{Kind: "replace", Find: "one", Replace: "../escape"}}); err == nil {
		t.Fatal("unsafe name accepted")
	}
}

func TestRenamePreviewExecuteAndRulePersistence(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	root := s.Config["root"]
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	input := renameRequest{StorageID: s.ID, Source: "/", IDs: []string{"/old.txt"}, Rules: []renameRule{{Kind: "replace", Find: "old", Replace: "new"}}}
	for _, endpoint := range []string{"/api/files/rename-preview", "/api/files/rename", "/api/files/rename-rules", "/api/files/directory-size"} {
		if request(t, h, "POST", endpoint, input, nil).Code != http.StatusUnauthorized {
			t.Fatal("unprotected endpoint", endpoint)
		}
	}
	result := request(t, h, "POST", "/api/files/rename-preview", input, cookie)
	if result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	if err := json.Unmarshal(result.Body.Bytes(), &input.Expected); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); err != nil {
		t.Fatal("preview modified files")
	}
	stale := input
	stale.Expected = append([]renameItem(nil), input.Expected...)
	stale.Expected[0].Name = "wrong"
	if result := request(t, h, "POST", "/api/files/rename", stale, cookie); result.Code != 409 {
		t.Fatal("stale preview accepted", result.Body.String())
	}
	if result := request(t, h, "POST", "/api/files/rename", input, cookie); result.Code != 200 {
		t.Fatal(result.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(root, "new.txt")); err != nil || string(data) != "keep" {
		t.Fatal("rename content", string(data), err)
	}
	result = request(t, h, "POST", "/api/files/rename-rules", renameRuleSet{Name: "测试规则", Rules: input.Rules}, cookie)
	var sets []renameRuleSet
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &sets) != nil || len(sets) != 1 {
		t.Fatal(result.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(a.store.dir, "organize", "rename-rules.json")); err != nil || !json.Valid(data) {
		t.Fatal("missing rule config", err)
	}
	if result := request(t, h, "DELETE", "/api/files/rename-rules", sets[0], cookie); result.Code != 200 || result.Body.String() != "[]\n" {
		t.Fatal(result.Body.String())
	}
}

func TestDirectorySizeCachedAndInvalidated(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	root := s.Config["root"]
	os.MkdirAll(filepath.Join(root, "folder", "nested"), 0755)
	os.WriteFile(filepath.Join(root, "folder", "one"), []byte("123"), 0600)
	os.WriteFile(filepath.Join(root, "folder", "nested", "two"), []byte("45678"), 0600)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	result := request(t, h, "POST", "/api/files/directory-size", map[string]string{"storageId": s.ID, "parent": "/", "id": "/folder"}, cookie)
	var folder File
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &folder) != nil || folder.Size != 8 || !folder.SizeKnown || folder.FolderCount != 1 || folder.FileCount != 2 {
		t.Fatal(result.Body.String())
	}
	list := func() File {
		t.Helper()
		var files []File
		result := request(t, h, "GET", "/api/files?storage="+s.ID+"&path=/&refresh=true", nil, cookie)
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &files) != nil {
			t.Fatal(result.Body.String())
		}
		for _, f := range files {
			if f.ID == "/folder" {
				return f
			}
		}
		t.Fatal("folder missing")
		return File{}
	}
	if f := list(); !f.SizeKnown || f.Size != 8 || f.FolderCount != 1 || f.FileCount != 2 {
		t.Fatal("directory size missing from listing", f)
	}
	legacy := folder
	legacy.CountsKnown, legacy.FolderCount, legacy.FileCount = false, 0, 0
	a.cache.put(directorySizeKey(s.ID, folder.ID), []File{legacy}, 30, a.store.snapshot().Settings)
	result = request(t, h, "POST", "/api/files/directory-size", map[string]string{"storageId": s.ID, "parent": "/", "id": "/folder"}, cookie)
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &folder) != nil || !folder.CountsKnown || folder.FileCount != 2 {
		t.Fatal("legacy size cache did not refresh counts", result.Body.String())
	}
	if err := a.cache.persist(a.dataDir); err != nil {
		t.Fatal(err)
	}
	a.cache.clear()
	a.cache.restore(a.dataDir, a.store.snapshot().Settings)
	if f := list(); !f.SizeKnown {
		t.Fatal("size not restored")
	}
	a.cache.clear()
	if f := list(); f.SizeKnown {
		t.Fatal("size retained after cache invalidation")
	}
}
