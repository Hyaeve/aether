package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

type telegramTestMarkup struct {
	Text   string `json:"text"`
	Markup struct {
		Rows [][]struct {
			Text string `json:"text"`
			Data string `json:"callback_data"`
		} `json:"inline_keyboard"`
	} `json:"reply_markup"`
}

func telegramModeTargets(t *testing.T, a *App, count int) {
	t.Helper()
	if err := a.store.update(func(st *State) error {
		st.Storages = append(st.Storages, Storage{ID: "115", Name: "115影视", Type: "115", Enabled: true, Config: map[string]string{"cookie": test115Cookie}})
		var targets []map[string]any
		for i := 0; i < count; i++ {
			targets = append(targets, map[string]any{"id": fmt.Sprintf("dir%d", i), "provider": "115", "storageId": "115", "parent": fmt.Sprint(10 + i), "path": fmt.Sprintf("/目录%d", i), "shareTransfer": i == 0, "ed2kOffline": i > 0})
		}
		raw, _ := json.Marshal(targets)
		return json.Unmarshal(raw, &st.Notifications.Targets)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTelegramTargetModesPersistence(t *testing.T) {
	a, _ := telegramFixture(t)
	telegramModeTargets(t, a, 2)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	w := request(t, h, "GET", "/api/notification-service", nil, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"shareTransfer":false`) || !strings.Contains(w.Body.String(), `"ed2kOffline":true`) {
		t.Fatal("target modes lost", w.Code, w.Body.String())
	}
	store, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(store.snapshot().Notifications.Targets)
	if !strings.Contains(string(raw), `"shareTransfer":false`) || !strings.Contains(string(raw), `"ed2kOffline":true`) {
		t.Fatal("modes lost after restart", string(raw))
	}
	invalid := map[string]any{"targets": []map[string]any{{"id": "bad", "provider": "quark", "storageId": "q", "parent": "0", "path": "/", "ed2kOffline": true}}}
	if w := request(t, h, "PUT", "/api/notification-service/targets", invalid, cookie); w.Code != 400 {
		t.Fatal("non-115 offline target accepted", w.Code)
	}
}

func TestTelegramED2KRoutesOnlyEnabledTargets(t *testing.T) {
	a, cfg := telegramFixture(t)
	telegramModeTargets(t, a, 2)
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			t.Fatal("routing wrote to cloud")
		}
		return `{"ok":true,"result":{"message_id":77}}`
	})
	uri := "ed2k://|file|movie.mkv|12|" + strings.Repeat("a", 32) + "|/"
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, uri)); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 1 || jobs[0].Status != "queued" || len(jobs[0].Targets) != 1 || jobs[0].Targets[0].ID != "dir1" {
		t.Fatal("ED2K did not route to its sole enabled directory", len(jobs))
	}
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(2, 123, 123, "https://115.com/s/code")); err != nil {
		t.Fatal(err)
	}
	jobs = a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 2 || len(jobs[1].Targets) != 1 || jobs[1].Targets[0].ID != "dir0" {
		t.Fatal("share used offline-only directory")
	}
}

func TestTelegramDirectoryPagination(t *testing.T) {
	a, cfg := telegramFixture(t)
	a.store.update(func(st *State) error {
		for i := 1; i < 12; i++ {
			st.Notifications.Targets = append(st.Notifications.Targets, telegramTarget{ID: fmt.Sprintf("q%d", i), Provider: "quark", StorageID: "q", Parent: fmt.Sprint(i), Path: fmt.Sprintf("/目录%d", i)})
		}
		return nil
	})
	var markup telegramTestMarkup
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			t.Fatal("pagination wrote cloud")
		}
		if err := json.NewDecoder(r.Body).Decode(&markup); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			return `{"ok":true,"result":{"message_id":77}}`
		}
		return `{"ok":true,"result":true}`
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "https://pan.quark.cn/s/code")); err != nil {
		t.Fatal(err)
	}
	job := a.store.snapshot().TelegramRuntime.Jobs[0]
	if len(markup.Markup.Rows) != 6 || markup.Markup.Rows[5][0].Text != "下一页" {
		t.Fatalf("first page must contain five directories and next: %+v", markup)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("t:%s:5", job.ID)); err == nil {
		t.Fatal("off-page selection accepted")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("p:%s:1", job.ID)); err != nil {
		t.Fatal(err)
	}
	if len(markup.Markup.Rows) != 6 || !strings.Contains(markup.Markup.Rows[0][0].Text, "/目录5") || len(markup.Markup.Rows[5]) != 2 {
		t.Fatal("middle page wrong", markup)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("p:%s:1", job.ID)); err == nil {
		t.Fatal("replayed page accepted")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("p:%s:2", job.ID)); err != nil {
		t.Fatal(err)
	}
	if len(markup.Markup.Rows) != 3 || markup.Markup.Rows[2][0].Text != "上一页" {
		t.Fatal("last page wrong", markup)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 999, 77, fmt.Sprintf("p:%s:1", job.ID)); err == nil {
		t.Fatal("foreign pagination accepted")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("p:%s:1", job.ID)); err != nil {
		t.Fatal("previous page failed", err)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("p:%s:2", job.ID)); err != nil {
		t.Fatal(err)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("p:%s:3", job.ID)); err == nil {
		t.Fatal("out of range page accepted")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, fmt.Sprintf("t:%s:10", job.ID)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup.Text, "/目录10") || markup.Markup.Rows[0][0].Text != "取消" || markup.Markup.Rows[0][1].Text != "确认转存" {
		t.Fatal("absolute index or confirmation wrong", markup)
	}
}

func TestTelegramED2KMultipleDirectoriesAndModeChange(t *testing.T) {
	a, cfg := telegramFixture(t)
	telegramModeTargets(t, a, 8)
	var markup telegramTestMarkup
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			t.Fatal("selection wrote cloud")
		}
		json.NewDecoder(r.Body).Decode(&markup)
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			return `{"ok":true,"result":{"message_id":77}}`
		}
		return `{"ok":true,"result":true}`
	})
	uri := "ed2k://|file|movie.mkv|12|" + strings.Repeat("a", 32) + "|/"
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, uri)); err != nil {
		t.Fatal(err)
	}
	job := a.store.snapshot().TelegramRuntime.Jobs[0]
	if job.Kind != "ed2k" || len(job.Targets) != 7 || job.Status != "choosing" || !strings.Contains(markup.Text, "ED2K 离线") {
		t.Fatal("offline choices incorrect")
	}
	a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs[0].Created = time.Now().Add(-11 * time.Minute)
		return nil
	})
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "p:"+job.ID+":1"); err != nil {
		t.Fatal(err)
	}
	current := a.store.snapshot().TelegramRuntime.Jobs[0]
	if current.Page != 1 || time.Since(current.LastActivity) > time.Minute {
		t.Fatal("page did not refresh idle deadline")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "t:"+job.ID+":5"); err != nil {
		t.Fatal(err)
	}
	if markup.Markup.Rows[0][1].Text != "确认ED2K 离线" || !strings.Contains(markup.Text, "/目录6") {
		t.Fatal("offline confirmation incorrect", markup)
	}
	a.store.update(func(st *State) error { st.Notifications.Targets[6].ED2KOffline = false; return nil })
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "c:"+job.ID+":5"); err == nil {
		t.Fatal("disabled offline mode accepted")
	}
}

func TestTelegramED2KSubmissionIntentAndNoReplay(t *testing.T) {
	for _, scenario := range []string{"success", "unknown", "mode-changed", "inaccessible", "cancelled", "credential-changed"} {
		t.Run(scenario, func(t *testing.T) {
			a, cfg := telegramFixture(t)
			telegramModeTargets(t, a, 2)
			target := a.store.snapshot().Notifications.Targets[1]
			uri := "ed2k://|file|movie.mkv|12|" + strings.Repeat("a", 32) + "|/"
			job := telegramTransfer{ID: "offline", Kind: "ed2k", Provider: "115", ChatID: 123, UserID: 123, Auth: telegramDigest(cfg), Targets: []telegramTarget{target}, Selected: 0, Status: "running", Link: uri, Created: time.Now()}
			a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
			s, _ := a.store.storage("115")
			sum := shareConfig(s)
			stamp := hex.EncodeToString(sum[:])
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			shareMock(t, func(r *http.Request) string {
				if r.URL.Host == "api.telegram.org" || r.URL.Path != "/files" {
					t.Fatal("unexpected network call", r.URL.Host, r.URL.Path)
				}
				if scenario == "mode-changed" {
					a.store.update(func(st *State) error { st.Notifications.Targets[1].ED2KOffline = false; return nil })
				}
				if scenario == "credential-changed" {
					a.store.update(func(st *State) error { st.Storages[1].Config["cookie"] = "UID=123_A1; CID=new; SEID=new"; return nil })
				}
				if scenario == "inaccessible" {
					return `{"state":false,"errno":20004}`
				}
				return `{"state":true,"cid":"11","count":0,"data":[]}`
			})
			writes := 0
			submit := func(_ context.Context, storage Storage, parent string, urls []string) []offlineResult {
				writes++
				current := a.store.snapshot().TelegramRuntime.Jobs[0]
				if current.Batches != 1 || current.Name != "movie.mkv" || parent != "11" || storage.ID != "115" || len(urls) != 1 || urls[0] != uri {
					t.Fatal("write without correct persisted intent")
				}
				return []offlineResult{{Success: scenario == "success", Message: "sensitive upstream body"}}
			}
			count, err := a.submitTelegramED2K(ctx, job, target, s, stamp, submit)
			want := 0
			if scenario == "success" || scenario == "unknown" {
				want = 1
			}
			if writes != want || (scenario == "success" && (err != nil || count != 1)) || (scenario != "success" && (err == nil || count != 0)) {
				t.Fatal(scenario, writes, count, err)
			}
			if _, err := a.submitTelegramED2K(ctx, job, target, s, stamp, submit); err == nil || writes != want {
				t.Fatal("submission replayed", writes, err)
			}
			logs, _ := json.Marshal(a.store.snapshot().Logs)
			for _, secret := range []string{cfg.Token, test115Cookie, strings.Repeat("a", 32), "sensitive upstream body"} {
				if strings.Contains(string(logs), secret) {
					t.Fatal("secret in offline log")
				}
			}
		})
	}
}

func TestTelegramED2KNativeSubmissionFailureIsNotReplayed(t *testing.T) {
	a, cfg := telegramFixture(t)
	telegramModeTargets(t, a, 2)
	job := telegramTransfer{ID: "native", Kind: "ed2k", Provider: "115", ChatID: 123, UserID: 123, Auth: telegramDigest(cfg), Targets: []telegramTarget{a.store.snapshot().Notifications.Targets[1]}, Selected: 0, Status: "queued", Link: "ed2k://|file|movie.mkv|12|" + strings.Repeat("a", 32) + "|/"}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
	writes := 0
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host == "api.telegram.org" {
			return `{"ok":true,"result":{"message_id":77}}`
		}
		if r.URL.Path == "/files" {
			return `{"state":true,"cid":"11","count":0,"data":[]}`
		}
		if r.URL.Host != "lixian.115.com" || r.URL.Path != "/lixianssp/" {
			t.Fatal("unexpected offline endpoint", r.URL.Host, r.URL.Path)
		}
		r.ParseForm()
		if r.Form.Get("data") == "" || !strings.Contains(r.Header.Get("Cookie"), "SEID=secret") {
			t.Fatal("native cookie/encrypted payload missing")
		}
		writes++
		return `{"state":false,"errno":10008,"error":"quota"}`
	})
	a.executeTelegramTransfer(context.Background(), job)
	a.executeTelegramTransfer(context.Background(), job)
	current := a.store.snapshot().TelegramRuntime.Jobs[0]
	if writes != 1 || current.Status != "failed" || current.Batches != 1 || current.Link != "" {
		t.Fatal("native failure replayed or lost", writes, current.Status)
	}
}

func TestTelegramMixedLinksValidateAndDeduplicate(t *testing.T) {
	uri := "ed2k://|file|book.pdf|12|" + strings.Repeat("a", 32) + "|/"
	links, err := telegramLinks(uri + "\nhttps://115.com/s/code?password=Ab12\n" + strings.Replace(strings.ToUpper(uri), "BOOK.PDF", "book.pdf", 1))
	if err != nil || len(links) != 2 || links[0].Kind != "ed2k" || links[1].Kind != "" {
		t.Fatal("mixed links or dedupe incorrect", links, err)
	}
	if _, err := telegramLinks(strings.Replace(uri, "|12|", "|0|", 1)); err == nil {
		t.Fatal("invalid ED2K accepted")
	}
	var many []string
	for i := 0; i < 51; i++ {
		many = append(many, strings.Replace(uri, "book.pdf", fmt.Sprintf("book%d.pdf", i), 1))
	}
	if _, err := telegramLinks(strings.Join(many, "\n")); err == nil {
		t.Fatal("link limit ignored")
	}
}
