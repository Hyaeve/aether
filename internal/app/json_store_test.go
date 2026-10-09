package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONModulesCredentialsAndUnchangedWrites(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.initModules(); err != nil {
		t.Fatal(err)
	}
	if err := s.update(func(st *State) error {
		st.Storages = []Storage{{ID: "one", Name: "Readable pool", Config: map[string]string{"root": "/readable", "cookie": "private-ck", "accessToken": "private-access", "refreshToken": "private-refresh", "authorization": "private-auth", "token": "private-token", "password": "private-password", "address": "https://example.test"}}, {ID: "two", Config: map[string]string{"cookie": "second-ck"}}}
		st.Links = []MediaLink{{ID: "link", APIKey: "private-link", Password: "private-link-password"}}
		st.Plugins = map[string]PluginConfig{"tmdb": {APIKey: "private-tmdb"}, "proxy": {Address: "http://name:private-proxy@example.test:80", Password: "private-proxy-pass"}}
		st.QuarkTV = map[string]QuarkTVBinding{"one": {AccessToken: "private-tv", RefreshToken: "private-tv-refresh"}}
		st.Password = "$2a$hashed-password"
		st.Tasks = []Task{{ID: "a", Kind: "cas"}, {ID: "b", Kind: "strm"}, {ID: "c", Kind: "unknown"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.dir, "storage", "storage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) || !bytes.Contains(raw, []byte("Readable pool")) || !bytes.Contains(raw, []byte("https://example.test")) || bytes.Contains(raw, []byte("private-")) {
		t.Fatal("configuration visibility or encryption wrong")
	}
	for _, key := range jsonModules {
		data, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(key)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("private-")) {
			t.Fatal("plaintext credential in", key)
		}
	}
	before, _ := os.Stat(filepath.Join(s.dir, "storage", "storage.json"))
	if err := s.update(func(st *State) error { st.Username = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(s.dir, "storage", "storage.json"))
	if before.ModTime() != after.ModTime() {
		t.Fatal("unrelated save rewrote storage credentials")
	}
	reopened, err := NewStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.snapshot()
	if got.Storages[0].Config["cookie"] != "private-ck" || got.QuarkTV["one"].RefreshToken != "private-tv-refresh" || got.Plugins["proxy"].Password != "private-proxy-pass" || got.Links[0].APIKey != "private-link" || got.Password != "$2a$hashed-password" || got.SignKey != s.state.SignKey || got.Tasks[0].ID != "a" || got.Tasks[1].ID != "b" || got.Tasks[2].ID != "c" {
		t.Fatal("restore incomplete")
	}
	// Even a valid outer MAC cannot authorize relocating a credential to another ID.
	var doc jsonConfig
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(doc.Data, &items); err != nil {
		t.Fatal(err)
	}
	items[1]["config"].(map[string]any)["cookie"] = items[0]["config"].(map[string]any)["cookie"]
	doc.Data, _ = json.Marshal(items)
	doc.Auth = s.configMAC("storage/storage", doc.Data)
	tampered, _ := json.Marshal(doc)
	if err := s.decodeJSONConfig("storage/storage", tampered, &[]Storage{}); err == nil {
		t.Fatal("relocated credential accepted")
	}
}

func TestJSONInterruptedTransactionRecoveryAndMigration(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.update(func(st *State) error {
		st.Storages = []Storage{{ID: "pool", Name: "Legacy", Config: map[string]string{"cookie": "legacy-ck"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A v0.4.0 content-addressed root remains intact after migration.
	s.modular = true
	if err := s.saveModulesLocked(); err != nil {
		t.Fatal(err)
	}
	legacyRoot, _ := os.ReadFile(filepath.Join(s.dir, "state.enc"))
	if err := s.initModules(); err != nil {
		t.Fatal(err)
	}
	retained, _ := os.ReadFile(filepath.Join(s.dir, "state.enc"))
	if !bytes.Equal(legacyRoot, retained) {
		t.Fatal("migration overwrote legacy backup")
	}
	storagePath := filepath.Join(s.dir, "storage", "storage.json")
	original, _ := os.ReadFile(storagePath)
	rootPath := filepath.Join(s.dir, "state.json")
	root, _ := os.ReadFile(rootPath)
	old := map[string][]byte{"storage/storage": original, "state": root}
	plain, _ := json.Marshal(old)
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(s.dir, ".config-transaction.enc"), s.aead.Seal(nonce, nonce, plain, []byte("config-transaction-v1"))); err != nil {
		t.Fatal(err)
	}
	changed, err := s.encodeJSONConfig("storage/storage", []Storage{{ID: "pool", Name: "Uncommitted"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(storagePath, changed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(rootPath); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Storages[0].Name != "Legacy" || reopened.state.Storages[0].Config["cookie"] != "legacy-ck" {
		t.Fatal("mixed configuration after crash")
	}
	if _, err := os.Stat(filepath.Join(s.dir, ".config-transaction.enc")); !os.IsNotExist(err) {
		t.Fatal("journal retained", err)
	}
	if err := os.Remove(filepath.Join(s.dir, "link", "links.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(s.dir); err == nil {
		t.Fatal("missing module silently accepted")
	}
}

func TestJSONMissingMasterKeyAndExternalKey(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.initModules(); err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(s.dir, "master.key"))
	if err := os.Remove(filepath.Join(s.dir, "master.key")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(s.dir); err == nil {
		t.Fatal("missing key regenerated")
	}
	external := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(external, key, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AETHER_MASTER_KEY_FILE", external)
	if _, err := NewStore(s.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "master.key")); !os.IsNotExist(err) {
		t.Fatal("external key copied into config")
	}
}

func TestJSONBackupImportsAcrossDifferentMasterKeys(t *testing.T) {
	a := testApp(t)
	if err := a.store.initModules(); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "pool", Name: "Restored", Config: map[string]string{"cookie": "private-cross-key"}}}
		st.Plugins = map[string]PluginConfig{"tmdb": {APIKey: "private-cross-plugin"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "POST", "/api/config/backup", map[string]string{"password": "backup-password"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	plain, err := openBackup(w.Body.Bytes(), "backup-password")
	if err != nil {
		t.Fatal(err)
	}
	var bundle configBundle
	if err := json.Unmarshal(plain, &bundle); err != nil {
		t.Fatal(err)
	}
	for _, key := range jsonModules {
		if _, exists := bundle.Files[key+".json"]; exists {
			t.Fatal("duplicate encrypted module in backup", key)
		}
	}
	b, err := newWithDirectories(context.Background(), t.TempDir(), t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bh := b.Handler(t.TempDir())
	bc := request(t, bh, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "y"}, nil).Result().Cookies()[0]
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "backup.aether")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(w.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("password", "backup-password"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/config/import", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(bc)
	response := httptest.NewRecorder()
	bh.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err := applyPendingConfig(b.store); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(b.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.state.Storages[0].Config["cookie"] != "private-cross-key" || reopened.state.Plugins["tmdb"].APIKey != "private-cross-plugin" {
		t.Fatal("cross-key import lost secrets")
	}
}
