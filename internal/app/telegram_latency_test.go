package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func telegramLatencyJob(t *testing.T, provider string) (*App, telegramConfig, telegramTransfer) {
	t.Helper()
	a, cfg := telegramFixture(t)
	storage := Storage{ID: "latency", Name: "影视存储", Type: provider, Enabled: true, Config: map[string]string{"cookie": test115Cookie}}
	link := "https://115.com/s/code"
	if provider == "quark" {
		link = "https://pan.quark.cn/s/code"
	}
	if provider == "mobile" {
		link = "https://yun.139.com/shareweb/#/w/i/code?pwd=Ab12"
		storage.Config = map[string]string{"mode": "native", "authorization": base64.StdEncoding.EncodeToString([]byte("pc:account:token"))}
	}
	target := telegramTarget{ID: "target", StorageID: storage.ID, Provider: provider, Parent: "destination", Path: "/电影"}
	job := telegramTransfer{ID: "latency", ChatID: 123, UserID: 123, MessageID: 77, Link: link, Provider: provider, Auth: telegramDigest(cfg), Bot: telegramDigest(cfg.Token),
		Status: "choosing", Selected: -1, Created: time.Now(), Targets: []telegramTarget{target}, TargetStamps: map[string]string{storage.ID: storageRevision(storage)}}
	if err := a.store.update(func(st *State) error {
		st.Storages = []Storage{storage}
		st.Notifications.Targets = []telegramTarget{target}
		st.TelegramRuntime.Jobs = []telegramTransfer{job}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return a, cfg, job
}

func telegramLatencyUpdate(t *testing.T, updateID int64, data string) telegramUpdate {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"update_id": updateID, "callback_query": map[string]any{
		"id": fmt.Sprint(updateID), "from": map[string]any{"id": 123},
		"message": map[string]any{"message_id": 77, "chat": map[string]any{"id": 123}}, "data": data,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var update telegramUpdate
	if err := json.Unmarshal(raw, &update); err != nil {
		t.Fatal(err)
	}
	return update
}

func TestTelegramConfirmationTitleFirstPage(t *testing.T) {
	for _, provider := range []string{"115", "mobile", "quark"} {
		t.Run(provider, func(t *testing.T) {
			a, cfg, job := telegramLatencyJob(t, provider)
			reads, edits := 0, 0
			shareMock(t, func(r *http.Request) string {
				if r.URL.Host == "api.telegram.org" {
					if !strings.HasSuffix(r.URL.Path, "/editMessageText") {
						t.Errorf("unexpected bot write: %s", r.URL.Path)
					}
					edits++
					var markup telegramTestMarkup
					if err := json.NewDecoder(r.Body).Decode(&markup); err != nil {
						t.Error(err)
					}
					if !strings.Contains(markup.Text, "影片000.mkv、影片001.mkv") || len(markup.Markup.Rows) != 1 || markup.Markup.Rows[0][1].Text != "确认转存" {
						t.Errorf("missing original names or confirmation: %+v", markup)
					}
					return `{"ok":true,"result":true}`
				}
				if provider == "quark" && strings.HasSuffix(r.URL.Path, "/token") {
					return `{"code":0,"data":{"stoken":"readonly"}}`
				}
				reads++
				start := 0
				switch provider {
				case "115":
					if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/snap") {
						t.Errorf("title lookup is not read-only: %s %s", r.Method, r.URL.Path)
					}
					fmt.Sscan(r.URL.Query().Get("offset"), &start)
				case "quark":
					if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/detail") {
						t.Errorf("title lookup is not read-only: %s %s", r.Method, r.URL.Path)
					}
					var page int
					fmt.Sscan(r.URL.Query().Get("_page"), &page)
					start = (page - 1) * 100
				case "mobile":
					if !strings.HasSuffix(r.URL.Path, "/getOutLinkInfoV6") {
						t.Errorf("title lookup submitted cloud operation: %s", r.URL.Path)
					}
					var body struct {
						Request struct {
							Start int `json:"bNum"`
						} `json:"getOutLinkInfoReq"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					start = body.Request.Start - 1
				}
				var files []map[string]any
				for i := start; i < min(start+100, 101); i++ {
					files = append(files, map[string]any{"fid": fmt.Sprint(i + 1), "n": fmt.Sprintf("影片%03d.mkv", i), "file_name": fmt.Sprintf("影片%03d.mkv", i),
						"coID": fmt.Sprint(i + 1), "coName": fmt.Sprintf("影片%03d.mkv", i), "coPath": fmt.Sprintf("root/%d", i+1)})
				}
				body := map[string]any{"code": 0, "state": true, "data": map[string]any{"count": 101, "list": files, "coLst": files}, "metadata": map[string]any{"_total": 101}}
				raw, _ := json.Marshal(body)
				return string(raw)
			})
			if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "t:"+job.ID+":0"); err != nil {
				t.Fatal(err)
			}
			if reads != 1 || edits != 1 || a.store.snapshot().TelegramRuntime.Jobs[0].Status != "confirming" {
				t.Fatalf("confirmation waited for all pages: reads=%d edits=%d", reads, edits)
			}
			storage, _ := a.store.storage("latency")
			_, code, pass, _ := parseShareLink(job.Link, "")
			reads = 0
			_, items, err := a.readShare(context.Background(), storage, code, pass)
			if err != nil || reads != 2 || len(items) != 101 {
				t.Fatalf("full transfer scan was truncated: reads=%d items=%d err=%v", reads, len(items), err)
			}
		})
	}
}

func TestTelegramConfirmationTitleBudget(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	var edits int
	var budget time.Duration
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			deadline, ok := r.Context().Deadline()
			if !ok {
				t.Error("name lookup has no deadline")
			}
			budget = time.Until(deadline)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		edits++
		if !strings.HasSuffix(r.URL.Path, "/editMessageText") {
			t.Errorf("unexpected message write: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Request: r}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.telegramCallback(ctx, cfg, 123, 123, 77, "t:"+job.ID+":0"); err != nil {
		t.Fatal(err)
	}
	if budget <= 0 || budget > 1500*time.Millisecond || edits != 1 {
		t.Fatalf("slow title delayed confirmation: budget=%s edits=%d", budget, edits)
	}
	current := a.store.snapshot().TelegramRuntime.Jobs[0]
	if current.Name != "" || current.Status != "confirming" || current.Batches != 0 {
		t.Fatal("timeout fabricated a name, queued or submitted the transfer")
	}
}

func TestTelegramCallbackAnswersDuringTitleRead(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	answered := make(chan struct{})
	var answers, edits atomic.Int32
	var answeredBeforeTitle atomic.Bool
	shareMock(t, func(r *http.Request) string {
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			if answers.Add(1) == 1 {
				current := a.store.snapshot().TelegramRuntime.Jobs[0]
				if current.Status != "confirming" || a.store.snapshot().TelegramRuntime.Offset != 2 {
					t.Error("callback acknowledged before durable authorization and state")
				}
				close(answered)
			}
			return `{"ok":true,"result":true}`
		}
		if r.URL.Host != "api.telegram.org" {
			if strings.HasSuffix(r.URL.Path, "/token") {
				select {
				case <-answered:
					answeredBeforeTitle.Store(true)
				case <-time.After(250 * time.Millisecond):
				}
				return `{"code":0,"data":{"stoken":"readonly"}}`
			}
			return `{"code":0,"data":{"list":[{"fid":"root","file_name":"分享目录","dir":true}]},"metadata":{"_total":1}}`
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits.Add(1)
			return `{"ok":true,"result":true}`
		}
		t.Errorf("unexpected request: %s", r.URL.Path)
		return `{}`
	})
	if err := a.handleTelegramUpdate(context.Background(), cfg, telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	if !answeredBeforeTitle.Load() || answers.Load() != 1 || edits.Load() != 1 {
		t.Fatal("button acknowledgement still waits for the cloud title", answeredBeforeTitle.Load(), answers.Load(), edits.Load())
	}
}

func TestTelegramCallbackAnswerBudget(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	if err := a.store.update(func(st *State) error { st.TelegramRuntime.Jobs[0].Name = "已有分享名称"; return nil }); err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	var edits atomic.Int32
	var budget time.Duration
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			deadline, ok := r.Context().Deadline()
			if !ok {
				t.Error("callback acknowledgement has no deadline")
			}
			budget = time.Until(deadline)
			<-r.Context().Done()
			if edits.Load() != 1 {
				t.Error("slow acknowledgement blocked the confirmation edit")
			}
			return nil, r.Context().Err()
		}
		if r.URL.Host == "api.telegram.org" && strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits.Add(1)
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Request: r}, nil
		}
		return nil, errors.New("cached title unexpectedly read the cloud")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	if budget <= 0 || budget > 2*time.Second || edits.Load() != 1 {
		t.Fatalf("acknowledgement held the update loop: budget=%s edits=%d", budget, edits.Load())
	}
}

func TestTelegramFastCallbackStillRejectsStaleTargets(t *testing.T) {
	for _, scenario := range []string{"expired", "credentials", "target", "foreign-message", "replay"} {
		t.Run(scenario, func(t *testing.T) {
			a, cfg, job := telegramLatencyJob(t, "quark")
			a.store.update(func(st *State) error {
				switch scenario {
				case "expired":
					st.TelegramRuntime.Jobs[0].Created = time.Now().Add(-11 * time.Minute)
				case "credentials":
					st.Storages[0].Config["cookie"] = "new-cookie"
				case "target":
					st.Notifications.Targets[0].Parent = "new-target"
				case "foreign-message":
					st.TelegramRuntime.Jobs[0].MessageID = 78
				case "replay":
					st.TelegramRuntime.Jobs[0].Status = "queued"
				}
				return nil
			})
			calls := 0
			shareMock(t, func(r *http.Request) string {
				calls++
				if r.URL.Host != "api.telegram.org" || !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
					t.Errorf("invalid callback performed work: %s", r.URL.Path)
				}
				var body map[string]any
				json.NewDecoder(r.Body).Decode(&body)
				if !strings.Contains(fmt.Sprint(body["text"]), "过期") {
					t.Error("invalid callback lost its user feedback")
				}
				return `{"ok":true,"result":true}`
			})
			if err := a.handleTelegramUpdate(context.Background(), cfg, telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err == nil || calls != 1 {
				t.Fatal("invalid callback accepted or acknowledged repeatedly", err, calls)
			}
			if a.store.snapshot().TelegramRuntime.Jobs[0].Batches != 0 {
				t.Fatal("invalid callback submitted a transfer")
			}
		})
	}
}
