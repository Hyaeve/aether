package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
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
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "saved-pool", Name: "saved", Type: "local", Enabled: true, Config: map[string]string{"root": t.TempDir()}})
		st.Links = append(st.Links, MediaLink{ID: "saved-link", Name: "saved", APIKey: "backup-private-key"})
		st.Simulcast = map[string]SimulcastConfig{"pan": {Enabled: true, Directory: "99"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	res := request(t, h, "POST", "/api/config/backup", map[string]string{"password": "backup-secret"}, cookie)
	if res.Code != 200 || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(res.Code)
	}
	plain, err := openBackup(res.Body.Bytes(), "backup-secret")
	if err != nil {
		t.Fatal(err)
	}
	var bundle configBundle
	if err := json.Unmarshal(plain, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.State.Username != "admin" || bundle.Files["organize/rules.json"] == nil {
		t.Fatal("missing configuration")
	}
	if bytes.Contains(res.Body.Bytes(), []byte("admin")) {
		t.Fatal("plaintext backup")
	}
	if _, err := openBackup(res.Body.Bytes(), "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if request(t, h, "POST", "/api/config/backup", map[string]string{"password": ""}, cookie).Code != 400 {
		t.Fatal("empty password accepted")
	}
	var envelope backupEnvelope
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Data[0] ^= 1
	tampered, _ := json.Marshal(envelope)
	if _, err := openBackup(tampered, "backup-secret"); err == nil {
		t.Fatal("tampered backup accepted")
	}
	if err := a.store.update(func(st *State) error { st.Username = "changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "backup.aether")
	part.Write(res.Body.Bytes())
	form.WriteField("password", "backup-secret")
	form.Close()
	req := httptest.NewRequest("POST", "/api/config/import", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(cookie)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if a.store.snapshot().Username != "changed" {
		t.Fatal("import applied before restart")
	}
	if err := applyPendingConfig(a.store); err != nil {
		t.Fatal(err)
	}
	if a.store.snapshot().Username != "admin" {
		t.Fatal("restore failed")
	}
	if cfg := a.store.snapshot().Simulcast["pan"]; !cfg.Enabled || cfg.Directory != "99" {
		t.Fatal("simulcast config missing from backup restore")
	}
	if len(a.store.snapshot().Links) != 1 || a.store.snapshot().Links[0].APIKey != "backup-private-key" || len(a.store.snapshot().Storages) != 1 {
		t.Fatal("storage or link missing")
	}
	reloaded, err := NewStore(a.store.dir)
	if err != nil || reloaded.snapshot().Links[0].APIKey != "backup-private-key" {
		t.Fatal("restored state not persisted", err)
	}
	if err := reloaded.initTools(a.store.toolsDir); err != nil {
		t.Fatal(err)
	}
	if reloaded.snapshot().Simulcast["pan"].Directory != "99" {
		t.Fatal("simulcast snapshot not persisted")
	}
	bundle.Files["../escape.json"] = json.RawMessage("{}")
	if validateBundle(bundle) == nil {
		t.Fatal("unsafe path accepted")
	}
}
