package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestTelegramDirectoryPromptShowsFirstFiveShareNames(t *testing.T) {
	a, cfg := telegramFixture(t)
	if err := a.store.update(func(st *State) error {
		for i := 1; i <= 5; i++ {
			st.Notifications.Targets = append(st.Notifications.Targets, telegramTarget{
				ID: fmt.Sprintf("q%d", i), Provider: "quark", StorageID: "q", Parent: fmt.Sprint(i), Path: fmt.Sprintf("/目录%d", i),
			})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	codes := []string{"third", "first", "fourth", "second", "fifth", "sixth", "seventh"}
	links := make([]string, len(codes))
	for i, code := range codes {
		links[i] = "https://pan.quark.cn/s/" + code
	}
	var reads, messages []string
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			if r.URL.Host != "drive.quark.cn" || r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/token") {
				t.Error("directory interaction performed a non-title cloud request", r.Method, r.URL.Path)
				return `{}`
			}
			var input map[string]string
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			code := input["pwd_id"]
			reads = append(reads, code)
			if code == "fourth" {
				return `{"code":0,"data":{}}`
			}
			return fmt.Sprintf(`{"code":0,"data":{"title":%q}}`, "片名\n"+code)
		}
		var markup telegramTestMarkup
		if err := json.NewDecoder(r.Body).Decode(&markup); err != nil {
			t.Error(err)
		}
		messages = append(messages, markup.Text)
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			return `{"ok":true,"result":{"message_id":77}}`
		}
		return `{"ok":true,"result":true}`
	})
	if err := a.handleTelegramUpdate(a.ctx, cfg, telegramTextUpdate(1, 123, 123, strings.Join(links, "\n"))); err != nil {
		t.Fatal(err)
	}
	const names = "分享名称：\n片名 third\n片名 first\n暂未获取\n片名 second\n片名 fifth\n…\n"
	if len(messages) != 1 || !strings.Contains(messages[0], names+"选择转存目录（1/2页）") || !strings.Contains(messages[0], "7条链接") {
		t.Fatal("initial directory message did not show the ordered title list", messages)
	}
	jobs := a.store.snapshot().TelegramRuntime.Jobs
	if len(jobs) != len(codes) || jobs[0].Status != "choosing" || jobs[0].MessageID != 77 {
		t.Fatal("title lookup changed the batch size or bypassed selection", len(jobs))
	}
	for _, action := range []struct {
		button string
		text   string
	}{
		{"p", "选择转存目录（2/2页）"},
		{"t", "确认转存到：夸克影视 · /目录5"},
		{"b", "选择转存目录（2/2页）"},
	} {
		index := 5
		if action.button == "p" {
			index = 1
		}
		if err := a.telegramCallback(a.ctx, cfg, 123, 123, 77, fmt.Sprintf("%s:%s:%d", action.button, jobs[0].ID, index)); err != nil {
			t.Fatal(err)
		}
		message := messages[len(messages)-1]
		if !strings.Contains(message, names) || !strings.Contains(message, action.text) {
			t.Fatal("directory navigation did not reuse the same title list", message)
		}
	}
	if len(messages) != 4 || strings.Join(reads, ",") != strings.Join(codes[:5], ",") {
		t.Fatal("titles were re-read, skipped out of order, or fetched beyond the preview", reads, messages)
	}
	for _, job := range a.store.snapshot().TelegramRuntime.Jobs {
		if job.Batches != 0 || job.Status != "choosing" && job.Status != "waiting" {
			t.Fatal("name preview changed the transfer state", job.Status, job.Batches)
		}
	}
}
