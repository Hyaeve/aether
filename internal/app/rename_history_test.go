package app

import (
	"context"
	"encoding/json"
	"golang.org/x/net/webdav"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameHistoryWebDAVContentGuard(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	remote := httptest.NewServer(&webdav.Handler{FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()})
	defer remote.Close()
	s := Storage{ID: "dav-history", Type: "webdav", Enabled: true, Config: map[string]string{"address": remote.URL, "root": "/"}}
	if err := a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := a.rawList(t.Context(), s, "/")
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	identity, err := a.pathRenameIdentity(t.Context(), s, "/", files[0])
	if err != nil {
		t.Fatal(err)
	}
	d := RenameDirectory{Source: "/", Revision: storageRevision(s)}
	item := RenameRecord{FileID: "/new.txt", Name: "old.txt", NewName: "new.txt", Identity: identity}
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("replaced"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.restoreRenameRecord(t.Context(), s, d, item); err == nil {
		t.Fatal("changed DAV content restored")
	}
	files, err = a.rawList(t.Context(), s, "/")
	if err != nil {
		t.Fatal(err)
	}
	item.Identity, err = a.pathRenameIdentity(t.Context(), s, "/", files[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := a.restoreRenameRecord(t.Context(), s, d, item); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := a.restoreRenameRecord(ctx, s, d, item); err == nil {
		t.Fatal("cancelled restore succeeded")
	}
}

func TestRenameHistoryDirectoryRestoreAndRuntime(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	run := func(source, name, next string) {
		t.Helper()
		os.MkdirAll(filepath.Join(s.Config["root"], source), 0700)
		if err := os.WriteFile(filepath.Join(s.Config["root"], source, name), []byte("unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
		in := renameRequest{StorageID: s.ID, Source: "/" + source, SourceLabel: "/" + source, IDs: []string{"/" + source + "/" + name}, Rules: []renameRule{{Kind: "replace", Find: name, Replace: next}}}
		p := request(t, h, "POST", "/api/files/rename-preview", in, cookie)
		if p.Code != 200 {
			t.Fatal(p.Body.String())
		}
		json.Unmarshal(p.Body.Bytes(), &in.Expected)
		out := request(t, h, "POST", "/api/files/rename", in, cookie)
		if out.Code != 200 || strings.Contains(out.Body.String(), "error") {
			t.Fatal(out.Body.String())
		}
	}
	run("A", "old.txt", "new.txt")
	run("A", "new.txt", "final.txt")
	history := a.store.snapshot().RenameHistory
	if len(history) != 1 || len(history[0].Items) != 1 || history[0].Items[0].Name != "new.txt" {
		t.Fatal(history)
	}
	// Manual renames and previews do not create workbench history.
	if out := request(t, h, "POST", "/api/files/action", map[string]any{"storageId": s.ID, "source": "/A", "action": "rename", "ids": []string{"/A/final.txt"}, "name": "manual.txt"}, cookie); out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	restored := request(t, h, "POST", "/api/files/rename-restore", map[string]any{"directoryId": history[0].ID, "ids": []string{history[0].Items[0].ID}}, cookie)
	if restored.Code != 200 || !strings.Contains(restored.Body.String(), `"status":"failed"`) {
		t.Fatal(restored.Body.String())
	}
	if out := request(t, h, "GET", "/api/files/rename-history", nil, cookie); out.Code != 200 || strings.Contains(out.Body.String(), "final.txt") {
		t.Fatal(out.Body.String())
	}
	run("A", "old.txt", "new.txt")
	history = a.store.snapshot().RenameHistory
	restored = request(t, h, "POST", "/api/files/rename-restore", map[string]any{"directoryId": history[0].ID, "ids": []string{history[0].Items[0].ID}}, cookie)
	if restored.Code != 200 || !strings.Contains(restored.Body.String(), `"status":"success"`) {
		t.Fatal(restored.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.Config["root"], "A", "old.txt")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"B", "C", "D", "E", "F"} {
		run(source, "old.txt", "new.txt")
	}
	history = a.store.snapshot().RenameHistory
	if len(history) != 5 || history[0].Source != "/F" || history[4].Source != "/B" {
		t.Fatal(history)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil || len(reopened.snapshotWithLogLimit(0).RenameHistory) != 5 {
		t.Fatal("history recovery", err)
	}
	for i, d := range reopened.snapshotWithLogLimit(0).RenameHistory {
		if d.ID != history[i].ID || d.Source != history[i].Source || len(d.Items) != len(history[i].Items) || d.Items[0].Name != history[i].Items[0].Name || d.Items[0].NewName != history[i].Items[0].NewName {
			t.Fatal("reopened history lost order or records", i)
		}
	}
	// A new workbench operation in an older directory moves that directory to the front.
	run("C", "another.txt", "renamed.txt")
	history = a.store.snapshot().RenameHistory
	if len(history) != 5 || history[0].Source != "/C" || history[1].Source != "/F" {
		t.Fatal("latest operated directory not first", history)
	}
	values := a.store.jsonValues()
	stripRuntimeConfig(values)
	raw, _ := json.Marshal(values)
	if strings.Contains(string(raw), "renameHistory") {
		t.Fatal("history leaked to config")
	}
	raw, _ = json.Marshal(a.store.runtimeValues())
	if !strings.Contains(string(raw), "renameHistory") {
		t.Fatal("runtime history missing")
	}
	if request(t, h, "GET", "/api/files/rename-history", nil, nil).Code != 401 || request(t, h, "POST", "/api/files/rename-restore", map[string]any{}, nil).Code != 401 {
		t.Fatal("unprotected history")
	}
}

func TestRenameHistoryIdentityCollisionAndReplacedFile(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	root := s.Config["root"]
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("original"), 0600)
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := localRenameIdentity(r, "new.txt")
	r.Close()
	if err != nil {
		t.Fatal(err)
	}
	d := RenameDirectory{Source: "/", Revision: storageRevision(s)}
	item := RenameRecord{FileID: "/new.txt", Name: "old.txt", NewName: "new.txt", Identity: identity}
	os.WriteFile(filepath.Join(root, "old.txt"), []byte("occupied"), 0600)
	if err := a.restoreRenameRecord(t.Context(), s, d, item); err == nil {
		t.Fatal("overwrote collision")
	}
	os.Remove(filepath.Join(root, "old.txt"))
	os.Remove(filepath.Join(root, "new.txt"))
	os.WriteFile(filepath.Join(root, "new.txt"), []byte("different replacement"), 0600)
	if err := a.restoreRenameRecord(t.Context(), s, d, item); err == nil {
		t.Fatal("restored replaced file")
	}
}
