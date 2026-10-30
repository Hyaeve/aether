package app

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	ec "github.com/SheltonZhu/115driver/pkg/crypto/ec115"
	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

func (a *App) rename115Current(s Storage) bool {
	current, err := a.store.storage(s.ID)
	return err == nil && current.Enabled && storageRevision(current) == storageRevision(s)
}

func rename115Failure(ctx context.Context, stage string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s：%w", stage, ctx.Err())
	}
	return fmt.Errorf("%s：%s", stage, safeStorageError(err))
}

func (a *App) rename115Extension(ctx context.Context, s Storage, parent string, selected File, name string) (File, error) {
	c, err := client115(ctx, s)
	if err != nil {
		return File{}, err
	}
	return a.rename115Recreate(ctx, s, parent, selected, name, c, func(original *driver.File) (string, bool, error) {
		return a.submit115Rename(ctx, s, c, original, name)
	})
}

// Upload submission is separate from read-back and recycling, so no successful
// API envelope alone can authorize removal of the original.
func (a *App) rename115Recreate(ctx context.Context, s Storage, parent string, selected File, name string, c *driver.Pan115Client, submit func(*driver.File) (string, bool, error)) (out File, err error) {
	if parent == "" || parent == "/" {
		parent = rootOf(s)
	}
	if selected.IsDir || !safeName(name) || !a.rename115Current(s) {
		return out, errors.New("115改后缀的文件或存储配置无效")
	}
	defer func() {
		if err != nil {
			a.store.event("error", "files", fmt.Sprintf("115改后缀：存储池 %q，文件 %q → %q：%s", s.Name, selected.Name, name, err))
		}
	}()
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return out, err
	}
	original, err := c.GetFile(selected.ID)
	if err != nil {
		return out, rename115Failure(ctx, "读取115源文件失败，未提交改后缀", err)
	}
	if original.FileID != selected.ID || original.ParentID != parent || original.IsDirectory || original.Name != selected.Name ||
		original.Size != selected.Size || original.Size < 0 || !simulcastSHA1(original.Sha1) || original.PickCode == "" ||
		!selected.Modified.IsZero() && !original.UpdateTime.Equal(selected.Modified) ||
		selected.PickCode != "" && original.PickCode != selected.PickCode {
		return out, errors.New("115源文件身份已变化或缺少SHA1，未提交改后缀")
	}
	before, err := a.simulcast.list(ctx, s, c, parent)
	if err != nil {
		return out, rename115Failure(ctx, "核验115源目录失败，未提交改后缀", err)
	}
	previous := map[string]bool{}
	sourceFound := false
	for _, f := range before {
		previous[f.FileID] = true
		if strings.EqualFold(f.Name, name) {
			return out, errors.New("目标名称已存在，不覆盖已有文件")
		}
		if same115RenameFile(original, f) {
			sourceFound = true
		}
	}
	if !sourceFound {
		return out, errors.New("115源目录已变化，未提交改后缀")
	}
	account, err := account115(s)
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256([]byte(account + "\x00" + parent + "\x00" + original.FileID + "\x00" + original.Name + "\x00" + strings.ToUpper(original.Sha1)))
	key := hex.EncodeToString(sum[:])
	if err = a.store.update(func(st *State) error {
		if st.Rename115Attempts[key] {
			return errors.New("此文件上次改后缀结果未确认，未重复提交；请刷新并核对云盘文件")
		}
		valid := false
		for _, current := range st.Storages {
			if current.ID == s.ID && current.Enabled && storageRevision(current) == storageRevision(s) {
				valid = true
			}
		}
		if !valid {
			return errors.New("存储配置已变化，未提交改后缀")
		}
		if len(st.Rename115Attempts) >= 10000 {
			return errors.New("115未确认改后缀记录已达保护上限")
		}
		if st.Rename115Attempts == nil {
			st.Rename115Attempts = map[string]bool{}
		}
		st.Rename115Attempts[key] = true
		return nil
	}); err != nil {
		return out, err
	}
	clearIntent := false
	defer func() {
		a.cache.clear()
		a.invalidateTransferPaths(s)
		if clearIntent {
			if saveErr := a.store.update(func(st *State) error { delete(st.Rename115Attempts, key); return nil }); saveErr != nil && err == nil {
				err = errors.New("115改后缀已完成，但回执保存失败，请刷新核对；未自动重试")
			}
		}
	}()
	a.store.event("info", "files", fmt.Sprintf("115改后缀：存储池 %q，文件 %q → %q，提交秒传；新文件核验前保留原件", s.Name, selected.Name, name))
	if err = ctx.Err(); err != nil || !a.rename115Current(s) {
		clearIntent = true
		return out, errors.New("请求或存储配置已变化，未提交改后缀")
	}
	pick, noWrite, err := submit(original)
	if err != nil {
		clearIntent = noWrite
		return out, err
	}
	if pick == "" || pick == original.PickCode {
		return out, errors.New("115秒传未返回独立的新文件凭据，已保留原文件；未自动重试")
	}
	var copied *driver.File
	for attempt := 0; attempt < 5; attempt++ {
		files, listErr := a.simulcast.list(ctx, s, c, parent)
		if listErr != nil {
			return out, rename115Failure(ctx, "115秒传结果无法核验，已保留原件；请刷新核对", listErr)
		}
		matches := 0
		for _, f := range files {
			if strings.EqualFold(f.Name, name) {
				matches++
				if f.Name == name && !previous[f.FileID] && f.ParentID == parent && f.PickCode == pick && simulcastSame(original, f) {
					copied = f
				}
			}
		}
		if matches > 1 || matches == 1 && copied == nil {
			return out, errors.New("115新文件重名或身份不符，已保留原件和云端结果，请刷新核对")
		}
		if copied != nil {
			break
		}
		if attempt < 4 {
			select {
			case <-ctx.Done():
				return out, ctx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	if copied == nil {
		return out, errors.New("115新文件尚未可见，已保留原件；请刷新核对，未自动重试")
	}
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return out, err
	}
	confirmed, err := c.GetFile(copied.FileID)
	if err != nil || !same115RenameFile(copied, confirmed) {
		return out, errors.New("115新文件校验失败，已保留原文件")
	}
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return out, err
	}
	current, err := c.GetFile(original.FileID)
	if err != nil || !same115RenameFile(original, current) || !a.rename115Current(s) || ctx.Err() != nil {
		return out, errors.New("115原文件或存储配置已变化，未回收原文件；请刷新核对")
	}
	a.store.event("info", "files", fmt.Sprintf("115改后缀：存储池 %q，新文件 %q校验通过，开始将原文件 %q移入回收站", s.Name, name, selected.Name))
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return out, err
	}
	if !a.rename115Current(s) {
		return out, errors.New("115存储配置已变化，未回收原文件")
	}
	// Delete is the existing recycle-bin API, irrespective of permanent-delete settings.
	if err = c.Delete(original.FileID); err != nil {
		return out, rename115Failure(ctx, "115新文件已生成，原文件回收结果未确认；请刷新核对，未自动重试", err)
	}
	after, err := a.simulcast.list(ctx, s, c, parent)
	if err != nil {
		return out, errors.New("115回收结果未能读回，请刷新核对；未自动重试")
	}
	found := false
	for _, f := range after {
		if f.FileID == original.FileID {
			return out, errors.New("115原文件仍在源目录，请刷新核对；未自动重复回收")
		}
		if same115RenameFile(confirmed, f) {
			found = true
		}
	}
	if !found {
		return out, errors.New("115新文件在回收后发生变化，请刷新核对")
	}
	clearIntent = true
	a.store.event("info", "files", fmt.Sprintf("115改后缀完成：存储池 %q，%q → %q，原文件已移入回收站", s.Name, selected.Name, name))
	return File{ID: confirmed.FileID, Name: confirmed.Name, Size: confirmed.Size, PickCode: confirmed.PickCode, Modified: confirmed.UpdateTime, Created: confirmed.CreateTime}, nil
}

func same115RenameFile(a, b *driver.File) bool {
	return a != nil && b != nil && a.FileID == b.FileID && a.ParentID == b.ParentID && a.Name == b.Name &&
		a.PickCode == b.PickCode && a.UpdateTime.Equal(b.UpdateTime) && simulcastSame(a, b)
}

// Match the pinned 115driver v1.3.5 upload protocol, but bound responses and
// challenges instead of the SDK's unbounded status-7 loop. Never fall back to OSS.
func (a *App) submit115Rename(ctx context.Context, s Storage, c *driver.Pan115Client, original *driver.File, name string) (string, bool, error) {
	account, err := account115(s)
	if err != nil {
		return "", true, err
	}
	if _, err := c.UploadAvailable(); err != nil {
		return "", true, rename115Failure(ctx, "读取115秒传权限失败，已保留原件", err)
	}
	if strconv.FormatInt(c.UserID, 10) != account || c.Userkey == "" {
		return "", true, errors.New("115秒传账号身份不一致，已保留原件")
	}
	if c.UploadMetaInfo == nil || !c.UploadMetaInfo.UploadAllowed || c.UploadMetaInfo.SizeLimit < original.Size {
		return "", true, errors.New("115当前账号不允许此文件秒传，已保留原件")
	}
	cipher, err := ec.NewEcdhCipher()
	if err != nil {
		return "", true, errors.New("115秒传请求初始化失败")
	}
	size := strconv.FormatInt(original.Size, 10)
	digest := strings.ToUpper(original.Sha1)
	target := "U_1_" + original.ParentID
	form := url.Values{
		"appid": {"0"}, "appversion": {"27.0.5.7"}, "userid": {strconv.FormatInt(c.UserID, 10)},
		"filename": {name}, "filesize": {size}, "fileid": {digest}, "target": {target},
		"sig": {c.GenerateSignature(digest, target)}, "topupload": {"true"},
	}
	exchange := func(signKey, signValue string) (*driver.UploadInitResp, bool, error) {
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return nil, true, err
		}
		if !a.rename115Current(s) {
			return nil, true, errors.New("115存储配置已变化，未提交秒传")
		}
		now := driver.NowMilli()
		token, err := cipher.EncodeToken(now.ToInt64())
		if err != nil {
			return nil, true, errors.New("115秒传签名初始化失败")
		}
		form.Set("t", now.String())
		form.Set("token", c.GenerateToken(digest, "", now.String(), size, signKey, signValue))
		if signKey != "" {
			form.Set("sign_key", signKey)
			form.Set("sign_val", signValue)
		}
		body, err := cipher.Encrypt([]byte(form.Encode()))
		if err != nil {
			return nil, true, errors.New("115秒传加密失败")
		}
		res, err := c.NewRequest().SetQueryParam("k_ec", token).SetBody(body).
			SetHeader("Content-Type", "application/x-www-form-urlencoded").SetDoNotParseResponse(true).Post(driver.ApiUploadInit)
		if err != nil {
			return nil, false, rename115Failure(ctx, "115秒传提交结果未确认，已保留原件；未自动重试", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.RawBody(), (64<<10)+1))
		res.RawBody().Close()
		if readErr != nil || res.StatusCode() != http.StatusOK || len(raw) > 64<<10 {
			return nil, false, errors.New("115秒传响应异常，已保留原件；未自动重试")
		}
		plain, err := cipher.Decrypt(raw)
		var result driver.UploadInitResp
		if err != nil || json.Unmarshal(plain, &result) != nil || result.ErrorCode != 0 && result.ErrorCode != 701 {
			return nil, false, errors.New("115秒传结果无法确认，已保留原件；未自动重试")
		}
		return &result, false, nil
	}
	return complete115RenameUpload(exchange, func(spec string) (string, error) { return a.rename115RangeSHA1(ctx, s, original, spec) })
}

func complete115RenameUpload(exchange func(string, string) (*driver.UploadInitResp, bool, error), checksum func(string) (string, error)) (string, bool, error) {
	signKey, signValue := "", ""
	for round := 0; round < 2; round++ {
		result, noWrite, err := exchange(signKey, signValue)
		if err != nil {
			return "", noWrite, err
		}
		if result == nil {
			return "", false, errors.New("115秒传响应为空，已保留原件；未自动重试")
		}
		switch result.Status {
		case 2:
			return result.PickCode, false, nil
		case 1:
			return "", true, errors.New("115未命中秒传，无法即时改后缀，已保留原件；不会下载整文件或转普通上传")
		case 7:
			if round != 0 || result.SignKey == "" || len(result.SignKey) > 1024 {
				return "", true, errors.New("115秒传分片校验异常，已保留原件")
			}
			signValue, err = checksum(result.SignCheck)
			if err != nil {
				return "", true, err
			}
			signKey = result.SignKey
		default:
			return "", false, errors.New("115秒传状态未知，已保留原件；未自动重试")
		}
	}
	return "", false, errors.New("115秒传未确认完成，已保留原件")
}

func (a *App) rename115RangeSHA1(ctx context.Context, s Storage, f *driver.File, spec string) (string, error) {
	parts := strings.Split(spec, "-")
	if len(parts) != 2 {
		return "", errors.New("115秒传校验范围无效")
	}
	start, firstErr := strconv.ParseInt(parts[0], 10, 64)
	end, lastErr := strconv.ParseInt(parts[1], 10, 64)
	if firstErr != nil || lastErr != nil || start < 0 || end < start || end >= f.Size || end-start >= 1<<20 {
		return "", errors.New("115秒传校验范围无效或超过1MiB，已保留原件")
	}
	download, err := a.download(ctx, s, f.FileID, f.PickCode)
	if err != nil {
		return "", rename115Failure(ctx, "读取115校验分片凭据失败，已保留原件", err)
	}
	return read115RenameRange(ctx, download, f.Size, start, end)
}

func read115RenameRange(ctx context.Context, download Download, size, start, end int64) (string, error) {
	if start < 0 || end < start || end >= size || end-start >= 1<<20 {
		return "", errors.New("115秒传校验范围无效")
	}
	reader := &davFile{ctx: ctx, download: download, info: davFileInfo(ctx, File{Size: size}), offset: start, bounded: true, rangeEnd: end}
	defer reader.Close()
	h := sha1.New()
	if _, err := io.CopyN(h, reader, end-start+1); err != nil {
		return "", rename115Failure(ctx, "读取115校验分片失败，已保留原件", err)
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}
