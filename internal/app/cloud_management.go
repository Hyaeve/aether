package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cloudShareOptions struct {
	Days      int    `json:"days"`
	CodeMode  string `json:"codeMode"`
	Code      string `json:"code"`
	Limit     int    `json:"limit"`
	Autofill  bool   `json:"autofill"`
	Encrypted bool   `json:"encrypted"`
}
type cloudManagementEntry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url,omitempty"`
	Code       string    `json:"code,omitempty"`
	Size       int64     `json:"size"`
	Path       string    `json:"path,omitempty"`
	Time       time.Time `json:"time"`
	Status     string    `json:"status"`
	Progress   float64   `json:"progress"`
	Expiration string    `json:"expiration,omitempty"`
	IsDir      bool      `json:"isDir,omitempty"`
}

func cloudString(m map[string]json.RawMessage, key string) string {
	raw := m[key]
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}
func cloudInt(m map[string]json.RawMessage, key string) int64 {
	n, _ := strconv.ParseInt(cloudString(m, key), 10, 64)
	return n
}
func cloudRows(raw json.RawMessage) []map[string]json.RawMessage {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) == nil {
		return rows
	}
	var object map[string]map[string]json.RawMessage
	_ = json.Unmarshal(raw, &object)
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rows = append(rows, object[key])
	}
	return rows
}
func randomShareCode() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	s := make([]byte, 4)
	for i, v := range b {
		s[i] = chars[int(v)%len(chars)]
	}
	return string(s)
}
func defaultCloudShareOptions(kind string) cloudShareOptions {
	v := cloudShareOptions{CodeMode: "random", Autofill: true}
	if kind == "115" {
		v.Days = 15
	}
	if kind == "quark" {
		v.Autofill = false
	}
	return v
}
func validateCloudShareOptions(kind string, v cloudShareOptions) error {
	if kind != "115" && kind != "quark" {
		return errors.New("此存储尚不支持创建分享")
	}
	if v.Limit < 0 || v.Limit > 999999 {
		return errors.New("接收次数无效")
	}
	if kind == "115" {
		if !map[int]bool{0: true, 1: true, 3: true, 5: true, 7: true, 15: true}[v.Days] {
			return errors.New("115分享有效期无效")
		}
		if v.CodeMode != "fixed" && v.CodeMode != "random" {
			return errors.New("访问码模式无效")
		}
		if v.CodeMode == "fixed" && !validCloudCode(v.Code) {
			return errors.New("访问码须为4位字母或数字")
		}
	}
	if kind == "quark" {
		if !map[int]bool{0: true, 1: true, 7: true, 30: true}[v.Days] {
			return errors.New("夸克有效期无效")
		}
		if v.Limit != 0 {
			return errors.New("夸克访问次数限制尚无已验证接口，暂不支持")
		}
	}
	return nil
}
func validCloudCode(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func (a *App) cloudSharePreferences(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil {
		fail(w, 400, err)
		return
	}
	if r.Method == http.MethodGet {
		v, ok := a.store.snapshotWithLogLimit(0).CloudShareOptions[s.Type]
		if !ok {
			v = defaultCloudShareOptions(s.Type)
		}
		jsonResponse(w, 200, v)
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(405)
		return
	}
	var v cloudShareOptions
	if !decode(w, r, &v) {
		return
	}
	if err = validateCloudShareOptions(s.Type, v); err != nil {
		fail(w, 400, err)
		return
	}
	err = a.store.update(func(st *State) error {
		if st.CloudShareOptions == nil {
			st.CloudShareOptions = map[string]cloudShareOptions{}
		}
		st.CloudShareOptions[s.Type] = v
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, v)
}
func (a *App) createCloudShare(w http.ResponseWriter, r *http.Request) {
	var v struct {
		StorageID string            `json:"storageId"`
		Parent    string            `json:"parent"`
		IDs       []string          `json:"ids"`
		Options   cloudShareOptions `json:"options"`
	}
	if !decode(w, r, &v) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if err := a.acquireCAS(ctx); err != nil {
		fail(w, 408, err)
		return
	}
	defer func() { <-a.casGate }()
	s, err := a.store.storage(v.StorageID)
	if err != nil || !s.Enabled {
		fail(w, 400, errors.New("请选择已启用的存储池"))
		return
	}
	if err = validateCloudShareOptions(s.Type, v.Options); err != nil {
		fail(w, 400, err)
		return
	}
	if err = a.cloudStorageCurrent(s); err != nil {
		fail(w, 409, err)
		return
	}
	if len(v.IDs) == 0 || len(v.IDs) > 100 {
		fail(w, 400, errors.New("每次请选择1至100项"))
		return
	}
	files, err := a.rawList(ctx, s, v.Parent)
	if err != nil {
		fail(w, 400, err)
		return
	}
	names := []string{}
	size := int64(0)
	seen := map[string]bool{}
	for _, key := range v.IDs {
		if seen[key] {
			fail(w, 400, errors.New("分享项目重复"))
			return
		}
		seen[key] = true
		found := false
		for _, f := range files {
			if f.ID == key {
				found = true
				names = append(names, f.Name)
				size += f.Size
				break
			}
		}
		if !found {
			fail(w, 400, errors.New("分享项目已移动或删除"))
			return
		}
	}
	result := cloudManagementEntry{Name: strings.Join(names, "、"), Size: size, Status: "有效", Time: time.Now()}
	if err = a.cloudStorageCurrent(s); err != nil {
		fail(w, 409, err)
		return
	}
	if s.Type == "115" {
		var response struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		err = shareRequest(ctx, s, http.MethodPost, "https://webapi.115.com/share/send", url.Values{"file_ids": {strings.Join(v.IDs, ",")}, "ignore_warn": {"1"}}, &response)
		if err == nil {
			result.ID = cloudString(response.Data, "share_code")
			result.Code = cloudString(response.Data, "receive_code")
			if !shareCodePattern.MatchString(result.ID) {
				err = errors.New("分享创建结果不完整，请检查网盘分享列表，不要重复提交")
			}
		}
		if err == nil {
			if err = a.cloudStorageCurrent(s); err != nil {
				fail(w, 409, errors.New("存储状态已变化，分享可能已创建，请检查分享列表"))
				return
			}
			if v.Options.CodeMode == "fixed" {
				result.Code = v.Options.Code
			} else {
				result.Code = randomShareCode()
			}
			days := v.Options.Days
			if days == 0 {
				days = -1
			}
			err = shareRequest(ctx, s, http.MethodPost, "https://webapi.115.com/share/updateshare", url.Values{"share_code": {result.ID}, "share_duration": {strconv.Itoa(days)}, "receive_code": {result.Code}, "is_custom_code": {"1"}, "receive_user_limit": {strconv.Itoa(v.Options.Limit)}, "auto_fill_recvcode": {boolDigit(v.Options.Autofill)}}, &response)
			result.URL = "https://115cdn.com/s/" + result.ID
			if v.Options.Autofill {
				result.URL += "?password=" + url.QueryEscape(result.Code) + "&#"
			}
		}
	} else {
		code := ""
		urlType := 1
		if v.Options.Encrypted {
			code = randomShareCode()
			urlType = 2
		}
		expired := map[int]int{0: 1, 1: 2, 7: 3, 30: 4}[v.Options.Days]
		var response struct {
			Data struct {
				TaskID   string `json:"task_id"`
				ShareID  string `json:"share_id"`
				Status   int    `json:"status"`
				ShareURL string `json:"share_url"`
				Passcode string `json:"passcode"`
				PwdID    string `json:"pwd_id"`
			} `json:"data"`
		}
		session := s
		err = shareSessionRequest(ctx, &session, http.MethodPost, quarkManagementURL("/share", nil), map[string]any{"fid_list": v.IDs, "title": result.Name, "url_type": urlType, "passcode": code, "expired_type": expired}, &response)
		if err == nil && response.Data.TaskID == "" {
			err = errors.New("分享创建未返回任务标识，请检查网盘结果，不要重复提交")
		}
		if err == nil {
			task := response.Data.TaskID
			for i := 0; i < 30; i++ {
				select {
				case <-ctx.Done():
					err = ctx.Err()
				case <-time.After(time.Second):
				}
				if err != nil {
					break
				}
				err = shareSessionRequest(ctx, &session, http.MethodGet, quarkManagementURL("/task", url.Values{"task_id": {task}, "retry_index": {strconv.Itoa(i)}}), nil, &response)
				if err != nil || response.Data.Status == 3 {
					if err == nil {
						err = errors.New("分享创建失败")
					}
					break
				}
				if response.Data.Status == 2 {
					result.ID = response.Data.ShareID
					break
				}
			}
			if err == nil && result.ID == "" {
				err = errors.New("创建结果未确认，请检查网盘结果，不要重复提交")
			}
		}
		if err == nil {
			err = shareSessionRequest(ctx, &session, http.MethodPost, quarkManagementURL("/share/password", nil), map[string]string{"share_id": result.ID}, &response)
			result.URL = response.Data.ShareURL
			if result.URL == "" && response.Data.PwdID != "" {
				result.URL = "https://pan.quark.cn/s/" + response.Data.PwdID
			}
			result.Code = response.Data.Passcode
			if result.Code == "" {
				result.Code = code
			}
			if v.Options.Autofill && result.Code != "" {
				result.URL += "?pwd=" + url.QueryEscape(result.Code)
			}
		}
	}
	if err != nil {
		a.store.event("error", "storage", s.Name+" 创建分享未完成；写请求不会自动重试")
		fail(w, 502, errors.New("创建或设置分享未完成，请检查分享列表，勿直接重复提交"))
		return
	}
	if _, _, _, err = parseShareLink(result.URL, result.Code); err != nil {
		fail(w, 502, errors.New("未返回有效官方分享链接，请查看分享列表"))
		return
	}
	a.store.event("info", "storage", s.Name+" 创建分享成功："+result.Name)
	jsonResponse(w, 200, result)
}
func boolDigit(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
func quarkManagementURL(p string, q url.Values) string {
	if q == nil {
		q = url.Values{}
	}
	q.Set("pr", "ucpro")
	q.Set("fr", "pc")
	return "https://drive-pc.quark.cn/1/clouddrive" + p + "?" + q.Encode()
}

func (a *App) cloudManagementList(ctx context.Context, s Storage, kind string) ([]cloudManagementEntry, error) {
	items := []cloudManagementEntry{}
	if s.Type == "115" && kind == "shares" {
		for offset := 0; offset < 10000; offset += 100 {
			var response struct {
				Data json.RawMessage `json:"data"`
			}
			if err := shareRequest(ctx, s, http.MethodGet, "https://webapi.115.com/share/slist?"+url.Values{"offset": {strconv.Itoa(offset)}, "limit": {"100"}, "show_cancel_share": {"0"}}.Encode(), nil, &response); err != nil {
				return nil, err
			}
			rows := cloudRows(response.Data)
			for _, row := range rows {
				code := cloudString(row, "share_code")
				if !shareCodePattern.MatchString(code) {
					return nil, errors.New("分享列表标识无效")
				}
				status := "有效"
				if cloudInt(row, "share_state") != 1 {
					status = "状态 " + cloudString(row, "share_state")
				}
				items = append(items, cloudManagementEntry{ID: code, Name: cloudString(row, "share_title"), URL: "https://115cdn.com/s/" + code, Code: cloudString(row, "receive_code"), Size: cloudInt(row, "file_size"), Status: status, Expiration: cloudString(row, "share_duration"), Time: time.Unix(cloudInt(row, "create_time"), 0)})
			}
			if len(rows) < 100 {
				return items, nil
			}
		}
		return nil, errors.New("分享列表超过10000项，未完整加载")
	}
	if s.Type == "quark" && kind == "shares" {
		for page := 1; page <= 100; page++ {
			var response struct {
				Data struct {
					List []map[string]json.RawMessage `json:"list"`
				} `json:"data"`
			}
			err := shareRequest(ctx, s, http.MethodGet, quarkManagementURL("/share/mypage/detail", url.Values{"_page": {strconv.Itoa(page)}, "_size": {"100"}, "_order_field": {"created_at"}, "_order_type": {"desc"}, "_filter": {"all"}, "_share_type": {"0,1"}}), nil, &response)
			if err != nil {
				return nil, err
			}
			for _, row := range response.Data.List {
				link := cloudString(row, "share_url")
				if link == "" {
					link = "https://pan.quark.cn/s/" + cloudString(row, "pwd_id")
				}
				status := "有效"
				if cloudInt(row, "audit_status") > 1 {
					status = "审核或违规"
				}
				items = append(items, cloudManagementEntry{ID: cloudString(row, "share_id"), Name: cloudString(row, "title"), URL: link, Code: cloudString(row, "passcode"), Size: cloudInt(row, "size"), Status: status, Time: time.UnixMilli(cloudInt(row, "created_at"))})
			}
			if len(response.Data.List) < 100 {
				return items, nil
			}
		}
		return nil, errors.New("分享列表超过10000项")
	}
	if kind == "downloads" && casStorage(s) {
		for _, record := range a.store.snapshotWithLogLimit(0).CloudCASRecords {
			if record.StorageID == s.ID {
				progress := float64(0)
				if record.Status == "success" {
					progress = 1
				}
				items = append(items, cloudManagementEntry{ID: record.ID, Name: record.CAS.Name, Size: record.CAS.Size, Path: record.Parent, Time: record.Time, Status: record.Status, Progress: progress})
			}
		}
		return items, nil
	}
	if s.Type != "115" {
		return nil, errors.New("此存储的该项云盘管理接口暂不支持")
	}
	c, err := client115(ctx, s)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "trash":
		for offset := 0; offset < 10000; offset += 100 {
			rows, err := c.ListRecycleBin(offset, 100)
			if err != nil {
				return nil, errors.New("115回收站读取失败")
			}
			for _, row := range rows {
				items = append(items, cloudManagementEntry{ID: row.FileId, Name: row.FileName, Size: int64(row.FileSize), Path: row.ParentName, Time: time.Unix(int64(row.DeleteTime), 0), Status: "已删除"})
			}
			if len(rows) < 100 {
				return items, nil
			}
		}
		return nil, errors.New("回收站超过10000项，无法保证完整操作")
	case "downloads":
		for page := int64(1); page <= 100; page++ {
			result, err := c.ListOfflineTask(page)
			if err != nil {
				return nil, errors.New("115云下载读取失败")
			}
			for _, row := range result.Tasks {
				status := "running"
				if row.IsDone() {
					status = "success"
				} else if row.IsFailed() {
					status = "error"
				} else if row.IsTodo() {
					status = "queued"
				}
				progress := row.Percent / 100
				if status == "success" {
					progress = 1
				}
				items = append(items, cloudManagementEntry{ID: row.InfoHash, Name: row.Name, URL: row.Url, Size: row.Size, Path: row.DirId, Time: time.Unix(row.AddTime, 0), Status: status, Progress: progress})
			}
			if page >= result.PageCount || len(result.Tasks) == 0 {
				return items, nil
			}
		}
		return nil, errors.New("云下载列表过大")
	}
	return nil, errors.New("管理栏目无效")
}
func (a *App) cloudManagement(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(405)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	if r.Method == http.MethodPost {
		if err := a.acquireCAS(ctx); err != nil {
			fail(w, 408, err)
			return
		}
		defer func() { <-a.casGate }()
	}
	key := r.URL.Query().Get("storage")
	s, err := a.store.storage(key)
	if err != nil || !s.Enabled {
		fail(w, 400, errors.New("请选择已启用的云盘存储"))
		return
	}
	kind := r.URL.Query().Get("kind")
	if err = a.cloudStorageCurrent(s); err != nil {
		fail(w, 409, err)
		return
	}
	rows, err := a.cloudManagementList(ctx, s, kind)
	if err == nil {
		err = validateCloudRows(rows)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	if r.Method == http.MethodGet {
		jsonResponse(w, 200, map[string]any{"items": rows, "edit": s.Type == "115" && kind == "shares", "password": s.Type == "115" && kind == "trash"})
		return
	}
	var v struct {
		IDs      []string          `json:"ids"`
		Action   string            `json:"action"`
		Password string            `json:"password"`
		Confirm  bool              `json:"confirm"`
		Options  cloudShareOptions `json:"options"`
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if !decode(w, r, &v) {
		return
	}
	if !v.Confirm || len(v.IDs) > 499 {
		fail(w, 400, errors.New("请二次确认操作，每批最多499项"))
		return
	}
	selected := []cloudManagementEntry{}
	seen := map[string]bool{}
	for _, key := range v.IDs {
		if seen[key] {
			fail(w, 400, errors.New("选择项目重复"))
			return
		}
		seen[key] = true
		for _, row := range rows {
			if row.ID == key {
				selected = append(selected, row)
				break
			}
		}
	}
	if v.Action == "clear" {
		if len(v.IDs) != 0 {
			fail(w, 400, errors.New("清空操作参数无效"))
			return
		}
		selected = rows
	}
	if len(selected) != len(v.IDs) && v.Action != "clear" {
		fail(w, 409, errors.New("项目已变化，请刷新重选"))
		return
	}
	if len(selected) == 0 {
		jsonResponse(w, 200, map[string]int{"count": 0})
		return
	}
	if err = a.cloudStorageCurrent(s); err != nil {
		fail(w, 409, err)
		return
	}
	switch kind {
	case "shares":
		if v.Action != "cancel" && v.Action != "edit" {
			err = errors.New("操作无效")
			break
		}
		if s.Type == "115" {
			form := url.Values{"share_code": {strings.Join(v.IDs, ",")}}
			if v.Action == "cancel" {
				form.Set("action", "cancel")
			} else {
				if len(v.IDs) != 1 {
					err = errors.New("请逐项编辑分享")
					break
				}
				if err = validateCloudShareOptions(s.Type, v.Options); err != nil {
					break
				}
				code := v.Options.Code
				if v.Options.CodeMode == "random" {
					code = randomShareCode()
				}
				days := v.Options.Days
				if days == 0 {
					days = -1
				}
				form.Set("share_duration", strconv.Itoa(days))
				form.Set("receive_code", code)
				form.Set("auto_fill_recvcode", boolDigit(v.Options.Autofill))
				form.Set("receive_user_limit", strconv.Itoa(v.Options.Limit))
			}
			var out any
			err = shareRequest(ctx, s, http.MethodPost, "https://webapi.115.com/share/updateshare", form, &out)
		} else if v.Action == "cancel" {
			var out any
			err = shareRequest(ctx, s, http.MethodPost, quarkManagementURL("/share/delete", nil), map[string]any{"share_ids": v.IDs}, &out)
		} else {
			err = errors.New("此存储暂不支持编辑分享")
		}
	case "trash":
		if v.Action != "restore" && v.Action != "delete" && v.Action != "clear" {
			err = errors.New("回收站操作无效")
			break
		}
		c, e := client115(ctx, s)
		if e != nil {
			err = e
			break
		}
		if v.Action != "restore" && (len(v.Password) != 4 || strings.Trim(v.Password, "0123456789") != "") {
			err = errors.New("请重新输入4位115回收站密码")
			break
		}
		ids := []string{}
		for _, row := range selected {
			ids = append(ids, row.ID)
		}
		if v.Action == "restore" {
			err = c.RevertRecycleBin(ids...)
		} else {
			for start := 0; start < len(ids); start += 499 {
				if err = a.cloudStorageCurrent(s); err != nil {
					break
				}
				if e = c.CleanRecycleBin(v.Password, ids[start:min(start+499, len(ids))]...); e != nil {
					err = e
					break
				}
			}
		}
	case "downloads":
		if casStorage(s) {
			if v.Action == "delete" {
				err = a.store.update(func(st *State) error {
					kept := []cloudCASRecord{}
					for _, record := range st.CloudCASRecords {
						if record.StorageID == s.ID && seen[record.ID] && record.Status == "running" {
							return errors.New("正在执行的CAS记录不可删除")
						}
						if record.StorageID != s.ID || !seen[record.ID] {
							kept = append(kept, record)
						}
					}
					st.CloudCASRecords = kept
					return nil
				})
			} else if v.Action == "retry" {
				for _, record := range a.store.snapshotWithLogLimit(0).CloudCASRecords {
					if record.StorageID != s.ID || !seen[record.ID] || record.Status == "success" {
						continue
					}
					if record.Status == "running" {
						err = errors.New("正在执行的CAS请求不可重复提交")
						break
					}
					if record.Revision != rapid115Stamp(s.Config) {
						err = errors.New("源账号已变化，拒绝重试")
						break
					}
					if err = a.cloudStorageCurrent(s); err != nil {
						break
					}
					if err = a.restoreCASInto(ctx, s, record.Parent, record.CAS); err != nil {
						break
					}
				}
			} else {
				err = errors.New("操作无效")
			}
			break
		}
		c, e := client115(ctx, s)
		if e != nil {
			err = e
			break
		}
		if v.Action == "delete" {
			err = c.DeleteOfflineTasks(v.IDs, false)
		} else if v.Action == "retry" {
			for _, row := range selected {
				if row.Status == "success" {
					continue
				}
				if row.URL == "" {
					err = errors.New("此任务没有可重试链接")
					break
				}
				if err = a.cloudStorageCurrent(s); err != nil {
					break
				}
				result := submit115URIs(ctx, s, row.Path, []string{row.URL})
				if !result[0].Success {
					err = errors.New("重试提交未确认，请核对云盘结果")
					break
				}
			}
		} else {
			err = errors.New("操作无效")
		}
	}
	if err != nil {
		a.store.event("error", "storage", fmt.Sprintf("%s %s %s未完成；勿直接重复提交", s.Name, kind, v.Action))
		fail(w, 502, errors.New("云盘操作未完成，请核对上游结果；写请求不会自动重试"))
		return
	}
	a.cache.clear()
	a.store.event("info", "storage", fmt.Sprintf("%s %s %s完成：%d项", s.Name, kind, v.Action, len(selected)))
	jsonResponse(w, 200, map[string]int{"count": len(selected)})
}

func (a *App) cloudStorageCurrent(s Storage) error {
	current, err := a.store.storage(s.ID)
	if err != nil || !current.Enabled || current.Type != s.Type || rapid115Stamp(current.Config) != rapid115Stamp(s.Config) {
		return errors.New("存储配置已变化，请重新选择")
	}
	return storageAuthBlocked(current, time.Now())
}

func validateCloudRows(rows []cloudManagementEntry) error {
	seen := map[string]bool{}
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" || seen[row.ID] {
			return errors.New("云盘列表标识缺失或重复，拒绝操作，请刷新")
		}
		seen[row.ID] = true
	}
	return nil
}
