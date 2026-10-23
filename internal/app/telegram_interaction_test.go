package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTelegramConfirmButtonOrder(t *testing.T) {
	a, cfg := telegramFixture(t)
	target := a.store.snapshot().Notifications.Targets[0]
	job := telegramTransfer{ID: "order", ChatID: 123, UserID: 123, MessageID: 77, Auth: telegramDigest(cfg), Status: "choosing", Targets: []telegramTarget{target}, Selected: -1, Created: time.Now(), Link: "https://pan.quark.cn/s/code"}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
	var buttons []string
	shareMock(t, func(r *http.Request) string {
		var body struct {
			Markup struct {
				Rows [][]struct {
					Text string `json:"text"`
				} `json:"inline_keyboard"`
			} `json:"reply_markup"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		for _, row := range body.Markup.Rows {
			for _, button := range row {
				buttons = append(buttons, button.Text)
			}
		}
		return `{"ok":true,"result":true}`
	})
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "t:order:0"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(buttons, ",") != "取消,确认转存" {
		t.Fatal("confirmation must be on the right", buttons)
	}
}

func TestTelegramSelectionRestartsInactivityDeadline(t *testing.T) {
	a, cfg := telegramFixture(t)
	job := telegramTransfer{ID: "idle", ChatID: 123, UserID: 123, MessageID: 77, Auth: telegramDigest(cfg), Status: "choosing", Targets: a.store.snapshot().Notifications.Targets, Selected: -1, Created: time.Now().Add(-9 * time.Minute), Link: "https://pan.quark.cn/s/code"}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
	shareMock(t, func(r *http.Request) string { return `{"ok":true,"result":true}` })
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "t:idle:0"); err != nil {
		t.Fatal(err)
	}
	// The creation time no longer defines the deadline after a valid selection.
	a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs[0].Created = time.Now().Add(-11 * time.Minute)
		return nil
	})
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "c:idle:0"); err != nil {
		t.Fatal("recent valid interaction expired by creation time", err)
	}
}

func TestTelegramInteractionCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, status  string
		created, last time.Duration
		remove        bool
	}{
		{"choosing boundary", "choosing", 10 * time.Minute, 0, true},
		{"confirming expired", "confirming", 20 * time.Minute, 11 * time.Minute, true},
		{"recent selection", "confirming", 20 * time.Minute, 3 * time.Minute, false},
		{"before boundary", "choosing", 10*time.Minute - time.Second, 0, false},
		{"queued protected", "queued", 20 * time.Minute, 0, false},
		{"running protected", "running", 20 * time.Minute, 0, false},
		{"result protected", "completed", 20 * time.Minute, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, cfg := telegramFixture(t)
			now := time.Now()
			job := telegramTransfer{ID: "cleanup", ChatID: 123, UserID: 123, MessageID: 77, Auth: telegramDigest(cfg), Bot: telegramDigest(cfg.Token), Status: tc.status, Targets: a.store.snapshot().Notifications.Targets, Created: now.Add(-tc.created), Link: "https://pan.quark.cn/s/private?pwd=Ab12"}
			if tc.last != 0 {
				job.LastActivity = now.Add(-tc.last)
			}
			a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
			calls := 0
			shareMock(t, func(r *http.Request) string {
				calls++
				if r.URL.Host != "api.telegram.org" || !strings.HasSuffix(r.URL.Path, "/deleteMessages") {
					t.Error("not a bot message deletion")
				}
				var body struct {
					Chat int64   `json:"chat_id"`
					IDs  []int64 `json:"message_ids"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				if body.Chat != 123 || len(body.IDs) != 1 || body.IDs[0] != 77 {
					t.Error("wrong message deleted", body)
				}
				current := a.store.snapshot().TelegramRuntime.Jobs[0]
				if current.Status != "expired" || current.Link != "" {
					t.Error("remote delete before durable expiry")
				}
				return `{"ok":true,"result":true}`
			})
			a.cleanupTelegramInteractions(context.Background(), now)
			a.cleanupTelegramInteractions(context.Background(), now.Add(500*time.Millisecond))
			current := a.store.snapshot().TelegramRuntime.Jobs[0]
			if tc.remove {
				if calls != 1 || current.Status != "expired" || current.Link != "" || current.CleanupPending {
					t.Fatal("expiry or cleanup failed", calls, current.Status)
				}
				if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "c:cleanup:0"); err == nil {
					t.Fatal("expired interaction replayed")
				}
			} else if calls != 0 || current.Status != tc.status || current.Link != job.Link {
				t.Fatal("active transfer or fresh interaction changed", calls, current.Status)
			}
			logs, _ := json.Marshal(a.store.snapshot().Logs)
			if strings.Contains(string(logs), cfg.Token) || strings.Contains(string(logs), "Ab12") || strings.Contains(string(logs), "/s/private") {
				t.Fatal("sensitive cleanup log")
			}
		})
	}
}

func TestTelegramCleanupFailurePersistsAndRetries(t *testing.T) {
	a, cfg := telegramFixture(t)
	now := time.Now()
	job := telegramTransfer{ID: "retry", ChatID: 123, UserID: 123, MessageID: 77, Auth: telegramDigest(cfg), Bot: telegramDigest(cfg.Token), Status: "confirming", Created: now.Add(-11 * time.Minute), Link: "private"}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if calls == 1 {
			return `{"ok":false,"description":"private sensitive response"}`
		}
		return `{"ok":true,"result":true}`
	})
	a.cleanupTelegramInteractions(context.Background(), now)
	if current := a.store.snapshot().TelegramRuntime.Jobs[0]; !current.CleanupPending || current.Link != "" {
		t.Fatal("failed cleanup lost durable intent")
	}
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	a.cleanupTelegramInteractions(context.Background(), now.Add(30*time.Second))
	if calls != 1 {
		t.Fatal("deletion retry did not back off")
	}
	a.cleanupTelegramInteractions(context.Background(), now.Add(time.Minute))
	if calls != 2 || a.store.snapshot().TelegramRuntime.Jobs[0].CleanupPending {
		t.Fatal("deletion did not resume safely after restart", calls)
	}
}

func TestTelegramCleanupCredentialBoundaryAndInterruptedPrompt(t *testing.T) {
	a, cfg := telegramFixture(t)
	now := time.Now()
	job := telegramTransfer{ID: "restart", ChatID: 123, UserID: 123, MessageID: 77, Auth: telegramDigest(cfg), Bot: telegramDigest(cfg.Token), Status: "choosing", Created: now.Add(-11 * time.Minute), Link: "private"}
	a.store.update(func(st *State) error { st.TelegramRuntime.Jobs = []telegramTransfer{job}; return nil })
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.initRuntime(a.store.runtimeDir); err != nil {
		t.Fatal(err)
	}
	a.store = reopened
	if current := a.store.snapshot().TelegramRuntime.Jobs[0]; current.Status != "interrupted" || !current.CleanupPending || current.Link != "" {
		t.Fatal("restart lost prompt cleanup or replay guard")
	}
	a.store.update(func(st *State) error {
		st.Notifications.Telegram.Token = "987654321:" + strings.Repeat("b", 35)
		return nil
	})
	calls := 0
	shareMock(t, func(r *http.Request) string { calls++; return `{"ok":true,"result":true}` })
	a.cleanupTelegramInteractions(context.Background(), now)
	if calls != 0 {
		t.Fatal("new bot attempted to delete an old bot message")
	}
	a.store.update(func(st *State) error { st.Notifications.Telegram = cfg; return nil })
	a.cleanupTelegramInteractions(context.Background(), now.Add(time.Minute))
	if calls != 1 || a.store.snapshot().TelegramRuntime.Jobs[0].Status != "interrupted" {
		t.Fatal("cleanup restarted transfer or lost pending prompt", calls)
	}
}

func TestTelegramMessageDeletionRequiresConfirmedResult(t *testing.T) {
	_, cfg := telegramFixture(t)
	for _, body := range []string{`{"ok":true,"result":false}`, `{"ok":true,"result":null}`, `{"ok":true,"result":{}}`, `{"ok":false,"description":"private"}`, `{"ok":true,"result":true}`} {
		shareMock(t, func(r *http.Request) string { return body })
		err := telegramRequest(context.Background(), cfg, "deleteMessages", map[string]any{"chat_id": 123, "message_ids": []int64{77}}, nil)
		if (err == nil) != (body == `{"ok":true,"result":true}`) {
			t.Fatal("unconfirmed deletion accepted", body, err)
		}
		if err != nil && strings.Contains(err.Error(), "private") {
			t.Fatal("upstream sensitive text leaked")
		}
	}
}
