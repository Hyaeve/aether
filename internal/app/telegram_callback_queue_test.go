package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTelegramCallbackAnswersWhileReplyWorkersAreBusy(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	if err := a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs[0].Name = "已有分享名称"
		st.TelegramRuntime.Jobs[0].NamesLoaded = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	blocked := make(chan struct{}, telegramUIWorkers)
	answered := make(chan struct{}, 1)
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			t.Error("callback performed cloud I/O")
			return nil, errors.New("unexpected cloud request")
		}
		if strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			st := a.store.snapshotWithLogLimit(0)
			if st.TelegramRuntime.Offset != 2 || st.TelegramRuntime.Jobs[0].Status != "confirming" {
				t.Error("answer preceded the durable interaction")
			}
			answered <- struct{}{}
			return telegramUIResponse(r, `true`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			blocked <- struct{}{}
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return telegramUIResponse(r, `true`), nil
	})
	ctx, stop := startTelegramUIForTest(t, a)
	defer stop()
	for range telegramUIWorkers {
		reserved, release, err := a.reserveTelegramReply(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = a.telegramTextReply(reserved, cfg, 123, 123, "另一个交互的消息")
		release()
		if err != nil {
			t.Fatal(err)
		}
	}
	for range telegramUIWorkers {
		select {
		case <-blocked:
		case <-time.After(3 * time.Second):
			t.Fatal("reply workers did not reach the blocked transport")
		}
	}
	if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-answered:
	case <-time.After(time.Second):
		t.Fatal("button answer waited behind unrelated Telegram messages")
	}
}

func TestTelegramConfirmationRunsBeforeQueueMessageFinishes(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	if err := a.store.update(func(st *State) error {
		current := &st.TelegramRuntime.Jobs[0]
		current.Status, current.Selected = "confirming", 0
		current.Name, current.NamesLoaded = "已有分享名称", true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	editing, cloud := make(chan struct{}), make(chan struct{})
	var cloudCalls atomic.Int32
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			if cloudCalls.Add(1) == 1 {
				close(cloud)
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			close(editing)
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return telegramUIResponse(r, `true`), nil
	})
	ctx, stop := startTelegramUIForTest(t, a)
	defer stop()
	if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 1, "c:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-editing:
	case <-time.After(3 * time.Second):
		t.Fatal("queue message was not dispatched")
	}
	st := a.store.snapshotWithLogLimit(0)
	if st.TelegramRuntime.Jobs[0].QueuePending {
		t.Fatal("accepted confirmation still depends on editing its message")
	}
	if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 2, "c:"+job.ID+":0")); err == nil {
		t.Fatal("repeated confirmation was accepted")
	}
	a.wg.Add(1)
	go a.telegramWorker()
	select {
	case <-cloud:
	case <-time.After(3 * time.Second):
		t.Fatal("background transfer waited for its Telegram message")
	}
	st = a.store.snapshotWithLogLimit(0)
	if len(st.TelegramRuntime.Jobs) != 1 || st.TelegramRuntime.Jobs[0].Status != "running" {
		t.Fatal("confirmation did not preserve a single background task")
	}
}

func TestTelegramQueuedMessageSurvivesBackgroundClaim(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	if err := a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs[0].Status, st.TelegramRuntime.Jobs[0].Selected = "confirming", 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	var edits atomic.Int32
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			t.Error("queued message accessed cloud storage")
			return nil, errors.New("unexpected cloud request")
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			var body telegramTestMarkup
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Text != "已加入转存队列" {
				t.Error("queued message lost its accepted state", body.Text)
			}
			edits.Add(1)
		}
		return telegramUIResponse(r, `true`), nil
	})
	ctx := context.WithValue(a.ctx, telegramUIAsyncKey{}, true)
	if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 1, "c:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	if err := a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs[0].Status = "running"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, stop := startTelegramUIForTest(t, a)
	defer stop()
	waitTelegramUI(t, a)
	if edits.Load() != 1 {
		t.Fatal("background claim left the old confirmation buttons visible")
	}
}

func TestTelegramPollContinuesAfterConsumedCallbackRejection(t *testing.T) {
	a, _, job := telegramLatencyJob(t, "quark")
	if err := a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs[0].Status, st.TelegramRuntime.Jobs[0].Selected = "confirming", 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	updates := []telegramUpdate{telegramLatencyUpdate(t, 1, "c:"+job.ID+":0"), telegramLatencyUpdate(t, 2, "c:"+job.ID+":0"), telegramTextUpdate(3, 123, 123, "/start")}
	raw, err := json.Marshal(updates)
	if err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	var polls atomic.Int32
	consumed := make(chan int64, 1)
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			t.Error("interaction performed cloud I/O")
			return nil, errors.New("unexpected cloud request")
		}
		if strings.HasSuffix(r.URL.Path, "/getUpdates") {
			if polls.Add(1) == 1 {
				return telegramUIResponse(r, string(raw)), nil
			}
			consumed <- a.store.snapshotWithLogLimit(0).TelegramRuntime.Offset
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if strings.HasSuffix(r.URL.Path, "/getMyCommands") {
			return telegramUIResponse(r, `[]`), nil
		}
		return telegramUIResponse(r, `true`), nil
	})
	_, stop := startTelegramUIForTest(t, a)
	defer stop()
	a.wg.Add(1)
	go a.telegramPoll()
	select {
	case offset := <-consumed:
		if offset != 4 {
			t.Fatal("a consumed rejection delayed remaining updates", offset)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("poll did not advance")
	}
}
