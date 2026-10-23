package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type telegramUser struct {
	ID  int64 `json:"id"`
	Bot bool  `json:"is_bot"`
}
type telegramMessage struct {
	ID   int64        `json:"message_id"`
	From telegramUser `json:"from"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	Text    string `json:"text"`
	Caption string `json:"caption"`
}
type telegramUpdate struct {
	ID       int64            `json:"update_id"`
	Message  *telegramMessage `json:"message"`
	Callback *struct {
		ID      string           `json:"id"`
		From    telegramUser     `json:"from"`
		Message *telegramMessage `json:"message"`
		Data    string           `json:"data"`
	} `json:"callback_query"`
}

func telegramRequest(ctx context.Context, cfg telegramConfig, method string, body any, out any) error {
	if !telegramTokenPattern.MatchString(cfg.Token) || !map[string]bool{"getMe": true, "getUpdates": true, "getWebhookInfo": true, "sendMessage": true, "editMessageText": true, "answerCallbackQuery": true, "deleteMessages": true}[method] {
		return errors.New("TG 请求配置无效")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return errors.New("TG 请求格式无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", "https://api.telegram.org/bot"+cfg.Token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return errors.New("TG 请求创建失败")
	}
	r.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: apiClient.Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("TG 连接失败，请检查服务器网络")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		if method == "deleteMessages" {
			return fmt.Errorf("TG 清理消息 HTTP %d，请检查机器人权限和消息删除期限", response.StatusCode)
		}
		return fmt.Errorf("TG 接口 HTTP %d，请核对授权、网络及是否已有其他轮询或Webhook", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err != nil || len(data) > 2<<20 || json.Unmarshal(data, &result) != nil || !result.OK {
		if method == "deleteMessages" {
			return errors.New("TG 未确认消息清理结果")
		}
		return errors.New("TG 未确认请求结果；发送消息不会自动重试")
	}
	if out != nil && json.Unmarshal(result.Result, out) != nil {
		return errors.New("TG 响应格式无效")
	}
	if method == "deleteMessages" {
		var removed bool
		if json.Unmarshal(result.Result, &removed) != nil || !removed {
			return errors.New("TG 未确认消息清理结果")
		}
	}
	return nil
}

func telegramAllowed(cfg telegramConfig, chat, user int64) bool {
	if !cfg.Enabled || strconv.FormatInt(chat, 10) != cfg.ChatID {
		return false
	}
	for _, allowed := range cfg.UserIDs {
		if strconv.FormatInt(user, 10) == allowed {
			return true
		}
	}
	return false
}

func telegramPause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *App) telegramPoll() {
	defer a.wg.Done()
	lastError := ""
	for a.ctx.Err() == nil {
		st := a.store.snapshotWithLogLimit(0)
		cfg := st.Notifications.Telegram
		if !cfg.Enabled || cfg.Token == "" {
			if !telegramPause(a.ctx, time.Second) {
				return
			}
			continue
		}
		bot := telegramDigest(cfg.Token)
		var updates []telegramUpdate
		offset := st.TelegramRuntime.Offset
		bootstrap := st.TelegramRuntime.Bot != bot
		if bootstrap {
			var webhook struct {
				URL string `json:"url"`
			}
			if err := telegramRequest(a.ctx, cfg, "getWebhookInfo", map[string]any{}, &webhook); err != nil || webhook.URL != "" {
				message := "TG 读取配置失败，请检查网络及授权"
				if webhook.URL != "" {
					message = "TG Bot 已设置Webhook，未擅自移除；请在原服务停用后再接入"
				}
				if lastError != message {
					a.store.event("warn", "notifications", message)
					lastError = message
				}
				if !telegramPause(a.ctx, 10*time.Second) {
					return
				}
				continue
			}
			offset = -1
		}
		timeout := 25
		if bootstrap {
			timeout = 0
		}
		err := telegramRequest(a.ctx, cfg, "getUpdates", map[string]any{"offset": offset, "timeout": timeout, "limit": 20, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		if err != nil {
			if a.ctx.Err() != nil {
				return
			}
			if lastError != err.Error() {
				a.store.event("warn", "notifications", err.Error())
				lastError = err.Error()
			}
			if !telegramPause(a.ctx, 5*time.Second) {
				return
			}
			continue
		}
		lastError = ""
		if telegramDigest(a.store.snapshotWithLogLimit(0).Notifications.Telegram) != telegramDigest(cfg) {
			continue
		}
		if bootstrap {
			err = a.store.update(func(st *State) error {
				if telegramDigest(st.Notifications.Telegram) != telegramDigest(cfg) {
					return errors.New("TG 配置已变化")
				}
				st.TelegramRuntime.Bot, st.TelegramRuntime.Offset = bot, 0
				for _, update := range updates {
					if update.ID >= st.TelegramRuntime.Offset {
						st.TelegramRuntime.Offset = update.ID + 1
					}
				}
				return nil
			})
			if err != nil {
				a.store.event("error", "notifications", "TG 起始回执保存失败，未处理消息")
				telegramPause(a.ctx, 5*time.Second)
			}
			continue
		}
		for _, update := range updates {
			if err := a.handleTelegramUpdate(a.ctx, cfg, update); err != nil {
				a.store.event("warn", "notifications", "TG 交互处理停止："+err.Error())
				break
			}
		}
	}
}

type telegramLink struct{ Provider, Link, Kind string }

func telegramLinks(text string) ([]telegramLink, error) {
	result := []telegramLink{}
	if len(text) > 200000 {
		return nil, errors.New("分享内容过长")
	}
	indices := shareURLPattern.FindAllStringIndex(text, -1)
	indices = append(indices, ed2kTextPattern.FindAllStringIndex(text, -1)...)
	sort.Slice(indices, func(i, j int) bool { return indices[i][0] < indices[j][0] })
	seen := map[string]string{}
	for i, at := range indices {
		addressText := text[at[0]:at[1]]
		if strings.HasPrefix(strings.ToLower(addressText), "ed2k://") {
			info, err := parseED2KText(addressText)
			if err != nil || len(addressText) > 8192 {
				return nil, errors.New("ED2K 链接的文件名、大小或哈希无效")
			}
			uri := info.URI()
			if len(uri) > 8192 {
				return nil, errors.New("ED2K 链接过长")
			}
			if _, ok := seen[uri]; !ok {
				seen[uri] = ""
				result = append(result, telegramLink{Provider: "115", Link: uri, Kind: "ed2k"})
			}
			if len(result) > 50 {
				return nil, errors.New("每次最多50条链接")
			}
			continue
		}
		end := len(text)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		raw := text[at[0]:end]
		if !strings.HasPrefix(raw, "https://") {
			continue
		}
		provider, code, pass, err := parseShareLink(raw, "")
		if err != nil {
			continue
		}
		codes := map[string]bool{}
		if pass != "" {
			codes[pass] = true
		}
		for _, match := range sharePasswordPattern.FindAllStringSubmatch(raw, -1) {
			codes[match[1]] = true
		}
		address, _ := url.Parse(text[at[0]:at[1]])
		queries := []url.Values{address.Query()}
		if fragment, e := url.Parse(address.Fragment); e == nil {
			queries = append(queries, fragment.Query())
		}
		for _, query := range queries {
			for _, key := range []string{"pwd", "password", "passwd", "passcode", "receive_code"} {
				for _, value := range query[key] {
					if value != "" {
						codes[value] = true
					}
				}
			}
		}
		if len(codes) > 1 {
			return nil, errors.New("同一分享含不同提取码，请重新检查")
		}
		key := provider + ":" + code
		if previous, ok := seen[key]; ok {
			if previous != pass {
				return nil, errors.New("同一分享含不同提取码，请重新检查")
			}
			continue
		}
		seen[key] = pass
		result = append(result, telegramLink{Provider: provider, Link: raw})
		if len(result) > 50 {
			return nil, errors.New("每次最多50条链接")
		}
	}
	return result, nil
}

func (a *App) telegramSend(ctx context.Context, cfg telegramConfig, chat int64, text string, keyboard [][]map[string]string) (int64, error) {
	body := map[string]any{"chat_id": chat, "text": text}
	if keyboard != nil {
		body["reply_markup"] = map[string]any{"inline_keyboard": keyboard}
	}
	var message telegramMessage
	err := telegramRequest(ctx, cfg, "sendMessage", body, &message)
	if err == nil && len(keyboard) > 0 && message.ID <= 0 {
		err = errors.New("TG 交互消息编号未确认；不会自动重新发送")
	}
	return message.ID, err
}

func (a *App) handleTelegramUpdate(ctx context.Context, cfg telegramConfig, update telegramUpdate) error {
	accepted := false
	err := a.store.update(func(st *State) error {
		if telegramDigest(st.Notifications.Telegram) != telegramDigest(cfg) || st.TelegramRuntime.Bot != telegramDigest(cfg.Token) {
			return errors.New("TG 配置已变化")
		}
		if update.ID < st.TelegramRuntime.Offset {
			return nil
		}
		// Acknowledge durably before any external write; never replay an uncertain update.
		st.TelegramRuntime.Offset = update.ID + 1
		accepted = true
		return nil
	})
	if err != nil || !accepted {
		return err
	}
	if callback := update.Callback; callback != nil {
		if callback.Message == nil || callback.From.Bot || !telegramAllowed(cfg, callback.Message.Chat.ID, callback.From.ID) {
			return nil
		}
		err := a.telegramCallback(ctx, cfg, callback.Message.Chat.ID, callback.From.ID, callback.Message.ID, callback.Data)
		text := "已处理"
		if err != nil {
			text = "操作已过期、配置已变化或无权操作，请重新发送链接"
		}
		_ = telegramRequest(ctx, cfg, "answerCallbackQuery", map[string]any{"callback_query_id": callback.ID, "text": text}, nil)
		return err
	}
	m := update.Message
	if m == nil || m.From.Bot || !telegramAllowed(cfg, m.Chat.ID, m.From.ID) {
		return nil
	}
	text := m.Text
	if text == "" {
		text = m.Caption
	}
	links, err := telegramLinks(text)
	if err != nil {
		_, e := a.telegramSend(ctx, cfg, m.Chat.ID, err.Error(), nil)
		return e
	}
	if len(links) == 0 {
		if strings.HasPrefix(text, "/start") {
			_, err = a.telegramSend(ctx, cfg, m.Chat.ID, "Aether 已连接。", nil)
		}
		return err
	}
	for _, link := range links {
		st := a.store.snapshotWithLogLimit(0)
		targets := []telegramTarget{}
		for _, target := range st.Notifications.Targets {
			if target.Provider == link.Provider && target.accepts(link.Kind) {
				targets = append(targets, target)
			}
		}
		if len(targets) == 0 {
			_, err = a.telegramSend(ctx, cfg, m.Chat.ID, "该类型未配置已启用的"+telegramAction(link.Kind)+"目录，未执行操作。", nil)
			continue
		}
		job := telegramTransfer{ID: id(), ChatID: m.Chat.ID, UserID: m.From.ID, Link: link.Link, Provider: link.Provider, Targets: targets, Selected: -1, Auth: telegramDigest(cfg), Status: "choosing", Created: time.Now()}
		job.Kind = link.Kind
		job.LastActivity, job.Bot = job.Created, telegramDigest(cfg.Token)
		if len(targets) == 1 {
			job.Status, job.Selected = "queued", 0
		}
		err = a.store.update(func(st *State) error {
			if telegramDigest(st.Notifications.Telegram) != job.Auth {
				return errors.New("TG 配置已变化")
			}
			for _, target := range job.Targets {
				if !telegramTargetCurrent(*st, job, target) {
					return errors.New("TG 目录配置已变化")
				}
			}
			active, pending := 0, 0
			kept := []telegramTransfer{}
			for index, item := range st.TelegramRuntime.Jobs {
				if telegramActive(item.Status) {
					active++
				}
				if item.CleanupPending {
					pending++
				}
				if telegramActive(item.Status) || item.CleanupPending || index >= len(st.TelegramRuntime.Jobs)-200 {
					kept = append(kept, item)
				}
			}
			if active >= 32 {
				return errors.New("TG 待处理转存过多，请等待已有任务结束")
			}
			if job.Status == "choosing" && pending >= 200 {
				return errors.New("TG 待清理交互消息过多，请检查机器人网络和删除权限")
			}
			st.TelegramRuntime.Jobs = append(kept, job)
			return nil
		})
		if err != nil {
			return err
		}
		if job.Status == "queued" {
			continue
		}
		text, keyboard := telegramDirectoryPage(st, job)
		message, e := a.telegramSend(ctx, cfg, m.Chat.ID, text, keyboard)
		if e != nil {
			a.finishTelegram(job.ID, "failed", "TG 目录选择消息未确认，请重新发送链接", 0)
			return e
		}
		err = a.store.update(func(st *State) error {
			for i := range st.TelegramRuntime.Jobs {
				if st.TelegramRuntime.Jobs[i].ID == job.ID {
					st.TelegramRuntime.Jobs[i].MessageID = message
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return err
}

func telegramActive(status string) bool {
	return status == "queued" || status == "running" || status == "choosing" || status == "confirming"
}

func telegramTargetCurrent(st State, job telegramTransfer, target telegramTarget) bool {
	if telegramDigest(st.Notifications.Telegram) != job.Auth || !telegramAllowed(st.Notifications.Telegram, job.ChatID, job.UserID) || !target.accepts(job.Kind) {
		return false
	}
	for _, current := range st.Notifications.Targets {
		if sameTelegramTarget(current, target) {
			return true
		}
	}
	return false
}

func (a *App) telegramCallback(ctx context.Context, cfg telegramConfig, chat, user, message int64, data string) error {
	parts := strings.Split(data, ":")
	if len(parts) != 3 || (parts[0] != "t" && parts[0] != "c" && parts[0] != "x" && parts[0] != "p") {
		return errors.New("无效操作")
	}
	index, err := strconv.Atoi(parts[2])
	if err != nil {
		return errors.New("无效目录")
	}
	var selected telegramTransfer
	err = a.store.update(func(st *State) error {
		for i := range st.TelegramRuntime.Jobs {
			job := &st.TelegramRuntime.Jobs[i]
			if job.ID != parts[1] {
				continue
			}
			if job.ChatID != chat || job.UserID != user || job.MessageID != message || telegramInteractionExpired(*job, time.Now()) {
				return errors.New("操作已过期或目录已变化")
			}
			if parts[0] == "p" {
				if job.Status != "choosing" || index < 0 || index > (len(job.Targets)-1)/telegramDirectoryPageSize || (index != job.Page-1 && index != job.Page+1) {
					return errors.New("目录页已变化或操作无效")
				}
				for _, target := range job.Targets {
					if !telegramTargetCurrent(*st, *job, target) {
						return errors.New("目录配置已变化")
					}
				}
				job.Page, job.LastActivity = index, time.Now()
				selected = *job
				return nil
			}
			if index < 0 || index >= len(job.Targets) || !telegramTargetCurrent(*st, *job, job.Targets[index]) {
				return errors.New("操作已过期或目录已变化")
			}
			if parts[0] == "t" && job.Status == "choosing" {
				if index/telegramDirectoryPageSize != job.Page {
					return errors.New("目录页已变化")
				}
				job.Selected, job.Status = index, "confirming"
			} else if parts[0] == "c" && job.Status == "confirming" && job.Selected == index {
				job.Status = "queued"
			} else if parts[0] == "x" && job.Status == "confirming" {
				job.Status, job.Link = "cancelled", ""
			} else {
				return errors.New("操作已处理")
			}
			job.LastActivity = time.Now()
			selected = *job
			return nil
		}
		return errors.New("操作不存在")
	})
	if err != nil {
		return err
	}
	action := telegramAction(selected.Kind)
	text := "已加入" + action + "队列"
	keyboard := [][]map[string]string{}
	if selected.Status == "choosing" {
		text, keyboard = telegramDirectoryPage(a.store.snapshotWithLogLimit(0), selected)
	}
	if selected.Status == "cancelled" {
		text = "已取消" + action
	}
	if selected.Status == "confirming" {
		target := selected.Targets[index]
		text = "确认" + action + "到：" + target.Path
		if s, e := a.store.storage(target.StorageID); e == nil {
			text = "确认" + action + "到：" + s.Name + " · " + target.Path
		}
		keyboard = [][]map[string]string{{{"text": "取消", "callback_data": fmt.Sprintf("x:%s:%d", selected.ID, index)}, {"text": "确认" + action, "callback_data": fmt.Sprintf("c:%s:%d", selected.ID, index)}}}
	}
	return telegramRequest(ctx, cfg, "editMessageText", map[string]any{"chat_id": chat, "message_id": message, "text": text, "reply_markup": map[string]any{"inline_keyboard": keyboard}}, nil)
}
