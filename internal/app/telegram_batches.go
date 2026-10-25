package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

func telegramCommand(text string) (library bool, content string, supported bool) {
	text = strings.TrimSpace(text)
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false, text, true
	}
	head := fields[0]
	if head == "/share" || head == "虚拟链库" {
		return true, strings.TrimSpace(text[len(head):]), true
	}
	if strings.HasPrefix(head, "/") && head != "/start" {
		return false, "", false
	}
	return false, text, true
}

func telegramProviderName(provider string) string {
	switch provider {
	case "115":
		return "115"
	case "mobile":
		return "移动云盘"
	case "quark":
		return "夸克"
	default:
		return "该云盘"
	}
}

func telegramSameGroup(a, b telegramTransfer) bool {
	return a.GroupID != "" && a.GroupID == b.GroupID && a.BatchID == b.BatchID && a.Auth == b.Auth && a.ChatID == b.ChatID && a.UserID == b.UserID
}

func telegramGroupLabel(job telegramTransfer) string {
	if job.GroupSize <= 1 && !job.RequireConfirm {
		return ""
	}
	return fmt.Sprintf("%s · %d条链接\n", telegramProviderName(job.Provider), job.GroupSize)
}

func telegramConfirmation(st State, job telegramTransfer) (string, [][]map[string]string) {
	target := job.Targets[job.Selected]
	destination := target.Path
	for _, storage := range st.Storages {
		if storage.ID == target.StorageID {
			destination = storage.Name + " · " + target.Path
		}
	}
	action := telegramAction(job.Kind)
	text := telegramGroupLabel(job) + "确认" + action + "到：" + destination
	if job.Name != "" {
		text = "分享内容：" + job.Name + "\n" + text
	}
	back := map[string]string{"text": "返回", "callback_data": fmt.Sprintf("b:%s:%d", job.ID, job.Selected)}
	if len(job.Targets) == 1 && job.RequireConfirm {
		back = map[string]string{"text": "取消", "callback_data": fmt.Sprintf("x:%s:%d", job.ID, job.Selected)}
	}
	return text, [][]map[string]string{{back, {"text": "确认" + action, "callback_data": fmt.Sprintf("c:%s:%d", job.ID, job.Selected)}}}
}

func (a *App) queueTelegramLinks(ctx context.Context, cfg telegramConfig, m telegramMessage, links []telegramLink, library bool) error {
	rank := map[string]int{"115": 0, "mobile": 1, "quark": 2}
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Provider != links[j].Provider {
			return rank[links[i].Provider] < rank[links[j].Provider]
		}
		if library {
			return false
		}
		return links[i].Kind < links[j].Kind
	})
	st := a.store.snapshotWithLogLimit(0)
	batch := id()
	jobs := []telegramTransfer{}
	group := ""
	key := ""
	for _, link := range links {
		nextKey := link.Provider
		if !library {
			nextKey += ":" + link.Kind
		}
		if key != nextKey {
			group, key = id(), nextKey
		}
		job := telegramTransfer{ID: id(), ChatID: m.Chat.ID, UserID: m.From.ID, Link: link.Link, Provider: link.Provider, Kind: link.Kind,
			BatchID: batch, GroupID: group, RequireConfirm: len(links) > 1, Selected: -1, Auth: telegramDigest(cfg),
			Status: "waiting", Created: time.Now(), Bot: telegramDigest(cfg.Token), TargetStamps: map[string]string{}}
		if len(jobs) == 0 || jobs[len(jobs)-1].GroupID != group {
			job.ID = group
		}
		job.LastActivity = job.Created
		if library {
			job.Kind, job.Status, job.LibraryStamp = "library", "queued", telegramLibraryStamp(st)
		} else {
			for _, target := range st.Notifications.Targets {
				if target.Provider == link.Provider && target.accepts(link.Kind) {
					job.Targets = append(job.Targets, target)
					for _, storage := range st.Storages {
						if storage.ID == target.StorageID {
							job.TargetStamps[storage.ID] = storageRevision(storage)
						}
					}
				}
			}
			if len(job.Targets) == 0 && len(links) == 1 {
				_, err := a.telegramSend(ctx, cfg, m.Chat.ID, "该类型未配置已启用的"+telegramAction(link.Kind)+"目录，未执行操作。", nil)
				return err
			}
		}
		jobs = append(jobs, job)
	}
	for i := range jobs {
		for _, item := range jobs {
			if item.GroupID == jobs[i].GroupID {
				jobs[i].GroupSize++
			}
		}
	}
	err := a.store.update(func(st *State) error {
		if telegramDigest(st.Notifications.Telegram) != telegramDigest(cfg) || !telegramAllowed(cfg, m.Chat.ID, m.From.ID) {
			return errors.New("TG 配置已变化")
		}
		groups, batches := map[string]bool{}, map[string]bool{}
		pending := 0
		for _, item := range st.TelegramRuntime.Jobs {
			if telegramActive(item.Status) {
				k := item.GroupID
				if k == "" {
					k = item.ID
				}
				groups[k] = true
				if item.BatchID != "" {
					batches[item.BatchID] = true
				}
			}
			if item.CleanupPending {
				pending++
			}
		}
		for _, job := range jobs {
			groups[job.GroupID] = true
			for _, target := range job.Targets {
				if !telegramTargetCurrent(*st, job, target) {
					return errors.New("TG 目录配置已变化")
				}
			}
		}
		if len(groups) > 32 || len(st.TelegramRuntime.Jobs)+len(jobs) > 2000 {
			return errors.New("TG 待处理批次过多，请等待已有任务结束")
		}
		needsPrompt := !library && (len(links) > 1 || len(jobs) > 0 && len(jobs[0].Targets) > 1)
		if needsPrompt && pending >= 200 {
			return errors.New("TG 待清理交互消息过多，请检查机器人网络和删除权限")
		}
		kept := []telegramTransfer{}
		for i, item := range st.TelegramRuntime.Jobs {
			if telegramActive(item.Status) || item.CleanupPending || batches[item.BatchID] || i >= len(st.TelegramRuntime.Jobs)-200 {
				kept = append(kept, item)
			}
		}
		st.TelegramRuntime.Jobs = append(kept, jobs...)
		return nil
	})
	if err != nil {
		current := a.store.snapshotWithLogLimit(0).Notifications.Telegram
		if telegramDigest(current) == telegramDigest(cfg) && telegramAllowed(current, m.Chat.ID, m.From.ID) {
			_, sendErr := a.telegramSend(ctx, cfg, m.Chat.ID, "未添加任务：配置已变化、待处理队列已满或状态保存失败，请检查运行日志后重发。", nil)
			return errors.Join(err, sendErr)
		}
		return err
	}
	if library {
		return err
	}
	return a.advanceTelegramBatch(ctx, batch)
}

// Only the first unfinished provider group in a message may open a prompt.
func (a *App) advanceTelegramBatch(ctx context.Context, batch string) error {
	var selected telegramTransfer
	st := a.store.snapshotWithLogLimit(0)
	ready := false
	for _, job := range st.TelegramRuntime.Jobs {
		if job.BatchID == batch && telegramActive(job.Status) {
			ready = job.Status == "waiting" && job.GroupID == job.ID
			break
		}
	}
	if !ready {
		return nil
	}
	err := a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			job := &st.TelegramRuntime.Jobs[i]
			if job.BatchID != batch || !telegramActive(job.Status) {
				continue
			}
			if job.Status != "waiting" || job.GroupID != job.ID {
				return nil
			}
			selected = *job
			valid := len(job.Targets) > 0
			for _, target := range job.Targets {
				valid = valid && telegramTargetCurrent(*st, *job, target)
			}
			job.Status, job.Selected = "choosing", -1
			if len(job.Targets) == 1 {
				job.Selected = 0
				job.Status = "queued"
				if job.RequireConfirm {
					job.Status = "confirming"
				}
			}
			if !valid {
				job.Status, job.Link = "failed", ""
			}
			job.LastActivity = time.Now()
			selected = *job
			for j := range st.TelegramRuntime.Jobs {
				member := &st.TelegramRuntime.Jobs[j]
				if member.ID != job.ID && telegramSameGroup(*job, *member) && member.Status == "waiting" && (job.Status == "queued" || job.Status == "failed") {
					member.Status, member.Selected = job.Status, job.Selected
					if job.Status == "failed" {
						member.Link = ""
					}
				}
			}
			return nil
		}
		return nil
	})
	if err != nil || selected.ID == "" || selected.Status == "queued" {
		return err
	}
	cfg := a.store.snapshotWithLogLimit(0).Notifications.Telegram
	if telegramDigest(cfg) != selected.Auth || !telegramAllowed(cfg, selected.ChatID, selected.UserID) {
		a.finishTelegram(selected.ID, "failed", "TG 授权已改变，未发送交互消息", 0)
		return nil
	}
	text, keyboard := telegramDirectoryPage(a.store.snapshotWithLogLimit(0), selected)
	if selected.Status == "failed" {
		text, keyboard = telegramProviderName(selected.Provider)+"：未配置可用目录或目录/授权已变化，未转存。", nil
		a.store.event("warn", "notifications", fmt.Sprintf("[TG:%s] %s", selected.ID, text))
	} else if selected.Status == "confirming" {
		text, keyboard = telegramConfirmation(a.store.snapshotWithLogLimit(0), selected)
	}
	message, err := a.telegramSend(ctx, cfg, selected.ChatID, text, keyboard)
	if err != nil {
		a.finishTelegram(selected.ID, "failed", "TG 交互消息未确认，不会自动重新发送", 0)
		return err
	}
	if selected.Status == "failed" {
		return nil
	}
	return a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			job := &st.TelegramRuntime.Jobs[i]
			if job.ID == selected.ID {
				job.MessageID = message
				if job.Status != selected.Status {
					job.CleanupPending = true
				}
			}
		}
		return nil
	})
}
