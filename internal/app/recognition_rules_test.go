package app

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRecognitionSettingsAPIAndBackup(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	for _, endpoint := range []string{"/api/recognition-rules", "/api/recognition-rules/preview"} {
		if w := request(t, h, "POST", endpoint, map[string]any{}, nil); w.Code != 401 {
			t.Fatal("unprotected settings", endpoint, w.Code)
		}
	}
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/recognition-rules", nil, cookie)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), `"matchRules":[]`) || a.store.snapshot().RecognitionRules != nil {
		t.Fatal("default read changed configuration", w.Code, w.Body.String())
	}
	if w := request(t, h, "DELETE", "/api/recognition-rules", nil, cookie); w.Code != 405 {
		t.Fatal(w.Code)
	}
	input := recognitionSettings{MatchRules: []recognitionReplacement{{From: "广告", To: ""}, {From: "_", To: "."}}, NamingRules: []recognitionReplacement{{From: "Film", To: "电影"}}, Blacklist: []string{"预告", ".MKV", ".mkv", "/花絮", "预告"}, ReleaseGroups: []string{"TnT", "WiKi"}, MinSizeMB: 10}
	w = request(t, h, "PUT", "/api/recognition-rules", input, cookie)
	var saved recognitionSettings
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || len(saved.Blacklist) != 4 || saved.Blacklist[1] != ".MKV" {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || !reflect.DeepEqual(reopened.snapshot().RecognitionRules, &saved) {
		t.Fatal("restart lost settings", err)
	}
	w = request(t, h, "POST", "/api/config/backup", map[string]string{"password": "recognition-backup"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	plain, err := openBackup(w.Body.Bytes(), "recognition-backup")
	var bundle configBundle
	if err != nil || json.Unmarshal(plain, &bundle) != nil || !reflect.DeepEqual(bundle.State.RecognitionRules, &saved) || validateBundle(bundle) != nil {
		t.Fatal("backup lost recognition rules", err)
	}
	invalid := bundle
	bad := saved
	bad.MatchRules = []recognitionReplacement{{From: "(?=bad)", Regex: true}}
	invalid.State.RecognitionRules = &bad
	if validateBundle(invalid) == nil {
		t.Fatal("import accepted invalid regex")
	}
	if w := request(t, h, "PUT", "/api/recognition-rules", recognitionSettings{}, cookie); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "recognition.aether")
	part.Write(w.Body.Bytes())
	writer.WriteField("password", "recognition-backup")
	writer.Close()
	req := httptest.NewRequest("POST", "/api/config/import", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	if err := applyPendingConfig(a.store); err != nil || !reflect.DeepEqual(a.store.snapshot().RecognitionRules, &saved) {
		t.Fatal("import lost settings", err)
	}
}

func TestRecognitionPreviewSequentialWithoutFileWrites(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	file := filepath.Join(s.Config["root"], "广告Film_2021.mkv")
	writeTest(t, file, "original")
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	before := a.store.snapshot().RecognitionRules
	body := map[string]any{"name": "广告Film_2021.mkv", "rules": []recognitionReplacement{{From: "广告", To: ""}, {From: "_", To: "."}, {From: `Film\.(\d+)`, To: `电影.${1}`, Regex: true}}}
	w := request(t, h, "POST", "/api/recognition-rules/preview", body, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `电影.2021.mkv`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "original" || !reflect.DeepEqual(a.store.snapshot().RecognitionRules, before) || len(a.running) != 0 {
		t.Fatal("preview changed source or settings", err)
	}
	c, err := compileRecognitionSettings(recognitionSettings{MatchRules: []recognitionReplacement{{From: "Film", To: "$1"}, {}, {From: "$1", To: "literal"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.recognitionName("Film.mkv"); err != nil || got != "literal.mkv" {
		t.Fatal(got, err)
	}
	if got, _ := c.recognitionName("film.mkv"); got != "film.mkv" {
		t.Fatal("literal matching became case insensitive", got)
	}
}

func TestRecognitionRuleValidationAndExpansion(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, row := range []recognitionReplacement{{From: "[", Regex: true}, {From: "(?=test)", Regex: true}, {To: "new"}, {From: "\n"}} {
		for _, field := range []string{"matchRules", "namingRules"} {
			w := request(t, h, "PUT", "/api/recognition-rules", map[string]any{field: []recognitionReplacement{row}}, cookie)
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"field":"`+field+`.0.from"`) || a.store.snapshot().RecognitionRules != nil {
				t.Fatal("invalid row persisted", field, w.Code, w.Body.String())
			}
		}
	}
	for _, cfg := range []recognitionSettings{{MinSizeMB: -1}, {MinSizeMB: 9007199254740992}, {Blacklist: []string{"."}}, {Blacklist: []string{"/"}}, {Blacklist: []string{".mkv/"}}, {Blacklist: []string{".m kv"}}, {Blacklist: []string{"/folder/child"}}, {ReleaseGroups: []string{"../team"}}, {ReleaseGroups: []string{".."}}, {MatchRules: make([]recognitionReplacement, 513)}} {
		if _, err := compileRecognitionSettings(cfg); err == nil {
			t.Fatal("invalid settings accepted", cfg)
		}
	}
	for _, value := range []any{-1, 1.5, "10"} {
		if w := request(t, h, "PUT", "/api/recognition-rules", map[string]any{"minSizeMB": value}, cookie); w.Code != 400 {
			t.Fatal("invalid size accepted", value, w.Code)
		}
	}
	c, err := compileRecognitionSettings(recognitionSettings{MatchRules: []recognitionReplacement{{From: "a", To: strings.Repeat("b", 1024)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.recognitionName(strings.Repeat("a", 1024)); err == nil {
		t.Fatal("unbounded expansion")
	}
}

func TestRecognitionBlacklistCaseAndDirectoryScope(t *testing.T) {
	c, err := compileRecognitionSettings(recognitionSettings{Blacklist: []string{"预告", ".MKV", ".cas.strm", "/花絮"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		dir  bool
		want bool
	}{{"Film预告.mkv", false, true}, {"Film.MKV", false, true}, {"Film.mkv", false, false}, {"Film.mkv.cas.strm", false, true}, {"Film.MKV.CAS.STRM", false, false}, {"电影/花絮", true, true}, {"电影/花絮/子目录/Film.mp4", false, true}, {"电影/花絮/子目录", true, true}, {"预告/Film.mp4", false, false}, {"Film.MKV", true, false}, {"电影/正片/Film.mp4", false, false}} {
		if got := c.blacklistMatch(tc.name, tc.dir); got != tc.want {
			t.Fatalf("%q dir=%v got %v", tc.name, tc.dir, got)
		}
	}
	if c.blacklistDestination(organizeTaskDefinition{}) != "recycle" || c.blacklistDestination(organizeTaskDefinition{FailureDirectory: "duplicates"}) != "failure-directory" {
		t.Fatal("blacklist fallback changed")
	}
}

func TestRecognitionPostNamingReleaseSuffixes(t *testing.T) {
	c, err := compileRecognitionSettings(recognitionSettings{NamingRules: []recognitionReplacement{{From: "Film", To: "影片"}, {From: "影片", To: "蜘蛛侠"}}, ReleaseGroups: []string{"TnT", "Other"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ file, want string }{
		{"Film.mkv", "蜘蛛侠-TnT.mkv"},
		{"Film.mp4", "蜘蛛侠-TnT.mp4"},
		{"Film.mkv.strm", "蜘蛛侠-TnT.mkv.strm"},
		{"Film.mkv.cas.strm", "蜘蛛侠-TnT.mkv.cas.strm"},
		{"Film.mp4.ed2k.strm", "蜘蛛侠-TnT.mp4.ed2k.strm"},
		{"Film.mkv.share.strm", "蜘蛛侠-TnT.mkv.share.strm"},
		{"Film.cas.strm", "蜘蛛侠-TnT.cas.strm"},
		{"Film.ed2k.strm", "蜘蛛侠-TnT.ed2k.strm"},
		{"Film.share.strm", "蜘蛛侠-TnT.share.strm"},
		{"Film.strm", "蜘蛛侠-TnT.strm"},
		{"Film.2021.strm", "蜘蛛侠.2021-TnT.strm"},
		{"Film.MKV.SHARE.STRM", "蜘蛛侠-TnT.MKV.SHARE.STRM"},
	} {
		if got, err := c.archiveName("Original-TnT-Other.mkv", tc.file); err != nil || got != tc.want {
			t.Fatal(tc.file, got, tc.want, err)
		}
	}
	if got, _ := c.archiveName("Original-TnT.mkv", "Film.2021.strm"); got != "蜘蛛侠.2021-TnT.strm" {
		t.Fatal("year treated as media suffix", got)
	}
	if got, _ := c.archiveName("Original-TnT.mkv", "Film-TnT.mkv"); got != "蜘蛛侠-TnT.mkv" {
		t.Fatal("duplicated release team", got)
	}
	if got, _ := c.archiveName("original-tnt.mkv", "Film.mkv"); got != "蜘蛛侠.mkv" {
		t.Fatal("release team case changed", got)
	}
}

func TestRecognitionSizeFilterBoundary(t *testing.T) {
	c, _ := compileRecognitionSettings(recognitionSettings{MinSizeMB: 10})
	for _, tc := range []struct {
		size int64
		want bool
	}{{-1, false}, {0, true}, {9_999_999, true}, {10_000_000, false}, {10_000_001, false}} {
		if c.tooSmall(tc.size) != tc.want {
			t.Fatal(tc)
		}
	}
	c, _ = compileRecognitionSettings(recognitionSettings{})
	if c.tooSmall(0) {
		t.Fatal("zero threshold must not filter")
	}
}
