package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func generationCASFixture() string {
	return `{"name":"Arrival.mkv","size":12,"sha256":"` + strings.Repeat("a", 64) + `","md5":""}`
}
func TestGenerationTextOrderAndInvalid(t *testing.T) {
	raw := generationCASFixture()
	items := parseGenerationText("cas", raw+"\n"+strings.Replace(raw, "Arrival", "Second", 1))
	if len(items) != 2 || items[0].Path != "Arrival.cas.strm" || items[1].Path != "Second.cas.strm" || items[0].CAS.MD5 != "" {
		t.Fatal(items)
	}
	items = parseGenerationText("cas", base64.StdEncoding.EncodeToString([]byte(raw))+"\nbroken")
	if len(items) != 2 || items[0].Error != "" || items[1].Error == "" {
		t.Fatal(items)
	}
	for _, bad := range []string{"ed2k://|file|../escape.mkv|12|" + strings.Repeat("a", 32) + "|/", "ed2k://|file|movie.mkv|0|" + strings.Repeat("a", 32) + "|/", "ed2k://|file|movie.mkv|12|invalid|/"} {
		if _, e := parseED2KText(bad); e == nil {
			t.Fatal(bad)
		}
	}
	i, e := parseED2KText("ed2k://|file|%E7%94%B5%E5%BD%B1.mkv|12|" + strings.Repeat("a", 32) + "|/")
	if e != nil || i.Name != "电影.mkv" {
		t.Fatal(i, e)
	}
}
func generationZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, raw := range files {
		f, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		f.Write([]byte(raw))
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestGenerationArchivePathsAndBounds(t *testing.T) {
	items, e := parseGenerationArchive(context.Background(), "cas", "A.zip", generationZIP(t, map[string]string{"A/A/Season 1/Arrival.cas": generationCASFixture()}))
	if e != nil || len(items) != 1 || items[0].Path != "A/Season 1/Arrival.cas.strm" {
		t.Fatal(items, e)
	}
	for _, name := range []string{"../bad.cas", "/bad.cas", "A/../../bad.cas", "A\\bad.cas"} {
		if _, e := parseGenerationArchive(context.Background(), "cas", "a.zip", generationZIP(t, map[string]string{name: generationCASFixture()})); e == nil {
			t.Fatal(name)
		}
	}
	if _, e := parseGenerationArchive(context.Background(), "cas", "a.zip", generationZIP(t, map[string]string{"large.cas": strings.Repeat("a", casLimit+1)})); e == nil {
		t.Fatal("unbounded entry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := parseGenerationArchive(ctx, "cas", "a.zip", generationZIP(t, map[string]string{"a.cas": generationCASFixture()})); e == nil {
		t.Fatal("ignored cancel")
	}
}
func TestGenerationHTTPQueueAndPortableClaim(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	fixture := strings.Replace(generationCASFixture(), `{"name":`, `{"provider":"mobile","name":`, 1)
	w := request(t, h, "POST", "/api/generation/preview", map[string]string{"kind": "cas", "text": fixture}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p struct {
		Preview string `json:"preview"`
		Valid   bool   `json:"valid"`
	}
	json.Unmarshal(w.Body.Bytes(), &p)
	if !p.Valid {
		t.Fatal(w.Body.String())
	}
	if _, e := a.readGenerationManifest(p.Preview); e != nil {
		t.Fatal(e)
	}
	target := t.TempDir()
	w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": p.Preview, "target": target}, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var j GenerationJob
	json.Unmarshal(w.Body.Bytes(), &j)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items := a.store.snapshot().GenerationJobs
		if len(items) > 0 && items[0].Status == "success" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, e := os.ReadFile(filepath.Join(target, "Arrival.cas.strm"))
	if e != nil {
		t.Fatal(e)
	}
	generatedCASClaim(t, raw)
	u, e := url.Parse(strings.TrimSpace(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/stream/"))
	var c streamClaim
	json.Unmarshal(b, &c)
	if c.CAS == nil || c.Storage != "" || c.CAS.Provider != "" || c.CAS.SHA256 == "" || c.CAS.MD5 != "" {
		t.Fatal(c)
	}
	repeated := request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": p.Preview, "target": target}, cookie)
	if repeated.Code != 200 || len(a.store.snapshot().GenerationJobs) != 1 {
		t.Fatal("duplicate queue", repeated.Code)
	}
	if w = request(t, h, "GET", "/api/generation/jobs", nil, nil); w.Code != 401 {
		t.Fatal("unprotected jobs", w.Code)
	}
}
func TestGenerationManifestExpiredOrForeign(t *testing.T) {
	a := testApp(t)
	key := id()
	m := generationManifest{User: "other", Created: time.Now()}
	if e := a.writeGenerationManifest(key, m); e != nil {
		t.Fatal(e)
	}
	if _, e := a.readGenerationManifest(key); e == nil {
		t.Fatal("foreign accepted")
	}
	m.User = a.store.snapshot().Username
	m.Created = time.Now().Add(-time.Hour)
	a.writeGenerationManifest(key, m)
	if _, e := a.readGenerationManifest(key); e == nil {
		t.Fatal("expired accepted")
	}
	if _, e := a.generationManifestPath("../../escape"); e == nil {
		t.Fatal("traversal accepted")
	}
}
func TestED2KPoolRotateReuseAndPersistence(t *testing.T) {
	a, _, s, _, old := ed2kFixture(t)
	second := s
	second.ID = "second"
	a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, second)
		st.ED2KPool = ED2KPoolConfig{StorageIDs: []string{second.ID}, RotationMinutes: 1440, RetentionHours: 24, Directory: "/最近接收/Aether"}
		return nil
	})
	p := requiredED2KPool(a.store.snapshot().ED2KPool, a.store.snapshot())
	if len(p.StorageIDs) != 2 || p.StorageIDs[0] != s.ID {
		t.Fatal(p)
	}
	claim := streamClaim{ED2K: old.ED2K, RetentionHours: 24, Redirect: true}
	var submits atomic.Int32
	ops := ed2kTestOps(claim, &submits)
	if _, e := a.ed2kDownloadWith(context.Background(), s, claim, ops); e != nil {
		t.Fatal(e)
	}
	if _, e := a.ed2kDownloadWith(context.Background(), s, claim, ops); e != nil {
		t.Fatal(e)
	}
	if submits.Load() != 1 || len(a.store.snapshot().ED2KTemporary) != 1 {
		t.Fatal(submits.Load())
	}
	chosen, _, e := a.selectED2KStorage(*claim.ED2K, time.Now())
	if e != nil || chosen.ID != s.ID {
		t.Fatal(chosen, e)
	}
	reopened, e := NewStore(a.store.dir)
	if e != nil {
		t.Fatal(e)
	}
	if reopened.snapshot().ED2KPool.Directory != "/最近接收/Aether" {
		t.Fatal("pool not persisted")
	}
	a.store.update(func(st *State) error {
		for i := range st.Storages {
			if st.Storages[i].ID == s.ID {
				st.Storages[i].Config = map[string]string{"cookie": "changed"}
			}
		}
		return nil
	})
	if e := a.checkED2KClaim(s, claim); e == nil {
		t.Fatal("stale credentials accepted")
	}
}
func TestSTRMConfigScopeAndPickOnly(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "PUT", "/api/strm/settings", strmSettings{Address: "http://10.0.0.1:1234", Format: "pickcode"}, cookie)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	s := Storage{ID: "115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	link, e := a.publicSTRMURL(s, File{ID: "123", PickCode: "abcdef123456", Name: "movie.mkv"})
	if e != nil || link != "http://10.0.0.1:1234/d/123/abcdef123456" {
		t.Fatal(link, e)
	}
	if _, e := a.readSTRMReference("123/abcdef123456"); e != nil {
		t.Fatal(e)
	}
	for _, address := range []string{"http://x/path", "http://x?foo=1", "file:///tmp", "http://u:p@x"} {
		if validSTRMSettings(strmSettings{address, "pickcode"}) == nil {
			t.Fatal(address)
		}
	}
}

func TestGenerationTextOverflowNotSilentlyTruncated(t *testing.T) {
	line := "ed2k://|file|film.mkv|12|" + strings.Repeat("a", 32) + "|/"
	items := parseGenerationText("ed2k", strings.Repeat(line+"\n", generationEntries+1))
	if len(items) != generationEntries+1 || items[generationEntries].Error == "" {
		t.Fatal("overflow must prevent generation", len(items))
	}
}

func TestGenerationPreviewPrunesAndBoundsDisk(t *testing.T) {
	a := testApp(t)
	m := generationManifest{User: a.store.snapshot().Username, Kind: "cas", Entries: parseGenerationText("cas", generationCASFixture()), Created: time.Now()}
	key := id()
	if err := a.writeGenerationManifest(key, m); err != nil {
		t.Fatal(err)
	}
	name, _ := a.generationManifestPath(key)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(name, old, old); err != nil {
		t.Fatal(err)
	}
	if err := a.writeGenerationManifest(id(), m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatal("expired preview not pruned", err)
	}
	for i := 1; i < 64; i++ {
		if err := a.writeGenerationManifest(id(), m); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := a.writeGenerationManifest(id(), m); err == nil {
		t.Fatal("unbounded preview count")
	}
}

func TestGenerationInvalidPreviewNeverPersists(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "POST", "/api/generation/preview", map[string]string{"kind": "cas", "text": "invalid"}, cookie)
	var preview struct {
		Preview string
		Valid   bool
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.Valid || preview.Preview != "" {
		t.Fatal(w.Code, w.Body.String())
	}
	files, err := os.ReadDir(filepath.Join(a.dataDir, "generation-preview"))
	if err != nil && !os.IsNotExist(err) || len(files) != 0 {
		t.Fatal(files, err)
	}
	if w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": "", "target": t.TempDir()}, cookie); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestGenerationMultipartArchiveAndNoOverwrite(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("kind", "cas")
	f, _ := form.CreateFormFile("files", "A.zip")
	_, _ = f.Write(generationZIP(t, map[string]string{"A/A/Season 1/input.cas": generationCASFixture()}))
	f, _ = form.CreateFormFile("files", "flat.txt")
	_, _ = f.Write([]byte(strings.Replace(generationCASFixture(), "Arrival", "Second", 1)))
	_ = form.Close()
	r := httptest.NewRequest("POST", "/api/generation/preview", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var p struct {
		Preview string
		Valid   bool
		Items   []GenerationEntry
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || !p.Valid || len(p.Items) != 2 || p.Items[0].Path != "A/Season 1/Arrival.cas.strm" || p.Items[1].Path != "Second.cas.strm" {
		t.Fatal(w.Code, w.Body.String())
	}
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "A", "Season 1"), 0755); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(target, "A", "Season 1", "Arrival.cas.strm")
	if err := os.WriteFile(name, []byte("existing-user-content"), 0600); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "POST", "/api/generation/jobs", map[string]string{"preview": p.Preview, "target": target}, cookie)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if jobs := a.store.snapshot().GenerationJobs; len(jobs) > 0 && jobs[0].Status == "error" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	jobs := a.store.snapshot().GenerationJobs
	if len(jobs) != 1 || jobs[0].Status != "error" || jobs[0].Success != 0 || jobs[0].Failed != 2 {
		t.Fatal(jobs)
	}
	raw, err := os.ReadFile(name)
	if err != nil || string(raw) != "existing-user-content" {
		t.Fatal(string(raw), err)
	}
	if _, err := os.Stat(filepath.Join(target, "Second.cas.strm")); !os.IsNotExist(err) {
		t.Fatal("partial collision continued", err)
	}
}

func TestED2KPoolUnavailableExtrasAndValidatedImport(t *testing.T) {
	a, _, s, _, old := ed2kFixture(t)
	if err := a.store.update(func(st *State) error {
		st.ED2KPool = ED2KPoolConfig{StorageIDs: []string{s.ID, "removed"}, Directory: "/最近接收/Aether", RetentionHours: 24, RotationMinutes: 1440}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	chosen, _, err := a.selectED2KStorage(*old.ED2K, time.Now())
	if err != nil || chosen.ID != s.ID {
		t.Fatal(chosen, err)
	}
	st := a.store.snapshot()
	st.ED2KPool.Directory = "/../Aether"
	if validateResourceState(&st) == nil {
		t.Fatal("unsafe imported directory")
	}
	st = a.store.snapshot()
	st.Settings.STRMURL = "http://user:password@example.test"
	if validateResourceState(&st) == nil {
		t.Fatal("unsafe imported origin")
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "PUT", "/api/ed2k/pool", ED2KPoolConfig{StorageIDs: []string{}, Directory: "/custom", RotationMinutes: 1440, RetentionHours: 24}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p := a.store.snapshot().ED2KPool
	if p.Directory != "/custom/Aether" || len(p.StorageIDs) != 1 || p.StorageIDs[0] != s.ID {
		t.Fatal(p)
	}
}

func TestGenerationRuntimeSeparatedFromModuleConfig(t *testing.T) {
	a := testApp(t)
	if err := a.store.update(func(st *State) error {
		st.GenerationJobs = []GenerationJob{{ID: "runtime-job", Status: "queued"}}
		st.ED2KStats.Success = 7
		st.ED2KTemporary = []ED2KTemporary{{Key: "temporary-secret", Revision: "private-revision"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, module := range []string{"state", "task/ed2k-pool", "task/ed2k"} {
		name, err := a.store.jsonPath(module)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("runtime-job")) || bytes.Contains(b, []byte("temporary-secret")) || bytes.Contains(b, []byte("private-revision")) {
			t.Fatal("runtime leaked into config", module)
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	if jobs := reopened.snapshot().GenerationJobs; len(jobs) != 1 || jobs[0].Status != "interrupted" {
		t.Fatal(jobs)
	}
	if reopened.snapshot().ED2KStats.Success != 7 {
		t.Fatal("runtime lost")
	}
}
