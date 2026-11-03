package app

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Hooks run at the native write boundary, after all read-only preflight checks.
// A rejected preflight must not leave a receipt claiming a delete was submitted.
type cloudDeleteReceiptKey struct{}
type cloudDeleteReceipt struct {
	before   func() error
	accepted func(string) error
	rejected func() error
}

func beforeCloudDelete(ctx context.Context) error {
	if receipt, _ := ctx.Value(cloudDeleteReceiptKey{}).(*cloudDeleteReceipt); receipt != nil {
		return receipt.before()
	}
	return nil
}
func acceptedCloudDelete(ctx context.Context, task string) error {
	if receipt, _ := ctx.Value(cloudDeleteReceiptKey{}).(*cloudDeleteReceipt); receipt != nil {
		return receipt.accepted(task)
	}
	return nil
}
func rejectedCloudDelete(ctx context.Context) error {
	if receipt, _ := ctx.Value(cloudDeleteReceiptKey{}).(*cloudDeleteReceipt); receipt != nil {
		return receipt.rejected()
	}
	return nil
}

func (a *App) shareLiveDirectoryPresent(ctx context.Context, s Storage, link shareLiveLink) (bool, error) {
	if link.DirectoryID == "" || link.DirectoryID == rootOf(s) || link.DirectoryID == link.Parent || !validShareDirectory(link.DirectoryName) || link.DirectoryName == "" {
		return false, errors.New("未确认活链保存目录身份")
	}
	files, err := a.rawList(ctx, s, link.Parent)
	if err != nil {
		return false, err
	}
	for _, file := range files {
		if file.ID != link.DirectoryID {
			continue
		}
		if !file.IsDir || file.Name != link.DirectoryName {
			return false, errors.New("活链保存目录已改名或身份变化，未执行删除")
		}
		return true, nil
	}
	return false, nil
}

func (a *App) deleteShareLive(ctx context.Context, s Storage, link shareLiveLink) error {
	// A newly added, never-executed link has no cloud directory to delete.
	if link.DirectoryID == "" && link.Intent == "" {
		return nil
	}
	for _, other := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
		if other.ID != link.ID && other.StorageID == link.StorageID && other.DirectoryID == link.DirectoryID && link.DirectoryID != "" {
			return errors.New("此目录还被其他活链记录使用，不能一并删除")
		}
	}
	a.runMu.Lock()
	release, err := a.reserveTaskFileNodes(ctx, []taskFileNode{{storage: s, id: link.DirectoryID}})
	a.runMu.Unlock()
	if err != nil {
		return err
	}
	defer release()
	present, err := a.shareLiveDirectoryPresent(ctx, s, link)
	if err != nil {
		return err
	}
	if link.Intent == "delete" {
		// Repeated confirmation first reconciles the prior receipt. Never delete
		// a same-name replacement or re-send a still-unknown delete request.
		if !present {
			return nil
		}
		if s.Type != "quark" || link.DeleteTaskID == "" {
			return errors.New("上次删除已提交，目录尚未确认移除；本次已重新查询，未重复删除，可稍后再检查或仅移除记录")
		}
		var result struct{ Data struct{ Status int } }
		endpoint := "https://drive.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&retry_index=0&task_id=" + url.QueryEscape(link.DeleteTaskID)
		if err := shareSessionRequest(ctx, &s, http.MethodGet, endpoint, nil, &result); err != nil {
			return err
		}
		present, err = a.shareLiveDirectoryPresent(ctx, s, link)
		if err != nil || !present {
			return err
		}
		if result.Data.Status != 3 {
			return errors.New("上次删除仍在确认中，未重复提交，请稍后再检查")
		}
		// A terminal failed task plus the same live directory permits a fresh,
		// explicitly confirmed attempt. Pending/unknown results never do.
	} else if link.Intent != "" && !strings.HasPrefix(link.Intent, "batch:") {
		return errors.New("上次目录创建或迁移结果未确认，请先核对保存目录，或仅移除记录")
	} else if !present {
		return errors.New("活链保存目录已移动或移除，请核对后仅移除记录")
	}
	var task string
	receipt := &cloudDeleteReceipt{
		before: func() error {
			if _, err := a.shareLiveGuard(link); err != nil {
				return err
			}
			if err := a.verifyShareLiveDirectory(ctx, s, link); err != nil {
				return err
			}
			return a.store.update(func(st *State) error {
				if _, err := shareLiveGuardState(*st, link); err != nil {
					return err
				}
				for i := range st.ShareLiveLinks {
					current := &st.ShareLiveLinks[i]
					if current.ID == link.ID {
						if current.Intent != link.Intent || current.DirectoryID != link.DirectoryID || current.Parent != link.Parent || current.DirectoryName != link.DirectoryName {
							return errors.New("活链目录或写入状态已变化，未提交删除")
						}
						current.Intent, current.DeleteTaskID = "delete", ""
						current.Status, current.Message, current.NextRun = "paused", "删除已提交，等待云盘确认", time.Time{}
						return nil
					}
				}
				return errors.New("活链记录已移除，未提交删除")
			})
		},
		accepted: func(id string) error {
			task = id
			return a.store.update(func(st *State) error {
				for i := range st.ShareLiveLinks {
					if st.ShareLiveLinks[i].ID == link.ID && st.ShareLiveLinks[i].Intent == "delete" {
						st.ShareLiveLinks[i].DeleteTaskID = id
						return nil
					}
				}
				return errors.New("删除回执保存失败，请核对云盘；不重复提交")
			})
		},
		rejected: func() error {
			return a.store.update(func(st *State) error {
				for i := range st.ShareLiveLinks {
					current := &st.ShareLiveLinks[i]
					if current.ID == link.ID && current.Intent == "delete" && current.DeleteTaskID == "" {
						current.Intent, current.DeleteTaskID = link.Intent, link.DeleteTaskID
						current.Status, current.Message, current.NextRun = link.Status, link.Message, link.NextRun
						return nil
					}
				}
				return errors.New("删除拒绝回执保存失败，请核对云盘")
			})
		},
	}
	ctx = context.WithValue(context.WithValue(ctx, shareLiveActionKey{}, link.ID), cloudDeleteReceiptKey{}, receipt)
	if err := a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "delete", Source: link.Parent, IDs: []string{link.DirectoryID}}); err != nil {
		return err
	}
	if task != "" && s.Type == "quark" {
		if err := waitShareQuark(ctx, &s, task, func() error { _, err := a.shareLiveGuard(link); return err }); err != nil {
			return err
		}
	}
	if _, err := a.shareLiveGuard(link); err != nil {
		return err
	}
	present, err = a.shareLiveDirectoryPresent(ctx, s, link)
	if err == nil && present {
		err = errors.New("删除已提交，云盘尚未确认目录移除；再次确认会先查询结果，不重复提交")
	}
	return err
}
