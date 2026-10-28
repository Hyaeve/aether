package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type virtual115NameFixture struct {
	a       *App
	handler http.Handler
	cookie  *http.Cookie
	tree    map[string][]shareEntry
	reads   []string
	pass    string
}

func newVirtual115NameFixture(t *testing.T, pass string) *virtual115NameFixture {
	t.Helper()
	a := testApp(t)
	f := &virtual115NameFixture{a: a, pass: pass, tree: map[string][]shareEntry{
		"0":  {{ID: "9", Name: "Show", IsDir: true}},
		"9":  {{ID: "10", Name: "第一季", IsDir: true}, {ID: "20", Name: "第二季", IsDir: true}},
		"10": {{ID: "101", Name: "第1集 #1.mkv", Size: 10, SHA1: strings.Repeat("a", 40)}},
		"20": {{ID: "102", Name: "第2集.mkv", Size: 20, SHA1: strings.Repeat("b", 40)}},
	}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{{ID: "115", Name: "115测试", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}}
		st.Settings.STRMURL = "http://example.test"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.handler = a.Handler(t.TempDir())
	f.cookie = setupTest(t, a, f.handler)
	shareMock(t, func(r *http.Request) string {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/share/snap") {
			t.Fatal("unexpected cloud write or endpoint", r.Method, r.URL.Path)
		}
		parent := r.URL.Query().Get("cid")
		f.reads = append(f.reads, parent)
		files := []map[string]any{}
		for _, item := range f.tree[parent] {
			entry := map[string]any{"n": item.Name, "s": fmt.Sprint(item.Size), "sha": item.SHA1}
			if item.IsDir {
				entry["cid"] = item.ID
			} else {
				entry["fid"] = item.ID
			}
			files = append(files, entry)
		}
		raw, _ := json.Marshal(map[string]any{"state": true, "data": map[string]any{"count": len(files), "list": files}})
		return string(raw)
	})
	return f
}

func (f *virtual115NameFixture) generate(t *testing.T, directory string) *httptest.ResponseRecorder {
	t.Helper()
	link := "https://115cdn.com/s/sample"
	if f.pass != "" {
		link += "?password=" + f.pass
	}
	w := request(t, f.handler, "POST", "/api/virtual-library/115/preview", map[string]any{"storageId": "115", "url": link}, f.cookie)
	if w.Code != 200 {
		t.Fatal("preview", w.Code, w.Body.String())
	}
	var preview struct {
		ID string `json:"preview"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	return request(t, f.handler, "POST", "/api/virtual-library/115/generate", map[string]any{"storageId": "115", "preview": preview.ID, "directory": directory}, f.cookie)
}

func (f *virtual115NameFixture) address(name string) string {
	share := "sample"
	if f.pass != "" {
		share += "-" + f.pass
	}
	return "/115-share/" + share + "/" + url.PathEscape(name)
}

func TestVirtual115FilenameURL(t *testing.T) {
	for _, pass := range []string{"", "aB12"} {
		address, err := virtualShareURL("http://example.test/", "sample", pass, shareEntry{ID: "123", Name: "第1集 #1.mkv"})
		share := "sample"
		if pass != "" {
			share += "-" + pass
		}
		want := "http://example.test/115-share/" + share + "/%E7%AC%AC1%E9%9B%86%20%231.mkv"
		if err != nil || address != want {
			t.Fatal(address, want, err)
		}
	}
}

func TestVirtual115FilenameNestedLookupAndPersistence(t *testing.T) {
	for _, pass := range []string{"", "aB12"} {
		t.Run(pass, func(t *testing.T) {
			f := newVirtual115NameFixture(t, pass)
			directory := t.TempDir()
			if w := f.generate(t, directory); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, item := range []struct{ directory, name, parent string }{{"第一季", "第1集 #1.mkv", "10"}, {"第二季", "第2集.mkv", "20"}} {
				body, err := os.ReadFile(filepath.Join(directory, "Show", item.directory, item.name+".strm"))
				if err != nil || string(body) != "http://example.test"+f.address(item.name)+"\n" {
					t.Fatal("directory should remain only in the generated tree", string(body), err)
				}
				f.reads = nil
				w := request(t, f.handler, "HEAD", f.address(item.name), nil, nil)
				if w.Code != 200 || len(f.reads) != 1 || f.reads[0] != item.parent {
					t.Fatal("filename index did not retain the exact source parent", w.Code, w.Body.String(), f.reads)
				}
			}
			entries, err := os.ReadDir(filepath.Join(f.a.dataDir, "virtual-library-names"))
			if err != nil || len(entries) != 1 {
				t.Fatal("durable filename index missing", entries, err)
			}
			raw, err := os.ReadFile(filepath.Join(f.a.dataDir, "virtual-library-names", entries[0].Name()))
			if err != nil || strings.Contains(string(raw), "第1集") || strings.Contains(string(raw), "sample") || pass != "" && strings.Contains(string(raw), pass) {
				t.Fatal("filename index is not encrypted", err)
			}
			for _, address := range []string{"/115-share/unknown/file.mkv", "/115-share/sample-wrong/file.mkv", f.address("missing.mkv"), "/115-share/sample/Show/file.mkv"} {
				f.reads = nil
				w := request(t, f.handler, "GET", address, nil, nil)
				if w.Code != 404 || len(f.reads) != 0 {
					t.Fatal("unregistered lookup reached the cloud", address, w.Code, f.reads)
				}
			}
			if _, err := os.Stat(filepath.Join(f.a.dataDir, "ed2k-playback")); !os.IsNotExist(err) {
				t.Fatal("HEAD created a temporary transfer", err)
			}
		})
	}
}

func TestVirtual115FilenameConflictHasNoCloudSideEffects(t *testing.T) {
	f := newVirtual115NameFixture(t, "aB12")
	f.tree["20"][0].Name = f.tree["10"][0].Name
	if w := f.generate(t, t.TempDir()); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.reads = nil
	for _, method := range []string{"HEAD", "GET"} {
		w := request(t, f.handler, method, f.address("第1集 #1.mkv"), nil, nil)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "同名") || len(f.reads) != 0 {
			t.Fatal("ambiguous URL resolved arbitrarily", method, w.Code, w.Body.String(), f.reads)
		}
	}
}

func TestVirtual115FilenameLegacyMigrationPreservesOtherContent(t *testing.T) {
	for _, mode := range []string{"registered", "unregistered", "modified", "current", "unrelated"} {
		t.Run(mode, func(t *testing.T) {
			f := newVirtual115NameFixture(t, "aB12")
			directory := t.TempDir()
			item := f.tree["10"][0]
			key, err := virtual115ResourceKey("sample", f.pass, item)
			if err != nil {
				t.Fatal(err)
			}
			legacy := legacyVirtualShareURL("http://example.test", key, item.Name) + "\n"
			body := legacy
			switch mode {
			case "modified":
				body += "# local edit\n"
			case "current":
				body = "http://example.test" + f.address(item.Name) + "\n"
			case "unrelated":
				body = "http://other.test/keep.mkv\n"
			}
			if mode != "unregistered" {
				if err := f.a.virtualGrant(key, true, "10"); err != nil {
					t.Fatal(err)
				}
			}
			filename := filepath.Join(directory, "Show", "第一季", item.Name+".strm")
			if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
				t.Fatal(err)
			}
			writeTest(t, filename, body)
			w := f.generate(t, directory)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if mode == "registered" {
				body = "http://example.test" + f.address(item.Name) + "\n"
			}
			raw, err := os.ReadFile(filename)
			if err != nil || string(raw) != body {
				t.Fatal("unexpected existing STRM rewrite", string(raw), err)
			}
			var result struct{ Generated, Skipped int }
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			wantGenerated := 1
			if mode == "registered" {
				wantGenerated = 2
			}
			if result.Generated != wantGenerated || result.Generated+result.Skipped != 2 {
				t.Fatal("migration count mismatch", result)
			}
		})
	}
}

func TestVirtual115FilenameSnapshotReplacementAndColdRead(t *testing.T) {
	f := newVirtual115NameFixture(t, "aB12")
	directory := t.TempDir()
	oldName := f.tree["10"][0].Name
	f.tree["20"][0].Name = oldName
	if w := f.generate(t, directory); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.tree["10"][0].Name = "new.mkv"
	f.tree["20"] = nil
	if w := f.generate(t, directory); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	f.reads = nil
	if w := request(t, f.handler, "HEAD", f.address(oldName), nil, nil); w.Code != 404 || len(f.reads) != 0 {
		t.Fatal("old snapshot alias survived", w.Code, f.reads)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reopened, err := newWithDirectories(ctx, f.a.store.dir, f.a.dataDir, t.TempDir())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); reopened.wg.Wait() })
	f.reads = nil
	w := request(t, reopened.Handler(t.TempDir()), "HEAD", f.address("new.mkv"), nil, nil)
	if w.Code != 200 || len(f.reads) != 1 || f.reads[0] != "10" {
		t.Fatal("filename authorization did not survive restart", w.Code, w.Body.String(), f.reads)
	}
}

func TestVirtual115FilenameIndexFailureHasNoFallback(t *testing.T) {
	for _, mode := range []string{"tampered", "missing-grant", "invalid-path", "method"} {
		t.Run(mode, func(t *testing.T) {
			f := newVirtual115NameFixture(t, "aB12")
			if w := f.generate(t, t.TempDir()); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			method, address, want := "GET", f.address(f.tree["10"][0].Name), 403
			switch mode {
			case "tampered":
				filename := f.a.virtual115NamesPath("sample", f.pass)
				raw, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				raw[len(raw)-1] ^= 1
				if err := os.WriteFile(filename, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-grant":
				key, err := virtual115ResourceKey("sample", f.pass, f.tree["10"][0])
				if err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256([]byte(key))
				if err := os.Remove(filepath.Join(f.a.dataDir, "virtual-library", hex.EncodeToString(sum[:])+".enc")); err != nil {
					t.Fatal(err)
				}
				want = 404
			case "invalid-path":
				address = "/115-share/sample-aB12/Show%2F" + url.PathEscape(f.tree["10"][0].Name)
				want = 404
			case "method":
				method, want = "POST", 405
			}
			f.reads = nil
			w := request(t, f.handler, method, address, nil, nil)
			if w.Code != want || len(f.reads) != 0 {
				t.Fatal("invalid lookup reached cloud or fallback", w.Code, w.Body.String(), f.reads)
			}
		})
	}
}

func TestVirtual115FilenameRejectsChangedSource(t *testing.T) {
	for _, field := range []string{"name", "size", "sha1", "id", "parent"} {
		t.Run(field, func(t *testing.T) {
			f := newVirtual115NameFixture(t, "aB12")
			name := f.tree["10"][0].Name
			if w := f.generate(t, t.TempDir()); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			switch field {
			case "name":
				f.tree["10"][0].Name = "changed.mkv"
			case "size":
				f.tree["10"][0].Size++
			case "sha1":
				f.tree["10"][0].SHA1 = strings.Repeat("c", 40)
			case "id":
				f.tree["10"][0].ID = "999"
			case "parent":
				f.tree["30"], f.tree["10"] = f.tree["10"], nil
			}
			for _, method := range []string{"GET", "HEAD"} {
				w := request(t, f.handler, method, f.address(name), nil, nil)
				if w.Code != 404 {
					t.Fatal("changed source accepted", field, method, w.Code, w.Body.String())
				}
			}
			if _, err := os.Stat(filepath.Join(f.a.dataDir, "ed2k-playback")); !os.IsNotExist(err) {
				t.Fatal("changed source triggered transfer", err)
			}
		})
	}
}

func TestVirtual115FilenamePartialGenerationKeepsPublishedSnapshot(t *testing.T) {
	f := newVirtual115NameFixture(t, "aB12")
	directory := t.TempDir()
	if w := f.generate(t, directory); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	filename := f.a.virtual115NamesPath("sample", f.pass)
	before, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	f.tree["10"][0].Name = "new.mkv"
	f.tree["20"] = []shareEntry{{ID: "30", Name: "Blocked", IsDir: true}}
	f.tree["30"] = []shareEntry{{ID: "103", Name: "blocked.mkv", Size: 30, SHA1: strings.Repeat("c", 40)}}
	writeTest(t, filepath.Join(directory, "Show", "第二季", "Blocked"), "preserve")
	if w := f.generate(t, directory); w.Code != 400 {
		t.Fatal("generation should fail before snapshot publication", w.Code, w.Body.String())
	}
	after, err := os.ReadFile(filename)
	if err != nil || string(before) != string(after) {
		t.Fatal("partial generation replaced published index", err)
	}
	f.reads = nil
	w := request(t, f.handler, "GET", f.address("new.mkv"), nil, nil)
	if w.Code != 404 || len(f.reads) != 0 {
		t.Fatal("unpublished filename reached cloud", w.Code, f.reads)
	}
}

func TestVirtual115FilenameGETUsesExistingPlaybackPipeline(t *testing.T) {
	f := newVirtual115NameFixture(t, "aB12")
	if w := f.generate(t, t.TempDir()); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	s, err := f.a.store.storage("115")
	if err != nil {
		t.Fatal(err)
	}
	item := f.tree["10"][0]
	resource, err := virtual115ResourceKey("sample", f.pass, item)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(ed2kRevision(s) + "\x00" + resource))
	key := hex.EncodeToString(sum[:])
	if err := f.a.writeED2KRecord(key, ed2kRecord{Folder: "99", FileID: "1001", Attempted: true}); err != nil {
		t.Fatal(err)
	}
	shareTransport := apiClient.Transport
	lists, downloads := 0, 0
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/files":
			lists++
			if r.Method != "GET" || r.URL.Query().Get("cid") != "99" {
				t.Fatal("incorrect temporary lookup", r.Method, r.URL.Path)
			}
			body = fmt.Sprintf(`{"state":true,"cid":"99","offset":0,"count":1,"data":[{"fid":"1001","cid":"99","n":%q,"s":"10","sha":%q,"pc":"validpickcode"}]}`, item.Name, item.SHA1)
		case "/app/chrome/downurl":
			downloads++
			if r.UserAgent() != "Filename-Player/1" {
				t.Fatal("player UA was not retained", r.UserAgent())
			}
			body = `{"state":false,"errno":10008,"error":"mock-ticket-rejected"}`
		default:
			return shareTransport.RoundTrip(r)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	r := httptest.NewRequest("GET", f.address(item.Name), nil)
	r.Header.Set("User-Agent", "Filename-Player/1")
	w := httptest.NewRecorder()
	f.reads = nil
	f.handler.ServeHTTP(w, r)
	if w.Code != 502 || lists != 1 || downloads != 1 || len(f.reads) != 1 || f.reads[0] != "10" ||
		!strings.Contains(w.Body.String(), "获取下载链接失败") {
		t.Fatal("short URL did not use the verified temporary and ticket pipeline", w.Code, w.Body.String(), lists, downloads, f.reads)
	}
}
