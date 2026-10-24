package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestShare115NumericDirectoryIdentifiers(t *testing.T) {
	a := testApp(t)
	shareMock(t, func(r *http.Request) string {
		return `{"state":true,"data":{"count":"2","list":[{"cid":1234567890123456,"n":"分享文件夹","fc":0},{"fid":"456","cid":0,"n":"Movie.mkv","s":"123","fc":1}]}}`
	})
	_, items, err := a.readShare(context.Background(), Storage{ID: "p", Type: "115"}, "share", "")
	if err != nil || len(items) != 2 || items[0].ID != "1234567890123456" || !items[0].IsDir || items[1].ID != "456" || items[1].IsDir {
		t.Fatalf("numeric directory compatibility: %#v %v", items, err)
	}
}

func TestQuarkShare404ClassifiesOnlyConfirmedUnavailable(t *testing.T) {
	old := apiClient.Transport
	t.Cleanup(func() { apiClient.Transport = old })
	for _, body := range []string{`{"status":404,"code":41006,"message":"private"}`, `<html>private gateway</html>`} {
		calls := 0
		apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		err := shareRequest(context.Background(), Storage{Type: "quark"}, "POST", "https://drive.quark.cn/1/clouddrive/share/sharepage/token", nil, &map[string]any{})
		var unavailable *quarkShareUnavailable
		if err == nil || errors.As(err, &unavailable) != strings.Contains(body, "41006") || calls != 1 || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe or inaccurate classification", calls, err)
		}
	}
}

func TestTelegramReturnRestoresOriginalDirectoryPage(t *testing.T) {
	a, cfg := telegramFixture(t)
	targets := []telegramTarget{}
	for i := 0; i < 7; i++ {
		targets = append(targets, telegramTarget{ID: string(rune('a' + i)), Provider: "quark", StorageID: "q", Parent: "0", Path: "/"})
	}
	job := telegramTransfer{ID: "back", ChatID: 123, UserID: 123, MessageID: 77, Auth: telegramDigest(cfg), Status: "confirming", Targets: targets, Page: 1, Selected: 6, Created: time.Now(), Link: "https://pan.quark.cn/s/code", Name: "分享目录"}
	a.store.update(func(st *State) error {
		st.Notifications.Targets, st.TelegramRuntime.Jobs = targets, []telegramTransfer{job}
		return nil
	})
	var text string
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			t.Fatal("return must not read or write cloud")
		}
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		text = body.Text
		return `{"ok":true,"result":true}`
	})
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "b:back:6"); err != nil {
		t.Fatal(err)
	}
	saved := a.store.snapshot().TelegramRuntime.Jobs[0]
	if saved.Status != "choosing" || saved.Selected != -1 || saved.Page != 1 || saved.Link != job.Link || !strings.Contains(text, "2/2") || !strings.Contains(text, job.Name) {
		t.Fatal("return did not restore page and resource", saved.Status, saved.Selected, text)
	}
	if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "c:back:6"); err == nil {
		t.Fatal("stale confirmation accepted after returning")
	}
}
