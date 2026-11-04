package app

import (
	"encoding/json"
	"testing"
)

func TestOrganizeRuleDefaultsOptionsAndReferenceValidation(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	a.store.update(func(st *State) error {
		st.RulePresets = []rulePreset{{ID: "n-first", Kind: "naming", Name: "默认命名"}, {ID: "n-next", Kind: "naming", Name: "备用"}, {ID: "c-first", Kind: "categories", Name: "默认分类"}, {ID: "u-first", Kind: "upgrade", Name: "默认洗版"}}
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if w := request(t, h, "GET", "/api/organize-options", nil, nil); w.Code != 401 {
		t.Fatal("unprotected options")
	}
	w := request(t, h, "GET", "/api/organize-options", nil, cookie)
	var options struct {
		Rules       map[string][]rulePreset `json:"rules"`
		AIAvailable bool                    `json:"aiAvailable"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &options) != nil || options.AIAvailable || options.Rules["naming"][0].ID != "n-first" {
		t.Fatal(w.Code, w.Body.String())
	}
	in := organizeTaskDefinition{Name: "规则配置", StorageID: s.ID, Source: "/in", TargetStorageID: s.ID, Target: "/out"}
	w = request(t, h, "POST", "/api/organize-tasks", in, cookie)
	var saved organizeTaskDefinition
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	if saved.NamingRuleID != "n-first" || saved.MediaCategoryRuleID != "c-first" || saved.UpgradeRuleID != "" || saved.MetadataExtensions != "srt;ass;ssa" || !saved.TMDBScrape || !saved.FFprobe || saved.SmartCleanup || saved.AIEnhance {
		t.Fatal(saved)
	}
	saved.NamingRuleID = "n-next"
	saved.UpgradeRuleID = "u-first"
	saved.MetadataExtensions = ".SRT;ass;SRT"
	saved.ExcludeList = " 预告 ;.CAS.STRM;/花絮;预告"
	saved.TMDBScrape = false
	saved.FFprobe = false
	saved.SmartCleanup = true
	w = request(t, h, "PUT", "/api/organize-tasks/"+saved.ID, saved, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	stored := a.store.snapshot().OrganizeTasks[0]
	if stored.UpgradeRuleID != "u-first" || stored.MetadataExtensions != "srt;ass" || stored.ExcludeList != "预告;.cas.strm;/花絮" || stored.TMDBScrape || stored.FFprobe || !stored.SmartCleanup {
		t.Fatal(stored)
	}
	for _, kind := range []string{"wrong-kind", "missing-rule", "ai", "extension", "exclude"} {
		bad := stored
		switch kind {
		case "wrong-kind":
			bad.MediaCategoryRuleID = "n-first"
		case "missing-rule":
			bad.NamingRuleID = "removed"
		case "ai":
			bad.AIEnhance = true
		case "extension":
			bad.MetadataExtensions = "srt,ass"
		case "exclude":
			bad.ExcludeList = "*"
		}
		if w := request(t, h, "PUT", "/api/organize-tasks/"+saved.ID, bad, cookie); w.Code != 400 {
			t.Fatal(kind, w.Code, w.Body.String())
		}
	}
	a.store.update(func(st *State) error {
		if st.Plugins == nil {
			st.Plugins = map[string]PluginConfig{}
		}
		st.Plugins["ai"] = PluginConfig{Enabled: true, APIURL: "http://127.0.0.1:9999/v1", Model: "local-no-auth"}
		return nil
	})
	stored.AIEnhance = true
	stored.UpgradeRuleID = ""
	stored.MetadataExtensions = ""
	w = request(t, h, "PUT", "/api/organize-tasks/"+saved.ID, stored, cookie)
	if w.Code != 200 {
		t.Fatal("valid AI service rejected", w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.snapshot().OrganizeTasks[0]
	if got.UpgradeRuleID != "" || !got.AIEnhance || got.MetadataExtensions != "" || got.NamingRuleID != "n-next" || got.OptionsVersion != 1 || got.ExcludeList != "预告;.cas.strm;/花絮" {
		t.Fatal(got)
	}
}

func TestOrganizeDotExclusionSyntax(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", ""},
		{" 预告 ; .MKV ;.mkv;; .CAS.STRM ; /花絮 ;预告", "预告;.mkv;.cas.strm;/花絮"},
		{"trailer.final;/Extras.MKV;.share.strm;.ed2k.strm", "trailer.final;/Extras.MKV;.share.strm;.ed2k.strm"},
		{"*MKV;*.CAS.STRM;.mkv", ".mkv;.cas.strm"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := normalizeOrganizeExclusions(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	for _, input := range []string{".", "..mkv", ".mkv/", ".cas..strm", ".*mkv", "*", "/", ".mkv；预告", ".mkv\n预告"} {
		t.Run("invalid:"+input, func(t *testing.T) {
			if _, err := normalizeOrganizeExclusions(input); err == nil {
				t.Fatalf("invalid exclusions accepted: %q", input)
			}
		})
	}
}

func TestOrganizeLegacyExclusionsReadAndSave(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	const legacy = "预告;*MKV;*.CAS.STRM;/花絮"
	if err := a.store.update(func(st *State) error {
		st.OrganizeTasks = []organizeTaskDefinition{{ID: "legacy", Name: "旧排除配置", StorageID: s.ID, Source: "/in", TargetStorageID: s.ID, Target: "/out", OptionsVersion: 1, ExcludeList: legacy}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/organize-tasks", nil, cookie)
	var items []organizeTaskDefinition
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) != 1 || items[0].ExcludeList != "预告;.mkv;.cas.strm;/花絮" {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.store.snapshot().OrganizeTasks[0].ExcludeList != legacy {
		t.Fatal("read rewrote old exclusions")
	}
	w = request(t, h, "PUT", "/api/organize-tasks/legacy", items[0], cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.snapshot().OrganizeTasks[0].ExcludeList; got != "预告;.mkv;.cas.strm;/花絮" {
		t.Fatal(got)
	}
}

func TestOrganizeLegacyOptionsReadDoesNotRewriteDefinition(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	a.store.update(func(st *State) error {
		st.OrganizeTasks = []organizeTaskDefinition{{ID: "legacy", Name: "旧配置", StorageID: s.ID, Source: "/in", TargetStorageID: s.ID, Target: "/out", FailureDirectory: "/duplicates", FileFilterMB: 128}}
		return nil
	})
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/organize-tasks", nil, cookie)
	var got []organizeTaskDefinition
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || !got[0].TMDBScrape || got[0].FailureDirectory != "/duplicates" || got[0].FileFilterMB != 128 {
		t.Fatal(w.Body.String())
	}
	if a.store.snapshot().OrganizeTasks[0].OptionsVersion != 0 {
		t.Fatal("read rewrote old definition")
	}
}
