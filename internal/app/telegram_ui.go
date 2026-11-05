package app

import (
	"context"
	"errors"
	"time"
)

// Only the live poll/worker opts into asynchronous replies. State transitions
// stay ordered in the poll; slow Telegram I/O never holds up the next update.
type telegramUIAsyncKey struct{}
type telegramUIReplyKey struct{}
type telegramUIReply struct {
	a        *App
	reserved bool
}

const telegramUIWorkers = 4

func (a *App) reserveTelegramReply(ctx context.Context) (context.Context, func(), error) {
	if ctx.Value(telegramUIReplyKey{}) != nil || ctx.Value(telegramUIAsyncKey{}) != true {
		return ctx, func() {}, nil
	}
	if ctx.Err() != nil {
		return ctx, func() {}, ctx.Err()
	}
	select {
	case a.telegramUISlots <- struct{}{}:
		reply := &telegramUIReply{a: a, reserved: true}
		return context.WithValue(ctx, telegramUIReplyKey{}, reply), func() {
			if reply.reserved {
				<-a.telegramUISlots
				reply.reserved = false
			}
		}, nil
	default:
		// No offset or interaction state has been changed by the caller yet.
		return ctx, func() {}, errors.New("TG 交互响应队列已满，稍后继续接收")
	}
}

func telegramReply(ctx context.Context, work func(context.Context) error) error {
	if reply, ok := ctx.Value(telegramUIReplyKey{}).(*telegramUIReply); ok && reply.reserved {
		// The slot was reserved before the durable state transition, so this send
		// cannot wait for a busy worker. Every accepted update has at most one reply.
		reply.a.telegramUI <- work
		reply.reserved = false
		return nil
	}
	return work(ctx)
}

func (a *App) telegramUIWorker() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case work := <-a.telegramUI:
			if a.ctx.Err() == nil {
				ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
				err := work(ctx)
				cancel()
				if err != nil && a.ctx.Err() == nil {
					a.store.event("warn", "notifications", "TG 交互响应未确认；未自动重发")
				}
			}
			<-a.telegramUISlots
		}
	}
}

func (a *App) telegramTextReply(ctx context.Context, cfg telegramConfig, chat, user int64, text string) error {
	return telegramReply(ctx, func(ctx context.Context) error {
		current := a.store.snapshotWithLogLimit(0).Notifications.Telegram
		if telegramDigest(current) != telegramDigest(cfg) || !telegramAllowed(current, chat, user) {
			return errors.New("TG 授权已变化，未发送响应")
		}
		_, err := a.telegramSend(ctx, cfg, chat, text, nil)
		return err
	})
}

func (a *App) releaseTelegramQueue(expected telegramTransfer) error {
	return a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			job := &st.TelegramRuntime.Jobs[i]
			if job.ID == expected.ID && job.Auth == expected.Auth || telegramSameGroup(*job, expected) {
				job.QueuePending = false
			}
		}
		return nil
	})
}
