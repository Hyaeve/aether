package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func telegramLibraryFixture(t *testing.T) (*App, telegramConfig, *quarkLibraryMock, *[]string) {
	t.Helper()
	a, cfg := telegramFixture(t)
	m := mockQuarkLibrary(t, a)
	cloud := apiClient.Transport
	messages := []string{}
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			return cloud.RoundTrip(r)
		}
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		messages = append(messages, body.Text)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":70}}`)), Request: r}, nil
	})
	return a, cfg, m, &messages
}

func TestTelegramLibraryCommandAndOutputDefaults(t *testing.T) {
	for _, prefix := range []string{"/share ", "虚拟链库 "} {
		t.Run(prefix, func(t *testing.T) {
			a, cfg, m, messages := telegramLibraryFixture(t)
			if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, prefix+"https://pan.quark.cn/s/example")); err != nil {
				t.Fatal(err)
			}
			jobs := a.store.snapshot().TelegramRuntime.Jobs
			if len(jobs) != 1 || jobs[0].Kind != "library" {
				t.Fatal("command must route to library, not transfer", jobs)
			}
			a.executeTelegramTransfer(context.Background(), jobs[0])
			st := a.store.snapshot()
			if len(st.QuarkLibraryCards) != 1 || st.QuarkLibraryCards[0].Enabled || st.QuarkLibraryCards[0].Directory != "" || len(st.QuarkLibraryRuntime.Files) != 0 || m.saves != 0 {
				t.Fatal("unconfigured output must create only a disabled card")
			}
			if !strings.Contains(strings.Join(*messages, "\n"), "未启用") {
				t.Fatal("missing disabled draft feedback", *messages)
			}
			reopened, err := NewStore(a.store.dir)
			if err != nil || len(reopened.snapshot().QuarkLibraryCards) != 1 {
				t.Fatal("draft did not survive reload", err)
			}
			raw, _ := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
			if strings.Contains(string(raw), "pan.quark.cn/s/example") {
				t.Fatal("share secret persisted unencrypted")
			}
		})
	}
}

func TestTelegramLibraryConfiguredOutputAndNoReplay(t *testing.T) {
	a, cfg, m, _ := telegramLibraryFixture(t)
	output := t.TempDir()
	a.store.update(func(st *State) error {
		settings := effectiveQuarkLibrary(*st)
		settings.Directory, settings.MediaExtensions, settings.MetadataExtensions = output, "mkv", ""
		st.QuarkLibrary = &settings
		return nil
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/share https://pan.quark.cn/s/example")); err != nil {
		t.Fatal(err)
	}
	job := a.store.snapshot().TelegramRuntime.Jobs[0]
	a.executeTelegramTransfer(context.Background(), job)
	st := a.store.snapshot()
	if len(st.QuarkLibraryCards) != 1 || !st.QuarkLibraryCards[0].Enabled || st.QuarkLibraryCards[0].Directory != output || len(st.QuarkLibraryRuntime.Files) != 1 || m.saves != 0 {
		t.Fatal("configured command did not generate STRM without transferring media", st.QuarkLibraryCards)
	}
	if _, err := os.Stat(filepath.Join(output, "Series", "Movie.mkv.strm")); err != nil {
		t.Fatal(err)
	}
	a.executeTelegramTransfer(context.Background(), job)
	if len(a.store.snapshot().QuarkLibraryCards) != 1 {
		t.Fatal("stale execution replayed")
	}
}

func TestTelegramMultipleSharesRequireConfirmation(t *testing.T) {
	a, cfg, _, _ := telegramLibraryFixture(t)
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "https://pan.quark.cn/s/one\nhttps://pan.quark.cn/s/two")); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 2 || jobs[0].Status != "confirming" || jobs[1].Status != "waiting" {
		t.Fatal("multiple links must not bypass confirmation with one target", jobs)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 70, "c:"+jobs[0].ID+":0"); err != nil {
		t.Fatal(err)
	}
	for _, job := range a.store.snapshot().TelegramRuntime.Jobs {
		if job.Status != "queued" {
			t.Fatal("confirmed group not queued", job.Status)
		}
	}
}

func TestTelegramPlainAndUnknownCommandsNeverGenerateLibrary(t *testing.T) {
	for _, prefix := range []string{"", "/sharex ", "/unknown "} {
		t.Run(prefix, func(t *testing.T) {
			a, cfg, _, _ := telegramLibraryFixture(t)
			if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, prefix+"https://pan.quark.cn/s/example")); err != nil {
				t.Fatal(err)
			}
			for _, job := range a.store.snapshot().TelegramRuntime.Jobs {
				if job.Kind == "library" {
					t.Fatal("non-library command reached library")
				}
			}
		})
	}
}

func TestTelegramMixedProviderBatchesAdvanceAfterFinishOrCancel(t *testing.T) {
	a, cfg, _, messages := telegramLibraryFixture(t)
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages,
			Storage{ID: "s115", Name: "115影视", Type: "115", Enabled: true},
			Storage{ID: "mobile", Name: "移动影视", Type: "mobile", Enabled: true, Config: map[string]string{"authorization": "mock"}})
		st.Notifications.Targets = append(st.Notifications.Targets,
			telegramTarget{ID: "d115", Provider: "115", StorageID: "s115", Parent: "0", Path: "/115"},
			telegramTarget{ID: "dmobile", Provider: "mobile", StorageID: "mobile", Parent: "root", Path: "/Mobile"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	text := "https://pan.quark.cn/s/quark\nhttps://yun.139.com/shareweb/#/w/i/mobile\nhttps://115.com/s/first\nhttps://115.com/s/second"
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, text)); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 4 || jobs[0].Provider != "115" || jobs[1].Provider != "115" || jobs[2].Provider != "mobile" || jobs[3].Provider != "quark" {
		t.Fatal("provider batching is not 115/mobile/Quark")
	}
	if len(*messages) != 1 || !strings.Contains((*messages)[0], "2条链接") || jobs[2].Status != "waiting" || jobs[3].Status != "waiting" {
		t.Fatal("later providers prompted before preceding batch finished", *messages)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 70, "c:"+jobs[0].ID+":0"); err != nil {
		t.Fatal(err)
	}
	a.finishTelegram(jobs[0].ID, "completed", "first result", 0)
	if err := a.advanceTelegramBatch(context.Background(), jobs[0].BatchID); err != nil {
		t.Fatal(err)
	}
	if a.store.snapshot().TelegramRuntime.Jobs[2].Status != "waiting" {
		t.Fatal("did not wait for every link in provider batch")
	}
	a.finishTelegram(jobs[1].ID, "completed", "second result", 0)
	if err := a.advanceTelegramBatch(context.Background(), jobs[0].BatchID); err != nil {
		t.Fatal(err)
	}
	if got := a.store.snapshot().TelegramRuntime.Jobs[2]; got.Status != "confirming" || got.MessageID != 70 {
		t.Fatal("mobile batch not confirmed separately", got.Status)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 70, "x:"+jobs[2].ID+":0"); err != nil {
		t.Fatal(err)
	}
	if err := a.advanceTelegramBatch(context.Background(), jobs[0].BatchID); err != nil {
		t.Fatal(err)
	}
	if a.store.snapshot().TelegramRuntime.Jobs[3].Status != "confirming" {
		t.Fatal("Quark batch not offered after cancelling mobile batch")
	}
	a.cleanupTelegramInteractions(context.Background(), time.Now().Add(11*time.Minute))
	for _, job := range a.store.snapshot().TelegramRuntime.Jobs {
		if telegramActive(job.Status) || job.Link != "" {
			t.Fatal("expired batch retained a live share capability", job.Status)
		}
	}
}

func TestTelegramWaitingBatchRestartAndChangedCredentials(t *testing.T) {
	a, cfg, _, _ := telegramLibraryFixture(t)
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "https://pan.quark.cn/s/one\nhttps://pan.quark.cn/s/two")); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if err := a.store.update(func(st *State) error {
		st.Storages[0].Config["cookie"] = "changed-identity"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 70, "c:"+jobs[0].ID+":0"); err == nil {
		t.Fatal("credential change must invalidate group confirmation")
	}
	reopened, err := NewStore(a.store.dir)
	if err == nil {
		err = reopened.initRuntime(a.store.runtimeDir)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range reopened.snapshot().TelegramRuntime.Jobs {
		if job.Status != "interrupted" || job.Link != "" {
			t.Fatal("waiting group replayed after restart", job.Status)
		}
	}
}

func TestTelegramLibraryProviderResultsAndChangedDefaults(t *testing.T) {
	a, cfg, m, messages := telegramLibraryFixture(t)
	text := "/share https://pan.quark.cn/s/quark\nhttps://yun.139.com/shareweb/#/w/i/mobile\nhttps://115.com/s/first"
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, text)); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 3 || jobs[0].Provider != "115" || jobs[1].Provider != "mobile" || jobs[2].Provider != "quark" {
		t.Fatal("library command provider order", len(jobs))
	}
	for _, job := range jobs {
		a.executeTelegramTransfer(context.Background(), job)
	}
	feedback := strings.Join(*messages, "\n")
	if len(a.store.snapshot().QuarkLibraryCards) != 1 || strings.Count(feedback, "暂未接入") != 2 || !strings.Contains(feedback, "未启用") || m.saves != 0 {
		t.Fatal("unsupported providers must report, not fall back to share transfer", feedback)
	}
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(2, 123, 123, "虚拟链库 https://pan.quark.cn/s/changed")); err != nil {
		t.Fatal(err)
	}
	st := a.store.snapshot()
	job := st.TelegramRuntime.Jobs[len(st.TelegramRuntime.Jobs)-1]
	a.store.update(func(st *State) error {
		settings := effectiveQuarkLibrary(*st)
		settings.Directory = t.TempDir()
		st.QuarkLibrary = &settings
		return nil
	})
	a.executeTelegramTransfer(context.Background(), job)
	if len(a.store.snapshot().QuarkLibraryCards) != 1 {
		t.Fatal("queued library command silently used changed defaults")
	}
}

func TestTelegramLibraryWithoutStorageStillCreatesDisabledDraft(t *testing.T) {
	a, cfg, _, messages := telegramLibraryFixture(t)
	a.store.update(func(st *State) error {
		st.Storages, st.Notifications.Targets = nil, nil
		return nil
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/share https://pan.quark.cn/s/example")); err != nil {
		t.Fatal(err)
	}
	a.executeTelegramTransfer(context.Background(), a.store.snapshot().TelegramRuntime.Jobs[0])
	st := a.store.snapshot()
	if len(st.QuarkLibraryCards) != 1 || st.QuarkLibraryCards[0].Enabled || len(st.QuarkLibraryRuntime.Files) != 0 || !strings.Contains(strings.Join(*messages, "\n"), "未启用") {
		t.Fatal("no-storage command must remain a disabled draft")
	}
	for _, log := range st.Logs {
		if strings.Contains(log.Message, "example") {
			t.Fatal("unresolved share code leaked through the fallback card name")
		}
	}
}

func TestTelegramProviderKindsStayInContiguousGroups(t *testing.T) {
	a, cfg, _, _ := telegramLibraryFixture(t)
	a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "s115", Type: "115", Enabled: true})
		st.Notifications.Targets = append(st.Notifications.Targets, telegramTarget{ID: "115", Provider: "115", StorageID: "s115", Parent: "0", Path: "/", ED2KOffline: true})
		return nil
	})
	links := []telegramLink{
		{Provider: "115", Link: "https://115.com/s/first"},
		{Provider: "115", Kind: "ed2k", Link: "ed2k://placeholder"},
		{Provider: "115", Link: "https://115.com/s/second"},
	}
	m := *telegramTextUpdate(1, 123, 123, "").Message
	if err := a.queueTelegramLinks(context.Background(), cfg, m, links, false); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 3 || jobs[0].GroupID != jobs[1].GroupID || jobs[0].GroupSize != 2 || jobs[2].Kind != "ed2k" {
		t.Fatal("same provider/use split into interleaved groups")
	}
}

func TestTelegramLibraryClearedDefaultDoesNotRunExistingCard(t *testing.T) {
	a, cfg, m, messages := telegramLibraryFixture(t)
	card := newQuarkLibraryCard(t, a, "mkv")
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/share "+card.Secret)); err != nil {
		t.Fatal(err)
	}
	a.executeTelegramTransfer(context.Background(), a.store.snapshot().TelegramRuntime.Jobs[0])
	if len(a.store.snapshot().QuarkLibraryRuntime.Files) != 0 || m.saves != 0 || !strings.Contains(strings.Join(*messages, "\n"), "默认生成位置") {
		t.Fatal("missing global output must not start an existing card through TG")
	}
}

func TestTelegramFiftyLinksAndActiveGroupLimit(t *testing.T) {
	a, cfg, _, _ := telegramLibraryFixture(t)
	links := []string{}
	for i := 0; i < 50; i++ {
		links = append(links, fmt.Sprintf("https://pan.quark.cn/s/share%d", i))
	}
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, strings.Join(links, "\n"))); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 50 || jobs[0].GroupSize != 50 || jobs[0].Status != "confirming" {
		t.Fatal("50 links must share a single confirmation group")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 70, "x:"+jobs[0].ID+":0"); err != nil {
		t.Fatal(err)
	}
	for _, job := range a.store.snapshot().TelegramRuntime.Jobs {
		if telegramActive(job.Status) || job.Link != "" {
			t.Fatal("cancel left a live batch member")
		}
	}
	a.store.update(func(st *State) error {
		for i := 0; i < 32; i++ {
			key := fmt.Sprintf("active%d", i)
			st.TelegramRuntime.Jobs = append(st.TelegramRuntime.Jobs, telegramTransfer{ID: key, GroupID: key, Status: "queued"})
		}
		return nil
	})
	m := *telegramTextUpdate(2, 123, 123, "").Message
	if err := a.queueTelegramLinks(context.Background(), cfg, m, []telegramLink{{Provider: "quark", Link: links[0]}}, false); err == nil {
		t.Fatal("active group limit bypassed")
	}
	if len(a.store.snapshot().TelegramRuntime.Jobs) != 82 {
		t.Fatal("rejected group partially persisted")
	}
}

func TestTelegramLibraryChangedSettingsDuringReadStopsPublication(t *testing.T) {
	a, cfg, m, _ := telegramLibraryFixture(t)
	output := t.TempDir()
	a.store.update(func(st *State) error {
		settings := effectiveQuarkLibrary(*st)
		settings.Directory, settings.MediaExtensions, settings.MetadataExtensions = output, "mkv", ""
		st.QuarkLibrary = &settings
		return nil
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/share https://pan.quark.cn/s/example")); err != nil {
		t.Fatal(err)
	}
	transport := apiClient.Transport
	changed := false
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if strings.HasSuffix(r.URL.Path, "/detail") && r.URL.Query().Get("pdir_fid") == "root-folder" && !changed {
			changed = true
			if err := a.store.update(func(st *State) error {
				st.QuarkLibrary.Directory = ""
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		return response, err
	})
	a.executeTelegramTransfer(context.Background(), a.store.snapshot().TelegramRuntime.Jobs[0])
	st := a.store.snapshot()
	if !changed || st.TelegramRuntime.Jobs[0].Status != "failed" || len(st.QuarkLibraryRuntime.Files) != 0 || m.saves != 0 {
		t.Fatal("changed defaults did not stop publication/cloud writes")
	}
	items, err := os.ReadDir(output)
	if err != nil || len(items) != 0 {
		t.Fatal("output changed after settings invalidated the generation", items, err)
	}
}

func TestTelegramLibraryTrustedCookieRotation(t *testing.T) {
	a, cfg, _, _ := telegramLibraryFixture(t)
	a.store.update(func(st *State) error {
		settings := effectiveQuarkLibrary(*st)
		settings.Directory, settings.MediaExtensions, settings.MetadataExtensions = t.TempDir(), "mkv", ""
		st.QuarkLibrary = &settings
		return nil
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "/share https://pan.quark.cn/s/example")); err != nil {
		t.Fatal(err)
	}
	transport := apiClient.Transport
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		response, err := transport.RoundTrip(r)
		if err == nil && strings.HasSuffix(r.URL.Path, "/token") {
			response.Header.Add("Set-Cookie", "__puus=trusted-rotation; Path=/; Secure")
		}
		return response, err
	})
	job := a.store.snapshot().TelegramRuntime.Jobs[0]
	a.executeTelegramTransfer(context.Background(), job)
	st := a.store.snapshot()
	if st.TelegramRuntime.Jobs[0].Status != "completed" || len(st.QuarkLibraryRuntime.Files) != 1 {
		t.Fatal("trusted automatic cookie rotation invalidated the library job")
	}
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(2, 123, 123, "/share https://pan.quark.cn/s/next")); err != nil {
		t.Fatal(err)
	}
	st = a.store.snapshot()
	job = st.TelegramRuntime.Jobs[len(st.TelegramRuntime.Jobs)-1]
	prior := st.Storages[0]
	a.store.update(func(st *State) error {
		st.Storages[0].Config["cookie"] = "manually-changed-account"
		return nil
	})
	late := prior
	late.Config = map[string]string{"cookie": "__puus=late-response"}
	if err := a.persistQuarkCookies(prior, late); err != nil {
		t.Fatal(err)
	}
	if telegramLibraryValid(a.store.snapshot(), job) {
		t.Fatal("late automatic response adopted a manually changed account")
	}
}

func TestTelegramBatchUnavailableProviderAndGroupExpiry(t *testing.T) {
	a, cfg, m, messages := telegramLibraryFixture(t)
	text := "https://pan.quark.cn/s/one\nhttps://115.com/s/unconfigured\nhttps://pan.quark.cn/s/two"
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, text)); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if jobs[0].Status != "failed" || jobs[0].Link != "" || jobs[1].Status != "waiting" {
		t.Fatal("unavailable first provider did not fail without cloud writes")
	}
	if err := a.advanceTelegramBatch(context.Background(), jobs[0].BatchID); err != nil {
		t.Fatal(err)
	}
	if a.store.snapshot().TelegramRuntime.Jobs[1].Status != "confirming" || len(*messages) != 2 {
		t.Fatal("available provider was blocked behind an unavailable provider")
	}
	a.cleanupTelegramInteractions(context.Background(), time.Now().Add(11*time.Minute))
	jobs = a.store.snapshot().TelegramRuntime.Jobs
	if jobs[1].Status != "expired" || jobs[2].Status != "expired" || jobs[1].Link != "" || jobs[2].Link != "" || m.saves != 0 {
		t.Fatal("expired group left executable followers")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 70, "c:"+jobs[1].ID+":0"); err == nil {
		t.Fatal("expired group accepted confirmation")
	}
}

func TestTelegramCleanupBacklogOnlyBlocksNewPrompts(t *testing.T) {
	a, cfg, _, _ := telegramLibraryFixture(t)
	a.store.update(func(st *State) error {
		for i := 0; i < 200; i++ {
			st.TelegramRuntime.Jobs = append(st.TelegramRuntime.Jobs, telegramTransfer{ID: fmt.Sprintf("cleanup%d", i), Status: "expired", CleanupPending: true})
		}
		return nil
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "https://pan.quark.cn/s/direct")); err != nil {
		t.Fatal("cleanup backlog must not block a single direct transfer", err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 201 || jobs[200].Status != "queued" {
		t.Fatal("single target did not retain direct queue behavior")
	}
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(2, 123, 123, "https://pan.quark.cn/s/one\nhttps://pan.quark.cn/s/two")); err == nil {
		t.Fatal("cleanup backlog allowed a new confirmation prompt")
	}
	if len(a.store.snapshot().TelegramRuntime.Jobs) != 201 {
		t.Fatal("rejected prompt left executable batch members")
	}
}
