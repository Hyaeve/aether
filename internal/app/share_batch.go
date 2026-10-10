package app

import (
	"context"
	"errors"
	"net/http"
	"path"
	"time"
)

type shareBatch struct {
	Source    string
	Directory string
	Items     []shareEntry
}

func (a *App) planShare(ctx context.Context, s Storage, code, pass string, items []shareEntry) ([]shareBatch, error) {
	result := []shareBatch{}
	seen, count := map[string]bool{}, 0
	var walk func([]shareEntry, string, string, int) error
	walk = func(entries []shareEntry, directory, source string, depth int) error {
		if depth > 32 {
			return errors.New("分享目录层级超过32层")
		}
		count += len(entries)
		if count > 5000 {
			return errors.New("分享文件总数超过5000，请缩小分享范围")
		}
		limit := 100
		files := []shareEntry{}
		names := map[string]bool{}
		for _, item := range entries {
			if !safeName(item.Name) || names[item.Name] {
				return errors.New("分享含无效或重复名称")
			}
			if s.Type != "115" && !item.IsDir && item.Token == "" {
				return errors.New("分享文件缺少转存凭据，请重新解析")
			}
			names[item.Name] = true
			if item.IsDir {
				if seen[item.ID] {
					return errors.New("分享目录循环或重复")
				}
				seen[item.ID] = true
				childSource := item.ID
				if nativeMobile(s) && item.Token != "" {
					childSource = item.Token
				}
				_, children, err := a.readShareAt(ctx, s, code, pass, childSource)
				if err != nil {
					return err
				}
				if err := walk(children, path.Join(directory, item.Name), item.ID, depth+1); err != nil {
					return err
				}
			} else {
				files = append(files, item)
			}
		}
		if len(entries) == 0 && directory != "" {
			result = append(result, shareBatch{Directory: directory})
		}
		for start := 0; start < len(files); start += limit {
			result = append(result, shareBatch{Directory: directory, Source: source, Items: files[start:min(start+limit, len(files))]})
		}
		return nil
	}
	err := walk(items, "", "root", 0)
	return result, err
}

func (a *App) saveShareBatch(w http.ResponseWriter, r *http.Request, ctx context.Context, s Storage, key, parent string, index int) {
	if parent == "" || parent == "/" {
		parent = rootOf(s)
	}
	a.shareMu.Lock()
	p := a.sharePreviews[key]
	if p == nil || p.Used || p.BatchBusy || (s.Type == "quark" && p.TaskID != "") || p.Owner != authorizationOwner(r) || p.Config != shareConfig(s) || p.Storage != s.ID || time.Now().After(p.Expires) || index != p.Next || index < 0 || index >= len(p.Batches) || (index > 0 && parent != p.Parent) {
		a.shareMu.Unlock()
		fail(w, 409, errors.New("批次已提交、正在处理或会话失效，请检查目标目录"))
		return
	}
	p.BatchBusy = true
	p.Parent = parent
	preview := *p
	batch := p.Batches[index]
	a.shareMu.Unlock()
	// Consume before any directory creation or upstream write; never replay an uncertain result.
	defer func() { a.shareMu.Lock(); p.BatchBusy = false; a.shareMu.Unlock() }()
	failure := func(err error) {
		a.shareMu.Lock()
		p.Used = true
		p.Failed = true
		a.shareMu.Unlock()
		fail(w, 400, err)
	}
	current, err := a.store.storage(s.ID)
	if err != nil || shareConfig(current) != preview.Config {
		failure(errors.New("存储配置已变化"))
		return
	}
	a.shareMu.Lock()
	p.Next++
	a.shareMu.Unlock()
	if batch.Directory != "" {
		parent, _, err = a.uploadParent(ctx, s, parent, path.Join(batch.Directory, ".aether-share-placeholder"))
		if err != nil {
			failure(err)
			return
		}
	}
	existing, err := a.rawList(ctx, s, parent)
	if err != nil {
		failure(errors.New("转存目录不可访问"))
		return
	}
	names := map[string]bool{}
	for _, item := range existing {
		names[item.Name] = true
	}
	filtered := []shareEntry{}
	skipped := 0
	for _, item := range batch.Items {
		if names[item.Name] {
			skipped++
		} else {
			filtered = append(filtered, item)
		}
	}
	batch.Items = filtered
	preview.SourceParent = batch.Source
	task := ""
	if len(batch.Items) > 0 {
		task, err = submitShare(ctx, s, preview, parent, batch.Items)
		if err != nil {
			failure(errors.New(err.Error() + "；请检查网盘结果，勿直接重复提交"))
			return
		}
	}
	a.shareMu.Lock()
	p.TaskID = task
	p.Skipped += skipped
	p.Submitted += len(batch.Items)
	p.Expires = time.Now().Add(10 * time.Minute)
	finished := *p
	finished.BatchBusy = false
	a.shareMu.Unlock()
	a.recordShareNotice(key, s, finished)
	a.cache.clear()
	jsonResponse(w, 200, map[string]any{"status": "submitted", "taskId": task, "message": "本批次已提交", "skipped": skipped, "done": index + 1, "total": len(preview.Batches)})
}
