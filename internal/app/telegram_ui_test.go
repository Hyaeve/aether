package app

import (
	"context"
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

func startTelegramUIForTest(t *testing.T, a *App) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(a.ctx)
	a.ctx = ctx
	a.wg.Add(telegramUIWorkers)
	for range telegramUIWorkers {
		go a.telegramUIWorker()
	}
	return context.WithValue(ctx, telegramUIAsyncKey{}, true), func() { cancel(); a.wg.Wait() }
}

func telegramUIResponse(r *http.Request, result string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":` + result + `}`)), Request: r}
}

func waitTelegramUI(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for len(a.telegramUISlots) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("Telegram replies did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTelegramAsyncPollReceivesWhileSendBlocked(t *testing.T) {
	a, _ := telegramFixture(t)
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	polledAgain, sending := make(chan struct{}), make(chan struct{})
	var polls atomic.Int32
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			t.Error("interaction performed cloud I/O")
			return nil, errors.New("unexpected cloud call")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMyCommands"):
			return telegramUIResponse(r, `[]`), nil
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			if polls.Add(1) == 1 {
				return telegramUIResponse(r, `[{"update_id":1,"message":{"message_id":1,"chat":{"id":123},"from":{"id":123},"text":"/start"}}]`), nil
			}
			close(polledAgain)
			<-r.Context().Done()
			return nil, r.Context().Err()
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			close(sending)
			<-r.Context().Done()
			return nil, r.Context().Err()
		default:
			return telegramUIResponse(r, `true`), nil
		}
	})
	_, stop := startTelegramUIForTest(t, a)
	defer stop()
	a.wg.Add(1)
	go a.telegramPoll()
	for _, signal := range []chan struct{}{sending, polledAgain} {
		select {
		case <-signal:
		case <-time.After(3 * time.Second):
			t.Fatal("poll waited for a blocked outgoing response")
		}
	}
	if a.store.snapshot().TelegramRuntime.Offset != 2 {
		t.Fatal("update offset was not durable before response")
	}
}

func TestTelegramAsyncChooseAndConfirmPersistBeforeSlowEdit(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	ctx, stop := startTelegramUIForTest(t, a)
	defer stop()
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var editsMu sync.Mutex
	var edits []string
	var editCount atomic.Int32
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			t.Error("choosing or confirming contacted cloud storage")
			return nil, errors.New("unexpected cloud request")
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			var body struct{ Text string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if editCount.Add(1) == 1 {
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			editsMu.Lock()
			edits = append(edits, body.Text)
			editsMu.Unlock()
		}
		return telegramUIResponse(r, `true`), nil
	})
	call := func(update telegramUpdate) error {
		done := make(chan error, 1)
		go func() { done <- a.handleTelegramUpdate(ctx, cfg, update) }()
		select {
		case err := <-done:
			return err
		case <-time.After(3 * time.Second):
			once.Do(func() { close(release) })
			<-done
			t.Fatal("interaction waited for Telegram network")
			return nil
		}
	}
	if err := call(telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("confirmation response was not dispatched")
	}
	if err := call(telegramLatencyUpdate(t, 2, "c:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	current := a.store.snapshot().TelegramRuntime.Jobs[0]
	if current.Status != "queued" || !current.QueuePending || current.Batches != 0 {
		t.Fatal("confirm did not durably enqueue without cloud submission", current.Status, current.QueuePending, current.Batches)
	}
	if err := call(telegramLatencyUpdate(t, 3, "c:"+job.ID+":0")); err == nil {
		t.Fatal("duplicate confirmation accepted")
	}
	once.Do(func() { close(release) })
	waitTelegramUI(t, a)
	current = a.store.snapshot().TelegramRuntime.Jobs[0]
	editsMu.Lock()
	defer editsMu.Unlock()
	if current.Status != "queued" || current.QueuePending || len(edits) != 2 || !strings.Contains(edits[1], "已加入转存队列") {
		t.Fatal("late confirmation replaced queued response or queue stayed blocked", current.Status, current.QueuePending, edits)
	}
}

func TestTelegramAsyncInitialQueueNotice(t *testing.T) {
	for _, prefix := range []string{"", "/share ", "/cas "} {
		t.Run(fmt.Sprintf("command=%s", prefix), func(t *testing.T) {
			a, cfg := telegramFixture(t)
			old := apiClient.Transport
			defer func() { apiClient.Transport = old }()
			ctx, stop := startTelegramUIForTest(t, a)
			defer stop()
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			defer once.Do(func() { close(release) })
			var sends atomic.Int32
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.telegram.org" || !strings.HasSuffix(r.URL.Path, "/sendMessage") {
					t.Error("initial queue acknowledgment contacted cloud storage or sent an unexpected request")
					return nil, errors.New("unexpected request")
				}
				var body struct{ Text string }
				_ = json.NewDecoder(r.Body).Decode(&body)
				if !strings.Contains(body.Text, "队列") {
					t.Error("missing immediate queue feedback", body.Text)
				}
				sends.Add(1)
				close(started)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				return nil, errors.New("unknown send outcome")
			})
			link := "https://pan.quark.cn/s/code"
			if prefix == "/cas " {
				link = "https://yun.139.com/shareweb/#/w/i/code?pwd=Ab12"
			}
			update := telegramTextUpdate(1, 123, 123, prefix+link)
			if err := a.handleTelegramUpdate(ctx, cfg, update); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("no initial queue acknowledgment")
			}
			jobs := a.store.snapshot().TelegramRuntime.Jobs
			if len(jobs) != 1 || jobs[0].Status != "queued" || !jobs[0].QueuePending || jobs[0].Batches != 0 {
				t.Fatal("job started before queue notice attempt")
			}
			once.Do(func() { close(release) })
			waitTelegramUI(t, a)
			if err := a.handleTelegramUpdate(ctx, cfg, update); err != nil {
				t.Fatal(err)
			}
			jobs = a.store.snapshot().TelegramRuntime.Jobs
			if len(jobs) != 1 || jobs[0].Status != "queued" || jobs[0].QueuePending || sends.Load() != 1 {
				t.Fatal("unknown notification outcome replayed, discarded or blocked authorized task")
			}
		})
	}
}

func TestTelegramAsyncQueueFullPreservesUpdateForLater(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	ctx := context.WithValue(a.ctx, telegramUIAsyncKey{}, true)
	releases := []func(){}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	for range cap(a.telegramUISlots) {
		_, release, err := a.reserveTelegramReply(ctx)
		if err != nil {
			t.Fatal(err)
		}
		releases = append(releases, release)
	}
	update := telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")
	if err := a.handleTelegramUpdate(ctx, cfg, update); err == nil {
		t.Fatal("unbounded interaction queue")
	}
	st := a.store.snapshot()
	if st.TelegramRuntime.Offset != 0 || st.TelegramRuntime.Jobs[0].Status != "choosing" {
		t.Fatal("queue exhaustion consumed update or changed interaction")
	}
	releases[0]()
	if err := a.handleTelegramUpdate(ctx, cfg, update); err != nil {
		t.Fatal(err)
	}
	if len(a.telegramUI) != 1 || a.store.snapshot().TelegramRuntime.Jobs[0].Status != "confirming" {
		t.Fatal("retained update did not resume after capacity available")
	}
	// This test deliberately does not start a worker or any network operation.
	<-a.telegramUI
	<-a.telegramUISlots
}

func TestTelegramAsyncTransferWorkerKeepsInteractionsAvailable(t *testing.T) {
	a, cfg, job := telegramLatencyJob(t, "quark")
	queued := job
	queued.ID, queued.Status, queued.Selected = "background", "queued", 0
	if err := a.store.update(func(st *State) error {
		st.TelegramRuntime.Jobs = append([]telegramTransfer{queued}, st.TelegramRuntime.Jobs...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := apiClient.Transport
	defer func() { apiClient.Transport = old }()
	cloudStarted, confirmation := make(chan struct{}), make(chan struct{}, 1)
	var once sync.Once
	apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.telegram.org" {
			once.Do(func() { close(cloudStarted) })
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			var body struct{ Text string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			if strings.Contains(body.Text, "确认转存到") {
				confirmation <- struct{}{}
			}
		}
		return telegramUIResponse(r, `true`), nil
	})
	ctx, stop := startTelegramUIForTest(t, a)
	defer stop()
	a.wg.Add(1)
	go a.telegramWorker()
	select {
	case <-cloudStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("queued transfer was not executed in the background")
	}
	if a.store.snapshot().TelegramRuntime.Jobs[0].Status != "running" {
		t.Fatal("worker did not claim queued task")
	}
	if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-confirmation:
	case <-time.After(3 * time.Second):
		t.Fatal("another interaction waited for a blocked cloud task")
	}
	if a.store.snapshot().TelegramRuntime.Jobs[1].Status != "confirming" {
		t.Fatal("new interaction was not persisted")
	}
}

func TestTelegramAsyncDropsRevokedAndStaleReplies(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprint(revoked), func(t *testing.T) {
			a, cfg, job := telegramLatencyJob(t, "quark")
			ctx := context.WithValue(a.ctx, telegramUIAsyncKey{}, true)
			old := apiClient.Transport
			defer func() { apiClient.Transport = old }()
			var edits atomic.Int32
			apiClient.Transport = casTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.telegram.org" || revoked {
					t.Error("revoked or cloud request escaped the response queue")
				}
				if strings.HasSuffix(r.URL.Path, "/editMessageText") {
					edits.Add(1)
					var body struct{ Text string }
					_ = json.NewDecoder(r.Body).Decode(&body)
					if !strings.Contains(body.Text, "选择") {
						t.Error("stale confirmation overwrote newer directory menu", body.Text)
					}
				}
				return telegramUIResponse(r, `true`), nil
			})
			if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 1, "t:"+job.ID+":0")); err != nil {
				t.Fatal(err)
			}
			if revoked {
				if err := a.store.update(func(st *State) error { st.Notifications.Telegram.Enabled = false; return nil }); err != nil {
					t.Fatal(err)
				}
			} else if err := a.handleTelegramUpdate(ctx, cfg, telegramLatencyUpdate(t, 2, "b:"+job.ID+":0")); err != nil {
				t.Fatal(err)
			}
			_, stop := startTelegramUIForTest(t, a)
			defer stop()
			waitTelegramUI(t, a)
			if revoked && edits.Load() != 0 || !revoked && edits.Load() != 1 {
				t.Fatal("unexpected stale/revoked edit count", edits.Load())
			}
		})
	}
}
