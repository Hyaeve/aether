package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const cloudPageSize = 100

func cloudManagementSupported(s Storage, kind string) bool {
	switch kind {
	case "shares":
		return s.Type == "115" || s.Type == "quark"
	case "trash":
		return s.Type == "115"
	case "downloads":
		return s.Type == "115" || casStorage(s)
	}
	return false
}

func (a *App) cloudManagementPage(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	page := 1
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err != nil || page < 1 || page > 100 {
		fail(w, 400, errors.New("分页参数无效"))
		return
	}
	kind, key := r.URL.Query().Get("kind"), r.URL.Query().Get("storage")
	var rows []cloudManagementEntry
	var more bool
	var s Storage
	if key == "cas" && kind == "downloads" {
		all := []cloudManagementEntry{}
		st := a.store.snapshotWithLogLimit(0)
		for _, record := range st.CloudCASRecords {
			for _, pool := range st.Storages {
				if pool.ID == record.StorageID && pool.Enabled && casStorage(pool) {
					all = append(all, cloudCASManagementEntry(pool, record))
					break
				}
			}
		}
		start := min((page-1)*cloudPageSize, len(all))
		end := min(start+cloudPageSize, len(all))
		rows, more = all[start:end], end < len(all)
	} else {
		s, err = a.store.storage(key)
		if err != nil || !s.Enabled || !cloudManagementSupported(s, kind) {
			fail(w, 400, errors.New("此存储暂不支持该云盘管理功能"))
			return
		}
		if err = a.cloudStorageCurrent(s); err != nil {
			fail(w, 409, err)
			return
		}
		rows, more, err = a.readCloudManagementPage(ctx, s, kind, page)
		if err != nil {
			fail(w, 502, err)
			return
		}
		if s.Type == "115" && (kind == "trash" || kind == "downloads") {
			paths := map[string]string{}
			for i := range rows {
				row := &rows[i]
				if strings.HasPrefix(row.Path, "/") {
					continue
				}
				if full, ok := paths[row.ParentID]; ok {
					row.Path = full
					continue
				}
				full := a.cloud115DirectoryPath(ctx, s, row.ParentID)
				if full == "" {
					full = row.Path + "（完整路径不可用）"
				}
				paths[row.ParentID], row.Path = full, full
			}
		}
	}
	if err = validateCloudRows(rows); err != nil {
		fail(w, 502, err)
		return
	}
	next := 0
	if more {
		next = page + 1
	}
	jsonResponse(w, 200, map[string]any{"items": rows, "nextPage": next, "edit": s.Type == "115" && kind == "shares", "password": s.Type == "115" && kind == "trash"})
}

func cloudCASManagementEntry(s Storage, record cloudCASRecord) cloudManagementEntry {
	progress := float64(0)
	if record.Status == "success" {
		progress = 1
	}
	return cloudManagementEntry{ID: record.ID, Name: record.CAS.Name, Size: record.CAS.Size, Path: record.Parent, ParentID: record.Parent, Time: record.Time, Status: record.Status, Progress: progress, StorageID: s.ID, StorageName: s.Name, Provider: s.Type}
}

func (a *App) readCloudManagementPage(ctx context.Context, s Storage, kind string, page int) ([]cloudManagementEntry, bool, error) {
	items := []cloudManagementEntry{}
	if !cloudManagementSupported(s, kind) {
		return nil, false, errors.New("此存储的该项云盘管理接口暂不支持")
	}
	if kind == "downloads" && casStorage(s) {
		for _, record := range a.store.snapshotWithLogLimit(0).CloudCASRecords {
			if record.StorageID == s.ID {
				items = append(items, cloudCASManagementEntry(s, record))
			}
		}
		start := min((page-1)*cloudPageSize, len(items))
		end := min(start+cloudPageSize, len(items))
		return items[start:end], end < len(items), nil
	}
	if s.Type == "quark" {
		var response struct {
			Data struct {
				List []map[string]json.RawMessage `json:"list"`
			} `json:"data"`
		}
		err := shareRequest(ctx, s, http.MethodGet, quarkManagementURL("/share/mypage/detail", url.Values{"_page": {strconv.Itoa(page)}, "_size": {"100"}, "_order_field": {"created_at"}, "_order_type": {"desc"}, "_filter": {"all"}, "_share_type": {"0,1"}}), nil, &response)
		if err != nil {
			return nil, false, err
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
		return items, len(items) == cloudPageSize, nil
	}
	var response map[string]json.RawMessage
	query := url.Values{"offset": {strconv.Itoa((page - 1) * cloudPageSize)}, "limit": {"100"}}
	address, method := "", http.MethodGet
	switch kind {
	case "shares":
		query.Set("show_cancel_share", "0")
		address = "https://webapi.115.com/share/slist?" + query.Encode()
	case "trash":
		query.Set("aid", "7")
		query.Set("cid", "0")
		query.Set("format", "json")
		address = "https://webapi.115.com/rb?" + query.Encode()
	case "downloads":
		query = url.Values{"ct": {"lixian"}, "ac": {"task_lists"}, "page": {strconv.Itoa(page)}}
		address, method = "https://lixian.115.com/lixian/?"+query.Encode(), http.MethodPost
	}
	if err := shareRequest(ctx, s, method, address, nil, &response); err != nil {
		return nil, false, err
	}
	raw := response["data"]
	if kind == "downloads" {
		raw = response["tasks"]
	}
	if len(raw) == 0 {
		raw = response["list"]
	}
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "false" {
		return nil, false, errors.New("115云盘列表响应不完整")
	}
	rows := cloudRows(raw)
	for _, row := range rows {
		entry := cloudManagementEntry{StorageID: s.ID, StorageName: s.Name, Provider: s.Type}
		switch kind {
		case "shares":
			entry.ID = cloudString(row, "share_code")
			if !shareCodePattern.MatchString(entry.ID) {
				return nil, false, errors.New("分享列表标识无效")
			}
			entry.Name, entry.Code, entry.Size = cloudString(row, "share_title"), cloudString(row, "receive_code"), cloudInt(row, "file_size")
			entry.URL, entry.Status = "https://115cdn.com/s/"+entry.ID, "有效"
			if entry.Code != "" {
				entry.URL += "?password=" + url.QueryEscape(entry.Code) + "&#"
			}
			if cloudInt(row, "share_state") != 1 {
				entry.Status = "状态 " + cloudString(row, "share_state")
			}
			entry.Expiration, entry.Time = cloudString(row, "share_duration"), time.Unix(cloudInt(row, "create_time"), 0)
		case "trash":
			entry.ID, entry.Name, entry.Size = cloudString(row, "id"), cloudString(row, "file_name"), cloudInt(row, "file_size")
			entry.ParentID, entry.Path = cloudString(row, "cid"), cloudString(row, "parent_path")
			if entry.Path == "" {
				entry.Path = cloudString(row, "parent_name")
			}
			entry.Time, entry.Status = time.Unix(cloudInt(row, "dtime"), 0), "已删除"
		case "downloads":
			entry.ID, entry.Name, entry.URL, entry.Size = cloudString(row, "info_hash"), cloudString(row, "name"), cloudString(row, "url"), cloudInt(row, "size")
			entry.ParentID = cloudString(row, "wp_path_id")
			entry.Path, entry.Time, entry.Status = entry.ParentID, time.Unix(cloudInt(row, "add_time"), 0), "running"
			switch cloudInt(row, "status") {
			case 0:
				entry.Status = "queued"
			case 2:
				entry.Status = "success"
			case -1:
				entry.Status = "error"
			}
			percent, _ := strconv.ParseFloat(cloudString(row, "percentDone"), 64)
			entry.Progress = min(1, max(0, percent/100))
			if entry.Status == "success" {
				entry.Progress = 1
			}
		}
		items = append(items, entry)
	}
	more := len(rows) == cloudPageSize
	if kind == "downloads" {
		more = int64(page) < cloudInt(response, "page_count") && len(rows) > 0
	}
	return items, more, nil
}

func (a *App) cloud115DirectoryPath(ctx context.Context, s Storage, key string) string {
	if key == "0" || key == "/" {
		return "/"
	}
	if key == "" {
		return ""
	}
	if err := a.waitAPI(ctx, s.ID); err != nil {
		return ""
	}
	var response struct {
		CID  json.RawMessage `json:"cid"`
		Path []struct {
			CID  json.RawMessage `json:"cid"`
			Name string          `json:"name"`
		} `json:"path"`
	}
	err := shareRequest(ctx, s, http.MethodGet, "https://webapi.115.com/files?"+url.Values{"cid": {key}, "limit": {"1"}, "offset": {"0"}}.Encode(), nil, &response)
	if err != nil || len(response.Path) == 0 {
		return ""
	}
	if strings.Trim(string(response.Path[len(response.Path)-1].CID), `"`) != key {
		return ""
	}
	names := []string{}
	for _, parent := range response.Path {
		if strings.Trim(string(parent.CID), `"`) != "0" {
			names = append(names, parent.Name)
		}
	}
	return "/" + strings.Join(names, "/")
}
