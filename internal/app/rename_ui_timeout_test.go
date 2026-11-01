package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenameNumberZeroAndJSONRoundTrip(t *testing.T) {
	var rule renameRule
	if err := json.Unmarshal([]byte(`{"kind":"number","start":0,"digits":2,"separator":""}`), &rule); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(rule)
	var restored renameRule
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	compiled, err := compileRenameRules([]renameRule{restored})
	if err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"Movie00.mkv", "Movie01.mkv"} {
		got, err := compiled.apply("Movie.mkv", index, false)
		if err != nil || got != want {
			t.Fatalf("index %d: %q, %v; want %q", index, got, err, want)
		}
	}
	if err := json.Unmarshal([]byte(`{"kind":"number","digits":1}`), &rule); err != nil {
		t.Fatal(err)
	}
	compiled, _ = compileRenameRules([]renameRule{rule})
	if got, _ := compiled.apply("Movie.mkv", 0, false); got != "Movie1.mkv" {
		t.Fatal("legacy missing start must remain 1", got)
	}
	if err := json.Unmarshal([]byte(`{"kind":"number","unknown":true}`), &rule); err == nil {
		t.Fatal("unknown rule fields must remain rejected")
	}
	for _, input := range []string{`{"kind":"number","start":-1}`, `{"kind":"number","start":1.5}`} {
		if err := json.Unmarshal([]byte(input), &rule); err != nil {
			continue
		}
		if _, err := compileRenameRules([]renameRule{rule}); err == nil {
			t.Fatal("invalid number accepted", input)
		}
	}
}

func TestRenameZeroNumberRuleSavedAndReopened(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	rules := decodeRenameRules(t, `[{"kind":"number","start":0,"digits":2,"separator":""}]`)
	res := request(t, h, "POST", "/api/files/rename-rules", renameRuleSet{Name: "从零编号", Rules: rules}, cookie)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(a.store.dir, "organize", "rename-rules.json"))
	var saved []renameRuleSet
	if err != nil || json.Unmarshal(data, &saved) != nil || len(saved) != 1 || saved[0].Rules[0].Start != 0 {
		t.Fatal("zero was lost on disk", string(data), err)
	}
	if err := os.WriteFile(filepath.Join(s.Config["root"], "Movie.mkv"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	input := renameRequest{StorageID: s.ID, Source: "/", IDs: []string{"/Movie.mkv"}, Rules: saved[0].Rules}
	res = request(t, h, "POST", "/api/files/rename-preview", input, cookie)
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &input.Expected) != nil || len(input.Expected) != 1 || input.Expected[0].NewName != "Movie00.mkv" {
		t.Fatal(res.Code, res.Body.String())
	}
	res = request(t, h, "POST", "/api/files/rename", input, cookie)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(s.Config["root"], "Movie00.mkv")); err != nil || string(data) != "original" {
		t.Fatal("zero numbering did not execute", string(data), err)
	}
}

func TestQuarkLibraryNameReadDoesNotScanAllRootPages(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	old := apiClient.Transport
	pages := 0
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/detail") {
			pages++
			if pages > 1 {
				return nil, errors.New("must not scan another root page for a title")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"list":[{"fid":"one","file_name":"one.mkv"},{"fid":"two","file_name":"two.mkv"}]},"metadata":{"_total":10000}}`)), Request: r}, nil
		}
		return old.RoundTrip(r)
	})
	s, _ := a.store.storage("q")
	title, _, batches, err := a.quarkLibraryRead(context.Background(), &s, "https://pan.quark.cn/s/sample", false)
	if err != nil || title != "Share title" || pages != 1 || len(batches) != 0 {
		t.Fatalf("title=%q pages=%d batches=%d err=%v", title, pages, len(batches), err)
	}
}

func TestQuarkLibraryDeadlineNamesStageWithoutSecrets(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	a.gateMu.Lock()
	a.gates["q"] = time.Now().Add(time.Hour)
	a.gateMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	s, _ := a.store.storage("q")
	_, _, _, err := a.quarkLibraryRead(ctx, &s, "https://pan.quark.cn/s/private?pwd=secret", false)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "等待夸克接口") || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("missing safe stage: %v", err)
	}
	for _, secret := range []string{"context deadline exceeded", "private", "secret", "https://"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("unsafe or unlocalized timeout: %v", err)
		}
	}
}

func TestQuarkLibraryTitleLookupRetainsScanAndEmptyPageGuards(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	old := apiClient.Transport
	empty := false
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/detail") {
			body := `{"code":0,"data":{"list":[{"fid":"one","file_name":"one.mkv"}]},"metadata":{"_total":10000}}`
			if empty {
				body = `{"code":0,"data":{"list":[]},"metadata":{"_total":10000}}`
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		return old.RoundTrip(r)
	})
	s, _ := a.store.storage("q")
	title, _, _, err := a.quarkLibraryRead(context.Background(), &s, "https://pan.quark.cn/s/sample", false)
	if err != nil || title != "Share title" {
		t.Fatal("single first-page item was mistaken for the whole share", title, err)
	}
	if _, _, _, err := a.quarkLibraryRead(context.Background(), &s, "https://pan.quark.cn/s/sample", true); err == nil || !strings.Contains(err.Error(), "最多") {
		t.Fatal("full generation lost its root scan bound", err)
	}
	empty = true
	if _, _, _, err := a.quarkLibraryRead(context.Background(), &s, "https://pan.quark.cn/s/sample", false); err == nil || !strings.Contains(err.Error(), "不完整") {
		t.Fatal("incomplete first page accepted", err)
	}
}
