package app

import (
	"context"
	"fmt"
	"time"
)

type ShareNotice struct {
	ID        string    `json:"id"`
	StorageID string    `json:"storageId"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Time      time.Time `json:"time"`
}

// Only a fully accepted plan is recorded; an upstream job ID is not completion.
func (a *App) recordShareNotice(key string, s Storage, p sharePreview) {
	if p.Failed || p.BatchBusy || (p.TaskID != "" && s.Type == "quark") || (!p.Used && (len(p.Batches) == 0 || p.Next != len(p.Batches))) {
		return
	}
	status, message := "submitted", fmt.Sprintf("转存已提交 · %d 项，请检查网盘结果", p.Submitted)
	if s.Type == "quark" || p.Submitted == 0 {
		status, message = "completed", fmt.Sprintf("转存成功 · %d 项", p.Submitted)
	}
	if p.Skipped > 0 {
		message += fmt.Sprintf(" · 跳过 %d 项", p.Skipped)
	}
	a.recordBrowserShare(key, s, p, status, message)
	n := ShareNotice{ID: key, StorageID: s.ID, Name: s.Name + " · 分享转存", Provider: s.Type, Status: status, Message: message, Time: time.Now()}
	added := false
	err := a.store.update(func(st *State) error {
		for _, existing := range st.ShareNotices {
			if existing.ID == key {
				return nil
			}
		}
		st.ShareNotices = append(st.ShareNotices, n)
		if len(st.ShareNotices) > 200 {
			st.ShareNotices = st.ShareNotices[len(st.ShareNotices)-200:]
		}
		added = true
		return nil
	})
	if err != nil {
		a.store.event("error", "files", "分享转存结果通知保存失败；请直接检查网盘结果")
	} else if added {
		a.store.event("info", "files", fmt.Sprintf("[%s] %s：%s", key, n.Name, message))
	}
}

func (a *App) recordBrowserShare(key string, s Storage, p sharePreview, status, message string) error {
	// Legacy/in-process previews have no URL and cannot become scheduled links.
	if p.Link == "" {
		return nil
	}
	if status != "running" && status != "failed" {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		a.resolveShareDestination(ctx, shareStorageSession(s, p), &p)
		cancel()
	}
	record := shareTransferRecord{ID: key, StorageID: s.ID, Provider: s.Type, Name: telegramShareName(p.Items), Link: p.Link,
		DirectoryName: shareSavedName(p), Source: "browser", Time: time.Now(), Status: status, Message: message, Submitted: p.Submitted, Skipped: p.Skipped}
	if err := a.recordShareTransfer(record, p); err != nil {
		a.store.event("error", "notifications", "转存记录保存失败，请检查运行态存储")
		return err
	}
	return nil
}
