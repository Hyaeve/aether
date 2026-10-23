package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type telegramConfig struct {
	Enabled  bool     `json:"enabled"`
	Token    string   `json:"token"`
	ChatID   string   `json:"chatId"`
	UserIDs  []string `json:"userIds"`
	HasToken bool     `json:"hasToken,omitempty"`
}

type telegramTarget struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	StorageID string `json:"storageId"`
	Parent    string `json:"parent"`
	Path      string `json:"path"`
}

type notificationConfig struct {
	Telegram telegramConfig   `json:"telegram"`
	Targets  []telegramTarget `json:"targets"`
}

type telegramTransfer struct {
	ID           string           `json:"id"`
	ChatID       int64            `json:"chatId"`
	UserID       int64            `json:"userId"`
	MessageID    int64            `json:"messageId"`
	Link         string           `json:"secret"`
	Provider     string           `json:"provider"`
	Targets      []telegramTarget `json:"targets"`
	Selected     int              `json:"selected"`
	Auth         string           `json:"auth"`
	StorageStamp string           `json:"storageStamp,omitempty"`
	Status       string           `json:"status"`
	Name         string           `json:"name,omitempty"`
	Processed    int              `json:"processed"`
	Batches      int              `json:"batches"`
	Created      time.Time        `json:"created"`
}

type telegramRuntime struct {
	Bot    string             `json:"bot"`
	Offset int64              `json:"offset"`
	Jobs   []telegramTransfer `json:"jobs,omitempty"`
}

var telegramTokenPattern = regexp.MustCompile(`^[0-9]{5,20}:[a-zA-Z0-9_-]{20,160}$`)

func telegramIdentity(value string, signed bool) bool {
	n, err := strconv.ParseInt(value, 10, 64)
	return err == nil && n != 0 && (signed || n > 0) && strconv.FormatInt(n, 10) == value
}

func validateNotifications(value notificationConfig, st State) error {
	tg := value.Telegram
	if tg.Token != "" && !telegramTokenPattern.MatchString(tg.Token) {
		return errors.New("Bot Token 格式无效")
	}
	if tg.ChatID != "" && !telegramIdentity(tg.ChatID, true) {
		return errors.New("Chat ID 须为非零整数，群组可使用负数")
	}
	if len(tg.UserIDs) > 20 {
		return errors.New("最多授权20个用户ID")
	}
	for _, user := range tg.UserIDs {
		if !telegramIdentity(user, false) {
			return errors.New("授权用户 ID 须为正整数")
		}
	}
	if tg.Enabled && (tg.Token == "" || tg.ChatID == "" || len(tg.UserIDs) == 0) {
		return errors.New("启用 TG 通知须填写 Bot Token、Chat ID 和授权用户 ID")
	}
	if len(value.Targets) > 60 {
		return errors.New("转存目录最多60项")
	}
	seen := map[string]bool{}
	for _, target := range value.Targets {
		if !shareCodePattern.MatchString(target.ID) || seen[target.ID] || target.Parent == "" || len(target.Parent) > 4096 || !strings.HasPrefix(target.Path, "/") || len(target.Path) > 4096 || strings.ContainsAny(target.Parent+target.Path, "\x00\r\n") {
			return errors.New("转存目录标识或完整路径无效")
		}
		seen[target.ID] = true
		found := false
		for _, s := range st.Storages {
			if s.ID == target.StorageID && s.Type == target.Provider && (s.Type == "115" || s.Type == "quark" || nativeMobile(s)) {
				found = true
			}
		}
		if !found {
			return errors.New("请选择对应类型的115、原生移动或夸克存储池")
		}
	}
	return nil
}

func telegramDigest(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func visibleNotifications(value notificationConfig) notificationConfig {
	value.Telegram.HasToken = value.Telegram.Token != ""
	value.Telegram.Token = ""
	if value.Targets == nil {
		value.Targets = []telegramTarget{}
	}
	return value
}

func (a *App) notificationSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		jsonResponse(w, 200, visibleNotifications(a.store.snapshotWithLogLimit(0).Notifications))
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(405)
		return
	}
	var input notificationConfig
	if section := r.PathValue("section"); section != "" && section != "targets" {
		fail(w, 400, errors.New("无效的通知服务栏目"))
		return
	}
	if !decode(w, r, &input) {
		return
	}
	err := a.store.update(func(st *State) error {
		value := st.Notifications
		if r.PathValue("section") == "targets" {
			value.Targets = input.Targets
			for i := range value.Targets {
				if value.Targets[i].ID == "" {
					value.Targets[i].ID = id()
				}
			}
		} else {
			value.Telegram = input.Telegram
			value.Telegram.HasToken = false
			value.Telegram.Token = strings.TrimSpace(value.Telegram.Token)
			value.Telegram.ChatID = strings.TrimSpace(value.Telegram.ChatID)
			if value.Telegram.Token == "" {
				value.Telegram.Token = st.Notifications.Telegram.Token
			}
			if len(value.Telegram.UserIDs) == 0 && telegramIdentity(value.Telegram.ChatID, false) {
				value.Telegram.UserIDs = []string{value.Telegram.ChatID}
			}
		}
		if err := validateNotifications(value, *st); err != nil {
			return err
		}
		st.Notifications = value
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	a.store.event("info", "notifications", "通知服务配置已保存")
	jsonResponse(w, 200, visibleNotifications(a.store.snapshotWithLogLimit(0).Notifications))
}

func (a *App) telegramTest(w http.ResponseWriter, r *http.Request) {
	cfg := a.store.snapshotWithLogLimit(0).Notifications.Telegram
	if cfg.Token == "" || cfg.ChatID == "" {
		fail(w, 400, errors.New("请先保存 Bot Token 和 Chat ID"))
		return
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := telegramRequest(r.Context(), cfg, "getMe", map[string]any{}, &result); err != nil {
		fail(w, 502, err)
		return
	}
	if err := telegramRequest(r.Context(), cfg, "sendMessage", map[string]any{"chat_id": cfg.ChatID, "text": "Aether TG 通知连接测试成功"}, nil); err != nil {
		fail(w, 502, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
