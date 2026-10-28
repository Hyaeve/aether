package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const casLimit = 1 << 20

var errCASIncomplete = errors.New("CAS信息缺失：需要SHA256，或同时包含MD5与分片MD5，不满足cas.strm生成要求")

func casRetentionHours(hours int) int {
	if hours < 1 || hours > 8760 {
		return 12
	}
	return hours
}

func (entry CASTemporary) expiresAt() time.Time {
	return entry.LastUsed.Add(time.Duration(casRetentionHours(entry.RetentionHours)) * time.Hour)
}

type CASInfo struct {
	PlaybackURL    string `json:"playback_url,omitempty"`
	RetentionHours int    `json:"retentionHours,omitempty"`
	Provider       string `json:"provider"`
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	SHA256         string `json:"sha256"`
	MD5            string `json:"md5"`
	SliceMD5       string `json:"slice_md5"`
	SliceSize      int64  `json:"slice_size"`
}

// marshalCASContent is the portable standalone .cas format. Provider is an
// execution detail owned by the binding pool and must not be persisted in the
// file. Keep the field order stable so exported files are easy to inspect.
func marshalCASContent(info CASInfo) ([]byte, error) {
	type content struct {
		Name      string `json:"name"`
		Size      int64  `json:"size"`
		SHA256    string `json:"sha256"`
		MD5       string `json:"md5"`
		SliceMD5  string `json:"slice_md5"`
		SliceSize int64  `json:"slice_size"`
	}
	return json.MarshalIndent(content{
		Name: info.Name, Size: info.Size, SHA256: info.SHA256,
		MD5: info.MD5, SliceMD5: info.SliceMD5, SliceSize: info.SliceSize,
	}, "", "  ")
}

type CASTemporary struct {
	Feature        string    `json:"feature,omitempty"`
	Key            string    `json:"key"`
	StorageID      string    `json:"storageId"`
	FileID         string    `json:"fileId"`
	Name           string    `json:"name"`
	Size           int64     `json:"size"`
	LastUsed       time.Time `json:"lastUsed"`
	Ready          bool      `json:"ready"`
	Trashed        bool      `json:"trashed,omitempty"`
	RetentionHours int       `json:"retentionHours,omitempty"`
}

type CASRestoreStats struct {
	Success uint64 `json:"success"`
	Failed  uint64 `json:"failed"`
	Mobile  uint64 `json:"mobile"`
	Tianyi  uint64 `json:"tianyi"`
}

func validateCAS(info CASInfo) error {
	// Tasks apply their configured extensions; portable CAS has no media whitelist.
	if !safeName(info.Name) || info.Size <= 0 || info.Size > 1<<40 {
		return errors.New("CAS 原始文件名或大小无效（最大 1 TiB）")
	}
	hash, err := hex.DecodeString(info.SHA256)
	md5, md5Err := hex.DecodeString(info.MD5)
	if info.SHA256 != "" && (err != nil || len(hash) != 32) || info.MD5 != "" && (md5Err != nil || len(md5) != 16) {
		return errors.New("CAS 哈希格式无效")
	}
	slice, sliceErr := hex.DecodeString(info.SliceMD5)
	if info.SliceMD5 != "" && (sliceErr != nil || len(slice) != 16) {
		return errors.New("CAS 分片MD5格式无效")
	}
	if len(hash) != 32 && (len(md5) != 16 || len(slice) != 16) {
		return errCASIncomplete
	}
	if info.SliceSize < 0 || info.SliceSize > 1<<40 || info.SliceSize != 0 && info.SliceMD5 == "" {
		return errors.New("CAS 分片大小无效")
	}
	return nil
}

func validateCASFor(s Storage, info CASInfo) error {
	info = normalizeCASPlayback(info)
	if err := validateCAS(info); err != nil {
		return err
	}
	value, size := info.SHA256, 32
	if nativeTianyi(s) {
		if info.MD5 == "" || info.SliceMD5 == "" {
			return errors.New("天翼CAS秒传需要同时包含整文件MD5和分片MD5")
		}
		if info.SliceSize <= 0 {
			return errors.New("天翼CAS缺少分片大小，无法按OpenList规则计算")
		}
		value, size = info.MD5, 16
	}
	hash, err := hex.DecodeString(value)
	if err != nil || len(hash) != size {
		return errors.New("CAS 特征与存储不兼容：移动需SHA256，天翼需MD5、分片MD5和分片大小")
	}
	return nil
}

// Older CAS/STRM files may carry slice_md5 but omit slice_size. The project
// uses the OpenList Cloud189 PC rule for that legacy input at playback time.
func normalizeCASPlayback(info CASInfo) CASInfo {
	if info.SliceMD5 != "" && info.SliceSize == 0 && info.Size > 0 {
		info.SliceSize = tianyiCASPartSize(info.Size)
	}
	return info
}

func decodeCAS(content []byte, filename string) (CASInfo, error) {
	var info CASInfo
	pointer := strings.HasSuffix(strings.ToLower(filename), ".cas.strm")
	if len(content) > casLimit || (!strings.EqualFold(path.Ext(filename), ".cas") && !pointer) {
		return info, errors.New("CAS 文件过大或后缀无效")
	}
	content = []byte(strings.TrimSpace(strings.TrimPrefix(string(content), "\ufeff")))
	var raw []byte
	var err error
	if pointer {
		u, e := url.Parse(string(content))
		if e != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.HasPrefix(u.Path, "/cas/") || strings.ContainsAny(string(content), "\r\n") {
			return info, errors.New("CAS STRM 内容不是有效播放地址")
		}
		claimRaw, e := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(u.Path, "/cas/"))
		var claim struct {
			CAS json.RawMessage `json:"cas"`
		}
		if e != nil || json.Unmarshal(claimRaw, &claim) != nil || len(claim.CAS) == 0 || string(claim.CAS) == "null" {
			return info, errors.New("CAS STRM 未包含哈希特征")
		}
		raw = claim.CAS
	} else if len(content) > 0 && content[0] == '{' {
		raw = content
	} else {
		raw, err = base64.StdEncoding.DecodeString(string(content))
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(string(content))
		}
	}
	if err != nil {
		return info, errors.New("CAS 内容不是合法 Base64 或 JSON")
	}
	// Some CAS writers serialize the byte count as a decimal string.
	var data struct {
		Provider       string          `json:"provider"`
		Name           string          `json:"name"`
		Size           json.RawMessage `json:"size"`
		SHA256         string          `json:"sha256"`
		MD5            string          `json:"md5"`
		SliceMD5       string          `json:"slice_md5"`
		CamelSliceMD5  string          `json:"sliceMd5"`
		SliceSize      json.RawMessage `json:"slice_size"`
		CamelSliceSize json.RawMessage `json:"sliceSize"`
		PlaybackURL    string          `json:"playback_url"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return info, errors.New("CAS 内容不是合法 JSON")
	}
	if data.SliceMD5 != "" && data.CamelSliceMD5 != "" && !strings.EqualFold(data.SliceMD5, data.CamelSliceMD5) {
		return info, errors.New("CAS分片MD5字段冲突")
	}
	if data.SliceMD5 == "" {
		data.SliceMD5 = data.CamelSliceMD5
	}
	parseSliceSize := func(raw json.RawMessage) (int64, error) {
		if len(raw) == 0 {
			return 0, nil
		}
		var number json.Number
		if json.Unmarshal(raw, &number) != nil {
			return 0, errors.New("CAS分片大小无效")
		}
		value, e := number.Int64()
		if e != nil {
			return 0, errors.New("CAS分片大小无效")
		}
		return value, nil
	}
	sliceSize, e := parseSliceSize(data.SliceSize)
	if e != nil {
		return info, e
	}
	camelSize, e := parseSliceSize(data.CamelSliceSize)
	if e != nil {
		return info, e
	}
	if sliceSize != 0 && camelSize != 0 && sliceSize != camelSize {
		return info, errors.New("CAS分片大小字段冲突")
	}
	if sliceSize == 0 {
		sliceSize = camelSize
	}
	var size json.Number
	if json.Unmarshal(data.Size, &size) != nil {
		return info, errors.New("CAS 大小无效")
	}
	n, err := size.Int64()
	if err != nil {
		return info, errors.New("CAS 大小无效")
	}
	if sliceSize == 0 && data.SliceMD5 != "" {
		// Legacy CAS follows the Cloud189PC size-based rule when omitted.
		sliceSize = tianyiCASPartSize(n)
	}
	if !safeName(data.Name) || !safeName(filename) {
		return info, errors.New("CAS 文件名包含非法路径")
	}
	name := strings.TrimSuffix(filename, path.Ext(filename))
	if pointer {
		name = filename[:len(filename)-len(".cas.strm")]
	}
	if strings.TrimSpace(name) == "" {
		return info, errors.New("CAS 文件名不能为空")
	}
	if path.Ext(name) == "" {
		name += path.Ext(data.Name)
	}
	info = CASInfo{Provider: strings.ToLower(data.Provider), Name: name, Size: n, SHA256: strings.ToLower(data.SHA256), MD5: strings.ToLower(data.MD5), SliceMD5: strings.ToUpper(data.SliceMD5), SliceSize: sliceSize, PlaybackURL: data.PlaybackURL}
	return info, validateCAS(info)
}

func (a *App) readCAS(ctx context.Context, s Storage, f File) (CASInfo, error) {
	if f.Size > casLimit {
		return CASInfo{}, errors.New("CAS 特征文件超过 1 MiB")
	}
	d, err := a.download(ctx, s, f.ID, f.PickCode)
	if err != nil {
		return CASInfo{}, err
	}
	var input io.ReadCloser
	if s.Type == "local" {
		root, e := os.OpenRoot(s.Config["root"])
		if e != nil {
			return CASInfo{}, e
		}
		defer root.Close()
		input, err = root.Open(d.Local)
	} else {
		input = &davFile{ctx: ctx, info: davInfo{f}, download: d}
	}
	if err != nil {
		return CASInfo{}, err
	}
	defer input.Close()
	content, err := io.ReadAll(io.LimitReader(&cancelReader{ctx, input}, casLimit+1))
	if err != nil {
		return CASInfo{}, err
	}
	return decodeCAS(content, f.Name)
}

func casParts(size int64) []map[string]any {
	partSize := int64(100 << 20)
	if size/(1<<30) > 30 {
		partSize = 512 << 20
	}
	parts := []map[string]any{}
	for offset := int64(0); offset < size && len(parts) < 100; offset += partSize {
		parts = append(parts, map[string]any{"partNumber": len(parts) + 1, "partSize": min(partSize, size-offset), "parallelHashCtx": map[string]int64{"partOffset": offset}})
	}
	return parts
}

func (a *App) casFolder(ctx context.Context, s Storage, host string) (string, error) {
	if nativeTianyi(s) {
		return a.tianyiCASFolder(ctx, s)
	}
	parent, err := a.mobileCASDirectory(ctx, s, host, "/", "Aether")
	if err != nil {
		return "", err
	}
	return a.mobileCASDirectory(ctx, s, host, parent, "CAS")
}

func (a *App) mobileCASDirectory(ctx context.Context, s Storage, host, parent, name string) (string, error) {
	files, err := a.mobileListAt(ctx, s, host, parent)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.Name == name {
			if !f.IsDir {
				return "", errors.New("CAS 临时目录与已有文件冲突")
			}
			return f.ID, nil
		}
	}
	var data struct {
		ID string `json:"fileId"`
	}
	err = a.mobilePost(ctx, s, host+"/file/create", map[string]any{"parentFileId": parent, "name": name, "description": "", "type": "folder", "fileRenameMode": "force_rename"}, false, &data)
	if err != nil {
		return "", err
	}
	if data.ID == "" {
		return "", errors.New("CAS 临时目录创建失败")
	}
	return data.ID, nil
}

func (a *App) removeCASTemporary(ctx context.Context, s Storage, host, fid string) error {
	if nativeTianyi(s) {
		return a.tianyiRemove(ctx, s, fid)
	}
	endpoint := "/recyclebin/batchTrash"
	if s.Config["deleteMode"] == "permanent" {
		endpoint = "/file/batchDelete"
	}
	return a.mobilePost(ctx, s, host+endpoint, map[string]any{"fileIds": []string{fid}}, false, nil)
}

// A gate serializes restore/cleanup and a lease prevents removal during a proxied response.
func (a *App) acquireCAS(ctx context.Context) error {
	select {
	case a.casGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) casLog(level string, s Storage, name, result string) {
	provider := map[string]string{"mobile": "移动云盘", "tianyi": "天翼云盘"}[s.Type]
	if provider == "" {
		provider = "未选定存储"
	}
	a.store.event(level, "tasks", fmt.Sprintf("CAS %s：文件 %q；%s；存储池 %q（%q）", result, name, provider, s.Name, s.ID))
}

func (a *App) casFailureLog(s Storage, name, stage string, err error) {
	level, result := "error", stage+"失败"
	if errors.Is(err, context.Canceled) {
		level, result = "warn", stage+"已取消"
	} else if errors.Is(err, context.DeadlineExceeded) {
		result = stage + "超时"
	}
	// Upstream errors may contain signed URLs or credentials; log only the stage.
	a.casLog(level, s, name, result)
}

func (a *App) casDownload(ctx context.Context, s Storage, claim streamClaim) (download Download, releaseResult func(), resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	attempted := false
	stage := "播放特征校验"
	defer func() {
		if resultErr != nil && !attempted {
			name := ""
			if claim.CAS != nil {
				name = claim.CAS.Name
			}
			a.casFailureLog(s, name, stage, resultErr)
		}
	}()
	done := func() {}
	if claim.CAS == nil {
		return Download{}, done, errors.New("CAS 播放仅支持原生移动新版个人云或天翼个人云")
	}
	if err := validateCAS(*claim.CAS); err != nil {
		return Download{}, done, err
	}
	normalized := normalizeCASPlayback(*claim.CAS)
	claim.CAS = &normalized
	stage = "秒传队列等待"
	if err := a.acquireCAS(ctx); err != nil {
		return Download{}, done, err
	}
	defer func() { <-a.casGate }()
	candidates := []casStorageCandidate{{s, claim.RetentionHours}}
	if claim.Storage == "" {
		stage = "绑定池存储选择"
		var err error
		candidates, err = a.casStorageCandidates(*claim.CAS, time.Now(), "")
		if err != nil {
			return Download{}, done, err
		}
	}
	var failures []error
	for i, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return Download{}, done, err
		}
		s, claim.RetentionHours = candidate.storage, candidate.hours
		stage = "存储与特征兼容性校验"
		if !casStorage(s) {
			return Download{}, done, errors.New("CAS 播放仅支持原生移动新版个人云或天翼个人云")
		}
		if err := validateCASFor(s, *claim.CAS); err != nil {
			return Download{}, done, err
		}
		if i > 0 {
			reason := "转交其他移动绑定存储"
			if candidates[i-1].storage.Type != s.Type {
				reason = "转交另一类绑定存储"
			}
			a.casLog("warn", s, claim.CAS.Name, reason+"（前一账号未取得直链，不重复提交同一账号）")
		}
		attempt, stop := ctx, func() {}
		if len(candidates) > 1 {
			deadline, _ := ctx.Deadline()
			budget := min(45*time.Second, time.Until(deadline)/time.Duration(len(candidates)-i))
			attempt, stop = context.WithTimeout(ctx, budget)
		}
		attempted = true
		d, release, err := a.casDownloadAttempt(attempt, s, claim)
		stop()
		if err == nil {
			return d, release, nil
		}
		failures = append(failures, err)
	}
	return Download{}, done, fmt.Errorf("CAS秒传失败：所有可用绑定账号均未取得直链：%w", errors.Join(failures...))
}

// The caller holds casGate across candidate attempts and pool configuration changes.
func (a *App) casDownloadAttempt(ctx context.Context, s Storage, claim streamClaim) (download Download, releaseResult func(), resultErr error) {
	stage := "存储服务准备"
	defer func() {
		if resultErr != nil {
			a.casFailureLog(s, claim.CAS.Name, stage, resultErr)
		}
	}()
	done := func() {}
	retention := casRetentionHours(claim.RetentionHours)
	for _, task := range a.store.snapshot().Tasks {
		if claim.TaskID != "" && task.ID == claim.TaskID && task.Kind == "cas" && (task.CASBindingID == s.ID || task.StorageID == s.ID) {
			retention = casRetentionHours(task.RetentionHours)
			break
		}
	}
	account, _, _ := mobileAccount(s)
	hash := claim.CAS.SHA256
	if nativeTianyi(s) {
		account, hash = s.Config["username"], claim.CAS.MD5
	}
	sum := sha256.Sum256([]byte(s.ID + "\x00" + account + "\x00" + claim.File + "\x00" + hash))
	key := hex.EncodeToString(sum[:])
	var entry CASTemporary
	feature := casFeatureKey(*claim.CAS)
	conflict := false
	for _, existing := range a.store.snapshot().CASTemporary {
		if existing.Key == key {
			conflict = true
		}
		matches := existing.Key == key
		if claim.Storage == "" {
			matches = existing.Feature == feature && existing.StorageID == s.ID
		}
		if matches && existing.Ready && time.Now().Before(existing.expiresAt()) {
			entry = existing
			key = existing.Key
			break
		}
	}
	reused := entry.FileID != ""
	if !reused && nativeMobile(s) {
		stage = "会员单文件额度预检"
		if err := a.checkMobileCASSize(ctx, s, claim.CAS.Size); err != nil {
			a.casLog("warn", s, claim.CAS.Name, "会员额度预检跳过："+err.Error()+"（未提交秒传）")
			return Download{}, done, err
		}
	}
	if !reused && nativeTianyi(s) {
		stage = "会员单文件额度预检"
		if err := a.checkTianyiCASSize(ctx, s, claim.CAS.Size); err != nil {
			a.casLog("warn", s, claim.CAS.Name, "会员额度预检跳过："+err.Error())
			return Download{}, done, err
		}
	}
	attemptKey := rapid115Stamp([]string{"mobile-playback", account, feature})
	if !reused && nativeMobile(s) {
		stage = "待确认秒传提交检查"
		if a.store.snapshotWithLogLimit(0).CASPlaybackAttempts[attemptKey] {
			return Download{}, done, errors.New("此移动账号存在未确认的CAS秒传提交，请核对临时目录；未重复提交")
		}
	}
	host, err := a.casHost(ctx, s)
	if err != nil {
		return Download{}, done, err
	}
	if !reused {
		rapidSuccess := false
		defer func() {
			if saveErr := a.store.update(func(st *State) error {
				if s.Type == "mobile" {
					st.CASStats.Mobile++
				} else {
					st.CASStats.Tianyi++
				}
				if rapidSuccess {
					st.CASStats.Success++
				} else {
					st.CASStats.Failed++
				}
				return nil
			}); saveErr != nil {
				a.store.event("error", "tasks", "CAS 秒传统计保存失败")
			}
		}()
		if conflict {
			key += ":" + id()
		}
		if len(a.store.snapshot().CASTemporary) >= 1000 {
			stage = "临时文件容量检查"
			return Download{}, done, errors.New("CAS 临时文件达到 1000 项，请先清理过期项")
		}
		stage = "临时目录准备"
		folder, err := a.casFolder(ctx, s, host)
		if err != nil {
			return Download{}, done, err
		}
		var data struct {
			ID    string          `json:"fileId"`
			Exist bool            `json:"exist"`
			Rapid bool            `json:"rapidUpload"`
			Parts json.RawMessage `json:"partInfos"`
		}
		name := "AETHER_" + id()[:12] + "_" + claim.CAS.Name
		stage = "秒传提交"
		if nativeTianyi(s) {
			data.ID, err = a.tianyiRestore(ctx, s, folder, name, *claim.CAS)
			data.Rapid = err == nil
		} else {
			if err := a.store.update(func(st *State) error {
				if st.CASPlaybackAttempts == nil {
					st.CASPlaybackAttempts = map[string]bool{}
				}
				if len(st.CASPlaybackAttempts) >= 100000 {
					return errors.New("CAS播放意图已达上限，请先核对未确认提交")
				}
				st.CASPlaybackAttempts[attemptKey] = true
				return ctx.Err()
			}); err != nil {
				return Download{}, done, err
			}
			err = a.mobilePost(ctx, s, host+"/file/create", map[string]any{
				"parentFileId": folder, "name": name, "type": "file", "fileRenameMode": "auto_rename",
				"contentHash": claim.CAS.SHA256, "contentHashAlgorithm": "SHA256",
				"contentType": "application/octet-stream", "parallelUpload": false,
				"size": claim.CAS.Size, "partInfos": casParts(claim.CAS.Size),
			}, true, &data)
		}
		if err != nil {
			return Download{}, done, err
		}
		if data.ID == "" {
			stage = "秒传结果校验（未返回文件 ID）"
			return Download{}, done, errors.New("CAS 秒传未返回文件 ID")
		}
		ready := data.Exist || data.Rapid
		if ready {
			a.casLog("info", s, claim.CAS.Name, "秒传成功")
		}
		recordKey := key
		if !ready {
			recordKey += ":failed:" + id()
		}
		entry = CASTemporary{Key: recordKey, Feature: casFeatureKey(*claim.CAS), StorageID: s.ID, FileID: data.ID, Name: name, Size: claim.CAS.Size, LastUsed: time.Now(), Ready: ready, RetentionHours: retention}
		// Persist every returned file ID, even failed uploads, so cleanup never needs a directory-wide delete.
		stage = "秒传临时记录保存"
		if err := a.store.update(func(st *State) error {
			st.CASTemporary = append(st.CASTemporary, entry)
			if nativeMobile(s) {
				delete(st.CASPlaybackAttempts, attemptKey)
			}
			return nil
		}); err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = a.removeCASTemporary(cleanup, s, host, data.ID)
			return Download{}, done, errors.New("CAS 临时文件记录持久化失败")
		}
		if !data.Exist && !data.Rapid {
			stage = "秒传结果校验（未命中秒传，不上传媒体）"
			return Download{}, done, errors.New("CAS 秒传未成功：源数据可能失效或账号权益不足，不会上传媒体内容")
		}
		rapidSuccess = true
	}
	stage = "直链获取"
	d, err := a.casLink(ctx, s, host, entry.FileID)
	if err == nil && d.URL == "" {
		err = errors.New("CAS 秒传未取得有效直链")
	}
	if err != nil {
		if a.casActive[key] == 0 {
			_ = a.store.update(func(st *State) error {
				for i := range st.CASTemporary {
					if st.CASTemporary[i].Key == key {
						st.CASTemporary[i].Key += ":stale:" + id()
						st.CASTemporary[i].Ready = false
					}
				}
				return nil
			})
		}
		return Download{}, done, err
	}
	stage = "秒传文件保活记录保存"
	if err := a.store.update(func(st *State) error {
		for i := range st.CASTemporary {
			if st.CASTemporary[i].Key == key {
				st.CASTemporary[i].LastUsed = time.Now()
				st.CASTemporary[i].RetentionHours = max(casRetentionHours(st.CASTemporary[i].RetentionHours), retention)
			}
		}
		return nil
	}); err != nil {
		return Download{}, done, err
	}
	a.casActive[key]++
	release := func() {
		_ = a.acquireCAS(context.Background())
		defer func() { <-a.casGate }()
		a.casActive[key]--
		if a.casActive[key] == 0 {
			delete(a.casActive, key)
		}
		_ = a.store.update(func(st *State) error {
			for i := range st.CASTemporary {
				if st.CASTemporary[i].Key == key {
					st.CASTemporary[i].LastUsed = time.Now()
				}
			}
			return nil
		})
	}
	d.storageID = s.ID
	if reused {
		a.casLog("info", s, claim.CAS.Name, "复用存活秒传文件，直链获取成功（未重新秒传）")
	} else {
		a.casLog("info", s, claim.CAS.Name, "直链获取成功")
	}
	return d, release, nil
}

func (a *App) cleanupCAS(ctx context.Context) (int, error) {
	return a.cleanupCASSelected(ctx, nil)
}

func (a *App) cleanupCASSelected(ctx context.Context, selected map[string]bool) (int, error) {
	if err := a.acquireCAS(ctx); err != nil {
		return 0, err
	}
	defer func() { <-a.casGate }()
	count := 0
	entries := a.store.snapshotWithLogLimit(0).CASTemporary
	if selected != nil {
		known := map[string]bool{}
		for _, entry := range entries {
			known[entry.Key] = true
		}
		for key := range selected {
			if !known[key] {
				return 0, errors.New("所选秒传文件已失效，请刷新")
			}
		}
	}
	var skipped error
	for _, entry := range entries {
		if selected != nil && !selected[entry.Key] {
			continue
		}
		if a.casActive[entry.Key] > 0 {
			if selected != nil {
				skipped = errors.New("正在播放的秒传文件已跳过")
			}
			continue
		}
		if selected == nil && time.Now().Before(entry.expiresAt()) {
			continue
		}
		s, err := a.store.storage(entry.StorageID)
		if err != nil {
			return count, err
		}
		host, err := a.casHost(ctx, s)
		if err != nil {
			return count, err
		}
		if err := a.removeCASTemporary(ctx, s, host, entry.FileID); err != nil {
			return count, err
		}
		if err := a.store.update(func(st *State) error {
			for i, item := range st.CASTemporary {
				if item.Key == entry.Key {
					st.CASTemporary = append(st.CASTemporary[:i], st.CASTemporary[i+1:]...)
					break
				}
			}
			return nil
		}); err != nil {
			return count, err
		}
		count++
	}
	return count, skipped
}

func (a *App) casHost(ctx context.Context, s Storage) (string, error) {
	if nativeTianyi(s) {
		return tianyiAPI, nil
	}
	return a.mobileHost(ctx, s)
}

func (a *App) casLink(ctx context.Context, s Storage, host, fid string) (Download, error) {
	if nativeTianyi(s) {
		return a.tianyiLink(ctx, s, fid)
	}
	return a.mobileLinkAt(ctx, s, host, fid)
}

func (a *App) casStatus(w http.ResponseWriter, r *http.Request) {
	if err := a.acquireCAS(r.Context()); err != nil {
		fail(w, 408, err)
		return
	}
	defer func() { <-a.casGate }()
	items := []map[string]any{}
	for _, item := range a.store.snapshot().CASTemporary {
		items = append(items, map[string]any{"key": item.Key, "name": item.Name, "size": item.Size, "storageId": item.StorageID, "lastUsed": item.LastUsed, "active": a.casActive[item.Key] > 0, "expiresAt": item.expiresAt(), "retentionHours": casRetentionHours(item.RetentionHours)})
	}
	jsonResponse(w, 200, map[string]any{"items": items, "stats": a.store.snapshotWithLogLimit(0).CASStats, "defaultRetentionHours": 12})
}

func (a *App) casCleanup(w http.ResponseWriter, r *http.Request) {
	var selected map[string]bool
	if r.ContentLength != 0 {
		var input struct {
			Keys []string `json:"keys"`
		}
		if !decode(w, r, &input) {
			return
		}
		if len(input.Keys) == 0 || len(input.Keys) > 1000 {
			fail(w, 400, errors.New("请选择1至1000个秒传文件"))
			return
		}
		selected = map[string]bool{}
		for _, key := range input.Keys {
			if key == "" || selected[key] {
				fail(w, 400, errors.New("秒传文件选择无效或重复"))
				return
			}
			selected[key] = true
		}
	}
	count, err := a.cleanupCASSelected(r.Context(), selected)
	if err != nil {
		if selected == nil {
			fail(w, 502, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"removed": count, "error": err.Error()})
		return
	}
	jsonResponse(w, 200, map[string]int{"removed": count})
}
