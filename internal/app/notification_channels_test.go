package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

func channelFixture(t *testing.T) (*App, http.Handler, *http.Cookie) {
	t.Helper()
	a := testApp(t)
	h := a.Handler(t.TempDir())
	return a, h, setupTest(t, a, h)
}

func TestNotificationChannelsValidationAndClear(t *testing.T) {
	_, h, cookie := channelFixture(t)
	for channel, cfg := range channelConfigs() {
		if w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: cfg}, cookie); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, invalid := range []string{
		"http://open.feishu.cn/open-apis/bot/v2/hook/test", "https://127.0.0.1/open-apis/bot/v2/hook/test",
		"https://open.feishu.cn.evil.example/open-apis/bot/v2/hook/test", "https://user:pass@open.feishu.cn/open-apis/bot/v2/hook/test",
		"https://open.feishu.cn/open-apis/bot/v2/hook/a%2Fb", "https://open.feishu.cn/open-apis/bot/v2/hook/test?secret=test",
		"https://open.feishu.cn/open-apis/bot/v2/hook/test#fragment", "https://open.feishu.cn/open-apis/bot/v2/hook/",
	} {
		w := request(t, h, "PUT", "/api/notification-service/feishu", map[string]any{"feishu": map[string]any{"webhook": invalid}}, cookie)
		if w.Code != 400 {
			t.Fatal("unsafe webhook accepted", invalid, w.Code)
		}
	}
	for _, cfg := range []map[string]any{
		{"appId": "not-an-id"}, {"appId": "123", "groupId": "../another"}, {"secret": "secret\nheader"},
		{"enabled": true}, {"clearSecrets": []string{"appId"}}, {"enabled": true, "appId": "123", "groupId": "group", "clearSecrets": []string{"secret"}},
	} {
		if w := request(t, h, "PUT", "/api/notification-service/qqBot", map[string]any{"qqBot": cfg}, cookie); w.Code != 400 {
			t.Fatal("invalid QQ config accepted", cfg, w.Code)
		}
	}
	for channel, clear := range map[string][]string{"qqBot": {"secret"}, "feishu": {"secret", "webhook"}} {
		w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: map[string]any{"enabled": false, "clearSecrets": clear}}, cookie)
		if w.Code != 200 {
			t.Fatal("explicit credential clear failed", channel, w.Code, w.Body.String())
		}
		var result map[string]json.RawMessage
		var value map[string]any
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || json.Unmarshal(result[channel], &value) != nil || value["hasSecret"] == true || value["hasWebhook"] == true {
			t.Fatal("credential clear did not take effect")
		}
	}
}

func TestFeishuNotificationWireAndReceipt(t *testing.T) {
	_, h, cookie := channelFixture(t)
	cfg := channelConfigs()["feishu"]
	if w := request(t, h, "PUT", "/api/notification-service/feishu", map[string]any{"feishu": cfg}, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	calls := 0
	reply := `{"code":0}`
	shareMock(t, func(r *http.Request) string {
		calls++
		if r.URL.String() != cfg["webhook"] || r.Method != "POST" || r.Header.Get("Authorization") != "" {
			t.Fatal("wrong Feishu destination or credential routing")
		}
		var payload struct {
			Type      string            `json:"msg_type"`
			Content   map[string]string `json:"content"`
			Timestamp string            `json:"timestamp"`
			Sign      string            `json:"sign"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload.Type != "text" || !strings.Contains(payload.Content["text"], "Aether") {
			t.Fatal("wrong Feishu text")
		}
		mac := hmac.New(sha256.New, []byte(payload.Timestamp+"\n"+cfg["secret"].(string)))
		if payload.Sign != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
			t.Fatal("invalid Feishu signature")
		}
		return reply
	})
	if w := request(t, h, "POST", "/api/notification-service/feishu/test", map[string]any{}, nil); w.Code != 401 || calls != 0 {
		t.Fatal("unauthorized send")
	}
	for _, good := range []string{`{"code":0}`, `{"StatusCode":0}`, `{"StatusCode":200}`} {
		reply = good
		if w := request(t, h, "POST", "/api/notification-service/feishu/test", map[string]any{}, cookie); w.Code != 200 {
			t.Fatal("valid receipt rejected", good, w.Code, w.Body.String())
		}
	}
	for _, bad := range []string{`{}`, `null`, `{"code":19021,"msg":"feishu-private-secret private-hook"}`, `{"StatusCode":99}`, `{"code":0,"StatusCode":99}`, `malformed`} {
		reply = bad
		before := calls
		w := request(t, h, "POST", "/api/notification-service/feishu/test", map[string]any{}, cookie)
		if w.Code != 502 || calls != before+1 || strings.Contains(w.Body.String(), "private-secret") || strings.Contains(w.Body.String(), "private-hook") {
			t.Fatal("invalid receipt, retry or unsafe error", w.Code, w.Body.String())
		}
	}
}

func TestQQBotNotificationTokenAndReceipt(t *testing.T) {
	a, h, cookie := channelFixture(t)
	cfg := channelConfigs()["qqBot"]
	if w := request(t, h, "PUT", "/api/notification-service/qqBot", map[string]any{"qqBot": cfg}, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	tokenCalls, sends := 0, 0
	tokenReply := `{"access_token":"qq-access-token","expires_in":"7200"}`
	messageReply := `{"id":"accepted"}`
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host == "bots.qq.com" {
			tokenCalls++
			var body map[string]string
			if r.URL.Path != "/app/getAppAccessToken" || json.NewDecoder(r.Body).Decode(&body) != nil || body["appId"] != cfg["appId"] || body["clientSecret"] != cfg["secret"] {
				t.Fatal("invalid QQ token request")
			}
			return tokenReply
		}
		if r.URL.Host != "api.sgroup.qq.com" || r.URL.Path != "/v2/groups/group_openid_example/messages" || r.Header.Get("Authorization") != "QQBot qq-access-token" {
			t.Fatal("wrong QQ message endpoint or authorization")
		}
		sends++
		var payload map[string]any
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["msg_type"] != float64(0) || payload["msg_id"] != nil {
			t.Fatal("unexpected passive reply or message type")
		}
		return messageReply
	})
	for range 2 {
		if w := request(t, h, "POST", "/api/notification-service/qqBot/test", map[string]any{}, cookie); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if tokenCalls != 1 || sends != 2 {
		t.Fatal("token not reused", tokenCalls, sends)
	}
	a.channels.qqExpiry = time.Time{}
	tokenReply = `{"access_token":"qq-access-token","expires_in":7200}`
	if w := request(t, h, "POST", "/api/notification-service/qqBot/test", map[string]any{}, cookie); w.Code != 200 || tokenCalls != 2 {
		t.Fatal("numeric token expiry rejected")
	}
	for _, bad := range []string{`{}`, `{"code":22009,"message":"qq-private-secret qq-access-token"}`, `{"id":"unknown","code":304082}`} {
		messageReply = bad
		before := sends
		w := request(t, h, "POST", "/api/notification-service/qqBot/test", map[string]any{}, cookie)
		if w.Code != 502 || sends != before+1 || strings.Contains(w.Body.String(), "qq-private") || strings.Contains(w.Body.String(), "qq-access-token") {
			t.Fatal("QQ message result or no-replay boundary", w.Code, w.Body.String())
		}
	}
	cfg["secret"] = "changed-secret"
	if w := request(t, h, "PUT", "/api/notification-service/qqBot", map[string]any{"qqBot": cfg}, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	messageReply = `{"id":"new-account-message"}`
	if w := request(t, h, "POST", "/api/notification-service/qqBot/test", map[string]any{}, cookie); w.Code != 200 || tokenCalls != 3 {
		t.Fatal("changed credentials reused token")
	}
}

func TestNotificationChannelsConfigurationChangeStopsSend(t *testing.T) {
	_, h, cookie := channelFixture(t)
	cfg := channelConfigs()["qqBot"]
	if w := request(t, h, "PUT", "/api/notification-service/qqBot", map[string]any{"qqBot": cfg}, cookie); w.Code != 200 {
		t.Fatal(w.Code)
	}
	calls := 0
	shareMock(t, func(r *http.Request) string {
		calls++
		if r.URL.Host != "bots.qq.com" {
			t.Fatal("message sent after configuration change")
		}
		cfg["enabled"] = false
		if w := request(t, h, "PUT", "/api/notification-service/qqBot", map[string]any{"qqBot": cfg}, cookie); w.Code != 200 {
			t.Fatal(w.Code)
		}
		return `{"access_token":"unused-token","expires_in":7200}`
	})
	w := request(t, h, "POST", "/api/notification-service/qqBot/test", map[string]any{}, cookie)
	if w.Code != 502 || calls != 1 {
		t.Fatal("configuration change did not stop send")
	}
}

func TestNotificationResultQueueIsBoundedAndSkipsOldConfiguration(t *testing.T) {
	a, h, cookie := channelFixture(t)
	for channel, cfg := range channelConfigs() {
		if w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: cfg}, cookie); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	a.queueResultNotification("任务", "film\npassword", "running", 1)
	if len(a.notificationQueue) != 0 {
		t.Fatal("nonterminal state notified")
	}
	for i := range 66 {
		a.queueResultNotification("任务", fmt.Sprintf("任务%d", i), "success", i)
	}
	if len(a.notificationQueue) != 64 {
		t.Fatal("unbounded notification queue", len(a.notificationQueue))
	}
	notice := <-a.notificationQueue
	next := a.store.snapshotWithLogLimit(0).Notifications
	next.QQBot.Enabled, next.Feishu.Enabled = false, false
	if err := a.store.update(func(st *State) error { st.Notifications = next; return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	shareMock(t, func(*http.Request) string { calls++; return `{}` })
	if err := a.sendQQBot(a.ctx, notice.QQBot, notice.Text); err == nil {
		t.Fatal("old QQ target accepted")
	}
	if err := a.sendFeishu(a.ctx, notice.Feishu, notice.Text); err == nil || calls != 0 {
		t.Fatal("old Feishu target accepted or upstream called")
	}
}

func TestNotificationChannelsNeverRedirectOrReplay(t *testing.T) {
	for _, channel := range []string{"qqBot", "feishu"} {
		for _, mode := range []string{"redirect", "http-error", "truncated", "oversized", "cancelled"} {
			t.Run(channel+"/"+mode, func(t *testing.T) {
				a, h, cookie := channelFixture(t)
				cfg := channelConfigs()[channel]
				if w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: cfg}, cookie); w.Code != 200 {
					t.Fatal(w.Code)
				}
				old := apiClient
				t.Cleanup(func() { apiClient = old })
				calls := 0
				apiClient = &http.Client{Transport: casTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if calls > 1 || r.URL.Host == "foreign.invalid" {
						t.Fatal("redirect or automatic retry")
					}
					status, body := 200, `{}`
					header := http.Header{}
					switch mode {
					case "redirect":
						status = 302
						header.Set("Location", "https://foreign.invalid/credentials")
					case "http-error":
						status, body = 403, "qq-private-secret feishu-private-secret private-hook"
					case "truncated":
						body = `{"code":`
					case "oversized":
						body = strings.Repeat("x", (1<<20)+1)
					case "cancelled":
						return nil, context.Canceled
					}
					return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				w := request(t, h, "POST", "/api/notification-service/"+channel+"/test", map[string]any{}, cookie)
				logs, _ := json.Marshal(a.store.snapshot().Logs)
				if w.Code != 502 || calls != 1 {
					t.Fatal("uncertain response accepted or retried", w.Code, calls)
				}
				for _, secret := range []string{"qq-private-secret", "feishu-private-secret", "private-hook"} {
					if strings.Contains(w.Body.String(), secret) || strings.Contains(string(logs), secret) {
						t.Fatal("credentials exposed in error or logs")
					}
				}
			})
		}
	}
}

func TestNotificationWorkerDeliversTerminalResultsAndStops(t *testing.T) {
	a, h, cookie := channelFixture(t)
	for channel, cfg := range channelConfigs() {
		if w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: cfg}, cookie); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	sent := make(chan string, 2)
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host == "bots.qq.com" {
			return `{"access_token":"token","expires_in":7200}`
		}
		var body struct {
			Content json.RawMessage `json:"content"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid notification")
		}
		sent <- string(body.Content)
		if r.URL.Host == "api.sgroup.qq.com" {
			return `{"id":"accepted"}`
		}
		return `{"code":0}`
	})
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	defer cancel()
	a.wg.Add(1)
	go a.channelNotificationWorker()
	a.queueResultNotification("任务", "Film", "success", 3)
	for range 2 {
		select {
		case text := <-sent:
			if !strings.Contains(text, "Film") || !strings.Contains(text, "3") || !strings.Contains(text, "成功") {
				t.Fatal("result details missing", text)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("terminal notification was not delivered")
		}
	}
	cancel()
	a.wg.Wait()
}

func channelConfigs() map[string]map[string]any {
	return map[string]map[string]any{
		"qqBot":  {"enabled": true, "appId": "123456789", "secret": "qq-private-secret", "groupId": "group_openid_example"},
		"feishu": {"enabled": true, "webhook": "https://open.feishu.cn/open-apis/bot/v2/hook/private-hook", "secret": "feishu-private-secret"},
	}
}

func TestNotificationChannelsEncryptedAndIndependent(t *testing.T) {
	a, h, cookie := channelFixture(t)
	for channel, cfg := range channelConfigs() {
		w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: cfg}, cookie)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", channel, w.Code, w.Body.String())
		}
	}
	for _, endpoint := range []string{"/api/notification-service", "/api/state"} {
		w := request(t, h, "GET", endpoint, nil, cookie)
		for _, private := range []string{"qq-private-secret", "feishu-private-secret", "private-hook"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("secret exposed by %s", endpoint)
			}
		}
	}
	for channel, cfg := range channelConfigs() {
		for _, field := range []string{"secret", "webhook"} {
			value, ok := cfg[field].(string)
			if !ok {
				continue
			}
			url := "/api/notification-service/" + channel + "/secret"
			if w := request(t, h, "POST", url, map[string]string{"field": field}, nil); w.Code != 401 {
				t.Fatal("unauthenticated secret read", channel, w.Code)
			}
			w := request(t, h, "POST", url, map[string]string{"field": field}, cookie)
			if w.Code != 200 || !strings.Contains(w.Body.String(), value) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("secret reveal", channel, field, w.Code, w.Body.String())
			}
		}
	}
	raw, err := os.ReadFile(filepath.Join(a.store.dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"qq-private-secret", "feishu-private-secret", "private-hook"} {
		if strings.Contains(string(raw), value) {
			t.Fatal("plaintext stored", value)
		}
	}
	before, _ := json.Marshal(a.store.snapshot().Notifications)
	reopened, err := NewStore(a.store.dir)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(reopened.snapshot().Notifications)
	if string(before) != string(after) {
		t.Fatal("notification settings lost after restart")
	}
	w := request(t, h, "PUT", "/api/notification-service", map[string]any{"telegram": map[string]any{}}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for channel, cfg := range channelConfigs() {
		cfg["secret"] = ""
		if channel == "feishu" {
			cfg["webhook"] = ""
		}
		if w := request(t, h, "PUT", "/api/notification-service/"+channel, map[string]any{channel: cfg}, cookie); w.Code != 200 {
			t.Fatal("saving unchanged placeholders", channel, w.Code, w.Body.String())
		}
	}
	w = request(t, h, "GET", "/api/notification-service", nil, cookie)
	if strings.Count(w.Body.String(), `"hasSecret":true`) != 2 || !strings.Contains(w.Body.String(), `"hasWebhook":true`) {
		t.Fatal("another panel erased channel settings", w.Body.String())
	}
}
