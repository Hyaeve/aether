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

func TestNamingTemplatesAndSafeSuffixes(t *testing.T) {
	if err := validateNaming(defaultNaming(nil)); err != nil {
		t.Fatal(err)
	}
	facts := namingSample("tv")
	facts["title"] = "A/B\\C"
	facts["original_title"] = ""
	for _, suffix := range []string{".mkv", ".mkv.strm", ".mkv.cas.strm", ".mp4.share.strm", ".ed2k.strm"} {
		r, err := renderNaming("ignored{title} ({year})/{title}.{season_episode}trailing", "tv", "original"+suffix, facts)
		if err != nil || r.Directory != "A_B_C (2023" || r.Filename != "A_B_C.S01E01"+suffix {
			t.Fatal(r, err)
		}
	}
	// Text outside the first/last variable is intentionally inactive. A closing
	// bracket is active when another variable follows it, as in the default path.
	r, err := renderNaming("{title} ({year}) -{tmdb_tag}/{title}.{season_episode}", "tv", "original.mkv", facts)
	if err != nil || r.Directory != "A_B_C (2023) -{tmdbid=204541}" {
		t.Fatal(r, err)
	}
	for _, source := range []string{"{unknown}", "{title}.{title}", "/{title}", "{title}/", "{title}//{year}", "{title}\\{year}", "{title}/{season_episode}", "{title}broken}", "{title}:", "{title}\n{year}", strings.Repeat("{title}/", 9) + "{title}"} {
		if _, err := parseNaming(source, "movie"); err == nil {
			t.Fatal("invalid template accepted", source)
		}
	}
	if _, err := parseNaming("{title}/{title}.{year}", "movie"); err != nil {
		t.Fatal("per-segment reuse rejected", err)
	}
	if _, err := renderNaming("{title}", "tv", "a.mkv", map[string]string{"title": ".."}); err == nil {
		t.Fatal("up-level result accepted")
	}
}

func upgradeTestConfig() upgradeDefinition {
	c := defaultUpgrade(nil)
	for i := range c.Groups {
		c.Groups[i].Enabled = false
	}
	return c
}
func TestUpgradeLinearTotalAndDisabledGroups(t *testing.T) {
	c := upgradeTestConfig()
	c.Groups[0].Enabled = true
	c.Groups[0].Values = []string{"4K", "1080p"}
	c.Groups[2].Enabled = true
	c.Groups[2].Values = []string{"HEVC", "AVC"}
	c.Groups[3].Enabled = true
	c.Groups[3].Values = []string{"AAC"}
	old := upgradeVersion{Facts: map[string][]string{"resolution": {"4K"}}}
	next := upgradeVersion{Facts: map[string][]string{"resolution": {"1080p"}, "video_codec": {"HEVC"}, "audio_codec": {"AAC"}}}
	r, err := compareUpgrade(c, old, next, organizeTaskDefinition{FailureDirectory: "losers"})
	if err != nil || r.Winner != "incoming" || r.Existing != 300 || r.Incoming != 450 || r.Groups[0].Weight != 3 || r.Groups[2].Weight != 1 || r.LoserAction != "move" || r.Directory != "losers" {
		t.Fatal(r, err)
	}
	c.Groups[2].Enabled = false
	c.Groups[3].Enabled = false
	r, err = compareUpgrade(c, old, next, organizeTaskDefinition{})
	if err != nil || r.Winner != "existing" || len(r.Groups) != 1 || r.Groups[0].Weight != 1 || r.LoserAction != "keep" {
		t.Fatal(r, err)
	}
	c.Groups[0].Enabled = false
	r, err = compareUpgrade(c, old, next, organizeTaskDefinition{FailureDirectory: "losers"})
	if err != nil || r.Winner != "tie" || r.LoserAction != "keep" || len(r.Groups) != 0 {
		t.Fatal(r, err)
	}
}
func TestUpgradeAliasesOrderAndSize(t *testing.T) {
	c := defaultUpgrade(nil)
	for _, v := range []struct{ id, a, b string }{{"resolution", "2160p", "4K"}, {"video_codec", "H.265", "HEVC"}, {"hdr", "Dolby Vision", "DV"}, {"color_depth", "10-bit", "10BIT"}, {"frame_rate", "30fps", "30FPS"}, {"file_extension", "MKV", ".mkv.cas.strm"}} {
		g := c.Groups[0]
		for _, group := range c.Groups {
			if group.ID == v.id {
				g = group
			}
		}
		if upgradePreference(g, []string{v.a}) != upgradePreference(g, []string{v.b}) {
			t.Fatal("alias changed quality", v)
		}
	}
	g := upgradeGroup{"resolution", true, []string{"1080p", "4K"}}
	if upgradePreference(g, []string{"1080p"}) <= upgradePreference(g, []string{"2160p"}) {
		t.Fatal("order not applied")
	}
	if upgradePreference(upgradeGroup{"subtitle", true, []string{"中文字幕", "有字幕", "无字幕"}}, []string{"CHS&ENG"}) != 100 {
		t.Fatal("Chinese subtitle not recognized")
	}
	for _, g := range c.Groups {
		if upgradePreference(g, []string{"unknown", "AAC-unknown"}) != 0 {
			t.Fatal("unknown metadata earned quality points", g.ID)
		}
	}
	c = upgradeTestConfig()
	c.Groups[9].Enabled = true
	r, err := compareUpgrade(c, upgradeVersion{Size: 50}, upgradeVersion{Size: 100}, organizeTaskDefinition{})
	if err != nil || r.Existing != 50 || r.Incoming != 100 || r.Winner != "incoming" || r.LoserAction != "keep" {
		t.Fatal(r, err)
	}
	if _, err = compareUpgrade(c, upgradeVersion{Size: -1}, upgradeVersion{}, organizeTaskDefinition{}); err == nil {
		t.Fatal("negative size accepted")
	}
	for _, mutate := range []func(*upgradeDefinition){func(c *upgradeDefinition) { c.Groups = c.Groups[:9] }, func(c *upgradeDefinition) { c.Groups[0].ID = "music" }, func(c *upgradeDefinition) { c.Groups[1].ID = c.Groups[0].ID }, func(c *upgradeDefinition) { c.Groups[0].Values = []string{"evil"} }, func(c *upgradeDefinition) { c.Groups[0].Values = []string{"4K", "4K"} }, func(c *upgradeDefinition) { c.Groups[9].Values = []string{"bytes"} }} {
		bad := defaultUpgrade(nil)
		mutate(&bad)
		if validateUpgrade(bad) == nil {
			t.Fatal("invalid policy accepted", bad)
		}
	}
}

func TestNamingUpgradePresetPersistenceAndEncryptedBackup(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	n := defaultNaming(nil)
	n.TV = "{title}/{title}.{season_episode}"
	u := defaultUpgrade(nil)
	u.Groups[0], u.Groups[1] = u.Groups[1], u.Groups[0]
	u.Groups[2].Enabled = false
	for _, input := range []rulePreset{{Kind: "naming", Name: "命名", Naming: &n}, {Kind: "upgrade", Name: "洗版", Upgrade: &u}} {
		w := request(t, h, "POST", "/api/rule-presets", input, cookie)
		if w.Code != 201 {
			t.Fatal(w.Body.String())
		}
		var p rulePreset
		json.Unmarshal(w.Body.Bytes(), &p)
		w = request(t, h, "PUT", "/api/rule-presets/"+p.ID, map[string]string{"kind": p.Kind, "name": p.Name + "改名"}, cookie)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	saved := a.store.snapshot().RulePresets
	if !reflect.DeepEqual(saved[0].Naming, &n) || !reflect.DeepEqual(saved[1].Upgrade, &u) {
		t.Fatal("metadata save lost definitions")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || !reflect.DeepEqual(reopened.snapshot().RulePresets, saved) {
		t.Fatal("restart", err)
	}
	w := request(t, h, "POST", "/api/config/backup", map[string]string{"password": "rules-backup"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	plain, err := openBackup(w.Body.Bytes(), "rules-backup")
	var bundle configBundle
	if err != nil || json.Unmarshal(plain, &bundle) != nil || validateBundle(bundle) != nil || !reflect.DeepEqual(bundle.State.RulePresets, saved) {
		t.Fatal("backup", err)
	}
	bad := bundle
	bad.State.RulePresets = append([]rulePreset{}, bundle.State.RulePresets...)
	wrong := n
	wrong.Movie = "{invalid}"
	bad.State.RulePresets[0].Naming = &wrong
	if validateBundle(bad) == nil {
		t.Fatal("invalid import accepted")
	}
	a.store.update(func(st *State) error { st.RulePresets = nil; return nil })
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, _ := form.CreateFormFile("file", "rules.aether")
	part.Write(w.Body.Bytes())
	form.WriteField("password", "rules-backup")
	form.Close()
	req := httptest.NewRequest("POST", "/api/config/import", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(cookie)
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	if err := applyPendingConfig(a.store); err != nil || !reflect.DeepEqual(a.store.snapshot().RulePresets, saved) {
		t.Fatal("import", err)
	}
}

func TestNamingUpgradeProtectedReadOnlyPreviewAndLegacyDefaults(t *testing.T) {
	a := testApp(t)
	s := addLocal(t, a)
	file := filepath.Join(s.Config["root"], "keep.mkv")
	writeTest(t, file, "unchanged")
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	for _, route := range []string{"naming-rules", "upgrade-rules"} {
		w := request(t, h, "GET", "/api/"+route+"/catalog", nil, nil)
		if w.Code != 401 {
			t.Fatal("unprotected catalog")
		}
		w = request(t, h, "GET", "/api/"+route+"/catalog", nil, cookie)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request(t, h, "POST", "/api/naming-rules/preview", map[string]string{"source": "{title}/{title}.{year}", "mediaType": "movie"}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "肖申克的救赎.1994.mkv") {
		t.Fatal(w.Body.String())
	}
	w = request(t, h, "POST", "/api/upgrade-rules/preview", map[string]any{"config": defaultUpgrade(nil), "existing": upgradeVersion{Size: 1}, "incoming": upgradeVersion{Size: 2}}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"winner":"incoming"`) {
		t.Fatal(w.Body.String())
	}
	for _, input := range []rulePreset{{Kind: "categories", Name: "bad", Naming: &namingDefinition{TV: "{title}", Movie: "{title}"}}, {Kind: "naming", Name: "bad", Upgrade: func() *upgradeDefinition { c := defaultUpgrade(nil); return &c }()}} {
		if request(t, h, "POST", "/api/rule-presets", input, cookie).Code != 400 {
			t.Fatal("cross kind accepted")
		}
	}
	a.store.update(func(st *State) error {
		st.RulePresets = []rulePreset{{ID: "legacy-name", Kind: "naming", Name: "旧命名"}, {ID: "legacy-upgrade", Kind: "upgrade", Name: "旧洗版"}}
		return nil
	})
	w = request(t, h, "GET", "/api/rule-presets", nil, cookie)
	var got []rulePreset
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got[0].Naming == nil || got[1].Upgrade == nil || a.store.snapshot().RulePresets[0].Naming != nil {
		t.Fatal("read defaults mutated state")
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "unchanged" || len(a.running) != 0 {
		t.Fatal("preview changed media", err)
	}
}
