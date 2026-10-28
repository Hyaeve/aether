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
		a.casLog("error", s, "", "离线秒传存储兼容性校验失败")
		fail(w, 400, errors.New("CAS 秒传仅支持移动和天翼存储"))
		return
	}
	files := r.MultipartForm.File["cas"]
	if len(files) == 0 || len(files) > 20 {
		a.casLog("error", s, "", "离线秒传批次文件数量校验失败")
		fail(w, 400, errors.New("每批请选择 1–20 个 CAS 文件"))
		return
	}
	infos := make([]CASInfo, len(files))
	for i, file := range files {
		if file.Size <= 0 || file.Size > casLimit {
			a.casLog("error", s, file.Filename, "离线秒传特征文件大小校验失败")
			fail(w, 400, errors.New("CAS 文件须小于 1 MiB"))
			return
		}
		f, err := file.Open()
		if err != nil {
			a.casFailureLog(s, file.Filename, "离线秒传特征文件读取", err)
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
			a.casFailureLog(s, file.Filename, "离线秒传特征校验", err)
			fail(w, 400, err)
			return
		}
	}
	if err := a.acquireCAS(ctx); err != nil {
		a.casFailureLog(s, "", "离线秒传队列等待", err)
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

func (a *App) restoreCASInto(ctx context.Context, s Storage, parent string, info CASInfo) (resultErr error) {
	record, recordErr := a.beginCloudCASRecord(s, parent, info)
	if recordErr != nil {
		return errors.New("无法保存CAS秒传执行记录，未提交")
	}
	defer func() { a.finishCloudCASRecord(record, resultErr == nil) }()
	stage := "离线秒传目标检查"
	defer func() {
		if resultErr != nil {
			a.casFailureLog(s, info.Name, stage, resultErr)
		} else {
			a.casLog("info", s, info.Name, "离线秒传成功")
		}
	}()
	if nativeTianyi(s) {
		stage = "离线秒传会员额度预检"
		if err := a.checkTianyiCASSize(ctx, s, info.Size); err != nil {
			return err
		}
	}
	existing, err := a.cloudByName(ctx, s, parent, info.Name)
	if err != nil {
		return err
	}
	if existing != nil {
		stage = "离线秒传目标检查（存在同名文件，不覆盖）"
		return errors.New("目标目录已存在同名文件，不覆盖")
	}
	if nativeTianyi(s) {
		stage = "离线秒传提交"
		_, err = a.tianyiRestore(ctx, s, parent, info.Name, info)
		return err
	}
	stage = "离线秒传存储服务准备"
	host, err := a.mobileHost(ctx, s)
	if err != nil {
		return err
	}
	var result struct {
		ID    string `json:"fileId"`
		Exist bool   `json:"exist"`
		Rapid bool   `json:"rapidUpload"`
	}
	stage = "离线秒传提交"
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
		stage = "离线秒传未命中记录保存"
		// Record failed placeholders so the existing targeted cleanup can retry.
		record := CASTemporary{Key: "offline-failed:" + id(), StorageID: s.ID, FileID: result.ID, Name: info.Name}
		if err := a.store.update(func(st *State) error { st.CASTemporary = append(st.CASTemporary, record); return nil }); err != nil {
			_ = a.removeCASTemporary(ctx, s, host, result.ID)
			return errors.New("秒传失败且清理记录保存失败")
		}
	}
	stage = "离线秒传结果校验（未命中秒传，不上传媒体）"
	return errors.New("CAS 未命中秒传，不会上传原始媒体")
}
