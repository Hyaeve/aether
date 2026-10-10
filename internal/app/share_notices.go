package app

import (
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
