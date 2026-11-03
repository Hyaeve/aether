package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func casLibraryFixture(t *testing.T, a *App) quarkLibraryCard {
	t.Helper()
	card := quarkLibraryCard{ID: "cas-card", Secret: "https://yun.139.com/shareweb/one?pwd=Ab12", Name: "CAS影视", Directory: t.TempDir(), DirectoryName: "作品", MediaExtensions: "mkv", Enabled: true, CascadeCleanup: "off"}
	if err := a.store.update(func(st *State) error { st.CASLibraryCards = []quarkLibraryCard{card}; initCASLibrary(st); return nil }); err != nil {
		t.Fatal(err)
	}
	return card
}
func TestCASLibraryDirectHashesIncrementalAndGuard(t *testing.T) {
	a := testApp(t)
	card := casLibraryFixture(t, a)
	calls := mockMobileGeneration(t, "valid")
	result, err := a.runCASLibrary(context.Background(), card, nil)
	if err != nil || result.Generated != 1 {
		t.Fatal(result, err)
	}
	file := filepath.Join(quarkLibraryRoot(card), "Season 1", "Episode.mkv.cas.strm")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	claim := generatedCASClaim(t, raw)
	if claim.SHA256 != strings.Repeat("a", 64) || claim.MD5 != "" {
		t.Fatal("invalid portable CAS", claim)
	}
	result, err = a.runCASLibrary(context.Background(), card, nil)
	if err != nil || result.Generated != 0 || result.Skipped != 1 {
		t.Fatal(result, err)
	}
	now, _ := os.ReadFile(file)
	if string(now) != string(raw) {
		t.Fatal("existing output rewritten")
	}
	if len(a.store.snapshot().GenerationJobs) != 1 || *calls != 8 {
		t.Fatal("incremental run queued transfer or generated duplicate")
	}
	os.Remove(file)
	checks := 0
	result, err = a.runCASLibrary(context.Background(), card, func() error {
		checks++
		if checks > 1 {
			return errors.New("authorization revoked")
		}
		return nil
	})
	if err == nil || result.Generated != 0 {
		t.Fatal("revoked operation wrote output", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("revoked output exists")
	}
}
func TestCASLibraryAPIBackupRestartAndUnknownReceipt(t *testing.T) {
	a := testApp(t)
	h := a.Handler(t.TempDir())
	if request(t, h, "GET", "/api/virtual-library/cas", nil, nil).Code == 200 {
		t.Fatal("unauthenticated settings")
	}
	cookie := setupTest(t, a, h)
	directory := t.TempDir()
	if w := request(t, h, "PUT", "/api/virtual-library/cas/settings", casLibrarySettings{directory, "mkv;mp4"}, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := map[string]any{"url": "https://pan.quark.cn/s/wrong", "name": "作品", "directory": directory, "directoryName": "作品", "enabled": true}
	if w := request(t, h, "POST", "/api/virtual-library/cas", body, cookie); w.Code != 400 {
		t.Fatal("wrong provider accepted", w.Code)
	}
	body["url"] = "https://yun.139.com/shareweb/one?pwd=Ab12"
	w := request(t, h, "POST", "/api/virtual-library/cas", body, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	card := a.store.snapshot().CASLibraryCards[0]
	if w := request(t, h, "POST", "/api/virtual-library/cas", body, cookie); w.Code != 409 {
		t.Fatal("duplicate directory accepted", w.Code)
	}
	if err := a.store.update(func(st *State) error {
		initCASLibrary(st)
		st.CASLibraryRuntime.Results[card.ID] = quarkLibraryResult{Status: "running", NextRun: time.Now()}
		st.GenerationJobs = []GenerationJob{{ID: "unknown-cas", LibrarySource: casLibrarySourceStamp(card.Secret), Status: "error", Temporary: []GenerationTemporary{{Phase: "submitting"}}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !casLibraryUncertain(a.store.snapshot(), card) {
		t.Fatal("unknown receipt not held")
	}
	if w := request(t, h, "POST", "/api/virtual-library/cas/"+card.ID+"/run", nil, cookie); w.Code != 409 {
		t.Fatal("unknown replay accepted", w.Code)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil {
		t.Fatal(err)
	}
	st := reopened.snapshot()
	if len(st.CASLibraryCards) != 1 || st.CASLibraryRuntime.Results[card.ID].Status != "interrupted" || !casLibraryUncertain(st, card) {
		t.Fatal("restart lost config/hold")
	}
	w = request(t, h, "POST", "/api/config/backup", map[string]string{"password": "cas-test"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	plain, err := openBackup(w.Body.Bytes(), "cas-test")
	if err != nil {
		t.Fatal(err)
	}
	var bundle configBundle
	if json.Unmarshal(plain, &bundle) != nil || len(bundle.State.CASLibraryCards) != 1 || bundle.State.CASLibraryRuntime.Results != nil || len(bundle.State.GenerationJobs) != 0 || validateBundle(bundle) != nil {
		t.Fatal("backup mixed run state and config")
	}
	var importBody bytes.Buffer
	form := multipart.NewWriter(&importBody)
	file, _ := form.CreateFormFile("file", "cas.aether")
	file.Write(w.Body.Bytes())
	form.WriteField("password", "cas-test")
	form.Close()
	req := httptest.NewRequest("POST", "/api/config/import", &importBody)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(cookie)
	imported := httptest.NewRecorder()
	h.ServeHTTP(imported, req)
	if imported.Code != 200 {
		t.Fatal(imported.Code, imported.Body.String())
	}
	if err := applyPendingConfig(a.store); err != nil {
		t.Fatal(err)
	}
	if !casLibraryUncertain(a.store.snapshot(), card) || len(a.store.snapshot().CASLibraryRuntime.Results) != 1 {
		t.Fatal("config import erased CAS receipt or runtime")
	}
	if w := request(t, h, "DELETE", "/api/virtual-library/cas/"+card.ID, nil, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	// Removing/readding a record must not remove an uncertain cloud write.
	if !casLibraryUncertain(a.store.snapshot(), card) {
		t.Fatal("removal cleared unknown write")
	}
	changedPass := card
	changedPass.Secret = "https://yun.139.com/shareweb/one?pwd=Changed"
	if !casLibraryUncertain(a.store.snapshot(), changedPass) {
		t.Fatal("changed password bypassed unknown write")
	}
}

func captureTelegramResults(t *testing.T) *[]string {
	t.Helper()
	cloud := apiClient.Transport
	messages := []string{}
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			return cloud.RoundTrip(r)
		}
		var in struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		messages = append(messages, in.Text)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":77}}`)), Request: r}, nil
	})
	return &messages
}
func TestTelegramCASCommandProviderAndNativeLibraryRouting(t *testing.T) {
	for _, command := range []string{"/cas", "/share"} {
		t.Run(command, func(t *testing.T) {
			a, cfg := telegramFixture(t)
			if command == "/cas" {
				mockMobileGeneration(t, "valid")
				dir := t.TempDir()
				a.store.update(func(st *State) error { st.CASLibrary = &casLibrarySettings{dir, "mkv"}; return nil })
			} else {
				_, _ = mockMobileLibrary(t, a)
				a.store.update(func(st *State) error {
					st.MobileLibraryCards = nil
					c := effectiveMobileLibrary(*st)
					c.Directory = t.TempDir()
					st.MobileLibrary = &c
					return nil
				})
			}
			messages := captureTelegramResults(t)
			if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, command+" https://yun.139.com/shareweb/one?pwd=Ab12")); err != nil {
				t.Fatal(err)
			}
			job := a.store.snapshot().TelegramRuntime.Jobs[0]
			want := "library"
			if command == "/cas" {
				want = "cas-library"
			}
			if job.Kind != want {
				t.Fatal("wrong library route", job.Kind)
			}
			a.executeTelegramTransfer(context.Background(), job)
			st := a.store.snapshot()
			if st.TelegramRuntime.Jobs[0].Status != "completed" || st.TelegramRuntime.Jobs[0].Processed != 1 {
				t.Fatal(st.TelegramRuntime.Jobs[0])
			}
			if len(*messages) != 1 || !strings.Contains((*messages)[0], "· ") || !strings.Contains((*messages)[0], "（1）") {
				t.Fatal("unmerged result", *messages)
			}
			if command == "/cas" && len(st.MobileLibraryCards) != 0 || command == "/share" && len(st.CASLibraryCards) != 0 {
				t.Fatal("CAS/share storage crossed")
			}
		})
	}
	for _, link := range []string{"https://pan.quark.cn/s/a", "https://115.com/s/a", "https://www.aliyundrive.com/s/a"} {
		t.Run(link, func(t *testing.T) {
			a, cfg := telegramFixture(t)
			shareMock(t, func(r *http.Request) string {
				if r.URL.Host != "api.telegram.org" {
					t.Fatal("wrong command reached cloud")
				}
				return `{"ok":true,"result":{"message_id":77}}`
			})
			if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/cas "+link)); err != nil {
				t.Fatal(err)
			}
			if len(a.store.snapshot().TelegramRuntime.Jobs) != 0 {
				t.Fatal("/cas accepted other provider")
			}
		})
	}
}
func TestTelegramMergedResultsWaitForGroupAndDoNotReplay(t *testing.T) {
	a, cfg := telegramFixture(t)
	shareMock(t, func(r *http.Request) string { return `{"ok":true,"result":{"message_id":77}}` })
	messages := captureTelegramResults(t)
	jobs := []telegramTransfer{}
	for i, name := range []string{"莫语丨者", "现在就出发 第4季(2026)"} {
		key := []string{"group", "second"}[i]
		jobs = append(jobs, telegramTransfer{ID: key, GroupID: "group", BatchID: "batch", GroupSize: 2, Provider: "quark", Kind: "library", Name: name, Auth: telegramDigest(cfg), ChatID: 123, UserID: 123, Status: "running"})
	}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = jobs; return nil })
	a.telegramComplete(context.Background(), jobs[0], "completed", jobs[0].Name, 14, 0, "成功")
	if len(*messages) != 0 {
		t.Fatal("sent before other link finished")
	}
	a.telegramComplete(context.Background(), jobs[1], "completed", jobs[1].Name, 10, 0, "成功")
	if len(*messages) != 1 || (*messages)[0] != "夸克分享STRM\n· 莫语丨者（14）\n· 现在就出发 第4季(2026)（10）" {
		t.Fatal(*messages)
	}
	a.sendTelegramGroupResult(context.Background(), jobs[0])
	if len(*messages) != 1 {
		t.Fatal("result replayed")
	}
}

func TestTelegram115LibraryDefaultsAndShareOutput(t *testing.T) {
	a, cfg := telegramFixture(t)
	s := Storage{ID: "115", Name: "115主账号", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	a.store.update(func(st *State) error { st.Storages = append(st.Storages, s); return nil })
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	directory := t.TempDir()
	if w := request(t, h, "PUT", "/api/virtual-library/115/settings", virtual115Defaults{s.ID, directory}, cookie); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host == "api.telegram.org" {
			return `{"ok":true,"result":{"message_id":77}}`
		}
		if r.Method != "GET" {
			t.Fatal("115 library wrote cloud")
		}
		if r.URL.Query().Get("cid") == "99" {
			return `{"state":true,"data":{"count":1,"list":[{"fid":"3416909918244961440","n":"片名.mkv","s":"10","sha":"0123456789012345678901234567890123456789"}]}}`
		}
		return `{"state":true,"data":{"count":1,"list":[{"cid":"99","n":"Show"}]}}`
	})
	messages := captureTelegramResults(t)
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/share https://115.com/s/swf1h7936et?password=YJFX")); err != nil {
		t.Fatal(err)
	}
	job := a.store.snapshot().TelegramRuntime.Jobs[0]
	a.executeTelegramTransfer(context.Background(), job)
	final := a.store.snapshot().TelegramRuntime.Jobs[0]
	if final.Status != "completed" || final.Processed != 1 {
		t.Fatal(final)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "Show", "片名.mkv.share.strm"))
	if err != nil || !strings.Contains(string(raw), "/115-share/swf1h7936et-YJFX/片名.mkv") {
		t.Fatal("wrong 115 output", err, string(raw))
	}
	if len(*messages) != 1 || !strings.Contains((*messages)[0], "115分享STRM\n· Show（1）") {
		t.Fatal(*messages)
	}
	a.executeTelegramTransfer(context.Background(), job)
	if len(*messages) != 1 {
		t.Fatal("115 command replayed")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().Virtual115.Directory != directory {
		t.Fatal("115 defaults not durable", err)
	}
}

func TestTelegramMergedTransferHeaderAndUnknownDelivery(t *testing.T) {
	a, cfg := telegramFixture(t)
	attempts := 0
	messages := []string{}
	shareMock(t, func(r *http.Request) string {
		attempts++
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		messages = append(messages, body.Text)
		return `{"ok":false}`
	})
	target := a.store.snapshot().Notifications.Targets[0]
	jobs := []telegramTransfer{{ID: "g", GroupID: "g", BatchID: "b", GroupSize: 2, Provider: "quark", Auth: telegramDigest(cfg), ChatID: 123, UserID: 123, Status: "submitted", Name: "分享甲", Processed: 14, Selected: 0, Targets: []telegramTarget{target}}, {ID: "j2", GroupID: "g", BatchID: "b", GroupSize: 2, Provider: "quark", Auth: telegramDigest(cfg), ChatID: 123, UserID: 123, Status: "failed", Name: "分享乙", Processed: 0, Result: "读取分享失败", Selected: 0, Targets: []telegramTarget{target}}}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = jobs; return nil })
	a.sendTelegramGroupResult(context.Background(), jobs[0])
	a.sendTelegramGroupResult(context.Background(), jobs[1])
	if attempts != 1 || strings.Count(messages[0], "夸克影视") != 1 || !strings.Contains(messages[0], "· 分享甲（已提交14，待网盘确认）") || !strings.Contains(messages[0], "· 分享乙（未完成0；读取分享失败）") {
		t.Fatal("wrong merged transfer or notification replay", attempts, messages)
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil || !reopened.snapshot().TelegramRuntime.Jobs[0].ResultSent {
		t.Fatal("unknown delivery intent lost", err)
	}
}
func TestTelegramMergedLongUnicodeNamesStayInOneMessage(t *testing.T) {
	a, cfg := telegramFixture(t)
	shareMock(t, func(r *http.Request) string { return `{"ok":true,"result":{"message_id":77}}` })
	messages := captureTelegramResults(t)
	jobs := []telegramTransfer{}
	for i := 0; i < 50; i++ {
		key := id()
		if i == 0 {
			key = "g"
		}
		jobs = append(jobs, telegramTransfer{ID: key, GroupID: "g", BatchID: "b", GroupSize: 50, Provider: "mobile", Kind: "library", Auth: telegramDigest(cfg), ChatID: 123, UserID: 123, Status: "completed", Name: strings.Repeat("🌟长标题", 100), Processed: i + 1})
	}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = jobs; return nil })
	a.sendTelegramGroupResult(context.Background(), jobs[0])
	if len(*messages) != 1 || strings.Count((*messages)[0], "\n· ") != 50 {
		t.Fatal("results split or omitted")
	}
	units := 0
	for _, r := range (*messages)[0] {
		units++
		if r > 0xffff {
			units++
		}
	}
	if units > 4096 {
		t.Fatal("Telegram message too long", units)
	}
}
