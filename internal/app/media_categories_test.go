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

func mediaCategoryTestConfig() mediaCategoryDefinition {
	cfg := defaultMediaCategory(nil)
	cfg.Directories = append(cfg.Directories, mediaCategoryDirectory{"tv-anime", "tv", "国漫", []string{"动画", "国漫"}, true}, mediaCategoryDirectory{"tv-cn", "tv", "国产", []string{"国产"}, true}, mediaCategoryDirectory{"movie-anime", "movie", "动画", []string{"动画"}, true})
	cfg.Rules = []mediaCategoryRule{
		{"anime-first", "国产动画", "tv", true, "tv-anime", mediaCategoryCondition{Kind: "all", Children: []mediaCategoryCondition{{Kind: "condition", Field: "genreIds", Operator: "contains_any", Value: json.RawMessage(`[16]`)}, {Kind: "condition", Field: "countries", Operator: "contains_any", Value: json.RawMessage(`["CN"]`)}}}},
		{"cn-next", "国产剧集", "tv", true, "tv-cn", mediaCategoryCondition{Kind: "condition", Field: "countries", Operator: "contains_any", Value: json.RawMessage(`["CN"]`)}},
	}
	return cfg
}

func TestMediaCategoryPresetPersistenceAndTaskSelection(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	h := a.Handler(t.TempDir())
	if w := request(t, h, "GET", "/api/media-categories/catalog", nil, nil); w.Code != 401 {
		t.Fatal("catalog unprotected", w.Code)
	}
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/media-categories/catalog", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), "音乐") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	cfg := mediaCategoryTestConfig()
	w = request(t, h, "POST", "/api/rule-presets", rulePreset{Kind: "categories", Name: "动画优先", MediaCategory: &cfg}, cookie)
	var first rulePreset
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &first) != nil || !reflect.DeepEqual(first.MediaCategory, &cfg) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/rule-presets", map[string]string{"kind": "categories", "name": "通用分类"}, cookie)
	var second rulePreset
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &second) != nil || len(second.MediaCategory.Directories) != 2 || len(second.MediaCategory.Rules) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	input := map[string]any{"name": "旧任务选择", "storageId": s.ID, "source": "/in", "targetStorageId": s.ID, "target": "/out", "categoryRuleId": first.ID}
	w = request(t, h, "POST", "/api/organize-tasks", input, cookie)
	var task organizeTaskDefinition
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &task) != nil || task.MediaCategoryRuleID != first.ID || strings.Contains(w.Body.String(), `"categoryRuleId"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(t, h, "PUT", "/api/rule-presets/"+first.ID, map[string]string{"kind": "categories", "name": "名称修改保留目录"}, cookie)
	if w.Code != 200 || a.store.snapshot().RulePresets[0].MediaCategory.Rules[0].DirectoryID != "tv-anime" {
		t.Fatal(w.Code, w.Body.String())
	}
	first = a.store.snapshot().RulePresets[0]
	first.MediaCategory.Directories[2].Name = "国产动画"
	first.MediaCategory.Directories[2].Path = []string{"动画", "国产动画"}
	w = request(t, h, "PUT", "/api/rule-presets/"+first.ID, first, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	st := a.store.snapshot()
	if st.OrganizeTasks[0].MediaCategoryRuleID != first.ID || !reflect.DeepEqual(st.RulePresets[1], second) || st.RulePresets[0].MediaCategory.Rules[0].DirectoryID != "tv-anime" {
		t.Fatal("edit changed stable identities or another preset")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || !reflect.DeepEqual(reopened.snapshot().RulePresets, st.RulePresets) || reopened.snapshot().OrganizeTasks[0].MediaCategoryRuleID != first.ID {
		t.Fatal("restart lost classification", err)
	}
	if w := request(t, h, "POST", "/api/rule-presets", rulePreset{Kind: "naming", Name: "wrong kind", MediaCategory: &cfg}, cookie); w.Code != 400 {
		t.Fatal("classification attached to naming")
	}
}

func TestMediaCategoryValidationAndTwoMediaTypes(t *testing.T) {
	for _, mutate := range []func(*mediaCategoryDefinition){
		func(c *mediaCategoryDefinition) { c.Directories[0].MediaType = "music" },
		func(c *mediaCategoryDefinition) { c.Directories[0].Name = "电视剧" },
		func(c *mediaCategoryDefinition) { c.Directories[0].Enabled = false },
		func(c *mediaCategoryDefinition) { c.Directories[2].Path = []string{"..", "outside"} },
		func(c *mediaCategoryDefinition) { c.Directories[2].Path = []string{"/absolute"} },
		func(c *mediaCategoryDefinition) { c.Directories[2].Path = []string{"动画", ""} },
		func(c *mediaCategoryDefinition) { c.Directories[2].Path = []string{"国产"} },
		func(c *mediaCategoryDefinition) { c.Directories[2].Enabled = false },
		func(c *mediaCategoryDefinition) { c.Fallbacks["movie"] = "tv-anime" },
		func(c *mediaCategoryDefinition) { c.Fallbacks["music"] = "tv-root" },
		func(c *mediaCategoryDefinition) { c.Rules[0].DirectoryID = "movie-anime" },
		func(c *mediaCategoryDefinition) { c.Rules[0].DirectoryID = "missing" },
		func(c *mediaCategoryDefinition) { c.Rules[0].When = mediaCategoryCondition{Kind: "all"} },
		func(c *mediaCategoryDefinition) {
			c.Rules[0].When = mediaCategoryCondition{Kind: "not", Children: []mediaCategoryCondition{{Kind: "all"}, {Kind: "any"}}}
		},
		func(c *mediaCategoryDefinition) {
			c.Rules[0].When = mediaCategoryCondition{Kind: "condition", Field: "unsupported", Operator: "exists"}
		},
		func(c *mediaCategoryDefinition) {
			c.Rules[0].When = mediaCategoryCondition{Kind: "condition", Field: "year", Operator: "equals", Value: json.RawMessage(`2021.5`)}
		},
		func(c *mediaCategoryDefinition) {
			c.Rules[0].When = mediaCategoryCondition{Kind: "condition", Field: "genreIds", Operator: "contains_any", Value: json.RawMessage(`["16"]`)}
		},
		func(c *mediaCategoryDefinition) {
			c.Rules[0].When = mediaCategoryCondition{Kind: "condition", Field: "title", Operator: "gt", Value: json.RawMessage(`1`)}
		},
		func(c *mediaCategoryDefinition) {
			c.Rules[0].When = mediaCategoryCondition{Kind: "condition", Field: "year", Operator: "between", Value: json.RawMessage(`[2025,2020]`)}
		},
	} {
		cfg := mediaCategoryTestConfig()
		mutate(&cfg)
		if err := validateMediaCategory(cfg); err == nil {
			t.Fatal("invalid category configuration accepted", cfg)
		}
	}
	cfg := mediaCategoryTestConfig()
	cfg.Directories = append(cfg.Directories, mediaCategoryDirectory{"other-nested", "tv", "国产", []string{"其他", "国产"}, true})
	if err := validateMediaCategory(cfg); err != nil {
		t.Fatal("distinct nested paths cannot reuse display names", err)
	}
	node := mediaCategoryCondition{Kind: "condition", Field: "title", Operator: "exists"}
	for i := 0; i < 8; i++ {
		node = mediaCategoryCondition{Kind: "all", Children: []mediaCategoryCondition{node}}
	}
	cfg.Rules[0].When = node
	if validateMediaCategory(cfg) == nil {
		t.Fatal("unbounded condition depth")
	}
}

func TestMediaCategoryNullListsCanonicalRead(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	cfg := defaultMediaCategory(nil)
	cfg.Rules = nil
	cfg.Directories[0].Path = nil
	w := request(t, h, "POST", "/api/rule-presets", rulePreset{Kind: "categories", Name: "空列表兼容", MediaCategory: &cfg}, cookie)
	if w.Code != 201 || strings.Contains(w.Body.String(), `"path":null`) || strings.Contains(w.Body.String(), `"rules":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	legacy := defaultMediaCategory(nil)
	legacy.Rules = nil
	legacy.Directories[0].Path = nil
	if err := a.store.update(func(st *State) error { st.RulePresets[0].MediaCategory = &legacy; return nil }); err != nil {
		t.Fatal(err)
	}
	w = request(t, h, "GET", "/api/rule-presets?kind=categories", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), `"path":null`) || strings.Contains(w.Body.String(), `"rules":null`) {
		t.Fatal(w.Code, w.Body.String())
	}
	stored := a.store.snapshot().RulePresets[0].MediaCategory
	if stored.Rules != nil || stored.Directories[0].Path != nil {
		t.Fatal("read rewrote legacy lists")
	}
}

func TestMediaCategoryEvaluationOrderFallbackAndConditions(t *testing.T) {
	cfg := mediaCategoryTestConfig()
	facts := map[string]any{"title": "动画作品", "genreIds": []int{16}, "countries": []string{"CN"}, "year": 2021}
	dir, rule, err := evaluateMediaCategory(cfg, "tv", facts)
	if err != nil || dir.ID != "tv-anime" || rule != "anime-first" {
		t.Fatal(dir, rule, err)
	}
	cfg.Rules[0], cfg.Rules[1] = cfg.Rules[1], cfg.Rules[0]
	dir, rule, err = evaluateMediaCategory(cfg, "tv", facts)
	if err != nil || dir.ID != "tv-cn" || rule != "cn-next" {
		t.Fatal("order not preserved", dir, rule, err)
	}
	dir, rule, err = evaluateMediaCategory(cfg, "movie", facts)
	if err != nil || dir.ID != "movie-root" || rule != "" {
		t.Fatal("media scope crossed", dir, rule, err)
	}
	if _, _, err := evaluateMediaCategory(cfg, "music", facts); err == nil {
		t.Fatal("music accepted")
	}
	for _, tc := range []struct {
		node mediaCategoryCondition
		want bool
	}{
		{mediaCategoryCondition{Kind: "condition", Field: "year", Operator: "between", Value: json.RawMessage(`[2020,2021]`)}, true},
		{mediaCategoryCondition{Kind: "condition", Field: "year", Operator: "lt", Value: json.RawMessage(`2021`)}, false},
		{mediaCategoryCondition{Kind: "condition", Field: "title", Operator: "starts_with", Value: json.RawMessage(`"动画"`)}, true},
		{mediaCategoryCondition{Kind: "condition", Field: "countries", Operator: "contains_all", Value: json.RawMessage(`["CN","JP"]`)}, false},
		{mediaCategoryCondition{Kind: "condition", Field: "source", Operator: "not_equals", Value: json.RawMessage(`"tmdb"`)}, false},
		{mediaCategoryCondition{Kind: "condition", Field: "source", Operator: "not_exists"}, true},
		{mediaCategoryCondition{Kind: "any", Children: []mediaCategoryCondition{{Kind: "condition", Field: "source", Operator: "exists"}, {Kind: "condition", Field: "year", Operator: "gte", Value: json.RawMessage(`2021`)}}}, true},
		{mediaCategoryCondition{Kind: "not", Children: []mediaCategoryCondition{{Kind: "condition", Field: "title", Operator: "contains", Value: json.RawMessage(`"纪录"`)}}}, true},
	} {
		c := mediaCategoryTestConfig()
		c.Rules = c.Rules[:1]
		c.Rules[0].When = tc.node
		_, rule, err := evaluateMediaCategory(c, "tv", facts)
		if err != nil || (rule != "") != tc.want {
			t.Fatal(tc, rule, err)
		}
	}
	if _, _, err := evaluateMediaCategory(cfg, "tv", map[string]any{"year": "2021"}); err == nil {
		t.Fatal("invalid fact type accepted")
	}
}

func TestMediaCategoryPreviewIsReadOnly(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	file := filepath.Join(s.Config["root"], "keep.mkv")
	writeTest(t, file, "unchanged")
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	body := map[string]any{"config": mediaCategoryTestConfig(), "mediaType": "tv", "facts": map[string]any{"genreIds": []int{16}, "countries": []string{"CN"}}}
	if w := request(t, h, "POST", "/api/media-categories/preview", body, nil); w.Code != 401 {
		t.Fatal("unprotected preview")
	}
	w := request(t, h, "POST", "/api/media-categories/preview", body, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `剧集/动画/国漫`) || !strings.Contains(w.Body.String(), `"ruleId":"anime-first"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "unchanged" || len(a.running) != 0 || len(a.store.snapshot().RulePresets) != 0 {
		t.Fatal("preview performed writes", err)
	}
}

func TestMediaCategoryLegacyFieldAndEncryptedBackup(t *testing.T) {
	var task organizeTaskDefinition
	if err := json.Unmarshal([]byte(`{"id":"legacy","name":"old","categoryRuleId":"saved-category"}`), &task); err != nil || task.MediaCategoryRuleID != "saved-category" {
		t.Fatal(task, err)
	}
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), `"categoryRuleId"`) || !strings.Contains(string(raw), `"mediaCategoryRuleId":"saved-category"`) {
		t.Fatal(string(raw))
	}
	for _, raw := range []string{`{"categoryRuleId":"old","mediaCategoryRuleId":"new"}`, `{"mediaCategoryRuleId":"new","unexpected":1}`} {
		if json.Unmarshal([]byte(raw), &task) == nil {
			t.Fatal("ambiguous or unknown task field accepted", raw)
		}
	}
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	cfg := mediaCategoryTestConfig()
	w := request(t, h, "POST", "/api/rule-presets", rulePreset{Kind: "categories", Name: "备份分类", MediaCategory: &cfg}, cookie)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	saved := a.store.snapshot().RulePresets
	w = request(t, h, "POST", "/api/config/backup", map[string]string{"password": "category-backup"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	plain, err := openBackup(w.Body.Bytes(), "category-backup")
	var bundle configBundle
	if err != nil || json.Unmarshal(plain, &bundle) != nil || validateBundle(bundle) != nil || !reflect.DeepEqual(bundle.State.RulePresets, saved) {
		t.Fatal("classification missing in backup", err)
	}
	bad := bundle
	bad.State.RulePresets[0].MediaCategory.Rules[0].DirectoryID = "missing"
	if validateBundle(bad) == nil {
		t.Fatal("invalid category import accepted")
	}
	if err := a.store.update(func(st *State) error { st.RulePresets = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "categories.aether")
	part.Write(w.Body.Bytes())
	form.WriteField("password", "category-backup")
	form.Close()
	req := httptest.NewRequest("POST", "/api/config/import", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(cookie)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if err := applyPendingConfig(a.store); err != nil || !reflect.DeepEqual(a.store.snapshot().RulePresets, saved) {
		t.Fatal("classification not restored", err)
	}
}
