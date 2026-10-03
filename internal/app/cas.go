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
	"path"
	"strings"
	"time"
)

const casIdleTTL = 2 * time.Hour
const casLimit = 1 << 20

type CASInfo struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	MD5      string `json:"md5,omitempty"`
}

type CASTemporary struct {
	Key       string    `json:"key"`
	StorageID string    `json:"storageId"`
	FileID    string    `json:"fileId"`
	Name      string    `json:"name"`
	LastUsed  time.Time `json:"lastUsed"`
	Ready     bool      `json:"ready"`
	Trashed   bool      `json:"trashed,omitempty"`
}

func validateCAS(info CASInfo) error {
	if !safeName(info.Name) || !isVideo(info.Name) || info.Size <= 0 || info.Size > 1<<40 {
		return errors.New("CAS 原始文件名、大小或视频类型无效（最大 1 TiB）")
	}
	hash, err := hex.DecodeString(info.SHA256)
	md5, md5Err := hex.DecodeString(info.MD5)
	if (err != nil || len(hash) != 32) && (md5Err != nil || len(md5) != 16) {
		return errors.New("CAS 需要有效的 64 位 SHA256 或 32 位 MD5")
	}
	return nil
}

func validateCASFor(s Storage, info CASInfo) error {
	if err := validateCAS(info); err != nil {
		return err
	}
	value, size := info.SHA256, 32
	if nativeTianyi(s) {
		value, size = info.MD5, 16
	}
	hash, err := hex.DecodeString(value)
	if err != nil || len(hash) != size {
		return errors.New("CAS 特征与存储不兼容：移动需 SHA256，天翼需 MD5")
	}
	return nil
}

func decodeCAS(content []byte, filename string) (CASInfo, error) {
	var info CASInfo
	if len(content) > casLimit || !strings.EqualFold(path.Ext(filename), ".cas") {
		return info, errors.New("CAS 文件过大或后缀无效")
	}
	content = []byte(strings.TrimSpace(strings.TrimPrefix(string(content), "\ufeff")))
	raw, err := base64.StdEncoding.DecodeString(string(content))
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(string(content))
	}
	if err != nil {
		return info, errors.New("CAS 内容不是合法 Base64")
	}
	// Some CAS writers serialize the byte count as a decimal string.
	var data struct {
		Provider string          `json:"provider"`
		Name     string          `json:"name"`
		Size     json.RawMessage `json:"size"`
		SHA256   string          `json:"sha256"`
		MD5      string          `json:"md5"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return info, errors.New("CAS 内容不是合法 JSON")
	}
	var size json.Number
	if json.Unmarshal(data.Size, &size) != nil {
		return info, errors.New("CAS 大小无效")
	}
	n, err := size.Int64()
	if err != nil {
		return info, errors.New("CAS 大小无效")
	}
	if !safeName(data.Name) || !safeName(filename) {
		return info, errors.New("CAS 文件名包含非法路径")
	}
	name := strings.TrimSuffix(filename, path.Ext(filename))
	if strings.TrimSpace(name) == "" {
		return info, errors.New("CAS 文件名不能为空")
	}
	if !isVideo(name) {
		name += path.Ext(data.Name)
	}
	info = CASInfo{Provider: strings.ToLower(data.Provider), Name: name, Size: n, SHA256: strings.ToLower(data.SHA256), MD5: strings.ToLower(data.MD5)}
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
	request, err := http.NewRequestWithContext(ctx, "GET", d.URL, nil)
	if err != nil {
		return CASInfo{}, err
	}
	request.Header = d.Headers.Clone()
	response, err := apiClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return CASInfo{}, ctx.Err()
		}
		return CASInfo{}, errors.New("CAS 特征文件读取失败")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return CASInfo{}, fmt.Errorf("CAS 特征文件 HTTP %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, casLimit+1))
	if err != nil {
		return CASInfo{}, err
	}
	info, err := decodeCAS(content, f.Name)
	if err == nil {
		err = validateCASFor(s, info)
	}
	return info, err
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
	name := "Aether_CAS_TEMP_" + s.ID
	files, err := a.mobileListAt(ctx, s, host, "/")
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if f.IsDir && f.Name == name {
			return f.ID, nil
		}
	}
	var data struct {
		ID string `json:"fileId"`
	}
	err = a.mobilePost(ctx, s, host+"/file/create", map[string]any{"parentFileId": "/", "name": name, "description": "", "type": "folder", "fileRenameMode": "force_rename"}, false, &data)
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

func (a *App) casDownload(ctx context.Context, s Storage, claim streamClaim) (Download, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	done := func() {}
	if !casStorage(s) || claim.CAS == nil {
		return Download{}, done, errors.New("CAS 播放仅支持原生移动新版个人云或天翼个人云")
	}
	if err := validateCASFor(s, *claim.CAS); err != nil {
		return Download{}, done, err
	}
	if err := a.acquireCAS(ctx); err != nil {
		return Download{}, done, err
	}
	defer func() { <-a.casGate }()
	account, _, _ := mobileAccount(s)
	hash := claim.CAS.SHA256
	if nativeTianyi(s) {
		account, hash = s.Config["username"], claim.CAS.MD5
	}
	sum := sha256.Sum256([]byte(s.ID + "\x00" + account + "\x00" + claim.File + "\x00" + hash))
	key := hex.EncodeToString(sum[:])
	host, err := a.casHost(ctx, s)
	if err != nil {
		return Download{}, done, err
	}
	var entry CASTemporary
	for _, existing := range a.store.snapshot().CASTemporary {
		if existing.Key == key && existing.Ready {
			entry = existing
			break
		}
	}
	if entry.FileID == "" {
		if len(a.store.snapshot().CASTemporary) >= 1000 {
			return Download{}, done, errors.New("CAS 临时文件达到 1000 项，请先清理过期项")
		}
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
		if nativeTianyi(s) {
			data.ID, err = a.tianyiRestore(ctx, s, folder, name, *claim.CAS)
			data.Rapid = err == nil
		} else {
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
			return Download{}, done, errors.New("CAS 秒传未返回文件 ID")
		}
		ready := data.Exist || data.Rapid
		recordKey := key
		if !ready {
			recordKey += ":failed:" + id()
		}
		entry = CASTemporary{Key: recordKey, StorageID: s.ID, FileID: data.ID, Name: name, LastUsed: time.Now(), Ready: ready}
		// Persist every returned file ID, even failed uploads, so cleanup never needs a directory-wide delete.
		if err := a.store.update(func(st *State) error { st.CASTemporary = append(st.CASTemporary, entry); return nil }); err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = a.removeCASTemporary(cleanup, s, host, data.ID)
			return Download{}, done, errors.New("CAS 临时文件记录持久化失败")
		}
		if !data.Exist && !data.Rapid {
			return Download{}, done, errors.New("CAS 秒传未成功：源数据可能失效或账号权益不足，不会上传媒体内容")
		}
	}
	d, err := a.casLink(ctx, s, host, entry.FileID)
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
	if err := a.store.update(func(st *State) error {
		for i := range st.CASTemporary {
			if st.CASTemporary[i].Key == key {
				st.CASTemporary[i].LastUsed = time.Now()
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
	return d, release, nil
}

func (a *App) cleanupCAS(ctx context.Context) (int, error) {
	if err := a.acquireCAS(ctx); err != nil {
		return 0, err
	}
	defer func() { <-a.casGate }()
	count := 0
	for _, entry := range a.store.snapshot().CASTemporary {
		if time.Since(entry.LastUsed) < casIdleTTL || a.casActive[entry.Key] > 0 {
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
	return count, nil
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
		items = append(items, map[string]any{"name": item.Name, "storageId": item.StorageID, "lastUsed": item.LastUsed, "active": a.casActive[item.Key] > 0, "expiresAt": item.LastUsed.Add(casIdleTTL)})
	}
	jsonResponse(w, 200, map[string]any{"items": items, "idleMinutes": 120})
}

func (a *App) casCleanup(w http.ResponseWriter, r *http.Request) {
	count, err := a.cleanupCAS(r.Context())
	if err != nil {
		fail(w, 502, err)
		return
	}
	jsonResponse(w, 200, map[string]int{"removed": count})
}
