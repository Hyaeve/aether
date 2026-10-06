package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Native cloud operations never transfer credentials or IDs between accounts.
func (a *App) cloudFileAction(ctx context.Context, s, target Storage, req fileActionRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if s.ID != target.ID {
		return errors.New("云端跨存储池传输尚未实现，请选择当前存储池内的目录")
	}
	if !nativeMobile(s) && !nativeTianyi(s) && s.Type != "115" && s.Type != "quark" {
		return errors.New("此存储驱动尚未接入写操作")
	}
	if req.Action == "delete" && s.Config["deleteMode"] == "permanent" && (s.Type == "115" || s.Type == "quark") {
		return errors.New("115/夸克暂仅支持移入回收站，请调整删除模式或在官方客户端永久删除")
	}
	items, err := a.rawList(ctx, s, req.Source)
	if err != nil {
		return err
	}
	selected := []File{}
	seen := map[string]bool{}
	for _, fid := range req.IDs {
		if seen[fid] {
			return errors.New("重复项目")
		}
		seen[fid] = true
		found := false
		for _, f := range items {
			if f.ID == fid && fid != rootOf(s) {
				selected = append(selected, f)
				found = true
				break
			}
		}
		if !found {
			return errors.New("源目录已变化，请刷新后重试")
		}
	}
	dest := req.Target
	if dest == "" || dest == "/" {
		dest = rootOf(s)
	}
	if req.Action == "move" || req.Action == "copy" || req.Action == "rename" {
		targetItems := items
		if req.Action != "rename" {
			for _, f := range selected {
				if f.ID == dest {
					return errors.New("目标不能是自身")
				}
			}
			targetItems, err = a.rawList(ctx, s, dest)
			if err != nil {
				return err
			}
		}
		for _, f := range selected {
			name := f.Name
			if req.Action == "rename" {
				name = req.Name
			}
			for _, existing := range targetItems {
				if strings.EqualFold(name, existing.Name) {
					return errors.New("目标名称已存在，不覆盖已有项目")
				}
			}
		}
	}
	if nativeMobile(s) {
		host, err := a.mobileHost(ctx, s)
		if err != nil {
			return err
		}
		endpoint := ""
		body := map[string]any{"fileIds": req.IDs}
		switch req.Action {
		case "rename":
			endpoint = "/file/update"
			body = map[string]any{"fileId": req.IDs[0], "name": req.Name, "description": ""}
		case "move":
			endpoint = "/file/batchMove"
			body["toParentFileId"] = dest
		case "copy":
			endpoint = "/file/batchCopy"
			body["toParentFileId"] = dest
		case "delete":
			endpoint = "/recyclebin/batchTrash"
			if s.Config["deleteMode"] == "permanent" {
				endpoint = "/file/batchDelete"
			}
		}
		return a.mobilePost(ctx, s, host+endpoint, body, false, nil)
	}
	if nativeTianyi(s) {
		if req.Action == "rename" {
			values := url.Values{"fileId": {req.IDs[0]}, "destFileName": {req.Name}}
			endpoint := "/renameFile.action"
			if selected[0].IsDir {
				values = url.Values{"folderId": {req.IDs[0]}, "destFolderName": {req.Name}}
				endpoint = "/renameFolder.action"
			}
			var result map[string]any
			return a.tianyiRequest(ctx, s, "POST", tianyiAPI+endpoint, values, &result)
		}
		infos := []map[string]any{}
		for _, f := range selected {
			folder := 0
			if f.IsDir {
				folder = 1
			}
			infos = append(infos, map[string]any{"fileId": f.ID, "fileName": f.Name, "isFolder": folder})
		}
		raw, _ := json.Marshal(infos)
		kinds := []string{strings.ToUpper(req.Action)}
		if req.Action == "delete" && s.Config["deleteMode"] == "permanent" {
			kinds = append(kinds, "CLEAR_RECYCLE")
		}
		for _, kind := range kinds {
			var created struct{ TaskID string }
			err := a.tianyiRequest(ctx, s, "POST", tianyiAPI+"/batch/createBatchTask.action", url.Values{"type": {kind}, "taskInfos": {string(raw)}, "targetFolderId": {dest}}, &created)
			if err != nil {
				return err
			}
			if created.TaskID == "" {
				return errors.New("天翼未返回任务ID")
			}
			for {
				var status struct{ TaskStatus, FailedCount, SkipCount, SuccessedCount int }
				if err := a.tianyiRequest(ctx, s, "POST", tianyiAPI+"/batch/checkBatchTask.action", url.Values{"type": {kind}, "taskId": {created.TaskID}}, &status); err != nil {
					return err
				}
				if status.FailedCount > 0 || status.SkipCount > 0 || status.TaskStatus == 2 {
					return errors.New("天翼操作失败或存在冲突，请刷新查看结果")
				}
				if status.TaskStatus == 4 {
					if status.SuccessedCount < len(selected) {
						return errors.New("天翼未确认全部项目完成")
					}
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(400 * time.Millisecond):
				}
			}
		}
		return nil
	}
	if s.Type == "115" {
		endpoint := ""
		form := url.Values{}
		switch req.Action {
		case "rename":
			endpoint = "update"
			form = url.Values{"file_id": {req.IDs[0]}, "file_name": {req.Name}}
		case "move":
			endpoint = "move"
			form = url.Values{"file_ids": {strings.Join(req.IDs, ",")}, "to_cid": {dest}}
		case "copy":
			endpoint = "copy"
			form = url.Values{"file_id": {strings.Join(req.IDs, ",")}, "pid": {dest}, "nodupli": {"1"}}
		case "delete":
			endpoint = "delete"
			form = url.Values{"file_ids": {strings.Join(req.IDs, ",")}}
		}
		var result struct{ State bool }
		if err := requestJSON(ctx, "POST", "https://proapi.115.com/open/ufile/"+endpoint, cloudHeaders(s), form, &result); err != nil {
			return err
		}
		if !result.State {
			return errors.New("115拒绝文件操作")
		}
		return nil
	}
	endpoint := ""
	body := map[string]any{}
	switch req.Action {
	case "rename":
		endpoint = "rename"
		body = map[string]any{"fid": req.IDs[0], "file_name": req.Name}
	case "move":
		endpoint = "move"
		body = map[string]any{"action_type": 1, "exclude_fids": []string{}, "filelist": req.IDs, "to_pdir_fid": dest}
	case "copy":
		endpoint = "copy"
		body = map[string]any{"filelist": req.IDs, "to_pdir_fid": dest}
	case "delete":
		endpoint = "delete"
		body = map[string]any{"filelist": req.IDs, "action_type": 1}
	}
	var result struct {
		Code, Status int
		Data         struct {
			TaskID string `json:"task_id"`
			Status int
		}
	}
	if err := requestJSON(ctx, "POST", "https://drive.quark.cn/1/clouddrive/file/"+endpoint+"?pr=ucpro&fr=pc", cloudHeaders(s), body, &result); err != nil {
		return err
	}
	if result.Code != 0 || result.Status >= 400 {
		return errors.New("夸克拒绝文件操作")
	}
	if req.Action == "copy" {
		if result.Data.TaskID == "" {
			return errors.New("夸克未返回复制任务ID")
		}
		taskID := result.Data.TaskID
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			if err := requestJSON(ctx, http.MethodGet, "https://drive.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&retry_index=0&task_id="+url.QueryEscape(taskID), cloudHeaders(s), nil, &result); err != nil {
				return err
			}
			if result.Code != 0 || result.Status >= 400 {
				return errors.New("夸克复制失败")
			}
			if result.Data.Status == 2 {
				break
			}
		}
	}
	return nil
}
