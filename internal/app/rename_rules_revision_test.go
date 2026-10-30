package app

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func decodeRenameRules(t *testing.T, raw string) []renameRule {
	t.Helper()
	var rules []renameRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		t.Fatal(err)
	}
	return rules
}

func TestRenameSixRuleTypes(t *testing.T) {
	tests := []struct{ name, input, rules, want string }{
		{"prefix", "原名.mkv", `[{"kind":"number","start":1,"digits":3,"numberPosition":"before","separator":"、"}]`, "001、原名.mkv"},
		{"suffix unified", "原名.mkv", `[{"kind":"number","name":"影片","start":9,"digits":2,"numberPosition":"after","separator":"-"}]`, "影片-09.mkv"},
		{"unicode insertion", "甲乙.mkv", `[{"kind":"insert","position":2,"text":"星🌟"}]`, "甲星🌟乙.mkv"},
		{"append", "甲乙.mkv", `[{"kind":"insert","position":99,"text":"末"}]`, "甲乙末.mkv"},
		{"categories", "Ab12_①，㎏→┼片.mkv", `[{"kind":"delete","deleteTypes":["english","digits","special","serial","punctuation","units","arrows","box"]}]`, "片.mkv"},
		{"range", "甲乙丙丁戊.mkv", `[{"kind":"delete","deleteRange":true,"from":2,"to":2,"toEnd":true}]`, "甲戊.mkv"},
		{"reverse range", "甲乙丙丁.mkv", `[{"kind":"delete","deleteRange":true,"from":1,"to":2,"fromEnd":true,"toEnd":true}]`, "甲乙.mkv"},
		{"custom first", "甲乙甲乙.mkv", `[{"kind":"delete","text":"甲","firstOnly":true}]`, "乙甲乙.mkv"},
		{"custom all", "甲乙甲乙.mkv", `[{"kind":"delete","text":"甲"}]`, "乙乙.mkv"},
		{"move", "甲🌟乙丙.mkv", `[{"kind":"move","from":2,"to":4}]`, "甲乙丙🌟.mkv"},
		{"move backwards", "甲乙丙.mkv", `[{"kind":"move","from":3,"to":1}]`, "丙甲乙.mkv"},
		{"move outside", "甲乙.mkv", `[{"kind":"move","from":99,"to":1}]`, "甲乙.mkv"},
		{"lower", "Hello WORLD.MKV", `[{"kind":"convert","caseMode":"lower"}]`, "hello world.MKV"},
		{"upper", "Hello world.mkv", `[{"kind":"convert","caseMode":"upper"}]`, "HELLO WORLD.mkv"},
		{"title", "hELLO-wORLD 你好abc.mkv", `[{"kind":"convert","caseMode":"title"}]`, "Hello-World 你好Abc.mkv"},
		{"sentence", "hELLO WORLD.mkv", `[{"kind":"convert","caseMode":"sentence"}]`, "Hello world.mkv"},
		{"simplified", "電腦與軟體.mkv", `[{"kind":"convert","chineseMode":"t2s"}]`, "电脑与软体.mkv"},
		{"Hong Kong", "里面的电脑.mkv", `[{"kind":"convert","chineseMode":"s2hk"}]`, "裏面的電腦.mkv"},
		{"Taiwan", "里面的电脑.mkv", `[{"kind":"convert","chineseMode":"s2tw"}]`, "裡面的電腦.mkv"},
		{"simplified phrases", "頭髮發展.mkv", `[{"kind":"convert","chineseMode":"t2s"}]`, "头发发展.mkv"},
		{"Hong Kong phrases", "里面发展理发.mkv", `[{"kind":"convert","chineseMode":"s2hk"}]`, "裏面發展理髮.mkv"},
		{"Taiwan phrases", "里面发展理发.mkv", `[{"kind":"convert","chineseMode":"s2tw"}]`, "裡面發展理髮.mkv"},
		{"English half", "ＡＢＣ１２，片。.mkv", `[{"kind":"convert","widthMode":"narrow_en"}]`, "ABC12,片..mkv"},
		{"Chinese half", "ＡＢＣ１２，片。.mkv", `[{"kind":"convert","widthMode":"narrow_cn"}]`, "ABC12，片。.mkv"},
		{"English full", "AB12,片..mkv", `[{"kind":"convert","widthMode":"wide_en"}]`, "ＡＢ１２，片．.mkv"},
		{"Chinese full", "AB12,片..mkv", `[{"kind":"convert","widthMode":"wide_cn"}]`, "ＡＢ１２，片。.mkv"},
		{"no conversion", "電腦Ab.mkv", `[{"kind":"convert","caseMode":"none","chineseMode":"none","widthMode":"none"}]`, "電腦Ab.mkv"},
		{"ordered extension", "OLD.mkv", `[{"kind":"replace","findType":"regex","find":"\\.mkv$","replace":".mp4"},{"kind":"number","name":"新名","start":1,"digits":1,"numberPosition":"after","separator":"-"},{"kind":"insert","position":1,"text":"片"}]`, "片新名-1.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyRenameRules(tt.input, decodeRenameRules(t, tt.rules))
			if err != nil || got != tt.want {
				t.Fatalf("got %q (%v), want %q", got, err, tt.want)
			}
		})
	}
}

func TestRenameNumberOrderAndFolderNames(t *testing.T) {
	files := []File{{ID: "a", Name: "甲.mkv"}, {ID: "b", Name: "乙.mp4"}, {ID: "c", Name: "有点.的目录", IsDir: true}}
	rules := decodeRenameRules(t, `[{"kind":"number","name":"片","start":7,"digits":2,"numberPosition":"before","separator":"-"}]`)
	items, err := renamePlan(files, renameRequest{IDs: []string{"b", "c", "a"}, Rules: rules})
	if err != nil || len(items) != 3 {
		t.Fatal(items, err)
	}
	for i, want := range []string{"07-片.mp4", "08-片", "09-片.mkv"} {
		if items[i].NewName != want || items[i].Error != "" {
			t.Fatalf("%+v want %s", items[i], want)
		}
	}
	items, err = renamePlan(files, renameRequest{IDs: []string{"a"}, Rules: rules})
	if err != nil || items[0].NewName != "07-片.mkv" {
		t.Fatal("ignored rows must not consume a number", items, err)
	}
}

func TestRenameNewRuleValidationAndPersistence(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	for _, raw := range []string{
		`[{"kind":"insert","position":-1,"text":"x"}]`,
		`[{"kind":"number","digits":99}]`,
		`[{"kind":"move","from":1,"to":-1}]`,
		`[{"kind":"convert","chineseMode":"bad"}]`,
		`[{"kind":"convert","widthMode":"bad"}]`,
		`[{"kind":"delete","deleteTypes":["bad"]}]`,
		`[{"kind":"replace","findType":"regex","find":"["}]`,
		`[{"kind":"missing"}]`,
	} {
		rules := decodeRenameRules(t, raw)
		res := request(t, h, "POST", "/api/files/rename-rules", renameRuleSet{Name: "bad", Rules: rules}, cookie)
		if res.Code != http.StatusBadRequest {
			t.Fatalf("invalid saved rule accepted %s: %d %s", raw, res.Code, res.Body.String())
		}
	}
	if err := os.WriteFile(filepath.Join(s.Config["root"], "old.mkv"), []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	rules := decodeRenameRules(t, `[{"kind":"replace","find":".mkv","replace":".mp4"},{"kind":"number","start":1,"digits":2,"separator":"-","numberPosition":"before"},{"kind":"convert","caseMode":"upper"}]`)
	res := request(t, h, "POST", "/api/files/rename-rules", renameRuleSet{Name: "多规则", Rules: rules}, cookie)
	var sets []renameRuleSet
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &sets) != nil || len(sets) != 1 {
		t.Fatal(res.Code, res.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(a.store.dir, "organize", "rename-rules.json"))
	var saved []renameRuleSet
	if err != nil || json.Unmarshal(data, &saved) != nil || len(saved) != 1 {
		t.Fatal(string(data), err)
	}
	input := renameRequest{StorageID: s.ID, Source: "/", IDs: []string{"/old.mkv"}, Rules: saved[0].Rules}
	res = request(t, h, "POST", "/api/files/rename-preview", input, cookie)
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &input.Expected) != nil || len(input.Expected) != 1 || input.Expected[0].NewName != "01-OLD.mp4" {
		t.Fatal(res.Code, res.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.Config["root"], "old.mkv")); err != nil {
		t.Fatal("preview modified file", err)
	}
	res = request(t, h, "POST", "/api/files/rename", input, cookie)
	if res.Code != 200 || strings.Contains(res.Body.String(), `"error"`) {
		t.Fatal(res.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(s.Config["root"], "01-OLD.mp4")); err != nil || string(data) != "unchanged" {
		t.Fatal(string(data), err)
	}
	if len(a.store.snapshot().RenameHistory) != 1 {
		t.Fatal("history missing")
	}
	var response struct {
		Renamed []struct {
			ID    string `json:"id"`
			NewID string `json:"newId"`
		} `json:"renamed"`
	}
	if json.Unmarshal(res.Body.Bytes(), &response) != nil || len(response.Renamed) != 1 ||
		response.Renamed[0].ID != "/old.mkv" || response.Renamed[0].NewID != "/01-OLD.mp4" {
		t.Fatal("new identity missing from response", res.Body.String())
	}
}

func TestRenameDeleteCategoriesIndependent(t *testing.T) {
	for _, tt := range []struct{ kind, removed string }{
		{"english", "Ab"}, {"digits", "12"}, {"special", "_🌟 "}, {"serial", "①Ⅱ"},
		{"punctuation", "，！"}, {"units", "㎏℃"}, {"arrows", "→⇧"}, {"box", "┼█"},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			rules := []renameRule{{Kind: "delete", DeleteTypes: []string{tt.kind}}}
			name := "Ab12_🌟 ①Ⅱ，！㎏℃→⇧┼█中文.mkv"
			want := name
			for _, r := range tt.removed {
				want = strings.ReplaceAll(want, string(r), "")
			}
			got, err := applyRenameRules(name, rules)
			if err != nil || got != want {
				t.Fatalf("%s got %q (%v), want %q", tt.kind, got, err, want)
			}
		})
	}
}

func TestRenameRuleBoundariesAndSharedConverters(t *testing.T) {
	for _, tt := range []struct {
		name string
		rule renameRule
	}{
		{"unsafe insertion", renameRule{Kind: "insert", Text: "/escape", Position: 1}},
		{"oversize output", renameRule{Kind: "insert", Text: strings.Repeat("中", 100), Position: 1}},
		{"oversize text", renameRule{Kind: "insert", Text: strings.Repeat("a", 256), Position: 1}},
		{"oversize number", renameRule{Kind: "number", Start: 1000000001}},
		{"oversize position", renameRule{Kind: "move", From: 4097}},
		{"unknown case", renameRule{Kind: "convert", CaseMode: "bad"}},
		{"duplicate delete type", renameRule{Kind: "delete", DeleteTypes: []string{"english", "english"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := applyRenameRules("原名.mkv", []renameRule{tt.rule}); err == nil {
				t.Fatal("unsafe rule accepted")
			}
		})
	}
	compiled, err := compileRenameRules([]renameRule{{Kind: "convert", ChineseMode: "s2hk"}})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				got, err := compiled.apply("里面的电脑.mkv", 0, false)
				if err != nil || got != "裏面的電腦.mkv" {
					t.Errorf("%q %v", got, err)
				}
			}
		})
	}
	wg.Wait()
}

func TestRenameWorkbenchExtension115UsesRecreation(t *testing.T) {
	a := testApp(t)
	s := Storage{ID: "115", Type: "115", Name: "测试115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	if err := a.store.update(func(st *State) error { st.Storages = []Storage{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient
	t.Cleanup(func() { apiClient = old })
	submits, writes := 0, 0
	file := map[string]any{"fid": "123", "cid": "0", "n": "movie.mkv", "s": "42", "sha": strings.Repeat("a", 40), "pc": "pick123"}
	apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
		var body any
		status := 200
		switch r.URL.Path {
		case "/files":
			body = map[string]any{"state": true, "cid": "0", "count": 1, "offset": 0, "data": []any{file}}
		case "/files/get_info":
			body = map[string]any{"state": true, "data": []any{file}}
		case "/app/uploadinfo":
			body = map[string]any{"state": true, "user_id": 123, "userkey": "private-key", "upload_allowed": true, "size_limit": 100000000000}
		case "/4.0/initupload.php":
			submits++
			raw, _ := io.ReadAll(r.Body)
			if len(raw) == 0 || strings.Contains(string(raw), "movie.mp4") {
				t.Error("expected encrypted rapid upload request")
			}
			status = 503
			body = map[string]string{"error": "private-body-must-not-leak"}
		default:
			writes++
			t.Errorf("native rename, upload, recycling or unknown call: %s", r.URL.Path)
			body = map[string]any{"state": false}
		}
		raw, _ := json.Marshal(body)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw))), Request: r}, nil
	})}
	h := a.Handler(t.TempDir())
	cookie := request(t, h, "POST", "/api/auth/setup", credentials{Username: "admin", Password: "x"}, nil).Result().Cookies()[0]
	input := renameRequest{StorageID: s.ID, Source: "0", IDs: []string{"123"}, Rules: []renameRule{{Kind: "replace", FindType: "regex", Find: `\.mkv$`, Replace: ".mp4"}}}
	result := request(t, h, "POST", "/api/files/rename-preview", input, cookie)
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &input.Expected) != nil || len(input.Expected) != 1 || input.Expected[0].NewName != "movie.mp4" || submits != 0 {
		t.Fatal("preview should not write", result.Code, result.Body.String(), submits)
	}
	result = request(t, h, "POST", "/api/files/rename", input, cookie)
	if result.Code != 200 || !strings.Contains(result.Body.String(), `"processed":0`) || !strings.Contains(result.Body.String(), `"error"`) ||
		strings.Contains(result.Body.String(), "private") || submits != 1 || writes != 0 || len(a.store.snapshot().RenameHistory) != 0 {
		t.Fatal("unsafe extension handling", result.Code, result.Body.String(), submits, writes)
	}
}
