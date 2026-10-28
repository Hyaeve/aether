package app

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestDAVFixtureCleanupWaitsForSharedShutdown(t *testing.T) {
	canceled := make(chan struct{})
	release := make(chan struct{})
	cleanupDone := make(chan struct{})
	waited := make(chan bool, 1)
	go func() {
		select {
		case <-canceled:
			select {
			case <-cleanupDone:
				waited <- false
			case <-time.After(100 * time.Millisecond):
				waited <- true
			}
		case <-cleanupDone:
			waited <- false
		}
		close(release)
	}()

	var session *davReadSession
	t.Run("fixture", func(t *testing.T) {
		t.Cleanup(func() { close(cleanupDone) })
		a := testApp(t)
		ctx := context.WithValue(a.ctx, davReadClientKey{}, "shutdown-client")
		session = a.attachDAVRead(ctx, Storage{ID: "local", Type: "local"}, File{ID: "movie", Name: "movie.mkv", Size: 10})
		// Keep cancellation in flight until the fixture has had a chance to exit.
		session.mu.Lock()
		go func() {
			<-a.ctx.Done()
			close(canceled)
			<-release
			session.mu.Unlock()
		}()
	})
	if !<-waited {
		t.Error("fixture removed temporary files before shared DAV shutdown finished")
	}
	if session == nil {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		session.mu.Lock()
		finished := session.finished
		session.mu.Unlock()
		if finished {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("shared DAV cancellation did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDAVShutdownSettlesSessionsAndRejectsNewAttachments(t *testing.T) {
	for _, state := range []string{"active", "idle", "completed", "failed"} {
		t.Run(state, func(t *testing.T) {
			a := testApp(t)
			ctx := context.WithValue(a.ctx, davReadClientKey{}, "shutdown-client")
			pool := Storage{ID: "local", Type: "local"}
			file := File{ID: "movie", Name: "movie.mkv", Size: 10}
			session := a.attachDAVRead(ctx, pool, file)
			session.add(0, 4)
			want := "error"
			switch state {
			case "idle":
				session.close()
			case "completed":
				session.add(4, 6)
				session.close()
				want = "completed"
			case "failed":
				session.fail(errors.New("read failed"))
				want = "failed"
			}
			a.closeDAVReads()
			if !session.finished || want == "error" && !errors.Is(session.failure, context.Canceled) {
				t.Fatal("shutdown did not cancel unfinished session", session.finished, session.failure)
			}
			a.transfers.mu.Lock()
			entry := a.transfers.items[session.progress.id]
			a.transfers.mu.Unlock()
			if entry.Status != want || entry.Total != 10 || want != "completed" && entry.Done != 4 {
				t.Fatal("shutdown changed status or coverage", entry)
			}
			logs := len(a.store.logSnapshot())
			a.closeDAVReads()
			session.expireIdle()
			if a.attachDAVRead(ctx, pool, file) != nil || len(a.store.logSnapshot()) != logs || len(a.davReads) != 0 {
				t.Fatal("closed application created a session or logged again")
			}
			if state == "active" || state == "failed" {
				session.close()
			}
		})
	}
}

func TestDAVCanceledApplicationCannotRegisterSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := New(ctx, t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.closeDAVReads)
	cancel()
	before := len(a.store.logSnapshot())
	client := context.WithValue(ctx, davReadClientKey{}, "late-client")
	if a.attachDAVRead(client, Storage{ID: "local", Type: "local"}, File{ID: "movie", Size: 10}) != nil ||
		len(a.davReads) != 0 || len(a.store.logSnapshot()) != before {
		t.Fatal("canceled application registered a shared read")
	}
}

func TestDAVPruningKeepsPendingSessionOwnedUntilShutdown(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
	pool := Storage{ID: "local", Type: "local"}
	first := a.attachDAVRead(ctx, pool, File{ID: "first", Name: "first.mkv", Size: 10})
	first.add(0, 4)
	first.close()
	first.mu.Lock()
	first.timer.Stop()
	first.last = time.Now().Add(-davReadSessionIdle - time.Second)
	first.mu.Unlock()
	second := a.attachDAVRead(ctx, pool, File{ID: "second", Name: "second.mkv", Size: 10})
	if len(a.davReads) != 2 {
		t.Fatal("unfinished session lost its shutdown owner", len(a.davReads))
	}
	a.closeDAVReads()
	for _, session := range []*davReadSession{first, second} {
		if !session.finished || !errors.Is(session.failure, context.Canceled) {
			t.Fatal("orphaned session did not finish", session.finished, session.failure)
		}
	}
}

func TestDAVConcurrentAttachmentsAndShutdownStayClosed(t *testing.T) {
	a := testApp(t)
	ctx := context.WithValue(a.ctx, davReadClientKey{}, "client")
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			<-start
			if session := a.attachDAVRead(ctx, Storage{ID: "local", Type: "local"}, File{ID: strconv.Itoa(i), Size: 10}); session != nil {
				session.add(0, 4)
				session.close()
			}
		})
	}
	for range 2 {
		group.Go(func() { <-start; a.closeDAVReads() })
	}
	close(start)
	group.Wait()
	if !a.davReadClosed || len(a.davReads) != 0 {
		t.Fatal("concurrent shutdown left shared sessions registered")
	}
	if a.attachDAVRead(ctx, Storage{ID: "local", Type: "local"}, File{ID: "late", Size: 10}) != nil {
		t.Fatal("shutdown allowed a new shared read")
	}
}
