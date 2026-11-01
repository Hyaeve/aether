package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const ali115DefaultExtensions = "mp4;mkv;avi;mov;wmv;flv;webm;m4v;ts;m2ts;mkvb;rm;3gp;iso;ass;srt;ssa;sub"

var ali115ExtensionPattern = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

type ali115Target struct {
	ID        string `json:"id"`
	StorageID string `json:"storageId"`
	Directory string `json:"directory"`
	Path      string `json:"path"`
}
type ali115Settings struct {
	DeviceKey    string         `json:"secret,omitempty"`
	RefreshToken string         `json:"refreshToken,omitempty"`
	UserID       string         `json:"userId,omitempty"`
	Nick         string         `json:"nick,omitempty"`
	Drive        string         `json:"drive,omitempty"`
	Revision     string         `json:"revision,omitempty"`
	Extensions   string         `json:"extensions,omitempty"`
	Targets      []ali115Target `json:"targets"`
}
type ali115FileResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
type ali115Receipt struct {
	ID           string             `json:"id"`
	RequestID    string             `json:"requestId"`
	Target       ali115Target       `json:"target"`
	Account      string             `json:"account"`
	Revision     string             `json:"revision"`
	StorageStamp string             `json:"storageStamp"`
	Extensions   string             `json:"extensions"`
	Link         string             `json:"link"`
	Source       string             `json:"source"`
	Status       string             `json:"status"`
	Name         string             `json:"name"`
	Message      string             `json:"message"`
	Created      time.Time          `json:"created"`
	Updated      time.Time          `json:"updated"`
	Total        int                `json:"total"`
	Success      int                `json:"success"`
	Skipped      int                `json:"skipped"`
	Failed       int                `json:"failed"`
	Files        []ali115FileResult `json:"files,omitempty"`
	Intent       string             `json:"intent,omitempty"`
	Temporary    []ali115Temp       `json:"temporary,omitempty"`
}
type ali115Temp struct {
	ID     string `json:"id"`
	Parent string `json:"parent"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size,omitempty"`
	SHA1   string `json:"sha1,omitempty"`
}

func ali115Extensions(value string) (string, error) {
	if len(value) > 4096 {
		return "", errors.New("秒传文件扩展名过长")
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return r == ';' || r == '；' || r == ',' || r == '，' || r == '\n' || r == ' ' }) {
		v = strings.TrimPrefix(v, ".")
		if !ali115ExtensionPattern.MatchString(v) {
			return "", errors.New("请输入有效的文件扩展名，以分号分隔")
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return "", errors.New("请至少填写一种秒传文件扩展名")
	}
	return strings.Join(out, ";"), nil
}

func validateAli115(cfg ali115Settings, st State, legacy bool) error {
	if cfg.DeviceKey != "" {
		if _, err := hex.DecodeString(cfg.DeviceKey); err != nil || len(cfg.DeviceKey) != 64 {
			return errors.New("阿里云盘设备凭据无效")
		}
	}
	if cfg.Extensions != "" {
		if _, err := ali115Extensions(cfg.Extensions); err != nil {
			return err
		}
	}
	if len(cfg.RefreshToken) > 8192 || strings.ContainsAny(cfg.RefreshToken, " \r\n\t") || len(cfg.Targets) > 60 {
		return errors.New("阿里云盘秒传账号或接收目录无效")
	}
	seen := map[string]bool{}
	for _, t := range cfg.Targets {
		key := t.StorageID + ":" + t.Directory
		if !aliIDPattern.MatchString(t.ID) || !simulcastID(t.Directory) || !strings.HasPrefix(t.Path, "/") || len(t.Path) > 4096 || strings.ContainsAny(t.Path, "\x00\r\n") || seen[key] || seen[t.ID] {
			return errors.New("同一115存储的同一目录不能重复添加，或目录信息无效")
		}
		seen[key], seen[t.ID] = true, true
		if !legacy {
			valid := false
			for _, s := range st.Storages {
				if s.ID == t.StorageID && s.Type == "115" {
					valid = true
				}
			}
			if !valid {
				return errors.New("请选择115存储目录")
			}
		}
	}
	return nil
}

func ali115ConfigStamp(cfg ali115Settings) string {
	return telegramDigest([]any{cfg.UserID, cfg.Revision, cfg.Extensions})
}

func (a *App) ali115View() map[string]any {
	st := a.store.snapshotWithLogLimit(0)
	cfg := st.Ali115
	ext := cfg.Extensions
	if ext == "" {
		ext = ali115DefaultExtensions
	}
	active := []map[string]any{}
	for _, r := range st.Ali115Receipts {
		if r.Status == "running" {
			active = append(active, map[string]any{"target": r.Target.ID, "done": r.Success + r.Skipped + r.Failed, "total": r.Total, "message": r.Message})
		}
	}
	return map[string]any{"connected": cfg.UserID != "" && cfg.RefreshToken != "" && cfg.Drive != "", "hasToken": cfg.RefreshToken != "", "nick": cfg.Nick, "userId": cfg.UserID, "extensions": ext, "targets": cfg.Targets, "active": active}
}

func (a *App) ali115API(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonResponse(w, 200, a.ali115View())
		return
	}
	if !a.ali115Gate.TryLock() {
		fail(w, 409, errors.New("阿里云盘秒传正在执行或登录，请稍后修改配置"))
		return
	}
	defer a.ali115Gate.Unlock()
	switch r.Method {
	case "PUT":
		var in struct {
			Extensions string `json:"extensions"`
		}
		if !decode(w, r, &in) {
			return
		}
		ext, err := ali115Extensions(in.Extensions)
		if err == nil {
			err = a.store.update(func(st *State) error { st.Ali115.Extensions = ext; return nil })
		}
		if err != nil {
			fail(w, 400, err)
			return
		}
	case "POST":
		var target ali115Target
		if !decode(w, r, &target) {
			return
		}
		s, err := a.store.storage(target.StorageID)
		if err != nil || !s.Enabled || s.Type != "115" {
			fail(w, 400, errors.New("请选择启用的115存储目录"))
			return
		}
		target.ID = id()
		target.Directory = backupDir(s, target.Directory)
		trail, err := a.cloud115DirectoryTrail(r.Context(), s, target.Directory)
		if err != nil {
			fail(w, 502, errors.New("无法核验115接收目录，请重新选择"))
			return
		}
		inside := false
		names := []string{}
		for _, c := range trail {
			if c.ID == rootOf(s) || rootOf(s) == "/" && c.ID == "0" {
				inside = true
				continue
			}
			if inside {
				names = append(names, c.Name)
			}
		}
		if !inside {
			fail(w, 400, errors.New("接收目录不在存储根目录内"))
			return
		}
		target.Path = "/" + strings.Join(names, "/")
		err = a.store.update(func(st *State) error {
			current := false
			for _, pool := range st.Storages {
				if pool.ID == s.ID && pool.Enabled && storageRevision(pool) == storageRevision(s) {
					current = true
				}
			}
			if !current {
				return errors.New("存储配置已变化")
			}
			cfg := st.Ali115
			cfg.Targets = append(append([]ali115Target{}, cfg.Targets...), target)
			if err := validateAli115(cfg, *st, false); err != nil {
				return err
			}
			st.Ali115 = cfg
			return nil
		})
		if err != nil {
			fail(w, 409, err)
			return
		}
	case "DELETE":
		key := r.PathValue("id")
		if err := a.store.update(func(st *State) error {
			targets := []ali115Target{}
			found := false
			for _, t := range st.Ali115.Targets {
				if t.ID == key {
					found = true
				} else {
					targets = append(targets, t)
				}
			}
			if !found {
				return errors.New("接收目录不存在")
			}
			st.Ali115.Targets = targets
			return nil
		}); err != nil {
			fail(w, 404, err)
			return
		}
	default:
		w.WriteHeader(405)
		return
	}
	jsonResponse(w, 200, a.ali115View())
}

func (a *App) ali115AccountAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Refresh string `json:"refreshToken"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !a.ali115Gate.TryLock() {
		fail(w, 409, errors.New("阿里云盘秒传正在执行或登录，请稍后再试"))
		return
	}
	defer a.ali115Gate.Unlock()
	refresh := strings.TrimSpace(in.Refresh)
	explicit := refresh != ""
	if !explicit {
		refresh = a.store.snapshotWithLogLimit(0).Ali115.RefreshToken
	}
	if _, err := a.aliLogin(r.Context(), refresh, explicit); err != nil {
		fail(w, 502, err)
		return
	}
	jsonResponse(w, 200, a.ali115View())
}

func (a *App) ali115RecordsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	records := []ali115Receipt{}
	st := a.store.snapshotWithLogLimit(0)
	var target ali115Target
	for _, v := range st.Ali115.Targets {
		if v.ID == r.URL.Query().Get("target") {
			target = v
		}
	}
	for _, v := range st.Ali115Receipts {
		if v.Target.ID == r.URL.Query().Get("target") || target.ID != "" && v.Target.StorageID == target.StorageID && v.Target.Directory == target.Directory {
			v.Intent, v.Account, v.Revision, v.StorageStamp, v.Extensions = "", "", "", "", ""
			v.Temporary = nil
			records = append(records, v)
		}
	}
	// Newest first; each job is one durable record, progress updates never append duplicates.
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	jsonResponse(w, 200, records)
}

func (a *App) ali115Update(key string, fn func(*ali115Receipt)) error {
	return a.store.update(func(st *State) error {
		for i := range st.Ali115Receipts {
			if st.Ali115Receipts[i].ID == key {
				fn(&st.Ali115Receipts[i])
				st.Ali115Receipts[i].Updated = time.Now()
				return nil
			}
		}
		return errors.New("阿里云盘秒传回执不存在，已停止")
	})
}

// Reserve once before launching work. Lost HTTP replies/restarts never replay writes.
func (a *App) newAli115Receipt(targetID, text, source, requestID string) (ali115Receipt, error) {
	var out ali115Receipt
	share, err := parseAliShare(text)
	if err != nil {
		return out, err
	}
	if !aliIDPattern.MatchString(requestID) {
		return out, errors.New("请求标识无效，请重新打开秒传窗口")
	}
	err = a.store.update(func(st *State) error {
		cfg := st.Ali115
		if cfg.RefreshToken == "" || cfg.UserID == "" || cfg.Drive == "" {
			return errors.New("请先配置阿里云盘账号")
		}
		var target ali115Target
		for _, t := range cfg.Targets {
			if t.ID == targetID {
				target = t
			}
		}
		if target.ID == "" {
			return errors.New("115接收目录不存在")
		}
		var s Storage
		for _, pool := range st.Storages {
			if pool.ID == target.StorageID {
				s = pool
			}
		}
		if !s.Enabled || s.Type != "115" {
			return errors.New("115接收存储不可用")
		}
		for _, old := range st.Ali115Receipts {
			if old.RequestID == requestID {
				return errors.New("此请求已经受理，请查看秒传记录；未重复提交")
			}
			if old.Link == share.URL && old.Target.StorageID == target.StorageID && old.Target.Directory == target.Directory && old.Account == cfg.UserID && (old.Status == "running" || old.Intent != "") {
				return errors.New("此分享有执行中或未确认的秒传记录，请先核对云盘；未重复提交")
			}
		}
		if len(st.Ali115Receipts) >= 1000 {
			return errors.New("秒传记录已达1000条保护上限，请先归档配置与运行记录")
		}
		ext := cfg.Extensions
		if ext == "" {
			ext = ali115DefaultExtensions
		}
		out = ali115Receipt{ID: id(), RequestID: requestID, Target: target, Account: cfg.UserID, Revision: cfg.Revision, StorageStamp: storageRevision(s), Extensions: ext, Link: share.URL, Source: source, Status: "running", Name: share.Code, Created: time.Now(), Updated: time.Now(), Message: "正在读取分享"}
		st.Ali115Receipts = append(st.Ali115Receipts, out)
		return nil
	})
	return out, err
}

func (a *App) ali115RunAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Target    string `json:"target"`
		Link      string `json:"link"`
		RequestID string `json:"requestId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !a.ali115Gate.TryLock() {
		fail(w, 409, errors.New("阿里云盘秒传正在执行或登录，请稍后再试"))
		return
	}
	job, err := a.newAli115Receipt(in.Target, in.Link, "浏览器", in.RequestID)
	if err != nil {
		a.ali115Gate.Unlock()
		fail(w, 409, err)
		return
	}
	a.wg.Add(1)
	go func() { defer a.wg.Done(); defer a.ali115Gate.Unlock(); a.runAli115(a.ctx, job, nil) }()
	jsonResponse(w, 202, map[string]string{"id": job.ID})
}

func (a *App) ali115Current(job ali115Receipt) error {
	st := a.store.snapshotWithLogLimit(0)
	if st.Ali115.UserID != job.Account || st.Ali115.Revision != job.Revision {
		return errors.New("阿里云盘账号已变化，已停止")
	}
	for _, t := range st.Ali115.Targets {
		if t == job.Target {
			for _, s := range st.Storages {
				if s.ID == t.StorageID && s.Enabled && storageRevision(s) == job.StorageStamp {
					return nil
				}
			}
		}
	}
	return errors.New("115接收目录或存储配置已变化，已停止")
}

func ali115Summary(job ali115Receipt) string {
	return fmt.Sprintf("秒传%d项，已存在%d项，失败%d项", job.Success, job.Skipped, job.Failed)
}

// Reused by browser and TG; all cloud writes pass the same persisted-intent path.
func (a *App) runAli115(parent context.Context, job ali115Receipt, guard func() error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Hour)
	defer cancel()
	err := a.executeAli115(ctx, job, guard)
	status, message := "completed", "秒传完成，临时文件已清理"
	if err != nil {
		status, message = "failed", err.Error()
	}
	for _, r := range a.store.snapshotWithLogLimit(0).Ali115Receipts {
		if r.ID == job.ID {
			if len(r.Temporary) > 0 {
				message += "；临时文件位置 /Aether/rapid-" + job.ID
			}
			if err == nil && r.Total == 0 {
				message = "未找到符合秒传扩展名的文件"
			}
		}
	}
	if saveErr := a.ali115Update(job.ID, func(r *ali115Receipt) { r.Status, r.Message = status, message }); saveErr != nil {
		a.store.event("error", "tasks", "阿里云盘秒传结果保存失败，请核对云盘；未自动重试")
		return
	}
	level := "info"
	if err != nil {
		level = "warn"
	}
	a.store.event(level, "tasks", "阿里云盘秒传："+message)
}
