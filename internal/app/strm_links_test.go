package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestReadableSTRMLinksAndDownloadRouting(t *testing.T) {
	a := testApp(t)
	pan := Storage{ID: "p115", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	quark := Storage{ID: "quark", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "test"}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{pan, quark}
		st.Settings.PublicURL = "http://10.0.0.31:15151"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	file := File{ID: "123", PickCode: "cq391yoe47hu5dd6u", Name: "爱的迫降.S01E01.ts"}
	link, err := a.publicSTRMURL(pan, file)
	if err != nil || link != "http://10.0.0.31:15151/d/cq391yoe47hu5dd6u.ts?/爱的迫降.S01E01.ts" {
		t.Fatal(link, err)
	}
	raw, err := os.ReadFile(a.referencePath(file.PickCode + ".ts"))
	if err != nil || strings.Contains(string(raw), file.Name) {
		t.Fatal("index not encrypted", err)
	}
	ref, err := a.readSTRMReference(file.PickCode + ".ts")
	if err != nil || ref.File != file.ID || ref.Storage != pan.ID {
		t.Fatal(ref, err)
	}
	other := pan
	other.ID = "another"
	if _, err := a.publicSTRMURL(other, file); err == nil {
		t.Fatal("ambiguous account mapping accepted")
	}
	old := apiClient
	defer func() { apiClient = old }()
	calls := 0
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.UserAgent() != "Browser-Test/1" {
			t.Error("lost browser UA")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"state":false,"errno":10008,"error":"rejected"}`)), Request: r}, nil
	})}
	h := a.Handler(t.TempDir())
	r := httptest.NewRequest("GET", link, nil)
	r.Header.Set("User-Agent", "Browser-Test/1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 502 || calls != 1 {
		t.Fatal("readable path did not reach 115 downloader", w.Code, w.Body.String(), calls)
	}
	if w := request(t, h, "GET", "/d/notregistered123.ts?/movie.ts", nil, nil); w.Code != 404 || calls != 1 {
		t.Fatal("unregistered pickcode exposed")
	}
	if w := request(t, h, "POST", "/d/"+file.PickCode+".ts", nil, nil); w.Code != 405 {
		t.Fatal(w.Code)
	}
	qLink, err := a.publicSTRMURL(quark, File{ID: "cloud/file", Name: "电影 & #1.mkv"})
	if err != nil || !strings.Contains(qLink, "/api/strm/play/quark/") || !strings.Contains(qLink, "/n/") {
		t.Fatal(qLink, err)
	}
	tampered := strings.Replace(qLink, "/quark/", "/p115/", 1)
	if w := request(t, h, "GET", tampered, nil, nil); w.Code != 403 {
		t.Fatal("quark signature bypass", w.Code)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	if got, err := a.readSTRMReference(file.PickCode + ".ts"); err != nil || got != ref {
		t.Fatal("reference did not survive reopen", err)
	}
	if err := a.store.update(func(st *State) error { st.Storages[0].Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if w := request(t, h, "GET", link, nil, nil); w.Code != 404 {
		t.Fatal("disabled pool still served", w.Code)
	}
}
