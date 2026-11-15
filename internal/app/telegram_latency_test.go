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
	"sync"
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

func TestTelegramConfirmationWaitsForCloudTitle(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	edited, cloud := make(chan string, 2), make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			cloud <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			if !strings.HasSuffix(r.URL.Path, "/token") {
				t.Error("read share contents after receiving the title")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"stoken":"readonly","title":"完整影片名称"}}`)), Request: r}, nil
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			var markup telegramTestMarkup
			if err := json.NewDecoder(r.Body).Decode(&markup); err != nil {
				t.Error(err)
			}
			edited <- markup.Text
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":true}`)), Request: r}, nil
	})
	done := make(chan error, 1)
	go func() { done <- a.telegramCallback(a.ctx, cfg, 123, 123, 77, "t:"+job.ID+":0") }()
	select {
	case text := <-edited:
		once.Do(func() { close(release) })
		<-done
		t.Fatal("confirmation appeared before cloud title lookup", text)
	case <-cloud:
	case <-time.After(3 * time.Second):
		once.Do(func() { close(release) })
		<-done
		t.Fatal("share title lookup did not start")
	}
	select {
	case text := <-edited:
		once.Do(func() { close(release) })
		<-done
		t.Fatal("confirmation appeared before the title response", text)
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(edited) != 1 {
		t.Fatal("expected exactly one confirmation edit", len(edited))
	}
	if text := <-edited; !strings.Contains(text, "分享名称：完整影片名称") || !strings.Contains(text, "影视存储 · /电影") {
		t.Fatal("first confirmation was incomplete", text)
	}
	current := a.store.snapshot().TelegramRuntime.Jobs[0]
	if current.Status != "confirming" || current.Name != "完整影片名称" || current.Batches != 0 {
		t.Fatal("showing prompt submitted a transfer", current.Status, current.Batches)
	}
}

func TestTelegramConfirmationTitleUsesMetadata(t *testing.T) {
	for _, provider := range []string{"115", "mobile", "quark", "115-fallback", "quark-fallback"} {
		t.Run(provider, func(t *testing.T) {
			kind := strings.TrimSuffix(provider, "-fallback")
			a, cfg, job := telegramLatencyJob(t, kind)
			fallback := kind != provider
			title := "官方分享标题"
			if fallback {
				title = "影片000.mkv、影片001.mkv"
			}
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
					if !strings.Contains(markup.Text, "分享名称："+title) || len(markup.Markup.Rows) != 1 || len(markup.Markup.Rows[0]) != 2 || markup.Markup.Rows[0][1].Text != "确认转存" {
						t.Errorf("missing original names or confirmation: %+v", markup)
					}
					return `{"ok":true,"result":true}`
				}
				reads++
				if kind == "quark" && strings.HasSuffix(r.URL.Path, "/token") {
					if fallback {
						return `{"code":0,"data":{"stoken":"readonly"}}`
					}
					return `{"code":0,"data":{"stoken":"readonly","title":"官方分享标题"}}`
				}
				switch kind {
				case "115":
					q := r.URL.Query()
					if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/snap") || q.Get("cid") != "0" || q.Get("offset") != "0" || q.Get("limit") != "2" {
						t.Errorf("115 title lookup read beyond the two root entries: %s %s", r.Method, r.URL)
					}
					if fallback {
						return `{"state":true,"data":{"count":101,"list":[{"n":"影片000.mkv"},{"n":"影片001.mkv"}]}}`
					}
					return `{"state":true,"data":{"count":101,"shareinfo":{"share_title":"官方分享标题"},"list":[{"n":"不可作为标题.mkv"}]}}`
				case "quark":
					q := r.URL.Query()
					if !fallback || r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/detail") || q.Get("pdir_fid") != "0" || q.Get("_page") != "1" || q.Get("_size") != "2" {
						t.Errorf("unexpected Quark content request: %s %s", r.Method, r.URL)
					}
					return `{"code":0,"data":{"list":[{"file_name":"影片000.mkv"},{"file_name":"影片001.mkv"}]},"metadata":{"_total":101}}`
				case "mobile":
					if !strings.HasSuffix(r.URL.Path, "/getOutLinkGeneral") {
						t.Errorf("mobile title lookup read share contents: %s", r.URL.Path)
					}
					return `{"resultCode":"0","data":{"getOutLinkGeneralResp":{"outLinkGeneral":[{"lkName":"官方分享标题"}]}}}`
				}
				return `{}`
			})
			if err := a.telegramCallback(context.Background(), cfg, 123, 123, 77, "t:"+job.ID+":0"); err != nil {
				t.Fatal(err)
			}
			wantReads := 1
			if provider == "quark-fallback" {
				wantReads = 2
			}
			current := a.store.snapshot().TelegramRuntime.Jobs[0]
			if reads != wantReads || edits != 1 || current.Name != title || current.Status != "confirming" || current.Batches != 0 {
				t.Fatalf("unexpected confirmation: reads=%d edits=%d name=%q status=%s batches=%d", reads, edits, current.Name, current.Status, current.Batches)
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
		var markup telegramTestMarkup
		if err := json.NewDecoder(r.Body).Decode(&markup); err != nil ||
			!strings.Contains(markup.Text, "分享名称：暂未获取") || !strings.Contains(markup.Text, "/电影") ||
			len(markup.Markup.Rows) != 1 || len(markup.Markup.Rows[0]) != 2 || markup.Markup.Rows[0][1].Text != "确认转存" {
			t.Errorf("incomplete confirmation after title timeout: %+v, %v", markup, err)
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
				feedback := "过期"
				if scenario == "replay" {
					feedback = "已处理"
				}
				if !strings.Contains(fmt.Sprint(body["text"]), feedback) {
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

func TestTelegramLateTitleCannotChangeNewInteraction(t *testing.T) {
	for _, scenario := range []string{"back", "confirm", "cancel", "credentials", "target", "authorization"} {
		t.Run(scenario, func(t *testing.T) {
			a, cfg, job := telegramLatencyJob(t, "quark")
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var edits atomic.Int32
			shareMock(t, func(r *http.Request) string {
				if r.URL.Host == "api.telegram.org" {
					edits.Add(1)
					return `{"ok":true,"result":true}`
				}
				if strings.HasSuffix(r.URL.Path, "/token") {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
					}
					return `{"code":0,"data":{"stoken":"read-only","title":"迟到的影片.mkv"}}`
				}
				t.Error("unexpected share contents request", r.URL.Path)
				return `{}`
			})
			done := make(chan error, 1)
			go func() { done <- a.telegramCallback(a.ctx, cfg, 123, 123, 77, "t:"+job.ID+":0") }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("title request did not start")
			}
			wantEdits := int32(0)
			action := map[string]string{"back": "b", "confirm": "c", "cancel": "x"}[scenario]
			if action != "" {
				if err := a.telegramCallback(a.ctx, cfg, 123, 123, 77, action+":"+job.ID+":0"); err != nil {
					t.Fatal(err)
				}
				wantEdits++
			} else {
				a.store.update(func(st *State) error {
					switch scenario {
					case "credentials":
						st.Storages[0].Config["cookie"] = "changed"
					case "target":
						st.Notifications.Targets[0].Parent = "changed"
					case "authorization":
						st.Notifications.Telegram.UserIDs = nil
					}
					return nil
				})
			}
			once.Do(func() { close(release) })
			if err := <-done; err == nil {
				t.Fatal("stale selection was accepted")
			}
			current := a.store.snapshot().TelegramRuntime.Jobs[0]
			if current.Name != "" || edits.Load() != wantEdits || current.Batches != 0 {
				t.Fatal("late name overwrote newer interaction or submitted work", current.Name, edits.Load(), current.Batches)
			}
		})
	}
}

func TestTelegramConfirmedActionFollowsInFlightConfirmationEdit(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	writes := make(chan string, 4)
	shareMock(t, func(r *http.Request) string {
		if r.URL.Host != "api.telegram.org" {
			if strings.HasSuffix(r.URL.Path, "/token") {
				return `{"code":0,"data":{"stoken":"read-only","title":"影片.mkv"}}`
			}
			t.Error("unexpected share contents request", r.URL.Path)
			return `{}`
		}
		var body struct {
			Text string `json:"text"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.Text, "分享名称：影片.mkv") {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		writes <- body.Text
		return `{"ok":true,"result":true}`
	})
	selection := make(chan error, 1)
	go func() { selection <- a.telegramCallback(a.ctx, cfg, 123, 123, 77, "t:"+job.ID+":0") }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("complete confirmation did not reach Telegram")
	}
	done := make(chan error, 1)
	go func() { done <- a.telegramCallback(a.ctx, cfg, 123, 123, 77, "c:"+job.ID+":0") }()
	deadline := time.Now().Add(time.Second)
	for a.store.snapshot().TelegramRuntime.Jobs[0].Status != "queued" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	once.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-selection; err != nil {
		t.Fatal(err)
	}
	close(writes)
	texts := []string{}
	for text := range writes {
		texts = append(texts, text)
	}
	if len(texts) != 2 || !strings.Contains(texts[0], "分享名称：影片.mkv") || texts[1] != "已加入转存队列" {
		t.Fatal("confirmation overwrote the queued state", texts)
	}
}
