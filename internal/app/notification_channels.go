package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type qqBotConfig struct {
	Enabled      bool     `json:"enabled"`
	AppID        string   `json:"appId"`
	Secret       string   `json:"secret"`
	GroupID      string   `json:"groupId"`
	HasSecret    bool     `json:"hasSecret,omitempty"`
	ClearSecrets []string `json:"clearSecrets,omitempty"`
}

type feishuConfig struct {
	Enabled      bool     `json:"enabled"`
	Webhook      string   `json:"webhook"`
	Secret       string   `json:"secret"`
	HasWebhook   bool     `json:"hasWebhook,omitempty"`
	HasSecret    bool     `json:"hasSecret,omitempty"`
	ClearSecrets []string `json:"clearSecrets,omitempty"`
}

type notificationChannels struct {
	qqMu     sync.Mutex
	feishuMu sync.Mutex
	qqToken  string
	qqStamp  string
	qqExpiry time.Time
}

var notificationID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var qqAppID = regexp.MustCompile(`^[0-9]{1,32}$`)

func mergeChannelSecret(value, previous, field string, clear []string) string {
	if slices.Contains(clear, field) {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" || value == "********" {
		return previous
	}
	return value
}

func mergeQQBot(value, previous qqBotConfig) (qqBotConfig, error) {
	for _, field := range value.ClearSecrets {
		if field != "secret" {
			return value, errors.New("不支持清除此 QQ Bot 凭据字段")
		}
	}
	value.Secret = mergeChannelSecret(value.Secret, previous.Secret, "secret", value.ClearSecrets)
	value.AppID, value.GroupID = strings.TrimSpace(value.AppID), strings.TrimSpace(value.GroupID)
	value.HasSecret, value.ClearSecrets = false, nil
	return value, validateQQBot(value)
}

func mergeFeishu(value, previous feishuConfig) (feishuConfig, error) {
	for _, field := range value.ClearSecrets {
		if field != "secret" && field != "webhook" {
			return value, errors.New("不支持清除此飞书凭据字段")
		}
	}
	value.Secret = mergeChannelSecret(value.Secret, previous.Secret, "secret", value.ClearSecrets)
	value.Webhook = mergeChannelSecret(value.Webhook, previous.Webhook, "webhook", value.ClearSecrets)
	value.HasSecret, value.HasWebhook, value.ClearSecrets = false, false, nil
	return value, validateFeishu(value)
}

func notificationSecretValid(value string) bool {
	return len(value) <= 512 && !strings.ContainsFunc(value, unicode.IsControl)
}

func validateQQBot(value qqBotConfig) error {
	if value.AppID != "" && !qqAppID.MatchString(value.AppID) {
		return errors.New("QQ Bot AppID 须为数字")
	}
	if !notificationSecretValid(value.Secret) {
		return errors.New("QQ Bot AppSecret 格式无效")
	}
	if value.GroupID != "" && !notificationID.MatchString(value.GroupID) {
		return errors.New("QQ Bot 群 OpenID 格式无效，请填写 group_openid")
	}
	if value.Enabled && (value.AppID == "" || value.Secret == "" || value.GroupID == "") {
		return errors.New("启用 QQ Bot 须填写 AppID、AppSecret 和群 OpenID")
	}
	return nil
}

func validateFeishu(value feishuConfig) error {
	if !notificationSecretValid(value.Secret) {
		return errors.New("飞书签名密钥格式无效")
	}
	if value.Webhook != "" {
		u, err := url.Parse(value.Webhook)
		const prefix = "/open-apis/bot/v2/hook/"
		if err != nil || u.Scheme != "https" || u.Host != "open.feishu.cn" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, prefix) || !notificationID.MatchString(strings.TrimPrefix(u.Path, prefix)) {
			return errors.New("飞书 Webhook 须为官方 HTTPS 群机器人地址")
		}
	}
	if value.Enabled && value.Webhook == "" {
		return errors.New("启用飞书通知须填写 Webhook 地址")
	}
	return nil
}

func visibleQQBot(value qqBotConfig) qqBotConfig {
	value.HasSecret = value.Secret != ""
	value.Secret, value.ClearSecrets = "", nil
	return value
}

func visibleFeishu(value feishuConfig) feishuConfig {
	value.HasSecret, value.HasWebhook = value.Secret != "", value.Webhook != ""
	value.Secret, value.Webhook, value.ClearSecrets = "", "", nil
	return value
}

func (a *App) notificationChannelSecret(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Field        string `json:"field"`
		MetadataOnly bool   `json:"metadataOnly"`
	}
	if !decode(w, r, &input) {
		return
	}
	cfg := a.store.snapshotWithLogLimit(0).Notifications
	var value string
	switch channel := r.PathValue("channel"); {
	case channel == "qqBot" && input.Field == "secret":
		value = cfg.QQBot.Secret
	case channel == "feishu" && input.Field == "secret":
		value = cfg.Feishu.Secret
	case channel == "feishu" && input.Field == "webhook":
		value = cfg.Feishu.Webhook
	default:
		fail(w, 400, errors.New("不支持的通知凭据字段"))
		return
	}
	if input.MetadataOnly {
		jsonResponse(w, 200, map[string]int{"length": len([]rune(value))})
		return
	}
	jsonResponse(w, 200, map[string]string{"value": value})
}

func notificationJSON(ctx context.Context, target, label, token string, payload, result any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return errors.New("通知请求格式无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return errors.New("通知请求创建失败")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "QQBot "+token)
	}
	client := &http.Client{Transport: apiClient.Transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s连接失败；通知不会自动重发", label)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s接口 HTTP %d；请核对平台权限、配额和服务器网络", label, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 || json.Unmarshal(body, result) != nil {
		return fmt.Errorf("%s未返回有效回执；通知不会自动重发", label)
	}
	return nil
}

func feishuSignature(timestamp, secret string) string {
	mac := hmac.New(sha256.New, []byte(timestamp+"\n"+secret))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (a *App) sendFeishu(ctx context.Context, cfg feishuConfig, text string) error {
	if !a.channels.feishuMu.TryLock() {
		return errors.New("飞书通知正在发送，请稍后重试")
	}
	defer a.channels.feishuMu.Unlock()
	if err := validateFeishu(cfg); err != nil {
		return err
	}
	if cfg.Webhook == "" {
		return errors.New("请先保存飞书 Webhook 地址")
	}
	if telegramDigest(a.store.snapshotWithLogLimit(0).Notifications.Feishu) != telegramDigest(cfg) {
		return errors.New("飞书配置已变化，已停止发送")
	}
	payload := map[string]any{"msg_type": "text", "content": map[string]string{"text": text}}
	if cfg.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"], payload["sign"] = timestamp, feishuSignature(timestamp, cfg.Secret)
	}
	var result struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if err := notificationJSON(ctx, cfg.Webhook, "飞书", "", payload, &result); err != nil {
		return err
	}
	if result.Code == nil && result.StatusCode == nil {
		return errors.New("飞书缺少消息回执；通知不会自动重发")
	}
	if result.Code != nil && *result.Code != 0 {
		return fmt.Errorf("飞书发送失败（错误码 %d），请核对签名及机器人权限", *result.Code)
	}
	if result.StatusCode != nil && *result.StatusCode != 0 && *result.StatusCode != 200 {
		return fmt.Errorf("飞书发送失败（错误码 %d）", *result.StatusCode)
	}
	return nil
}

func (a *App) sendQQBot(ctx context.Context, cfg qqBotConfig, text string) error {
	if !a.channels.qqMu.TryLock() {
		return errors.New("QQ Bot 通知正在发送，请稍后重试")
	}
	defer a.channels.qqMu.Unlock()
	if err := validateQQBot(cfg); err != nil {
		return err
	}
	if cfg.AppID == "" || cfg.Secret == "" || cfg.GroupID == "" {
		return errors.New("请先保存 QQ Bot AppID、AppSecret 和群 OpenID")
	}
	stamp := telegramDigest(cfg)
	if telegramDigest(a.store.snapshotWithLogLimit(0).Notifications.QQBot) != stamp {
		return errors.New("QQ Bot 配置已变化，已停止发送")
	}
	if a.channels.qqStamp != stamp || time.Now().After(a.channels.qqExpiry) {
		var result struct {
			Token   string          `json:"access_token"`
			Expires json.RawMessage `json:"expires_in"`
			Code    *int            `json:"code"`
		}
		if err := notificationJSON(ctx, "https://bots.qq.com/app/getAppAccessToken", "QQ Bot 授权", "", map[string]string{"appId": cfg.AppID, "clientSecret": cfg.Secret}, &result); err != nil {
			return err
		}
		var seconds int64
		if json.Unmarshal(result.Expires, &seconds) != nil {
			var value string
			if json.Unmarshal(result.Expires, &value) == nil {
				seconds, _ = strconv.ParseInt(value, 10, 64)
			}
		}
		if result.Code != nil && *result.Code != 0 || result.Token == "" || len(result.Token) > 4096 || strings.ContainsAny(result.Token, "\r\n\x00") || seconds < 1 || seconds > 86400 {
			return errors.New("QQ Bot 未返回有效令牌，请核对应用凭据及 IP 白名单")
		}
		a.channels.qqToken, a.channels.qqStamp = result.Token, stamp
		a.channels.qqExpiry = time.Now().Add(time.Duration(max(1, seconds-60)) * time.Second)
	}
	if telegramDigest(a.store.snapshotWithLogLimit(0).Notifications.QQBot) != stamp {
		return errors.New("QQ Bot 配置已变化，已停止发送")
	}
	var result struct {
		ID   string `json:"id"`
		Code *int   `json:"code"`
	}
	err := notificationJSON(ctx, "https://api.sgroup.qq.com/v2/groups/"+url.PathEscape(cfg.GroupID)+"/messages", "QQ Bot", a.channels.qqToken,
		map[string]any{"content": text, "msg_type": 0}, &result)
	if err != nil {
		return err
	}
	if result.Code != nil && *result.Code != 0 {
		return fmt.Errorf("QQ Bot 发送失败（错误码 %d），请核对群 OpenID、主动消息权限和配额", *result.Code)
	}
	if result.ID == "" {
		return errors.New("QQ Bot 缺少消息回执；通知不会自动重发")
	}
	return nil
}

func (a *App) notificationChannelTest(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.snapshotWithLogLimit(0).Notifications
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var err error
	switch r.PathValue("channel") {
	case "qqBot":
		err = a.sendQQBot(ctx, cfg.QQBot, "Aether QQ Bot 通知连接测试成功")
	case "feishu":
		err = a.sendFeishu(ctx, cfg.Feishu, "Aether 飞书通知连接测试成功")
	default:
		fail(w, 400, errors.New("不支持的通知渠道"))
		return
	}
	if err != nil {
		a.store.event("warn", "notifications", err.Error())
		fail(w, 502, err)
		return
	}
	a.store.event("info", "notifications", r.PathValue("channel")+" 测试通知已发送")
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

type channelNotice struct {
	Text   string
	QQBot  qqBotConfig
	Feishu feishuConfig
}

func notificationName(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	runes := []rune(value)
	if len(runes) > 200 {
		value = string(runes[:200]) + "..."
	}
	return value
}

func (a *App) queueResultNotification(kind, name, status string, count int) {
	cfg := a.store.snapshotWithLogLimit(0).Notifications
	if !cfg.QQBot.Enabled && !cfg.Feishu.Enabled || a.ctx.Err() != nil {
		return
	}
	result := map[string]string{"success": "成功", "completed": "成功", "error": "失败，详情请查看运行日志", "failed": "失败，详情请查看运行日志", "cancelled": "已停止", "stopped": "已停止"}[status]
	if result == "" {
		return
	}
	message := fmt.Sprintf("Aether %s结果\n名称：%s\n状态：%s\n处理：%d 项", kind, notificationName(name), result, count)
	select {
	case a.notificationQueue <- channelNotice{Text: message, QQBot: cfg.QQBot, Feishu: cfg.Feishu}:
	default:
		a.store.event("warn", "notifications", "通知队列已满，本次结果通知未发送；请查看运行日志")
	}
}

func (a *App) channelNotificationWorker() {
	defer a.wg.Done()
	for {
		select {
		case <-a.ctx.Done():
			return
		case notice := <-a.notificationQueue:
			for _, channel := range []string{"qqBot", "feishu"} {
				ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
				var err error
				if channel == "qqBot" && notice.QQBot.Enabled {
					err = a.sendQQBot(ctx, notice.QQBot, notice.Text)
				} else if channel == "feishu" && notice.Feishu.Enabled {
					err = a.sendFeishu(ctx, notice.Feishu, notice.Text)
				} else {
					cancel()
					continue
				}
				cancel()
				if err != nil {
					a.store.event("warn", "notifications", channel+" 结果通知失败："+err.Error())
				} else {
					a.store.event("info", "notifications", channel+" 结果通知已发送")
				}
			}
		}
	}
}
