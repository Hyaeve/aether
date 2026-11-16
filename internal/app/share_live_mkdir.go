package app

import (
	"context"
	"errors"
	"strings"
	"time"
)

func shareLiveMkdirPending(link shareLiveLink) bool {
	return link.DirectoryID == "" && link.DirectoryName != "" && validShareDirectory(link.DirectoryName) &&
		link.Intent == "mkdir:"+link.DirectoryName && link.TransferTaskID == "" && link.PendingCursor == "" && link.DeleteTaskID == ""
}

func shareLiveDirectoryRecoverable(link shareLiveLink) bool {
	return shareLiveMkdirPending(link) && link.PendingDirectoryID != ""
}

func (a *App) checkShareLiveDirectoryName(ctx context.Context, s Storage, parent, name string) error {
	files, err := a.rawList(ctx, s, parent)
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.EqualFold(file.Name, name) {
			return errors.New("保存目录名称已占用，请换一个未占用的名称")
		}
	}
	return nil
}

func (a *App) resetShareLiveDirectoryName(ctx context.Context, s Storage, link shareLiveLink, input shareLiveInput) error {
	if err := a.checkShareLiveDirectoryName(ctx, s, link.Parent, input.DirectoryName); err != nil {
		return err
	}
	return a.updateShareLiveReceipt(link, func(current *shareLiveLink) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !shareLiveMkdirPending(*current) || current.PendingDirectoryID != link.PendingDirectoryID {
			return errors.New("保存目录创建回执已变化，请刷新活动链接")
		}
		current.DirectoryName, current.Intent, current.PendingDirectoryID = input.DirectoryName, "", ""
		current.Cron, current.IntervalDays = strings.TrimSpace(input.Cron), 0
		current.Status, current.Message = "idle", "已更换保存目录名称，等待执行"
		current.NextRun = shareLiveNext(*current, time.Now())
		return nil
	})
}

func (a *App) initializeShareLiveDirectory(ctx context.Context, s Storage, link *shareLiveLink) error {
	if shareLiveDirectoryRecoverable(*link) {
		return a.confirmShareLiveMkdir(ctx, s, link)
	}
	if link.Intent != "" {
		return shareLiveUnconfirmedError(*link)
	}
	if _, err := a.shareLiveGuard(*link); err != nil {
		return err
	}
	if err := a.checkShareLiveDirectoryName(ctx, s, link.Parent, link.DirectoryName); err != nil {
		return err
	}
	intent := "mkdir:" + link.DirectoryName
	receipt := &shareBatchReceipt{
		before: func() error {
			err := a.updateShareLiveReceipt(*link, func(current *shareLiveLink) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if current.Intent != "" || current.PendingDirectoryID != "" {
					return shareLiveUnconfirmedError(*current)
				}
				current.Intent = intent
				return nil
			})
			if err == nil {
				link.Intent = intent
			}
			return err
		},
		directoryCreated: func(id string) error {
			if !shareLiveMkdirIDValid(s, *link, id) {
				return errors.New("创建目录返回的编号无效")
			}
			err := a.updateShareLiveReceipt(*link, func(current *shareLiveLink) error {
				if !shareLiveMkdirPending(*current) || current.PendingDirectoryID != "" {
					return errors.New("保存目录创建回执已变化，未登记目录编号")
				}
				current.PendingDirectoryID = id
				return nil
			})
			if err == nil {
				link.PendingDirectoryID = id
			}
			return err
		},
	}
	if err := a.createDirectory(context.WithValue(ctx, shareBatchReceiptKey{}, receipt), s, link.Parent, link.DirectoryName); err != nil {
		return err
	}
	// A successful response without an ID may be confirmed during this run
	// after the empty preflight. A later run always needs a stored ID.
	return a.confirmShareLiveMkdir(ctx, s, link)
}

func shareLiveMkdirIDValid(s Storage, link shareLiveLink, id string) bool {
	return strings.TrimSpace(id) != "" && id != "/" && id != "0" && id != rootOf(s) && id != link.Parent
}

func (a *App) confirmShareLiveMkdir(ctx context.Context, s Storage, link *shareLiveLink) error {
	expected := link.PendingDirectoryID
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 && !telegramPause(ctx, 300*time.Millisecond) {
			return ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := a.shareLiveGuard(*link); err != nil {
			return err
		}
		files, err := a.rawList(ctx, s, link.Parent)
		if err != nil {
			var auth *storageAuthError
			if ctx.Err() != nil || errors.As(err, &auth) {
				return err
			}
			lastErr = err
			continue
		}
		var found string
		for _, file := range files {
			if !strings.EqualFold(file.Name, link.DirectoryName) && (expected == "" || file.ID != expected) {
				continue
			}
			if found != "" || !file.IsDir || file.Name != link.DirectoryName ||
				!shareLiveMkdirIDValid(s, *link, file.ID) || expected != "" && file.ID != expected {
				return &storageRequestError{message: "创建回执与保存目录列表中的身份不符"}
			}
			found = file.ID
		}
		if found == "" {
			lastErr = &storageRequestError{message: "创建请求已返回，保存目录暂未出现在云盘列表"}
			continue
		}
		err = a.updateShareLiveReceipt(*link, func(current *shareLiveLink) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !shareLiveMkdirPending(*current) || current.PendingDirectoryID != expected {
				return errors.New("保存目录创建回执已变化，未登记保存目录")
			}
			current.DirectoryID, current.Intent, current.PendingDirectoryID = found, "", ""
			return nil
		})
		if err == nil {
			link.DirectoryID, link.Intent, link.PendingDirectoryID = found, "", ""
		}
		return err
	}
	return lastErr
}
