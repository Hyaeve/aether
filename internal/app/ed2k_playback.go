package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

type ED2KInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Hash    string `json:"hash"`
	Binding string `json:"binding"`
}

func (i ED2KInfo) URI() string {
	return fmt.Sprintf("ed2k://|file|%s|%d|%s|/", url.PathEscape(i.Name), i.Size, strings.ToLower(i.Hash))
}

func (a *App) validateED2KBinding(t Task, source Storage) (Storage, error) {
	s, err := a.store.storage(t.ED2KBindingID)
	if t.Kind != "ed2k" || source.ID != t.StorageID || (source.Type != "local" && source.Type != "115") || !source.Enabled || err != nil || !s.Enabled || s.Type != "115" {
		return Storage{}, errors.New("ED2K 需要本地或 115 源和已启用的 115 绑定存储")
	}
	return s, nil
}

func (a *App) ed2kBindingStamp(t Task, source, binding Storage) string {
	// Runtime health/progress must not invalidate generated links; configuration must.
	b, _ := json.Marshal([]any{t.ID, t.StorageID, t.ED2KBindingID, t.Source, t.Target, t.MediaExtensions, t.MetadataExtensions, t.RetainedExtensions, t.ExcludeDirs, t.ExcludeFiles, t.ExcludeTypes,
		source.Type, source.Config, binding.Type, binding.Config})
	m := hmac.New(sha256.New, []byte(a.store.snapshotWithLogLimit(0).SignKey))
	m.Write(b)
	return hex.EncodeToString(m.Sum(nil))
}

func (a *App) checkED2KClaim(s Storage, claim streamClaim) error {
	i := claim.ED2K
	if i == nil || claim.TaskID == "" || claim.Storage != s.ID || !safeName(i.Name) || strings.ContainsAny(i.Name, "\r\n|") || i.Size <= 0 || i.Size > 1<<40 {
		return errors.New("ED2K 播放声明无效")
	}
	hash, err := hex.DecodeString(i.Hash)
	if err != nil || len(hash) != 16 {
		return errors.New("ED2K 哈希无效")
	}
	for _, t := range a.store.snapshotWithLogLimit(0).Tasks {
		if t.ID != claim.TaskID {
			continue
		}
		source, err := a.store.storage(t.StorageID)
		if err != nil || !t.Enabled {
			break
		}
		binding, err := a.validateED2KBinding(t, source)
		if err != nil || binding.ID != s.ID || i.Binding != a.ed2kBindingStamp(t, source, binding) || i.Binding != a.ed2kBindingStamp(t, source, s) {
			break
		}
		return nil
	}
	return errors.New("ED2K 任务已解绑、停用或配置改变，请重新生成")
}

// Fixed stripes bound memory and serialize the same task even across App instances.
var ed2kGates = func() [64]chan struct{} {
	var gates [64]chan struct{}
	for i := range gates {
		gates[i] = make(chan struct{}, 1)
	}
	return gates
}()
var ed2kFolderGate = make(chan struct{}, 1)

type ed2kRecord struct {
	Folder    string `json:"folder"`
	Hash      string `json:"hash,omitempty"`
	Attempted bool   `json:"attempted"`
	FileID    string `json:"fileId,omitempty"`
}

func (a *App) ed2kRecordPath(key string) string {
	return filepath.Join(a.dataDir, "ed2k-playback", key+".enc")
}

func (a *App) readED2KRecord(key string) (ed2kRecord, error) {
	var record ed2kRecord
	b, err := os.ReadFile(a.ed2kRecordPath(key))
	if err != nil {
		return record, err
	}
	n := a.store.aead.NonceSize()
	if len(b) < n || len(b) > 64<<10 {
		return record, errors.New("ED2K 提交记录损坏")
	}
	b, err = a.store.aead.Open(nil, b[:n], b[n:], []byte("ed2k:"+key))
	if err == nil {
		err = json.Unmarshal(b, &record)
	}
	if err == nil && (!record.Attempted || record.Folder == "") {
		err = errors.New("ED2K 提交记录无效")
	}
	return record, err
}

func (a *App) writeED2KRecord(key string, record ed2kRecord) error {
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	b = a.store.aead.Seal(nonce, nonce, b, []byte("ed2k:"+key))
	if err = os.MkdirAll(filepath.Dir(a.ed2kRecordPath(key)), 0700); err != nil {
		return err
	}
	return atomicWrite(a.ed2kRecordPath(key), b)
}

type ed2kPlaybackOps struct {
	folder   func(context.Context) (string, error)
	submit   func(context.Context, string, string) (string, error)
	status   func(context.Context, ed2kRecord, ED2KInfo) (*driver.OfflineTask, error)
	files    func(context.Context, string) ([]File, error)
	download func(context.Context, File) (Download, error)
	interval time.Duration
}

// ed2kDownload returns only a verified completed file. No cleanup deletes user data.
func (a *App) ed2kDownload(ctx context.Context, s Storage, claim streamClaim) (Download, error) {
	return a.ed2kDownloadWithUA(ctx, s, claim, pan115UA)
}

// HTTP playback callers must pass their request UA for 115's UA-bound URLs.
func (a *App) ed2kDownloadWithUA(ctx context.Context, s Storage, claim streamClaim, userAgent string) (Download, error) {
	ops := ed2kPlaybackOps{
		folder: func(ctx context.Context) (string, error) {
			select {
			case ed2kFolderGate <- struct{}{}:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			defer func() { <-ed2kFolderGate }()
			// ID 0 is the account root, independent of the configured browsing root.
			folder, _, err := a.uploadParent(ctx, s, "0", "Aether/ed2k/.placeholder")
			return folder, err
		},
		submit: func(ctx context.Context, folder, uri string) (string, error) {
			c, err := client115(ctx, s)
			if err != nil {
				return "", err
			}
			hashes, err := c.AddOfflineTaskURIs([]string{uri}, folder)
			if err != nil {
				return "", err
			}
			if len(hashes) != 1 || hashes[0] == "" {
				return "", errors.New("115 未确认离线任务")
			}
			return hashes[0], nil
		},
		status: func(ctx context.Context, record ed2kRecord, info ED2KInfo) (*driver.OfflineTask, error) {
			c, err := client115(ctx, s)
			if err != nil {
				return nil, err
			}
			for page := int64(1); page <= 20; page++ {
				if err := a.waitAPI(ctx, s.ID); err != nil {
					return nil, err
				}
				result, err := c.ListOfflineTask(page)
				if err != nil {
					return nil, err
				}
				for _, task := range result.Tasks {
					if task != nil && task.DirId == record.Folder && task.Url == info.URI() && (record.Hash == "" || task.InfoHash == record.Hash) {
						return task, nil
					}
				}
				if page >= result.PageCount {
					return nil, nil
				}
			}
			return nil, errors.New("115 离线任务列表超过查询上限，不会重复提交")
		},
		files: func(ctx context.Context, folder string) ([]File, error) { return a.list115(ctx, s, folder) },
		download: func(ctx context.Context, f File) (Download, error) {
			return a.downloadWithUA(ctx, s, f.ID, f.PickCode, userAgent)
		},
		interval: 2 * time.Second,
	}
	return a.ed2kDownloadWith(ctx, s, claim, ops)
}

func (a *App) ed2kDownloadWith(ctx context.Context, s Storage, claim streamClaim, ops ed2kPlaybackOps) (Download, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	fail := func(err error) (Download, error) { return Download{}, err }
	if err := a.checkED2KClaim(s, claim); err != nil {
		return fail(err)
	}
	stripe := sha256.Sum256([]byte(a.dataDir + "\x00" + claim.TaskID))
	gate := ed2kGates[int(stripe[0])%len(ed2kGates)]
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return fail(ctx.Err())
	}
	defer func() { <-gate }()
	if err := a.checkED2KClaim(s, claim); err != nil {
		return fail(err)
	}
	info := *claim.ED2K
	b, _ := json.Marshal([]any{claim.TaskID, info})
	sum := sha256.Sum256(b)
	key := hex.EncodeToString(sum[:])
	record, err := a.readED2KRecord(key)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if os.IsNotExist(err) {
		folder, err := ops.folder(ctx)
		if err != nil {
			return fail(err)
		}
		if folder == "" {
			return fail(errors.New("115 专用目录无效"))
		}
		if err = a.checkED2KClaim(s, claim); err != nil {
			return fail(err)
		}
		record = ed2kRecord{Folder: folder, Attempted: true}
		// Write-ahead intent: even a crash or ambiguous response must never replay a POST.
		if err = a.writeED2KRecord(key, record); err != nil {
			return fail(err)
		}
		record.Hash, err = ops.submit(ctx, folder, info.URI())
		if err != nil {
			return fail(errors.New("115 离线提交结果未确认；已保留记录，后续仅查询，不自动重试提交"))
		}
		if err = a.writeED2KRecord(key, record); err != nil {
			return fail(err)
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return fail(fmt.Errorf("ED2K 尚未确认完成（不会重复提交）: %w", err))
		}
		if err := a.checkED2KClaim(s, claim); err != nil {
			return fail(err)
		}
		if record.FileID == "" {
			task, err := ops.status(ctx, record, info)
			if err != nil {
				return fail(err)
			}
			if task != nil {
				if task.IsFailed() {
					return fail(errors.New("115 离线任务失败，不会自动重复提交"))
				}
				if task.IsDone() && task.FileId != "" && task.Size == info.Size && task.DirId == record.Folder && task.Url == info.URI() && (record.Hash == "" || task.InfoHash == record.Hash) {
					record.FileID = task.FileId
				}
			}
		}
		if record.FileID != "" {
			files, err := ops.files(ctx, record.Folder)
			if err != nil {
				return fail(err)
			}
			for _, f := range files {
				if !f.IsDir && f.ID == record.FileID && f.Size == info.Size && f.Name == info.Name && f.PickCode != "" {
					if err := a.checkED2KClaim(s, claim); err != nil {
						return fail(err)
					}
					if err := a.writeED2KRecord(key, record); err != nil {
						return fail(err)
					}
					d, err := ops.download(ctx, f)
					if err != nil {
						return fail(err)
					}
					if err := a.checkED2KClaim(s, claim); err != nil {
						return fail(err)
					}
					return d, nil
				}
			}
		}
		timer := time.NewTimer(ops.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail(fmt.Errorf("ED2K 等待完成超时（不会重复提交）: %w", ctx.Err()))
		case <-timer.C:
		}
	}
}
