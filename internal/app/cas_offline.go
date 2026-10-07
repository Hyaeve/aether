package app

import (
	"context"
	"errors"
	"io"
	"net/http"
)

// Explicit imports are durable user files, never playback-temporary records.
func (a *App) importCAS(w http.ResponseWriter, r *http.Request, ctx context.Context, s Storage, parent string) {
	if !casStorage(s) {
		fail(w, 400, errors.New("CAS 秒传仅支持移动和天翼存储"))
		return
	}
	files := r.MultipartForm.File["cas"]
	if len(files) == 0 || len(files) > 20 {
		fail(w, 400, errors.New("每批请选择 1–20 个 CAS 文件"))
		return
	}
	infos := make([]CASInfo, len(files))
	for i, file := range files {
		if file.Size <= 0 || file.Size > casLimit {
			fail(w, 400, errors.New("CAS 文件须小于 1 MiB"))
			return
		}
		f, err := file.Open()
		if err != nil {
			fail(w, 400, errors.New("CAS 文件读取失败"))
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, casLimit+1))
		f.Close()
		if err == nil {
			infos[i], err = decodeCAS(data, file.Filename)
		}
		if err == nil {
			err = validateCASFor(s, infos[i])
		}
		if err != nil {
			fail(w, 400, err)
			return
		}
	}
	if err := a.acquireCAS(ctx); err != nil {
		fail(w, 408, err)
		return
	}
	defer func() { <-a.casGate }()
	defer a.cache.clear()
	results := make([]offlineResult, 0, len(files))
	for _, info := range infos {
		err := a.restoreCASInto(ctx, s, parent, info)
		result := offlineResult{Name: info.Name, Success: err == nil}
		if err != nil {
			result.Message = err.Error()
		} else {
			result.Message = "秒传完成"
		}
		results = append(results, result)
	}
	a.store.event("info", "files", "完成 CAS 离线秒传批次")
	jsonResponse(w, 200, results)
}

func (a *App) restoreCASInto(ctx context.Context, s Storage, parent string, info CASInfo) error {
	existing, err := a.cloudByName(ctx, s, parent, info.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New("目标目录已存在同名文件，不覆盖")
	}
	if nativeTianyi(s) {
		_, err = a.tianyiRestore(ctx, s, parent, info.Name, info)
		return err
	}
	host, err := a.mobileHost(ctx, s)
	if err != nil {
		return err
	}
	var result struct {
		ID    string `json:"fileId"`
		Exist bool   `json:"exist"`
		Rapid bool   `json:"rapidUpload"`
	}
	err = a.mobilePost(ctx, s, host+"/file/create", map[string]any{
		"parentFileId": parent, "name": info.Name, "type": "file", "fileRenameMode": "refuse",
		"contentHash": info.SHA256, "contentHashAlgorithm": "SHA256", "contentType": "application/octet-stream",
		"parallelUpload": false, "size": info.Size, "partInfos": casParts(info.Size),
	}, true, &result)
	if err != nil {
		return err
	}
	if result.ID != "" && (result.Exist || result.Rapid) {
		return nil
	}
	if result.ID != "" {
		// Record failed placeholders so the existing targeted cleanup can retry.
		record := CASTemporary{Key: "offline-failed:" + id(), StorageID: s.ID, FileID: result.ID, Name: info.Name}
		if err := a.store.update(func(st *State) error { st.CASTemporary = append(st.CASTemporary, record); return nil }); err != nil {
			_ = a.removeCASTemporary(ctx, s, host, result.ID)
			return errors.New("秒传失败且清理记录保存失败")
		}
	}
	return errors.New("CAS 未命中秒传，不会上传原始媒体")
}
