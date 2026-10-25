package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// Sources and the confirmation snapshot live only in the encrypted preview.
type generationMobileSource struct {
	Code, Pass, ID, Token, Name, Owner string
}
type generationMobilePlan struct {
	StorageID, Revision, Pool, Directory string
}

// Write-ahead receipts survive interruption. They contain no share credentials;
// an uncertain write is never replayed or automatically cleaned after restart.
type GenerationTemporary struct {
	StorageID string                    `json:"storageId"`
	Revision  string                    `json:"revision"`
	Directory string                    `json:"directory"`
	FolderID  string                    `json:"folderId,omitempty"`
	TaskID    string                    `json:"taskId,omitempty"`
	Phase     string                    `json:"phase"`
	Files     []GenerationTemporaryFile `json:"files,omitempty"`
}
type GenerationTemporaryFile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Output string `json:"output"`
	Phase  string `json:"phase"`
}

func generationHasTemporary(job GenerationJob) bool {
	for _, receipt := range job.Temporary {
		if receipt.Phase != "cleaned" {
			return true
		}
	}
	return false
}

func generationMobilePool(st State) (CASPoolConfig, string) {
	p := requiredCASPool(st.CASPool, st)
	raw, _ := json.Marshal(struct {
		IDs      []string
		Rotation int
	}{p.Mobile.StorageIDs, p.RotationMinutes})
	sum := sha256.Sum256(raw)
	return p, hex.EncodeToString(sum[:])
}

func (a *App) selectGenerationMobile(now time.Time) (Storage, string, error) {
	st := a.store.snapshotWithLogLimit(0)
	p, stamp := generationMobilePool(st)
	if p.RotationMinutes < 1 || p.RotationMinutes > 1440 {
		return Storage{}, "", errors.New("CAS 绑定池轮换设置无效，请重新保存")
	}
	eligible := []Storage{}
	for _, sid := range p.Mobile.StorageIDs {
		for _, s := range st.Storages {
			if s.ID == sid && s.Enabled && nativeMobile(s) && storageAuthBlocked(s, now) == nil {
				if _, _, err := mobileAccount(s); err == nil {
					eligible = append(eligible, s)
				}
				break
			}
		}
	}
	if len(eligible) == 0 {
		return Storage{}, "", errors.New("没有可用的移动 CAS 绑定存储，请检查绑定池、启用状态和授权")
	}
	index := (now.Unix() / int64(p.RotationMinutes*60)) % int64(len(eligible))
	return eligible[index], stamp, nil
}

func (a *App) generationMobileCurrent(plan generationMobilePlan) (Storage, error) {
	st := a.store.snapshotWithLogLimit(0)
	p, stamp := generationMobilePool(st)
	if stamp == plan.Pool {
		for _, sid := range p.Mobile.StorageIDs {
			if sid != plan.StorageID {
				continue
			}
			for _, s := range st.Storages {
				if s.ID == sid && nativeMobile(s) && s.Enabled && storageRevision(s) == plan.Revision && storageAuthBlocked(s, time.Now()) == nil {
					return s, nil
				}
			}
		}
	}
	return Storage{}, errors.New("移动绑定存储不可用或配置已变化，请重新解析并确认；已转存资源保留供核对")
}

func (a *App) generationReceipt(job string, index int, fn func(*GenerationTemporary)) error {
	return a.generationUpdate(job, func(j *GenerationJob) { fn(&j.Temporary[index]) })
}

func (a *App) generationCreateFolder(ctx context.Context, s Storage, host, parent, name string) (string, error) {
	items, err := a.mobileListAt(ctx, s, host, parent)
	if err != nil {
		return "", err
	}
	for _, f := range items {
		if strings.EqualFold(f.Name, name) {
			return "", errors.New("临时目录名称已存在，未复用或覆盖")
		}
	}
	var data struct {
		ID string `json:"fileId"`
	}
	if err := a.mobilePost(ctx, s, host+"/file/create", map[string]any{"parentFileId": parent, "name": name, "description": "", "type": "folder", "fileRenameMode": "force_rename"}, false, &data); err != nil {
		return "", err
	}
	if data.ID == "" || data.ID == "/" || data.ID == parent {
		return "", errors.New("临时目录创建结果未确认，请在网盘核对，勿重复提交")
	}
	items, err = a.mobileListAt(ctx, s, host, parent)
	if err != nil {
		return "", err
	}
	found := false
	for _, f := range items {
		if f.ID == data.ID && f.Name == name && f.IsDir {
			found = true
		}
	}
	if !found {
		return "", errors.New("临时目录身份未确认，停止转存")
	}
	items, err = a.mobileListAt(ctx, s, host, data.ID)
	if err != nil {
		return "", err
	}
	if len(items) != 0 {
		return "", errors.New("新建临时目录非空，停止转存")
	}
	return data.ID, nil
}

func (a *App) generationTransfer(ctx context.Context, s Storage, entries []GenerationEntry, folder string) (string, error) {
	if len(entries) < 1 || len(entries) > shareTransferBatchLimit {
		return "", errors.New("临时转存批次大小无效")
	}
	account, _, err := mobileAccount(s)
	if err != nil {
		return "", err
	}
	source := entries[0].Mobile
	if source == nil {
		return "", errors.New("转存来源无效")
	}
	files := []string{}
	for _, entry := range entries {
		if entry.Mobile == nil || entry.Mobile.Code != source.Code || entry.Mobile.Pass != source.Pass || entry.Mobile.Token == "" {
			return "", errors.New("转存来源无效")
		}
		files = append(files, entry.Mobile.Token)
	}
	var data struct {
		ID     string `json:"taskID"`
		Nested struct {
			ID string `json:"taskID"`
		} `json:"createOuterLinkBatchOprTaskRes"`
	}
	if err := a.waitAPI(ctx, s.ID); err != nil {
		return "", err
	}
	err = mobileGenerationRequest(ctx, "createOuterLinkBatchOprTask", map[string]any{"createOuterLinkBatchOprTaskReq": map[string]any{
		"msisdn": account, "ownerAccount": source.Owner, "taskType": 1, "linkID": source.Code, "needPassword": source.Pass != "",
		"taskInfo": map[string]any{"linkID": source.Code, "needPassword": source.Pass != "", "contentInfoList": files, "catalogInfoList": []string{}, "newCatalogID": folder},
	}}, &data, &s)
	if err != nil {
		return "", err
	}
	if data.ID == "" {
		data.ID = data.Nested.ID
	}
	if data.ID == "" {
		return "", errors.New("转存任务编号未确认，请在网盘核对，勿重复提交")
	}
	return data.ID, nil
}

func generationMobileBatches(entries []GenerationEntry) [][]GenerationEntry {
	groups := [][]GenerationEntry{}
	for _, entry := range entries {
		if entry.Mobile == nil {
			continue
		}
		joined := false
		for i, g := range groups {
			first := g[0]
			if len(g) < shareTransferBatchLimit && first.Mobile.Code == entry.Mobile.Code && first.Mobile.Pass == entry.Mobile.Pass && path.Dir(first.Path) == path.Dir(entry.Path) {
				groups[i] = append(groups[i], entry)
				joined = true
				break
			}
		}
		if !joined {
			groups = append(groups, []GenerationEntry{entry})
		}
	}
	return groups
}

func (a *App) generationWaitTransfer(ctx context.Context, s Storage, task string) error {
	account, _, err := mobileAccount(s)
	if err != nil {
		return err
	}
	for {
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return err
		}
		var data struct {
			Result struct {
				Task struct {
					ID     string          `json:"taskID"`
					Status json.RawMessage `json:"taskStatus"`
					Result json.RawMessage `json:"taskResultCode"`
					Code   json.RawMessage `json:"resultCode"`
				} `json:"batchOprTask"`
			} `json:"queryBatchOprTaskDetailRes"`
		}
		if err := mobileGenerationRequest(ctx, "queryBatchOprTaskDetail", map[string]any{"queryBatchOprTaskDetailReq": map[string]any{"taskID": task, "msisdn": account}}, &data, &s); err != nil {
			return err
		}
		result := data.Result.Task
		if result.ID != task {
			return errors.New("转存任务身份未确认，临时资源保留")
		}
		switch mobileQRString(result.Status) {
		case "2":
			code, outcome := mobileQRString(result.Code), mobileQRString(result.Result)
			if code == "0" && (outcome == "0" || outcome == "1") {
				return nil
			}
			return errors.New("移动转存未全部成功，临时资源保留供核对")
		case "0", "1":
		default:
			return errors.New("移动转存状态未确认，临时资源保留")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

func (a *App) executeMobileGeneration(ctx context.Context, job GenerationJob, m generationManifest, base, rel string) {
	success := 0
	err := a.runMobileGeneration(ctx, job, m, base, rel, &success)
	status, message, failed := "success", fmt.Sprintf("生成完成：%d 项，已清理成功产物对应的临时文件", success), 0
	if err != nil {
		status, failed = "error", job.Total-success
		s, _ := a.store.storage(m.Mobile.StorageID)
		a.casFailureLog(s, "/Aether/"+m.Mobile.Directory, "分享解析生成（任务 "+job.ID+"）", err)
		// Never include upstream bodies, links or credentials in job responses/logs.
		message = "生成或临时清理未完成，请核对运行日志及网盘临时目录；勿直接重复提交"
		if errors.Is(err, context.Canceled) {
			status = "interrupted"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			message = "转存结果确认超时，临时资源保留；勿直接重复提交"
		}
	}
	_ = a.generationUpdate(job.ID, func(j *GenerationJob) {
		j.Status = status
		j.Message = message
		j.Processed = success
		j.Success = success
		j.Failed = failed
	})
	a.store.event(status, "tasks", job.Name+"："+message)
	if name, e := a.generationManifestPath(job.Preview); e == nil {
		_ = os.Remove(name)
	}
}

func (a *App) runMobileGeneration(ctx context.Context, job GenerationJob, m generationManifest, base, rel string, success *int) error {
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	// Preflight all destinations before any cloud mutation, still using exclusive
	// publication below to protect against a file arriving after this check.
	for _, e := range m.Entries {
		if _, err := root.Stat(path.Join(rel, e.Path)); !os.IsNotExist(err) {
			a.casLog("error", Storage{}, e.Path, "分享生成目标已存在或不可访问，未转存")
			return errors.New("生成目标已存在或不可访问，未转存")
		}
	}
	s, err := a.generationMobileCurrent(*m.Mobile)
	if err != nil {
		return err
	}
	host, err := a.mobileHost(ctx, s)
	if err != nil {
		a.casFailureLog(s, m.Mobile.Directory, "分享存储路由读取", err)
		return err
	}
	if err := a.generationUpdate(job.ID, func(j *GenerationJob) {
		j.Message = "准备临时转存：" + s.Name
		j.Temporary = append(j.Temporary, GenerationTemporary{StorageID: s.ID, Revision: m.Mobile.Revision, Directory: "/Aether/" + m.Mobile.Directory, Phase: "creating"})
	}); err != nil {
		return err
	}
	parent, err := a.mobileCASDirectory(ctx, s, host, "/", "Aether")
	if err != nil {
		a.casFailureLog(s, m.Mobile.Directory, "分享临时目录", err)
		return err
	}
	parents, err := a.mobileListAt(ctx, s, host, "/")
	if err != nil {
		return err
	}
	matchedParent := false
	for _, p := range parents {
		if p.ID == parent && p.IsDir && p.Name == "Aether" {
			matchedParent = true
		}
	}
	if !matchedParent {
		a.casLog("error", s, m.Mobile.Directory, "分享临时根目录与确认路径不一致，未转存")
		return errors.New("临时根目录与确认路径不一致，未转存")
	}
	if _, err = a.generationMobileCurrent(*m.Mobile); err != nil {
		return err
	}
	folder, err := a.generationCreateFolder(ctx, s, host, parent, m.Mobile.Directory)
	if err != nil {
		a.casFailureLog(s, m.Mobile.Directory, "分享临时目录", err)
		return err
	}
	if err = a.generationReceipt(job.ID, 0, func(r *GenerationTemporary) { r.FolderID = folder; r.Phase = "ready" }); err != nil {
		return err
	}
	for _, entry := range m.Entries {
		if entry.Mobile == nil {
			if err := a.generationPublish(root, rel, entry, job.ID, success); err != nil {
				return err
			}
		}
	}
	for index, entries := range generationMobileBatches(m.Entries) {
		if _, err := a.generationMobileCurrent(*m.Mobile); err != nil {
			return err
		}
		batchName := fmt.Sprintf("batch-%06d", index+1)
		receiptIndex := index + 1
		directory := "/Aether/" + m.Mobile.Directory + "/" + batchName
		if err := a.generationUpdate(job.ID, func(j *GenerationJob) {
			j.Temporary = append(j.Temporary, GenerationTemporary{StorageID: s.ID, Revision: m.Mobile.Revision, Directory: directory, Phase: "creating"})
		}); err != nil {
			return err
		}
		batch, err := a.generationCreateFolder(ctx, s, host, folder, batchName)
		if err != nil {
			a.casFailureLog(s, directory, "分享批次目录", err)
			return err
		}
		if err = a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) { r.FolderID = batch; r.Phase = "submitting" }); err != nil {
			return err
		}
		if err = a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) {
			for _, entry := range entries {
				r.Files = append(r.Files, GenerationTemporaryFile{Name: entry.Mobile.Name, Size: entry.Size, Output: entry.Path, Phase: "pending"})
			}
		}); err != nil {
			return err
		}
		if _, err := a.generationMobileCurrent(*m.Mobile); err != nil {
			return err
		}
		a.casLog("info", s, directory, fmt.Sprintf("分享临时转存开始，共%d项", len(entries)))
		task, err := a.generationTransfer(ctx, s, entries, batch)
		if err != nil {
			a.casFailureLog(s, directory, "分享转存提交（结果未确认，未重试）", err)
			return err
		}
		if err = a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) { r.TaskID = task; r.Phase = "submitted" }); err != nil {
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		err = a.generationWaitTransfer(waitCtx, s, task)
		cancel()
		if err != nil {
			a.casFailureLog(s, directory, "分享转存确认", err)
			return err
		}
		a.casLog("info", s, directory, "分享临时转存确认完成，读取 SHA256")
		if _, err := a.generationMobileCurrent(*m.Mobile); err != nil {
			return err
		}
		items, err := a.mobileListAt(ctx, s, host, batch)
		if err != nil {
			a.casFailureLog(s, directory, "分享临时文件读取", err)
			return err
		}
		files := []GenerationTemporaryFile{}
		seen := map[string]bool{}
		for _, entry := range entries {
			matches := []File{}
			for _, f := range items {
				if !f.IsDir && f.Name == entry.Mobile.Name {
					matches = append(matches, f)
				}
			}
			if len(matches) != 1 || matches[0].Size != entry.Size || seen[matches[0].ID] {
				a.casLog("error", s, entry.Path, "分享转存身份或大小未确认，未生成、未清理")
				return errors.New("临时文件身份或大小未确认，未生成、未清理")
			}
			f := matches[0]
			if validateCAS(CASInfo{Name: f.Name, Size: f.Size, SHA256: f.SHA256}) != nil {
				a.casLog("error", s, entry.Path, "分享转存后仍缺少有效 SHA256，未生成、未清理")
				return errors.New("转存后仍未取得有效 SHA256，未清理")
			}
			seen[f.ID] = true
			files = append(files, GenerationTemporaryFile{ID: f.ID, Name: f.Name, Size: f.Size, SHA256: strings.ToLower(f.SHA256), Output: entry.Path, Phase: "ready"})
		}
		if err = a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) { r.Files = files; r.Phase = "generating" }); err != nil {
			return err
		}
		for i, f := range files {
			if _, err := a.generationMobileCurrent(*m.Mobile); err != nil {
				return err
			}
			info := CASInfo{Name: f.Name, Size: f.Size, SHA256: f.SHA256}
			entry := GenerationEntry{Path: f.Output, Size: f.Size, CAS: &info}
			if err := a.generationPublish(root, rel, entry, job.ID, success); err != nil {
				a.casFailureLog(s, f.Name, "分享STRM发布", err)
				return err
			}
			if err := a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) { r.Files[i].Phase = "generated" }); err != nil {
				return err
			}
			if err := a.generationCleanup(ctx, s, host, batch, root, rel, entry, f, *m.Mobile); err != nil {
				a.casFailureLog(s, f.Name, "分享临时清理（保留供核对）", err)
				return err
			}
			if err := a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) { r.Files[i].Phase = "cleaned" }); err != nil {
				return err
			}
			a.casLog("info", s, f.Name, "分享STRM生成校验完成，临时文件已清理")
		}
		if err := a.generationReceipt(job.ID, receiptIndex, func(r *GenerationTemporary) { r.Phase = "cleaned" }); err != nil {
			return err
		}
	}
	// Empty remote directories deliberately remain: no atomic empty-only removal.
	return a.generationReceipt(job.ID, 0, func(r *GenerationTemporary) { r.Phase = "cleaned" })
}

func (a *App) generationPublish(root *os.Root, rel string, entry GenerationEntry, job string, success *int) error {
	if entry.CAS == nil || validateCAS(*entry.CAS) != nil {
		return errors.New("缺少有效 CAS 特征，未生成")
	}
	content := []byte(a.generatedStreamURL(streamClaim{CAS: entry.CAS, Redirect: true}) + "\n")
	filename := path.Join(rel, entry.Path)
	if _, err := root.Stat(filename); !os.IsNotExist(err) {
		return errors.New("生成文件已存在或不可访问，未覆盖")
	}
	if err := writeCASOutput(root, filename, content, true, true); err != nil {
		return err
	}
	raw, err := readGenerationOutput(root, filename)
	if err != nil || !bytes.Equal(raw, content) {
		return errors.New("STRM 发布校验失败，临时文件保留")
	}
	(*success)++
	return a.generationUpdate(job, func(j *GenerationJob) { j.Processed = *success; j.Success = *success; j.Message = entry.Path })
}

func (a *App) generationCleanup(ctx context.Context, s Storage, host, folder string, root *os.Root, rel string, entry GenerationEntry, f GenerationTemporaryFile, plan generationMobilePlan) error {
	if _, err := a.generationMobileCurrent(plan); err != nil {
		return err
	}
	raw, err := readGenerationOutput(root, path.Join(rel, entry.Path))
	if err != nil || string(raw) != a.generatedStreamURL(streamClaim{CAS: entry.CAS, Redirect: true})+"\n" {
		return errors.New("STRM 已变化，未删除临时源")
	}
	items, err := a.mobileListAt(ctx, s, host, folder)
	if err != nil {
		return err
	}
	matched := false
	for _, item := range items {
		if item.ID == f.ID && !item.IsDir && item.Name == f.Name && item.Size == f.Size && strings.EqualFold(item.SHA256, f.SHA256) {
			matched = true
		}
	}
	if !matched {
		return errors.New("临时源身份已变化，未删除")
	}
	if _, err := a.generationMobileCurrent(plan); err != nil {
		return err
	}
	raw, err = readGenerationOutput(root, path.Join(rel, entry.Path))
	if err != nil || string(raw) != a.generatedStreamURL(streamClaim{CAS: entry.CAS, Redirect: true})+"\n" {
		return errors.New("STRM 已变化，未删除临时源")
	}
	if err := a.removeCASTemporary(ctx, s, host, f.ID); err != nil {
		return err
	}
	items, err = a.mobileListAt(ctx, s, host, folder)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.ID == f.ID {
			return errors.New("临时清理结果未确认，未重复删除")
		}
	}
	return nil
}

func readGenerationOutput(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > casLimit {
		return nil, errors.New("STRM 产物类型或大小已变化")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, casLimit+1))
	if len(raw) > casLimit {
		return nil, errors.New("STRM 产物过大")
	}
	return raw, err
}
