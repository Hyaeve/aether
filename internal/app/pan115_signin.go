package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type pan115SigninRule struct {
	StorageID string `json:"storageId"`
	Cron      string `json:"cron"`
}
type pan115SigninSettings struct {
	Enabled bool               `json:"enabled"`
	Rules   []pan115SigninRule `json:"rules"`
}
type pan115SigninReceipt struct {
	Status  string    `json:"status"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

func validate115Signin(value pan115SigninSettings, st State, legacy bool) error {
	if len(value.Rules) > 100 {
		return errors.New("最多添加100个签到任务")
	}
	seen := map[string]bool{}
	for _, rule := range value.Rules {
		if rule.StorageID == "" || seen["pool:"+rule.StorageID] {
			return errors.New("每个115账号只能添加一个签到任务")
		}
		seen["pool:"+rule.StorageID] = true
		pool, err := backupStorage(st, rule.StorageID)
		if !legacy {
			if err != nil || pool.Type != "115" || !pool.Enabled {
				return errors.New("请选择已启用的115存储")
			}
			account, err := account115(pool)
			if err != nil {
				return err
			}
			if seen["account:"+account] {
				return errors.New("每个115账号只能添加一个签到任务")
			}
			seen["account:"+account] = true
		}
		if len(rule.Cron) > 128 {
			return errors.New("Cron过长")
		}
		parse := cronParser.Parse
		if legacy {
			parse = rawCronParser.Parse
		}
		if _, err := parse(rule.Cron); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) pan115SigninAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		value := a.store.snapshotWithLogLimit(0).Pan115Signin
		jsonResponse(w, 200, value)
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(405)
		return
	}
	var value pan115SigninSettings
	if !decode(w, r, &value) {
		return
	}
	if err := a.store.update(func(st *State) error {
		if err := validate115Signin(value, *st, false); err != nil {
			return err
		}
		st.Pan115Signin = value
		return nil
	}); err != nil {
		fail(w, 400, err)
		return
	}
	a.store.event("info", "tasks", "115签到配置已保存")
	jsonResponse(w, 200, value)
}

func (a *App) pan115SigninRunAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var input struct {
		StorageID string `json:"storageId"`
	}
	if !decode(w, r, &input) {
		return
	}
	var rule *pan115SigninRule
	for _, item := range a.store.snapshotWithLogLimit(0).Pan115Signin.Rules {
		if item.StorageID == input.StorageID {
			copy := item
			rule = &copy
		}
	}
	if rule == nil {
		fail(w, 404, errors.New("未找到该账号的签到任务"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	message, err := a.run115Signin(ctx, *rule, "手动执行")
	if err != nil {
		fail(w, 502, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"message": message})
}

func (a *App) run115Signin(ctx context.Context, rule pan115SigninRule, source string) (string, error) {
	if !a.pan115SigninMu.TryLock() {
		return "", errors.New("已有签到正在执行，请稍后查看运行日志")
	}
	defer a.pan115SigninMu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	pool, err := backupStorage(st, rule.StorageID)
	if err != nil || !pool.Enabled || !st.Pan115Signin.Enabled {
		return "", errors.New("签到或关联存储已停用")
	}
	if err := validate115Signin(st.Pan115Signin, st, false); err != nil {
		return "", err
	}
	account, err := account115(pool)
	if err != nil {
		return "", err
	}
	if err := storageAuthBlocked(pool, time.Now()); err != nil {
		return "", err
	}
	if err := a.waitAPI(ctx, pool.ID); err != nil {
		return "", err
	}
	now := time.Now()
	day := now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	key := rapid115Stamp([]string{account, day})
	var previous pan115SigninReceipt
	if err := a.store.update(func(current *State) error {
		found := false
		for _, saved := range current.Pan115Signin.Rules {
			if saved == rule {
				found = true
			}
		}
		p, e := backupStorage(*current, pool.ID)
		if e != nil || !sameBackupStorage(p, pool) || !current.Pan115Signin.Enabled || !found {
			return errors.New("签到或存储配置已变化，未执行")
		}
		if current.Pan115SigninReceipts == nil {
			current.Pan115SigninReceipts = map[string]pan115SigninReceipt{}
		}
		previous = current.Pan115SigninReceipts[key]
		if previous.Status != "" {
			return nil
		}
		for k, receipt := range current.Pan115SigninReceipts {
			if now.Sub(receipt.Time) > 32*24*time.Hour {
				delete(current.Pan115SigninReceipts, k)
			}
		}
		if len(current.Pan115SigninReceipts) >= 5000 {
			return errors.New("签到回执已达上限，未提交")
		}
		current.Pan115SigninReceipts[key] = pan115SigninReceipt{Status: "pending", Time: now}
		return ctx.Err()
	}); err != nil {
		return "", err
	}
	if previous.Status == "success" {
		return "今日签到已完成", nil
	}
	if previous.Status != "" {
		return "", errors.New("今日已有签到提交回执，请核对115账号或运行日志；未重复提交")
	}
	message, callErr := submit115Signin(ctx, pool, account, now)
	status, level := "success", "info"
	if callErr != nil {
		status, level, message = "unconfirmed", "warn", callErr.Error()
	}
	if err := a.store.update(func(st *State) error {
		st.Pan115SigninReceipts[key] = pan115SigninReceipt{Status: status, Message: message, Time: now}
		return nil
	}); err != nil {
		return "", errors.New("签到结果未能保存，请核对账号；未自动重试")
	}
	a.store.event(level, "tasks", fmt.Sprintf("115签到：%s · %s：%s", pool.Name, source, message))
	return message, callErr
}

// Protocol independently implemented from p115client user_points_sign_post,
// revision 856e4f8f09ce034534bd458f06d3dee4474a1698; never replay a POST.
func submit115Signin(ctx context.Context, pool Storage, account string, now time.Time) (string, error) {
	credential, err := credential115(pool.Config["cookie"])
	if err != nil {
		return "", err
	}
	ts := fmt.Sprint(now.Unix())
	sum := sha1.Sum([]byte(account + "-Points_Sign@#115-" + ts))
	form := url.Values{"token": {hex.EncodeToString(sum[:])}, "token_time": {ts}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://proapi.115.com/android/2.0/user/points_sign", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", pan115UA)
	for key, value := range map[string]string{"UID": credential.UID, "CID": credential.CID, "SEID": credential.SEID, "KID": credential.KID} {
		if value != "" {
			req.AddCookie(&http.Cookie{Name: key, Value: value})
		}
	}
	client := *apiClient
	client.Timeout = 45 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("115签到连接失败，结果未确认；未自动重试")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("115签到返回HTTP %d，结果未确认；未自动重试", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return "", errors.New("115签到响应读取失败，结果未确认；未自动重试")
	}
	var result struct {
		State *bool `json:"state"`
	}
	if json.Unmarshal(raw, &result) != nil || result.State == nil {
		return "", errors.New("115签到响应格式异常，结果未确认；未自动重试")
	}
	if !*result.State {
		return "", errors.New("115未确认签到成功，请检查账号授权、设备类型和运行日志；今日未自动重试")
	}
	return "签到成功", nil
}

func due115SigninRules(settings pan115SigninSettings, next map[string]time.Time, now time.Time) []pan115SigninRule {
	dueRules := []pan115SigninRule{}
	if !settings.Enabled {
		return dueRules
	}
	for _, rule := range settings.Rules {
		schedule, err := cronParser.Parse(rule.Cron)
		if err != nil {
			continue
		}
		due := next[rule.StorageID]
		if due.IsZero() {
			next[rule.StorageID] = schedule.Next(now)
		} else if !now.Before(due) {
			next[rule.StorageID] = schedule.Next(now)
			dueRules = append(dueRules, rule)
		}
	}
	return dueRules
}

func (a *App) pan115SigninMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	revision := ""
	next := map[string]time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			settings := a.store.snapshotWithLogLimit(0).Pan115Signin
			stamp := rapid115Stamp(settings)
			if stamp != revision {
				revision, next = stamp, map[string]time.Time{}
			}
			for _, rule := range due115SigninRules(settings, next, now) {
				ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
				_, err := a.run115Signin(ctx, rule, "Cron计划")
				cancel()
				if err != nil {
					a.store.event("warn", "tasks", fmt.Sprintf("115签到计划 · 存储%s：%s", rule.StorageID, err))
				}
			}
		}
	}
}
