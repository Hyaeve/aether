package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const shareLiveSeenLimit = 200000

type shareLiveCheckpoint struct {
	Scope   string    `json:"scope"`
	Through time.Time `json:"through"`
	Seen    []string  `json:"seen"`
}

func shareLiveCheckpointScope(link shareLiveLink) string {
	return archiveStamp(link.StorageID, link.Provider, shareRecordURL(link.Link))
}
func shareLiveFileKey(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
func shareLiveCursorValid(cursor string) bool {
	_, err := hex.DecodeString(cursor)
	return len(cursor) == 64 && err == nil
}
func (a *App) shareLiveCheckpointPath(link shareLiveLink, cursor string) string {
	return filepath.Join(a.dataDir, "runtime", "share-live-seen", archiveStamp(link.ID), cursor+".json")
}
func (a *App) loadShareLiveCheckpoint(link shareLiveLink) (map[string]bool, error) {
	seen := map[string]bool{}
	if link.IncrementalCursor == "" {
		return seen, nil
	}
	if !shareLiveCursorValid(link.IncrementalCursor) {
		return nil, errors.New("活链新增记录无效，已停止以防重复转存")
	}
	f, err := os.Open(a.shareLiveCheckpointPath(link, link.IncrementalCursor))
	if err != nil {
		return nil, errors.New("活链新增记录无法读取，已停止以防重复转存")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	sum := sha256.Sum256(raw)
	var saved shareLiveCheckpoint
	if err != nil || len(raw) > 32<<20 || hex.EncodeToString(sum[:]) != link.IncrementalCursor || a.store.decodeJSONConfig("runtime/share-live-seen/"+archiveStamp(link.ID), raw, &saved) != nil || saved.Scope != shareLiveCheckpointScope(link) || saved.Through.IsZero() || len(saved.Seen) > shareLiveSeenLimit {
		return nil, errors.New("活链新增记录完整性或来源校验失败，未执行转存")
	}
	for _, key := range saved.Seen {
		if !shareLiveCursorValid(key) || seen[key] {
			return nil, errors.New("活链新增记录损坏，未执行转存")
		}
		seen[key] = true
	}
	return seen, nil
}
func shareLiveObserved(batches []shareBatch, seen map[string]bool) error {
	for _, batch := range batches {
		for _, item := range batch.Items {
			if item.IsDir {
				continue
			}
			if item.ID == "" {
				return errors.New("分享文件缺少身份，未执行新增转存")
			}
			seen[shareLiveFileKey(item.ID)] = true
			if len(seen) > shareLiveSeenLimit {
				return errors.New("活链新增记录超过200000项，已停止；不会丢弃历史并重新转存")
			}
		}
	}
	return nil
}

// Immutable, authenticated generations are written before any cloud mutation.
// Only the runtime commit publishes the new cursor; an interrupted run retains
// the old generation and its write intent rather than silently rebasing.
func (a *App) stageShareLiveCheckpoint(link shareLiveLink, seen map[string]bool, at time.Time) (string, error) {
	if at.IsZero() {
		at = time.Now()
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > shareLiveSeenLimit {
		return "", errors.New("活链新增记录超过200000项")
	}
	raw, err := a.store.encodeJSONConfig("runtime/share-live-seen/"+archiveStamp(link.ID), shareLiveCheckpoint{Scope: shareLiveCheckpointScope(link), Through: at, Seen: keys})
	if err != nil {
		return "", err
	}
	if len(raw) > 32<<20 {
		return "", errors.New("活链新增记录超过32MiB")
	}
	sum := sha256.Sum256(raw)
	cursor := hex.EncodeToString(sum[:])
	file := a.shareLiveCheckpointPath(link, cursor)
	if err = os.MkdirAll(filepath.Dir(file), 0700); err == nil {
		err = durableConfigWrite(file, raw)
	}
	return cursor, err
}
func (a *App) removeShareLiveCheckpoint(link shareLiveLink, cursor string) {
	if cursor != "" && cursor != link.IncrementalCursor && shareLiveCursorValid(cursor) {
		_ = os.Remove(a.shareLiveCheckpointPath(link, cursor))
	}
}
func (a *App) planShareLiveIncremental(link shareLiveLink, batches []shareBatch, at time.Time) (filtered []shareBatch, cursor string, skipped int, baseline bool, err error) {
	seen, err := a.loadShareLiveCheckpoint(link)
	if err != nil {
		return nil, "", 0, false, err
	}
	priorCount := len(seen)
	baseline = link.IncrementalCursor == "" && link.DirectoryID != "" && !link.LastRun.IsZero()
	for _, batch := range batches {
		items := []shareEntry{}
		for _, item := range batch.Items {
			if !item.IsDir && (baseline || seen[shareLiveFileKey(item.ID)]) {
				skipped++
				continue
			}
			items = append(items, item)
		}
		if !baseline && (len(items) > 0 || (link.IncrementalCursor == "" && len(batch.Items) == 0)) {
			batch.Items = items
			filtered = append(filtered, batch)
		}
	}
	if err = shareLiveObserved(batches, seen); err != nil {
		return nil, "", 0, baseline, err
	}
	if link.IncrementalCursor != "" && len(seen) == priorCount {
		return filtered, link.IncrementalCursor, skipped, baseline, nil
	}
	cursor, err = a.stageShareLiveCheckpoint(link, seen, at)
	return
}

func (a *App) seedShareLiveCheckpoint(link *shareLiveLink, p sharePreview, at time.Time) {
	// A receipt without a complete initial plan cannot establish a trustworthy
	// old-file set. Its first live run will establish a read-only baseline.
	if p.Batches == nil || p.Destination != link.DirectoryID {
		return
	}
	seen, err := a.loadShareLiveCheckpoint(*link)
	if err != nil {
		return
	}
	if shareLiveObserved(p.Batches, seen) != nil {
		return
	}
	if cursor, err := a.stageShareLiveCheckpoint(*link, seen, at); err == nil {
		link.IncrementalCursor, link.LastSuccess = cursor, at
	}
}
