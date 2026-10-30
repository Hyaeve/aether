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
		st.Tasks = []Task{{ID: "manual", Kind: "strm", Cron: ""}, {ID: "cas", Kind: "cas", CASOperation: "generate", CASInfoMode: "tianyi", Mode: "incremental"}}
		st.FileFavorites = []FileFavorite{{Storage: "saved-pool", ID: "/a", Name: "a", History: []FavoriteCrumb{{ID: "/", Name: "a"}}}}
		st.FileVisits = []FileVisit{{Storage: "saved-pool", ID: "/a/b", History: []FavoriteCrumb{{ID: "/", Name: "a"}, {ID: "/a", Name: "b"}}}}
		st.CloudCASRecords = []cloudCASRecord{{ID: "runtime-cas", StorageID: "saved-pool", Status: "error", CAS: CASInfo{Name: "film.mp4", Size: 4}}}
		st.Pan115SigninReceipts = map[string]pan115SigninReceipt{"proof": {Status: "pending"}}
		st.CASPlaybackAttempts = map[string]bool{"pending-mobile-playback": true}
		st.Rename115Attempts = map[string]bool{"pending-extension-rename": true}
		st.ShareTransfers = []shareTransferRecord{{ID: "share-record", Link: "https://pan.quark.cn/s/private", Status: "completed"}}
		st.ShareLiveLinks = []shareLiveLink{{ID: "share-live", Intent: "pending-write", Link: "https://pan.quark.cn/s/private"}}
		st.BackupRules = []BackupRule{{ID: "backup-rule", Name: "备份规则", SourceID: "saved-pool", Source: "/", TargetID: "saved-pool", Target: "/other", Replace: "skip"}}
		st.Automations = []Automation{{ID: "rule", Name: "Rule", Enabled: true, Trigger: "manual", Steps: []AutomationStep{{Kind: "refresh", Condition: "always"}}}}
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
	if len(bundle.State.FileFavorites) != 0 || len(bundle.State.FileVisits) != 0 {
		t.Fatal("file records leaked into configuration backup")
	}
	if len(bundle.State.CloudCASRecords) != 0 {
		t.Fatal("CAS run records leaked into configuration backup")
	}
	if len(bundle.State.Pan115SigninReceipts) != 0 {
		t.Fatal("signin receipts leaked into configuration backup")
	}
	if len(bundle.State.CASPlaybackAttempts) != 0 {
		t.Fatal("CAS playback write intents leaked into configuration backup")
	}
	if len(bundle.State.Rename115Attempts) != 0 {
		t.Fatal("115 extension rename intents leaked into configuration backup")
	}
	if len(bundle.State.ShareTransfers) != 0 || len(bundle.State.ShareLiveLinks) != 0 {
		t.Fatal("share history or live writes leaked into configuration backup")
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
	if len(a.store.snapshot().CloudCASRecords) != 1 {
		t.Fatal("import erased CAS run records")
	}
	if len(a.store.snapshot().Pan115SigninReceipts) != 1 {
		t.Fatal("import erased signin anti-replay receipts")
	}
	if !a.store.snapshot().CASPlaybackAttempts["pending-mobile-playback"] {
		t.Fatal("import erased CAS playback anti-replay intents")
	}
	if !a.store.snapshot().Rename115Attempts["pending-extension-rename"] {
		t.Fatal("import erased 115 extension rename anti-replay intents")
	}
	if current := a.store.snapshot(); len(current.ShareTransfers) != 1 || len(current.ShareLiveLinks) != 1 || current.ShareLiveLinks[0].Intent != "pending-write" {
		t.Fatal("configuration import erased live share write receipts")
	}
	if len(a.store.snapshot().FileFavorites) != 1 || len(a.store.snapshot().FileVisits) != 1 {
		t.Fatal("configuration import lost existing file records")
	}
	if len(a.store.snapshot().BackupRules) != 1 || a.store.snapshot().BackupRules[0].Name != "备份规则" {
		t.Fatal("backup rules not restored")
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
	tasks := reloaded.snapshot().Tasks
	if len(tasks) != 2 || tasks[1].CASInfoMode != "tianyi" || tasks[1].Mode != "incremental" {
		t.Fatal("CAS feature selection lost across backup, import or restart", tasks)
	}
	if reloaded.snapshot().Simulcast["pan"].Directory != "99" {
		t.Fatal("simulcast snapshot not persisted")
	}
	if got := reloaded.snapshot(); len(got.Automations) != 1 || len(got.Tasks) != 2 || got.Tasks[0].ID != "manual" || got.Tasks[0].Cron != "" || !got.Tasks[0].NextRun.IsZero() {
		t.Fatal("automation or manual task lost in backup")
	}
	bundle.Files["../escape.json"] = json.RawMessage("{}")
	if validateBundle(bundle) == nil {
		t.Fatal("unsafe path accepted")
	}
}
