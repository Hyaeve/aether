package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transferLocationJSON(t *testing.T, entry transferEntry, side string) map[string]any {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	location, ok := result[side].(map[string]any)
	if !ok {
		t.Fatalf("missing %s transfer location: %s", side, data)
	}
	return location
}

func TestTransferLocationsLocalCopyAndBrowserUpload(t *testing.T) {
	a := testApp(t)
	source, target := addLocal(t, a), addLocal(t, a)
	source.Name, target.Name = "来源资料", "目标资料"
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{source, target}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(source.Config["root"], "电影", "原片", "movie.mkv"), "movie")
	if err := os.MkdirAll(filepath.Join(target.Config["root"], "归档", "完成"), 0755); err != nil {
		t.Fatal(err)
	}
	handler := a.Handler(t.TempDir())
	cookie := request(t, handler, "POST", "/api/auth/setup", credentials{Username: "owner", Password: "x"}, nil).Result().Cookies()[0]
	response := request(t, handler, "POST", "/api/files/action", fileActionRequest{
		StorageID: source.ID, Source: "/电影/原片", Action: "copy",
		IDs: []string{"/电影/原片/movie.mkv"}, TargetStorage: target.ID, Target: "/归档/完成",
	}, cookie)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if len(a.transfers.items) != 1 {
		t.Fatal("copy record missing", a.transfers.items)
	}
	for _, entry := range a.transfers.items {
		from, to := transferLocationJSON(t, entry, "from"), transferLocationJSON(t, entry, "to")
		if from["storage"] != source.Name || from["directory"] != "原片" || to["storage"] != target.Name || to["directory"] != "完成" {
			t.Fatal(from, to)
		}
	}
	if err := a.receiveFile(a.ctx, target, "/归档/完成", "Series/E01.mkv", strings.NewReader("episode"), 7); err != nil {
		t.Fatal(err)
	}
	for _, entry := range a.transfers.items {
		if entry.Kind != "upload" {
			continue
		}
		from, to := transferLocationJSON(t, entry, "from"), transferLocationJSON(t, entry, "to")
		if from["storage"] != "浏览器" || from["directory"] != "Series" || to["storage"] != target.Name || to["directory"] != "Series" || to["path"] != "/归档/完成/Series" {
			t.Fatal(from, to)
		}
	}
}

func TestTransferLocationsBrowserDownload(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	writeTest(t, filepath.Join(s.Config["root"], "Movies", "movie.mkv"), "movie")
	recorder := httptest.NewRecorder()
	a.Handler(t.TempDir()).ServeHTTP(recorder, httptest.NewRequest("GET", a.streamURL(s.ID, "/Movies/movie.mkv", ""), nil))
	if recorder.Code != 200 || recorder.Body.String() != "movie" {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	if len(a.transfers.items) != 1 {
		t.Fatal("download record missing", a.transfers.items)
	}
	for _, entry := range a.transfers.items {
		from, to := transferLocationJSON(t, entry, "from"), transferLocationJSON(t, entry, "to")
		if from["storage"] != s.Name || from["directory"] != "Movies" || to["storage"] != "浏览器" || to["directory"] != "" {
			t.Fatal(from, to)
		}
	}
}

func TestTransferLocationsOpaqueDirectoryWithoutExtraCloudRequests(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error { st.Settings.CacheEnabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	s := Storage{ID: "quark", Name: "夸克影视", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private-cookie"}}
	previous := apiClient
	defer func() { apiClient = previous }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"code":0,"data":{"list":[{"fid":"opaque-dir","file_name":"剧集","dir":true}]}}`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	if _, err := a.listFiles(context.Background(), s, "/", 1, true); err != nil {
		t.Fatal(err)
	}
	err := a.receiveFile(a.ctx, s, "opaque-dir", "episode.mkv", strings.NewReader("short"), 20)
	if err == nil {
		t.Fatal("truncated upload accepted")
	}
	if calls != 1 {
		t.Fatal("display metadata issued extra cloud requests", calls)
	}
	for _, entry := range a.transfers.items {
		to := transferLocationJSON(t, entry, "to")
		if to["directory"] != "剧集" || to["path"] != "/剧集" || to["storage"] != "夸克影视" {
			t.Fatal(to)
		}
		encoded, _ := json.Marshal(entry)
		if strings.Contains(string(encoded), "private-cookie") {
			t.Fatal("storage credentials leaked")
		}
	}
}

func TestTransferLocationsUnknownIdentityAndInvalidatedHints(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "quark", Name: "夸克", Type: "quark", Config: map[string]string{"cookie": "one"}}
	generation := a.transferPathGeneration()
	a.rememberTransferPaths(s, "0", []File{{ID: "a", Name: "A", IsDir: true}}, generation)
	a.rememberTransferPaths(s, "a", []File{{ID: "b", Name: "B", IsDir: true}}, generation)
	if got := a.transferDirectory(s, "b"); got.Directory != "B" || got.Path != "/A/B" {
		t.Fatal(got)
	}
	changed := s
	changed.Config = map[string]string{"cookie": "two"}
	for _, got := range []transferLocation{a.transferDirectory(changed, "b"), a.transferDirectory(s, "unknown-id")} {
		if got.Directory != "" || got.Path != "" {
			t.Fatal("unknown identity was presented as a known folder", got)
		}
	}
	a.cache.clear()
	if got := a.transferDirectory(s, "b"); got.Directory != "B" {
		t.Fatal("ordinary file-cache refresh lost the selected folder name", got)
	}
	a.invalidateTransferPaths(s)
	a.rememberTransferPaths(s, "0", []File{{ID: "a", Name: "Late old name", IsDir: true}}, generation)
	if got := a.transferDirectory(s, "b"); got.Directory != "" || got.Path != "" {
		t.Fatal("directory mutation retained old names", got)
	}
	// A known deepest name is still useful without inventing missing ancestors.
	a.rememberTransferPaths(s, "missing-parent", []File{{ID: "c", Name: "C", IsDir: true}}, a.transferPathGeneration())
	if got := a.transferDirectory(s, "c"); got.Directory != "C" || got.Path != "" {
		t.Fatal("unverified ancestry was invented", got)
	}
}

func TestTransferLocationsFailuresPersistAndLegacyRecordsStillLoad(t *testing.T) {
	a := testApp(t)
	file := filepath.Join(t.TempDir(), "failures.json")
	a.transfers.restore(file)
	p := a.beginTransfer(a.ctx, "copy", Storage{Name: "源"}, "movie.mkv", 10,
		transferLocations{From: transferLocation{Storage: "源", Directory: "影片"}, To: transferLocation{Storage: "目标", Directory: "归档"}})
	p.finish(io.ErrUnexpectedEOF)
	var restored transferLog
	restored.restore(file)
	if len(restored.items) != 1 {
		t.Fatal(restored.items)
	}
	to := transferLocationJSON(t, restored.items[p.id], "to")
	if to["storage"] != "目标" || to["directory"] != "归档" {
		t.Fatal(to)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var legacy []map[string]any
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy[0], "from")
	delete(legacy[0], "to")
	raw, _ = json.Marshal(legacy)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored.restore(file)
	if len(restored.items) != 1 {
		t.Fatal("legacy failure dropped", restored.items)
	}
}

func TestTransferLocationsMetadataBound(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "quark", Type: "quark"}
	files := make([]File, 8200)
	for i := range files {
		files[i] = File{ID: strings.Repeat("x", i%8+1) + string(rune(i+128)), Name: "directory", IsDir: true}
	}
	a.rememberTransferPaths(s, "0", files, a.transferPathGeneration())
	if len(a.transfers.paths) != 8192 {
		t.Fatal("directory hints are not bounded", len(a.transfers.paths))
	}
}
