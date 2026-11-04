package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultRuleFamiliesAndLastCardProtection(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	first := a.store.snapshot().RulePresets
	if len(first) != 3 {
		t.Fatal("missing default families", first)
	}
	for _, p := range first {
		copy := p
		if err := validatePreset(&copy); err != nil {
			t.Fatal(err)
		}
		if w := request(t, h, "DELETE", "/api/rule-presets/"+p.ID, nil, cookie); w.Code != 409 {
			t.Fatal("last preset removable", w.Code)
		}
		w := request(t, h, "POST", "/api/rule-presets", map[string]string{"kind": p.Kind, "name": "自定义"}, cookie)
		var added rulePreset
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &added) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := request(t, h, "DELETE", "/api/rule-presets/"+p.ID, nil, cookie); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if w := request(t, h, "DELETE", "/api/rule-presets/"+added.ID, nil, cookie); w.Code != 409 {
			t.Fatal("new last card removable", w.Code)
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if cards := reopened.snapshot().RulePresets; len(cards) != 3 || cards[0].Name != "自定义" {
		t.Fatal("defaults duplicated or replaced", cards)
	}
	st := State{RulePresets: []rulePreset{{ID: "default-upgrade", Kind: "naming", Name: "Existing"}}}
	ensureRulePresets(&st)
	ensureRulePresets(&st)
	if len(st.RulePresets) != 3 || st.RulePresets[0].Name != "Existing" || validateRulesWorkspace(st) != nil {
		t.Fatal("missing family migration changed existing rules", st.RulePresets)
	}
}

func TestLibraryIntervalAnchoredAcrossMonthsAndManualRuns(t *testing.T) {
	days := 7
	anchor := time.Date(2026, 1, 31, 13, 19, 0, 0, time.Local)
	card := quarkLibraryCard{Enabled: true}
	botLibrarySchedule(&card, &days, anchor)
	if card.Cron != "@every 168h" || card.ScheduleAnchor.Minute() != 0 || card.ScheduleAnchor.Location() != time.UTC {
		t.Fatal(card)
	}
	for _, now := range []time.Time{anchor, anchor.Add(3 * 24 * time.Hour), anchor.Add(7 * 24 * time.Hour)} {
		next := quarkLibraryNext(card, now)
		if next.In(time.Local).Hour() != 13 || next.Minute() != 0 || !next.After(now) || next.Sub(card.ScheduleAnchor)%(7*24*time.Hour) != 0 {
			t.Fatal("calendar drift", now, next)
		}
	}
	changed := card
	changed.Name = "仅改名称"
	setLibrarySchedule(&changed, card, anchor.Add(6*24*time.Hour))
	if changed.ScheduleAnchor != card.ScheduleAnchor {
		t.Fatal("manual edit shifted interval")
	}
	for _, value := range []string{"@every 1m", "@every 0h", "@every -1h", "@every 1h30s", "bad"} {
		if _, err := parseLibraryCron(value); err == nil {
			t.Fatal("invalid interval accepted", value)
		}
	}
	if _, err := cronParser.Parse(card.Cron); err == nil {
		t.Fatal("library descriptor leaked into ordinary task Cron")
	}
	for _, n := range []int{0, -1, 106752} {
		if validateLibraryUpdateDays(&n) == nil {
			t.Fatal("bad days", n)
		}
	}
	if libraryUpdateDays(nil) != 7 {
		t.Fatal("legacy default missing")
	}
	botLibrarySchedule(&card, &days, time.Date(2026, 1, 1, 13, 19, 0, 0, time.FixedZone("UTC-alias", 0)))
	raw, _ := json.Marshal(card)
	var restored quarkLibraryCard
	if err := json.Unmarshal(raw, &restored); err != nil || restored != card {
		t.Fatal("serialized schedule changed comparable card identity", err)
	}
}

func TestLibraryInternalIdentityMigratesLegacyAndRejectsReplacement(t *testing.T) {
	a := testApp(t)
	mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	directory := quarkLibraryRoot(card)
	marker := filepath.Join(directory, quarkLibraryMarker)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("new external marker generated", err)
	}
	old := a.store.snapshot().QuarkLibraryRuntime.Roots[card.ID]
	if old.Identity == "" {
		t.Fatal("missing internal authority")
	}
	if err := a.store.update(func(st *State) error {
		r := st.QuarkLibraryRuntime.Roots[card.ID]
		r.Identity = ""
		r.Token = "legacy-token"
		st.QuarkLibraryRuntime.Roots[card.ID] = r
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	writeTest(t, marker, "legacy-token")
	if _, err := a.runQuarkLibrary(a.ctx, card); err != nil {
		t.Fatal("legacy migration failed", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("legacy marker remained", err)
	}
	owner := a.store.snapshot().QuarkLibraryRuntime.Roots[card.ID]
	if owner.Identity != old.Identity {
		t.Fatal("same directory lost identity")
	}
	if err := os.Rename(directory, directory+"-old"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(directory+"-old", "Movie.mkv.share.strm"))
	if err != nil {
		t.Fatal(err)
	}
	writeTest(t, filepath.Join(directory, "Movie.mkv.share.strm"), string(raw))
	if _, err := a.runQuarkLibrary(a.ctx, card); err == nil {
		t.Fatal("copied directory acquired deletion authority")
	}
	if got, err := os.ReadFile(filepath.Join(directory, "Movie.mkv.share.strm")); err != nil || string(got) != string(raw) {
		t.Fatal("replacement altered", err)
	}
}

func TestLibraryExplicitInvalidityCleansOutputsAndMatchingMetadata(t *testing.T) {
	t.Run("quark", func(t *testing.T) {
		a := testApp(t)
		m := mockQuarkLibrary(t, a)
		card := newQuarkLibraryCard(t, a, "mkv")
		card.CascadeCleanup = "off"
		a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil })
		if _, err := a.runQuarkLibrary(a.ctx, card); err != nil {
			t.Fatal(err)
		}
		writeTest(t, filepath.Join(quarkLibraryRoot(card), "Movie.nfo"), "scraped")
		writeTest(t, filepath.Join(quarkLibraryRoot(card), "personal.txt"), "keep")
		m.fail = true
		if _, err := a.runQuarkLibrary(a.ctx, card); err == nil {
			t.Fatal("network failure accepted")
		}
		if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "Movie.nfo")); err != nil {
			t.Fatal("network error removed metadata", err)
		}
		m.fail = false
		m.invalid = "41007"
		result, err := a.runQuarkLibrary(a.ctx, card)
		if err == nil || result.Status != "invalid" {
			t.Fatal(result, err)
		}
		for _, name := range []string{"Movie.mkv.share.strm", "Movie.nfo"} {
			if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), name)); !os.IsNotExist(err) {
				t.Fatal("invalid output retained", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "personal.txt")); err != nil {
			t.Fatal("unrelated file removed", err)
		}
		if len(a.store.snapshot().QuarkLibraryCards) != 1 {
			t.Fatal("invalid card removed before retention deadline")
		}
	})
	t.Run("mobile", func(t *testing.T) {
		a := testApp(t)
		m, card := mockMobileLibrary(t, a)
		if _, err := a.runMobileLibrary(a.ctx, card); err != nil {
			t.Fatal(err)
		}
		writeTest(t, filepath.Join(quarkLibraryRoot(card), "剧集 [A] #1.nfo"), "scraped")
		writeTest(t, filepath.Join(quarkLibraryRoot(card), "personal.txt"), "keep")
		m.extra = func(r *http.Request) (string, bool) {
			if strings.HasSuffix(r.URL.Path, "getOutLinkGeneral") {
				return `{"resultCode":"0","data":{"getOutLinkGeneralResp":{"outLinkGeneral":[]}}}`, true
			}
			return "", false
		}
		result, err := a.runMobileLibrary(a.ctx, card)
		if err == nil || result.Status != "invalid" {
			t.Fatal(result, err)
		}
		for _, name := range []string{"剧集 [A] #1.mkv.share.strm", "剧集 [A] #1.nfo"} {
			if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), name)); !os.IsNotExist(err) {
				t.Fatal("invalid output retained", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "personal.txt")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTelegramLibraryQueueSummaryAndExistingLinkUpdates(t *testing.T) {
	a, cfg, _, messages := telegramLibraryFixture(t)
	output := t.TempDir()
	a.store.update(func(st *State) error {
		c := effectiveQuarkLibrary(*st)
		c.Directory, c.MediaExtensions, c.MetadataExtensions = output, "mkv", ""
		st.QuarkLibrary = &c
		return nil
	})
	for i := int64(1); i <= 2; i++ {
		update := telegramTextUpdate(i, 123, 123, "/share https://pan.quark.cn/s/example")
		update.Message.Date = time.Date(2026, 1, 31, 13, 19, 0, 0, time.Local).Unix()
		if err := a.handleTelegramUpdate(context.Background(), cfg, update); err != nil {
			t.Fatal(err)
		}
		if len(*messages) != int(i*2-1) || !strings.Contains((*messages)[len(*messages)-1], "夸克链库任务队列") {
			t.Fatal("queue notice missing or result raced it", *messages)
		}
		jobs := a.store.snapshot().TelegramRuntime.Jobs
		a.executeTelegramTransfer(a.ctx, jobs[len(jobs)-1])
		if len(*messages) != int(i*2) || !strings.HasPrefix((*messages)[len(*messages)-1], "夸克链库已更新\n本次完成：识别 1 条，") {
			t.Fatal("separate result missing", *messages)
		}
	}
	cards := a.store.snapshot().QuarkLibraryCards
	if len(cards) != 1 || cards[0].Cron != "@every 168h" || cards[0].ScheduleAnchor.In(time.Local).Hour() != 13 {
		t.Fatal("duplicate card or missing schedule", cards)
	}
	rows := []telegramTransfer{{Name: "A", Status: "completed", Processed: 14}, {Name: "B", Status: "completed", Processed: 10}, {Name: "C", Status: "completed"}, {Name: "D", Status: "completed"}}
	result := strings.Join(telegramLibraryResultLines(telegramTransfer{Provider: "mobile", Kind: "library"}, rows), "\n")
	if !strings.Contains(result, "识别 4 条，成功 2 条，失败 0 条，跳过 2 条。\n本次共处理 24 个文件。\n\n· A（14）") {
		t.Fatal(result)
	}
}

func TestLibrary115CardsAreManualSnapshotsAndBotReusesExisting(t *testing.T) {
	f := newVirtual115NameFixture(t, "")
	a := f.a
	output := t.TempDir()
	input := map[string]any{"url": "https://115.com/s/sample", "storageId": "115", "name": "Show", "directory": output, "directoryName": "Show", "enabled": false}
	w := request(t, f.handler, "POST", "/api/virtual-library/115", input, f.cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cards := a.store.snapshot().Library115Cards
	if len(cards) != 1 || cards[0].Cron != "" {
		t.Fatal(cards)
	}
	card := cards[0]
	card.Enabled = true
	a.store.update(func(st *State) error { st.Library115Cards[0] = card; return nil })
	result, err := a.runLibrary115(a.ctx, card)
	if err != nil || result.Generated != 2 || !result.NextRun.IsZero() {
		t.Fatal(result, err)
	}
	result, err = a.runLibrary115(a.ctx, card)
	if err != nil || result.Generated != 0 || result.Skipped != 2 {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), quarkLibraryMarker)); !os.IsNotExist(err) {
		t.Fatal("115 owner marker generated", err)
	}
	// A new spelling of the same share may not create a second snapshot card.
	input["url"] = "https://115cdn.com/s/sample"
	if w := request(t, f.handler, "POST", "/api/virtual-library/115", input, f.cookie); w.Code != 409 {
		t.Fatal("canonical duplicate accepted", w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(output, "Show", "第一季", "第1集 #1.mkv.share.strm"))
	if err != nil || !strings.Contains(string(raw), "/115-share/sample/第1集 %231.mkv") {
		t.Fatal(string(raw), err)
	}
	input["directoryName"] = "新目录"
	if w := request(t, f.handler, "PUT", "/api/virtual-library/115/"+card.ID, input, f.cookie); w.Code != 200 {
		t.Fatal("115 snapshot migration", w.Code, w.Body.String())
	}
	if data, err := os.ReadFile(filepath.Join(output, "新目录", "第一季", "第1集 #1.mkv.share.strm")); err != nil || string(data) != string(raw) {
		t.Fatal("115 migration lost registered content", err)
	}
	if _, err := os.Stat(filepath.Join(output, "Show", "第一季", "第1集 #1.mkv.share.strm")); !os.IsNotExist(err) {
		t.Fatal("115 migration retained owned old file", err)
	}
	backup := request(t, f.handler, "POST", "/api/config/backup", map[string]string{"password": "115-backup"}, f.cookie)
	plain, err := openBackup(backup.Body.Bytes(), "115-backup")
	var bundle configBundle
	if backup.Code != 200 || err != nil || json.Unmarshal(plain, &bundle) != nil || len(bundle.State.Library115Cards) != 1 || len(bundle.State.Library115Runtime.Files) != 0 {
		t.Fatal("115 config/runtime backup boundary", backup.Code, err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || len(reopened.snapshot().Library115Cards) != 1 {
		t.Fatal("115 cards not durable", err)
	}
}

func TestLibraryUpdateDaysAPIValidatesAndPersists(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	directory := t.TempDir()
	for _, provider := range []string{"quark", "mobile", "cas"} {
		cfg := map[string]any{"directory": directory, "mediaExtensions": "mkv", "updateDays": 3}
		if provider != "cas" {
			cfg["invalidDays"], cfg["retentionHours"], cfg["rotationHours"] = 15, 12, 24
			cfg["storageIds"] = []string{}
			cfg["metadataExtensions"] = "nfo"
		}
		if provider == "mobile" {
			cfg["playbackMode"] = "share"
		}
		endpoint := "/api/virtual-library/" + provider + "/settings"
		if w := request(t, h, "PUT", endpoint, cfg, nil); w.Code != 401 {
			t.Fatal("unprotected settings", w.Code)
		}
		if w := request(t, h, "PUT", endpoint, cfg, cookie); w.Code != 200 {
			t.Fatal(provider, w.Code, w.Body.String())
		}
		for _, value := range []any{0, -1, 1.5} {
			cfg["updateDays"] = value
			if w := request(t, h, "PUT", endpoint, cfg, cookie); w.Code != 400 {
				t.Fatal("invalid days accepted", provider, value, w.Code)
			}
		}
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	st := reopened.snapshot()
	for _, days := range []*int{effectiveQuarkLibrary(st).UpdateDays, effectiveMobileLibrary(st).UpdateDays, effectiveCASLibrary(st).UpdateDays} {
		if libraryUpdateDays(days) != 3 {
			t.Fatal("update period not durable")
		}
	}
}

func TestLibraryRemovedSourceCleansCompanionsButKeepsUnrelatedPhotos(t *testing.T) {
	for _, provider := range []string{"quark", "mobile"} {
		t.Run(provider, func(t *testing.T) {
			a := testApp(t)
			var card quarkLibraryCard
			var removeSource func()
			var run func() (quarkLibraryResult, error)
			stem := "Movie"
			if provider == "quark" {
				m := mockQuarkLibrary(t, a)
				card = newQuarkLibraryCard(t, a, "mkv")
				card.CascadeCleanup = "off"
				a.store.update(func(st *State) error { st.QuarkLibraryCards[0] = card; return nil })
				removeSource = func() { m.files = nil }
				run = func() (quarkLibraryResult, error) { return a.runQuarkLibrary(a.ctx, card) }
			} else {
				m, c := mockMobileLibrary(t, a)
				card = c
				stem = "剧集 [A] #1"
				removeSource = func() { m.list["root"] = nil }
				run = func() (quarkLibraryResult, error) { return a.runMobileLibrary(a.ctx, card) }
			}
			if _, err := run(); err != nil {
				t.Fatal(err)
			}
			writeTest(t, filepath.Join(quarkLibraryRoot(card), stem+".nfo"), "scraped")
			writeTest(t, filepath.Join(quarkLibraryRoot(card), "FamilyPhoto.jpg"), "personal")
			removeSource()
			if _, err := run(); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{stem + ".mkv.share.strm", stem + ".nfo"} {
				if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), name)); !os.IsNotExist(err) {
					t.Fatal("removed source retained output", name, err)
				}
			}
			if raw, err := os.ReadFile(filepath.Join(quarkLibraryRoot(card), "FamilyPhoto.jpg")); err != nil || string(raw) != "personal" {
				t.Fatal("unrelated image removed", err)
			}
		})
	}
}

func TestQuarkArchivedCardNeverGeneratesDuringInvalidityMaintenance(t *testing.T) {
	a := testApp(t)
	m := mockQuarkLibrary(t, a)
	card := newQuarkLibraryCard(t, a, "mkv")
	if _, err := a.runQuarkLibrary(a.ctx, card); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	a.store.update(func(st *State) error {
		r := st.QuarkLibraryRuntime.Results[card.ID]
		r.Status, r.InvalidSince, r.LastRun = "invalid", old, old
		st.QuarkLibraryRuntime.Results[card.ID] = r
		return nil
	})
	m.files = append(m.files, shareEntry{ID: "new", Name: "New.mkv", Size: 10})
	a.quarkLibraryTick(time.Now())
	r := a.store.snapshot().QuarkLibraryRuntime.Results[card.ID]
	if r.Status != "idle" || !r.LastRun.Equal(old) || !r.NextRun.IsZero() {
		t.Fatal("archived maintenance executed generation", r)
	}
	if _, err := os.Stat(filepath.Join(quarkLibraryRoot(card), "New.mkv.share.strm")); !os.IsNotExist(err) {
		t.Fatal("archived library updated", err)
	}
}

func TestTelegramLibraryLargeSummaryStaysWithinTelegramLimit(t *testing.T) {
	jobs := []telegramTransfer{}
	for i := 0; i < 50; i++ {
		jobs = append(jobs, telegramTransfer{Name: strings.Repeat("🌌名称", 100), Status: "failed", Processed: 20000, Result: strings.Repeat("错误信息", 200)})
	}
	result := strings.Join(telegramLibraryResultLines(telegramTransfer{Provider: "mobile", Kind: "library"}, jobs), "\n")
	units := 0
	for _, r := range result {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units > 4096 || strings.Count(result, "\n· ") != 50 {
		t.Fatal("result exceeded provider limit or dropped links", units)
	}
}
