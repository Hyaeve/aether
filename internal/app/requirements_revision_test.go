package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecycleLegacyPINLoadsWithoutExecutingOrGuessing(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "legacy-pin", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	rule := recycleCleanupRule{StorageID: s.ID, PIN: "0123", Cron: "0 3 * * *", Days: 10}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{s}
		st.RecycleCleanup = recycleCleanupSettings{Enabled: true, Rules: []recycleCleanupRule{rule}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := newWithDirectories(a.ctx, a.store.dir, a.dataDir, a.outputDir)
	if err != nil {
		t.Fatal("legacy PIN must not prevent startup", err)
	}
	if loaded.store.snapshot().RecycleCleanup.Rules[0].PIN != "0123" {
		t.Fatal("legacy PIN was guessed or padded")
	}
	shareMock(t, func(r *http.Request) string { t.Fatal("invalid password accessed upstream", r.URL.Path); return "" })
	if _, err := loaded.cleanup115Recycle(context.Background(), rule); err == nil {
		t.Fatal("legacy PIN executed")
	}
	h := loaded.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	w := request(t, h, "GET", "/api/115-recycle-cleanup", nil, cookie)
	if !strings.Contains(w.Body.String(), `"needsPassword":true`) || strings.Contains(w.Body.String(), "0123") {
		t.Fatal(w.Body.String())
	}
	value := recycleCleanupSettings{Enabled: false, Rules: []recycleCleanupRule{rule}}
	if w := request(t, h, "PUT", "/api/115-recycle-cleanup", value, cookie); w.Code != 400 {
		t.Fatal("new four-digit PIN accepted", w.Code)
	}
	value.Rules[0].PIN = ""
	if w := request(t, h, "PUT", "/api/115-recycle-cleanup", value, cookie); w.Code != 200 {
		t.Fatal("cannot disable legacy rule", w.Code, w.Body.String())
	}
	value.Rules[0].PIN = "040123"
	if w := request(t, h, "PUT", "/api/115-recycle-cleanup", value, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if loaded.store.snapshot().RecycleCleanup.Rules[0].PIN != "040123" {
		t.Fatal("leading zero lost")
	}
}

func TestCloudCleanupOnlyTerminalRecordsAndRevalidates(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "cloud-cleanup", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil })
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	writes := 0
	shareMock(t, func(r *http.Request) string {
		if r.URL.Path == "/lixian/" && r.URL.Query().Get("ac") == "task_lists" {
			return `{"state":true,"page_count":1,"tasks":[{"info_hash":"ok","status":2},{"info_hash":"failed","status":-1},{"info_hash":"running","status":1},{"info_hash":"queued","status":0}]}`
		}
		writes++
		t.Fatal("unexpected cloud write", r.URL.Path)
		return ""
	})
	w := request(t, h, "GET", "/api/cloud/manage?kind=downloads&storage="+s.ID+"&prepare=cleanup", nil, cookie)
	var data struct {
		Items []cloudManagementEntry `json:"items"`
		Count int                    `json:"count"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Count != 2 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, row := range data.Items {
		if !cloudDownloadTerminal(row.Status) || row.StorageID != s.ID {
			t.Fatal(row)
		}
	}
	w = request(t, h, "POST", "/api/cloud/manage?kind=downloads&storage="+s.ID, map[string]any{"action": "cleanup", "ids": []string{"ok", "running"}, "confirm": true}, cookie)
	if w.Code != 409 || writes != 0 {
		t.Fatal("active record cleaned", w.Code, w.Body.String())
	}
}

func TestCASPriorityWithMissingSliceSize(t *testing.T) {
	a := testApp(t)
	info := CASInfo{Name: "film.mkv", Size: 20, SHA256: strings.Repeat("a", 64), MD5: strings.Repeat("b", 32), SliceMD5: strings.Repeat("c", 32)}
	for _, preferred := range []string{"mobile", "tianyi"} {
		m := Storage{ID: "m", Type: "mobile", Enabled: true, Config: map[string]string{"mode": "native"}}
		c := Storage{ID: "t", Type: "tianyi", Enabled: true, Config: map[string]string{"username": "u", "password": "p"}}
		a.store.update(func(st *State) error {
			st.Storages = []Storage{m, c}
			st.CASPool = normalizedCASPool(CASPoolConfig{Preferred: preferred, Mobile: CASPoolGroup{StorageIDs: []string{"m"}}, Tianyi: CASPoolGroup{StorageIDs: []string{"t"}}})
			st.CASTemporary = nil
			return nil
		})
		selected, _, err := a.selectCASStorage(info, time.Now())
		if err != nil || selected.Type != preferred {
			t.Fatal(preferred, selected.Type, err)
		}
	}
}

func TestExtensionRulesAllowUnderscoreAndHyphen(t *testing.T) {
	if got, err := normalizeExtensions("MP4;_ext;my-ext;ssa;sub"); err != nil || got != "mp4;_ext;my-ext;ssa;sub" {
		t.Fatal(got, err)
	}
	for _, value := range []string{"mp4;中文", "mp4;bad/name", "mp4;bad name", "mp4;abc.xyz"} {
		if _, err := normalizeExtensions(value); err == nil {
			t.Fatal("invalid extension accepted", value)
		}
	}
}

func TestCASMetadataKeepsRelativeHierarchy(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, s.Config["root"]+"/source/Season 1/poster.nfo", "metadata")
	writeTest(t, s.Config["root"]+"/source/Season 1/ignored.txt", "not metadata")
	task := Task{Kind: "cas", Name: "metadata", StorageID: s.ID, Source: "/source", Target: "cas-metadata", Mode: "incremental", MediaExtensions: "mkv", MetadataExtensions: "nfo"}
	if n, err := a.executeTask(context.Background(), task, s); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	data, err := os.ReadFile(filepath.Join(a.outputDir, "cas-metadata", "Season 1", "poster.nfo"))
	if err != nil || string(data) != "metadata" {
		t.Fatal(string(data), err)
	}
	if _, err := os.Stat(filepath.Join(a.outputDir, "cas-metadata", "Season 1", "ignored.txt")); !os.IsNotExist(err) {
		t.Fatal("unselected metadata copied", err)
	}
}
