package app

import (
	"context"
	"errors"
	"strings"
)

type shareBatchReceiptKey struct{}

type shareBatchReceipt struct {
	before           func() error
	directoriesReady func() error
	accepted         func(string) error
}

func beforeShareBatchWrite(ctx context.Context) error {
	if receipt, _ := ctx.Value(shareBatchReceiptKey{}).(*shareBatchReceipt); receipt != nil && receipt.before != nil {
		return receipt.before()
	}
	return nil
}

func (a *App) updateShareLiveReceipt(link shareLiveLink, update func(*shareLiveLink) error) error {
	return a.store.update(func(st *State) error {
		if _, err := shareLiveGuardState(*st, link); err != nil {
			return err
		}
		for i := range st.ShareLiveLinks {
			current := &st.ShareLiveLinks[i]
			if current.ID != link.ID {
				continue
			}
			if current.Link != link.Link || current.Stamp != link.Stamp || current.TargetID != link.TargetID || current.Provider != link.Provider ||
				current.StorageID != link.StorageID || current.Parent != link.Parent || current.DirectoryID != link.DirectoryID ||
				current.DirectoryName != link.DirectoryName || current.IncrementalCursor != link.IncrementalCursor {
				return errors.New("活链来源、目录或新增基线已变化，未更新写入回执")
			}
			return update(current)
		}
		return errors.New("活链记录已移除，未更新写入回执")
	})
}

func (a *App) shareLiveBatchReceipt(link shareLiveLink, intent, cursor string) *shareBatchReceipt {
	return &shareBatchReceipt{
		before: func() error {
			return a.updateShareLiveReceipt(link, func(current *shareLiveLink) error {
				if current.Intent != "" && (current.Intent != intent || current.PendingCursor != cursor) || current.TransferTaskID != "" {
					return errors.New("上次活链写入仍待确认，未提交新批次")
				}
				current.Intent, current.PendingCursor = intent, cursor
				return nil
			})
		},
		directoriesReady: func() error {
			return a.updateShareLiveReceipt(link, func(current *shareLiveLink) error {
				if current.Intent != "" && (current.Intent != intent || current.PendingCursor != cursor) || current.TransferTaskID != "" {
					return errors.New("活链目录创建回执已变化")
				}
				current.Intent, current.PendingCursor = "", ""
				return nil
			})
		},
		accepted: func(task string) error {
			return a.updateShareLiveReceipt(link, func(current *shareLiveLink) error {
				if current.Intent != intent || current.PendingCursor != cursor {
					return errors.New("活链转存回执已变化，未登记任务编号")
				}
				current.TransferTaskID = task
				return nil
			})
		},
	}
}

func (a *App) commitShareLiveBatch(link *shareLiveLink, intent, cursor, task string) error {
	err := a.updateShareLiveReceipt(*link, func(current *shareLiveLink) error {
		if current.Intent != "" && (current.Intent != intent || current.PendingCursor != cursor || current.TransferTaskID != task) ||
			current.Intent == "" && (task != "" || current.TransferTaskID != "" || current.PendingCursor != "") {
			return errors.New("活链批次回执已变化，未推进新增基线")
		}
		current.IncrementalCursor = cursor
		current.Intent, current.TransferTaskID, current.PendingCursor = "", "", ""
		return nil
	})
	if err == nil {
		previous := link.IncrementalCursor
		link.IncrementalCursor = cursor
		link.Intent, link.TransferTaskID, link.PendingCursor = "", "", ""
		a.removeShareLiveCheckpoint(*link, previous)
	}
	return err
}

func shareLiveTransferRecoverable(link shareLiveLink) bool {
	return link.Provider == "quark" && strings.HasPrefix(link.Intent, "batch:") &&
		link.TransferTaskID != "" && shareLiveCursorValid(link.PendingCursor)
}

func shareLiveUnconfirmedError(link shareLiveLink) error {
	switch {
	case strings.HasPrefix(link.Intent, "batch:"):
		return errors.New("上次转存结果未确认，缺少可查询的任务回执；请查看该活链上次执行日志并核对保存目录")
	case strings.HasPrefix(link.Intent, "mkdir:"):
		return errors.New("上次创建保存目录的结果未确认，请核对该活链的保存目录")
	case strings.HasPrefix(link.Intent, "relocate:"):
		return errors.New("上次保存目录改名或移动的结果未确认，请核对保存目录位置")
	case link.Intent == "delete":
		return errors.New("上次删除目录的结果尚待确认，请在删除窗口核对结果")
	default:
		return errors.New("上次活链操作结果未确认，请查看该活链执行日志")
	}
}

func (a *App) reconcileShareLiveTransfer(ctx context.Context, link *shareLiveLink, s Storage) error {
	if link.Intent == "" {
		return nil
	}
	if !shareLiveTransferRecoverable(*link) {
		return shareLiveUnconfirmedError(*link)
	}
	pending := *link
	pending.IncrementalCursor = link.PendingCursor
	if _, err := a.loadShareLiveCheckpoint(pending); err != nil {
		return err
	}
	guard := func() error {
		if _, err := a.shareLiveGuard(*link); err != nil {
			return err
		}
		for _, current := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
			if current.ID == link.ID && current.Intent == link.Intent && current.TransferTaskID == link.TransferTaskID &&
				current.PendingCursor == link.PendingCursor && current.IncrementalCursor == link.IncrementalCursor &&
				current.Link == link.Link && current.DirectoryID == link.DirectoryID {
				return nil
			}
		}
		return errors.New("活链任务回执已变化，未继续查询")
	}
	err := waitShareQuark(ctx, &s, link.TransferTaskID, guard)
	if err == nil {
		return a.commitShareLiveBatch(link, link.Intent, link.PendingCursor, link.TransferTaskID)
	}
	if !errors.Is(err, errQuarkShareTaskFailed) {
		return err
	}
	err = a.updateShareLiveReceipt(*link, func(current *shareLiveLink) error {
		if current.Intent != link.Intent || current.TransferTaskID != link.TransferTaskID || current.PendingCursor != link.PendingCursor {
			return errors.New("活链失败回执已变化")
		}
		current.Intent, current.TransferTaskID, current.PendingCursor = "", "", ""
		return nil
	})
	if err == nil {
		link.Intent, link.TransferTaskID, link.PendingCursor = "", "", ""
	}
	return err
}
