package app

import (
	"context"
	"fmt"
	"strings"
)

func telegramOneLine(text string) string { return strings.Join(strings.Fields(text), " ") }
func telegramLimit(text string, n int) string {
	r := []rune(telegramOneLine(text))
	units := 0
	for i, char := range r {
		width := 1
		if char > 0xffff {
			width = 2
		}
		units += width
		if units > n {
			limit := max(0, n-1)
			used := 0
			for j := 0; j <= i; j++ {
				w := 1
				if r[j] > 0xffff {
					w = 2
				}
				if used+w > limit {
					return string(r[:j]) + "…"
				}
				used += w
			}
			return "…"
		}
	}
	return string(r)
}

func (a *App) telegramComplete(ctx context.Context, job telegramTransfer, status, name string, count, skipped int, message string) {
	if err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			j := &st.TelegramRuntime.Jobs[i]
			if j.ID == job.ID {
				j.Name, j.Skipped = name, skipped
			}
		}
		return nil
	}); err != nil {
		return
	}
	a.finishTelegram(job.ID, status, message, count)
	a.sendTelegramGroupResult(ctx, job)
}

func (a *App) sendTelegramGroupResult(ctx context.Context, job telegramTransfer) {
	st := a.store.snapshotWithLogLimit(0)
	cfg := st.Notifications.Telegram
	if telegramDigest(cfg) != job.Auth || !telegramAllowed(cfg, job.ChatID, job.UserID) {
		return
	}
	members := []telegramTransfer{}
	leader := ""
	for _, j := range st.TelegramRuntime.Jobs {
		if j.ID != job.ID && !telegramSameGroup(job, j) {
			continue
		}
		if telegramActive(j.Status) || j.ResultSent {
			return
		}
		if leader == "" || j.ID == j.GroupID {
			leader = j.ID
		}
		members = append(members, j)
	}
	if len(members) == 0 {
		return
	}
	header := telegramProviderName(job.Provider) + " · " + telegramAction(job.Kind)
	if job.Kind == "library" {
		header = telegramProviderName(job.Provider) + "分享STRM"
	}
	if job.Kind == "cas-library" {
		header = "移动云盘 · CAS链库"
	}
	if len(job.Targets) > 0 && job.Selected >= 0 && job.Selected < len(job.Targets) {
		target := job.Targets[job.Selected]
		for _, s := range st.Storages {
			if s.ID == target.StorageID {
				header += " · " + s.Name
				break
			}
		}
		header += "\n保存目录：" + telegramLimit(target.Path, 160)
	}
	lines := []string{header}
	budget := max(40, (3900-len([]rune(header)))/len(members)-3)
	for i, j := range members {
		name := j.Name
		if name == "" {
			name = fmt.Sprintf("链接%d", i+1)
		}
		detail := fmt.Sprint(j.Processed)
		switch j.Status {
		case "submitted":
			detail = fmt.Sprintf("已提交%d，待网盘确认", j.Processed)
		case "completed", "success":
			if j.Processed == 0 && j.Result != "" {
				if strings.Contains(j.Result, "默认生成位置未设置") {
					detail = "0；默认生成位置未设置，卡片未启用"
				} else if strings.Contains(j.Result, "未启用") {
					detail = "0；已保存未启用的卡片"
				} else {
					detail = "0；" + telegramLimit(j.Result, 40)
				}
			}
		default:
			detail = fmt.Sprintf("未完成%d；%s", j.Processed, telegramLimit(j.Result, 60))
		}
		if j.Skipped > 0 {
			detail += fmt.Sprintf("，跳过%d", j.Skipped)
		}
		detail = telegramLimit(detail, max(20, budget/2))
		lines = append(lines, "· "+telegramLimit(name, max(8, budget-len([]rune(detail))-2))+"（"+detail+"）")
	}
	if job.Kind == "library" || job.Kind == "cas-library" {
		lines = telegramLibraryResultLines(job, members)
	}
	claimed := false
	if err := a.store.update(func(st *State) error {
		if telegramDigest(st.Notifications.Telegram) != job.Auth {
			return nil
		}
		for i := range st.TelegramRuntime.Jobs {
			j := &st.TelegramRuntime.Jobs[i]
			if j.ID == leader {
				if j.ResultSent {
					return nil
				}
				j.ResultSent = true
				claimed = true
				break
			}
		}
		return nil
	}); err != nil || !claimed {
		return
	}
	// Persist the send intent first: an unknown Telegram response must not replay.
	if _, err := a.telegramSend(ctx, cfg, job.ChatID, strings.Join(lines, "\n"), nil); err != nil {
		a.store.event("warn", "notifications", "TG 合并结果消息未确认，请查看逐项运行日志；不会自动重发")
	}
}
