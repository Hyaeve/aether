package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func telegramFixture(t *testing.T) (*App, telegramConfig) {
	t.Helper()
	a := testApp(t)
	cfg := telegramConfig{Enabled: true, Token: "123456789:" + strings.Repeat("a", 35), ChatID: "123", UserIDs: []string{"123"}}
	if err := a.store.update(func(st *State) error {
		st.Notifications = notificationConfig{Telegram: cfg, Targets: []telegramTarget{{ID: "one", Provider: "quark", StorageID: "q", Parent: "0", Path: "/"}}}
		st.Storages = []Storage{{ID: "q", Name: "夸克影视", Type: "quark", Enabled: true, Config: map[string]string{"cookie": "private-cookie"}}}
		st.TelegramRuntime.Bot = telegramDigest(cfg.Token)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return a, cfg
}

func telegramTextUpdate(id, user, chat int64, text string) telegramUpdate {
	m := &telegramMessage{ID: id, Text: text, From: telegramUser{ID: user}}
	m.Chat.ID = chat
	return telegramUpdate{ID: id, Message: m}
}

func TestTelegramSettingsPersistenceAndRedaction(t *testing.T) {
	a, cfg := telegramFixture(t)
	h := a.Handler(t.TempDir())
	cookie := setupTest(t, a, h)
	if w := request(t, h, "GET", "/api/notification-service", nil, nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := request(t, h, "GET", "/api/notification-service", nil, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), cfg.Token) || !strings.Contains(w.Body.String(), `"hasToken":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(t, h, "POST", "/api/notification-service/secret", map[string]any{"field": "token"}, nil); w.Code != 401 {
		t.Fatal("unauthorized token reveal", w.Code)
	}
	w = request(t, h, "POST", "/api/notification-service/secret", map[string]any{"field": "token", "metadataOnly": true}, cookie)
	if w.Code != 200 || strings.Contains(w.Body.String(), cfg.Token) || !strings.Contains(w.Body.String(), `"length":`) {
		t.Fatal("token metadata", w.Code, w.Body.String())
	}
	w = request(t, h, "POST", "/api/notification-service/secret", map[string]any{"field": "token"}, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), cfg.Token) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("token reveal", w.Code)
	}
	if w := request(t, h, "POST", "/api/notification-service/secret", map[string]string{"field": "password"}, cookie); w.Code != 400 {
		t.Fatal("unrelated secret field accepted", w.Code)
	}
	cfg.Token = ""
	w = request(t, h, "PUT", "/api/notification-service", notificationConfig{Telegram: cfg}, cookie)
	if w.Code != 200 || a.store.snapshot().Notifications.Telegram.Token == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	if err != nil || strings.Contains(string(raw), strings.Repeat("a", 35)) {
		t.Fatal("token not encrypted", err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil || reopened.snapshot().Notifications.Telegram.Token != a.store.snapshot().Notifications.Telegram.Token {
		t.Fatal("notification configuration lost", err)
	}
	invalid := a.store.snapshot().Notifications
	invalid.Telegram.ChatID = "@publicchannel"
	if w := request(t, h, "PUT", "/api/notification-service", invalid, cookie); w.Code != 400 {
		t.Fatal("non-numeric identity accepted", w.Code)
	}
	invalid = a.store.snapshot().Notifications
	invalid.Telegram.ChatID, invalid.Telegram.UserIDs = "-100123", nil
	if w := request(t, h, "PUT", "/api/notification-service", invalid, cookie); w.Code != 400 {
		t.Fatal("group without authorized users accepted", w.Code)
	}
	items := a.store.snapshot().Notifications.Targets
	items = append(items, telegramTarget{ID: "two", Provider: "quark", StorageID: "q", Parent: "folder", Path: "/Movies"})
	items[0], items[1] = items[1], items[0]
	w = request(t, h, "PUT", "/api/notification-service/targets", notificationConfig{Targets: items}, cookie)
	if w.Code != 200 || a.store.snapshot().Notifications.Targets[0].ID != "two" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestTelegramAuthorizedMessagesAndOrderedConfirmation(t *testing.T) {
	a, cfg := telegramFixture(t)
	var sent []map[string]any
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			if r.URL.Host == "drive.quark.cn" && strings.HasSuffix(r.URL.Path, "/token") && r.Method == "POST" {
				return `{"code":0,"data":{"stoken":"readonly"}}`
			}
			if r.URL.Host == "drive.quark.cn" && strings.HasSuffix(r.URL.Path, "/detail") && r.Method == "GET" {
				return `{"code":0,"data":{"list":[{"fid":"shared","file_name":"分享目录","dir":true}]},"metadata":{"_total":1}}`
			}
			t.Fatal("message handling wrote cloud", r.URL.Host)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		sent = append(sent, body)
		return `{"ok":true,"result":{"message_id":77}}`
	})
	for _, update := range []telegramUpdate{telegramTextUpdate(1, 999, 123, "https://pan.quark.cn/s/code"), telegramTextUpdate(2, 123, 999, "https://pan.quark.cn/s/code")} {
		if err := a.handleTelegramUpdate(context.Background(), cfg, update); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.store.snapshot().TelegramRuntime.Jobs) != 0 || len(sent) != 0 {
		t.Fatal("unauthorized interaction accepted")
	}
	update := telegramTextUpdate(3, 123, 123, "https://pan.quark.cn/s/code")
	if err := a.handleTelegramUpdate(context.Background(), cfg, update); err != nil {
		t.Fatal(err)
	}
	if err := a.handleTelegramUpdate(context.Background(), cfg, update); err != nil {
		t.Fatal(err)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != 1 || jobs[0].Status != "queued" || len(sent) != 0 {
		t.Fatal("single target did not queue exactly once", len(jobs), len(sent))
	}
	a.store.update(func(st *State) error {
		st.Notifications.Targets = append([]telegramTarget{{ID: "first", Provider: "quark", StorageID: "q", Parent: "first", Path: "/First"}}, st.Notifications.Targets...)
		return nil
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(4, 123, 123, "https://pan.quark.cn/s/second")); err != nil {
		t.Fatal(err)
	}
	jobs = a.store.snapshot().TelegramRuntime.Jobs
	job := jobs[1]
	if job.Status != "choosing" || job.Targets[0].ID != "first" || job.MessageID != 77 {
		t.Fatal(job.Status, job.Targets)
	}
	keyboard := sent[0]["reply_markup"].(map[string]any)["inline_keyboard"].([]any)
	if !strings.HasPrefix(keyboard[0].([]any)[0].(map[string]any)["text"].(string), "1. 夸克影视 · /First") {
		t.Fatal(keyboard)
	}
	selectData := fmt.Sprintf("t:%s:0", job.ID)
	if err := a.telegramCallback(context.Background(), cfg, 123, 999, 77, selectData); err == nil {
		t.Fatal("foreign user callback accepted")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 78, selectData); err == nil {
		t.Fatal("foreign message callback accepted")
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, selectData); err != nil {
		t.Fatal(err)
	}
	if a.store.snapshot().TelegramRuntime.Jobs[1].Status != "confirming" {
		t.Fatal("selection bypassed confirmation")
	}
	confirm := fmt.Sprintf("c:%s:0", job.ID)
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, confirm); err != nil {
		t.Fatal(err)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, confirm); err == nil {
		t.Fatal("confirmation replayed")
	}
	if a.store.snapshot().TelegramRuntime.Jobs[1].Status != "queued" {
		t.Fatal("confirmation did not queue")
	}
}

func TestTelegramNoTargetsChangedTargetAndRestart(t *testing.T) {
	a, cfg := telegramFixture(t)
	shareMock(t, func(r *http.Request) string { return `{"ok":true,"result":{"message_id":44}}` })
	a.store.update(func(st *State) error { st.Notifications.Targets = nil; return nil })
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramTextUpdate(1, 123, 123, "https://pan.quark.cn/s/code")); err != nil {
		t.Fatal(err)
	}
	if len(a.store.snapshot().TelegramRuntime.Jobs) != 0 {
		t.Fatal("empty pool queued work")
	}
	target := telegramTarget{ID: "q", Provider: "quark", StorageID: "q", Parent: "0", Path: "/"}
	job := telegramTransfer{ID: "pending", ChatID: 123, UserID: 123, MessageID: 44, Status: "confirming", Selected: 0, Targets: []telegramTarget{target}, Auth: telegramDigest(cfg), Created: time.Now(), Link: "https://pan.quark.cn/s/code?pwd=Ab12"}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 44, "c:pending:0"); err == nil {
		t.Fatal("removed directory accepted")
	}
	a.store.update(func(st *State) error { st.Notifications.Targets = []telegramTarget{target}; return nil })
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 44, "c:pending:0"); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	runtime := reopened.snapshot().TelegramRuntime
	if runtime.Offset != 2 || runtime.Jobs[0].Status != "interrupted" || runtime.Jobs[0].Link != "" {
		t.Fatal("restart replay protection lost", runtime.Offset, runtime.Jobs[0].Status)
	}
}

func TestTelegramTransferSharesBatchesAndLogs(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		t.Run(fmt.Sprint(failSecond), func(t *testing.T) {
			a, cfg := telegramFixture(t)
			job := telegramTransfer{ID: "transfer", ChatID: 123, UserID: 123, Status: "queued", Selected: 0, Targets: a.store.snapshot().Notifications.Targets, Auth: telegramDigest(cfg), Created: time.Now(), Link: "https://pan.quark.cn/s/code?pwd=Ab12"}
			a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
			writes := 0
			prepared := false
			shareMock(t, func(r *http.Request) string {
				if r.URL.Host == "api.telegram.org" {
					if strings.HasSuffix(r.URL.Path, "/sendMessage") {
						var body struct {
							Text string `json:"text"`
						}
						json.NewDecoder(r.Body).Decode(&body)
						if strings.HasPrefix(body.Text, "准备转存") {
							if !strings.Contains(body.Text, "0.mkv") || !strings.Contains(body.Text, "夸克影视") {
								t.Fatal("preparation lacks resource or storage", body.Text)
							}
							prepared = true
						}
					}
					return `{"ok":true,"result":{"message_id":5}}`
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/token"):
					return `{"code":0,"data":{"stoken":"private"}}`
				case strings.HasSuffix(r.URL.Path, "/detail"):
					var files []map[string]any
					page := 1
					fmt.Sscan(r.URL.Query().Get("_page"), &page)
					for i := (page - 1) * 100; i < min(page*100, 500); i++ {
						files = append(files, map[string]any{"fid": fmt.Sprint(i), "file_name": fmt.Sprintf("%d.mkv", i), "share_fid_token": "private"})
					}
					b, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"list": files}, "metadata": map[string]any{"_total": 500}})
					return string(b)
				case strings.HasSuffix(r.URL.Path, "/sort"):
					return `{"code":0,"data":{"list":[]}}`
				case strings.HasSuffix(r.URL.Path, "/task"):
					return `{"code":0,"data":{"status":2}}`
				case strings.HasSuffix(r.URL.Path, "/save"):
					if !prepared {
						t.Fatal("write before named preparation")
					}
					writes++
					var body struct {
						IDs []string `json:"fid_list"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					want := 499
					if writes == 2 {
						want = 1
					}
					if len(body.IDs) != want {
						t.Fatal("incorrect batch size", len(body.IDs))
					}
					if failSecond && writes == 2 {
						return `{"code":400}`
					}
					return `{"code":0,"data":{"task_id":"job"}}`
				default:
					t.Fatal("unexpected cloud call", r.URL.Path)
					return ""
				}
			})
			a.executeTelegramTransfer(context.Background(), job)
			final := a.store.snapshot().TelegramRuntime.Jobs[0]
			want := "completed"
			if failSecond {
				want = "failed"
			}
			if final.Status != want || writes != 2 || final.Link != "" {
				t.Fatal(final.Status, writes)
			}
			logs, _ := json.Marshal(a.store.snapshot().Logs)
			if !strings.Contains(string(logs), "TG:transfer") || !strings.Contains(string(logs), "夸克影视") || strings.Contains(string(logs), cfg.Token) || strings.Contains(string(logs), "Ab12") {
				t.Fatal("missing log or sensitive data leak", string(logs))
			}
			a.executeTelegramTransfer(context.Background(), job)
			if writes != 2 {
				t.Fatal("terminal transfer replayed")
			}
		})
	}
}

func TestTelegramBootstrapSkipsBacklogAndDurablyConsumesNewMessages(t *testing.T) {
	a, cfg := telegramFixture(t)
	a.store.update(func(st *State) error { st.TelegramRuntime = telegramRuntime{}; return nil })
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	a.ctx = ctx
	polls := 0
	shareMock(t, func(r *http.Request) string {
		if strings.HasSuffix(r.URL.Path, "/getWebhookInfo") {
			return `{"ok":true,"result":{"url":""}}`
		}
		if !strings.HasSuffix(r.URL.Path, "/getUpdates") {
			t.Fatal("bootstrap unexpectedly wrote", r.URL.Path)
		}
		polls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if polls == 1 {
			if body["offset"] != float64(-1) || body["timeout"] != float64(0) {
				t.Fatal("not a safe bootstrap", body)
			}
			return `{"ok":true,"result":[{"update_id":10,"message":{"message_id":1,"from":{"id":123},"chat":{"id":123},"text":"https://pan.quark.cn/s/old"}}]}`
		}
		if polls == 2 {
			if body["offset"] != float64(11) {
				t.Fatal("old updates not skipped", body)
			}
			return `{"ok":true,"result":[{"update_id":11,"message":{"message_id":2,"from":{"id":123},"chat":{"id":123},"text":"https://pan.quark.cn/s/new"}}]}`
		}
		cancel()
		return `{"ok":true,"result":[]}`
	})
	a.wg.Add(1)
	a.telegramPoll()
	runtime := a.store.snapshot().TelegramRuntime
	if runtime.Bot != telegramDigest(cfg.Token) || runtime.Offset != 12 || len(runtime.Jobs) != 1 || !strings.Contains(runtime.Jobs[0].Link, "/new") {
		t.Fatal("bootstrap or offset durability failed", runtime.Offset, len(runtime.Jobs))
	}
}

func TestTelegramLinkConflictsAndFixedHostErrors(t *testing.T) {
	for _, text := range []string{
		"https://pan.quark.cn/s/code?pwd=Ab12 提取码: Cd34",
		"https://yun.139.com/shareweb/#/w/i/code?pwd=Ab12&pwd=Cd34",
		"https://115.com/s/code?password=Ab12\nhttps://115.com/s/code?password=Cd34",
	} {
		if _, err := telegramLinks(text); err == nil {
			t.Fatal("conflicting code accepted")
		}
	}
	if links, err := telegramLinks("https://example.test/share\nhttp://115.com/s/code"); err != nil || len(links) != 0 {
		t.Fatal("unknown share accepted")
	}
}
