package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type shareBatch struct {
	Source    string
	Directory string
	Items     []shareEntry
}

const shareTransferBatchLimit = 499
const mobileShareScanLimit = 200000

func shareScanLimit(s Storage) int {
	if nativeMobile(s) {
		return mobileShareScanLimit
	}
	return 5000
}

func validShareDirectory(name string) bool {
	return name == "" || utf8.RuneCountInString(name) <= 255 && safeName(name) && !strings.ContainsAny(name, `<>"|?*`) && strings.IndexFunc(name, unicode.IsControl) < 0
}

func shareRootDirectory(items []shareEntry) string {
	if len(items) == 1 && items[0].IsDir {
		return items[0].Name
	}
	return ""
}

func shareDirectoryBatches(items []shareEntry, batches []shareBatch, name *string) ([]shareBatch, error) {
	if name == nil {
		return batches, nil
	}
	if !validShareDirectory(*name) {
		return nil, errors.New("保存目录名称无效，请填写单个目录名，不支持路径或特殊字符")
	}
	root := shareRootDirectory(items)
	result := append([]shareBatch(nil), batches...)
	for i := range result {
		dir := result[i].Directory
		if root != "" {
			if dir == root {
				dir = ""
			} else if strings.HasPrefix(dir, root+"/") {
				dir = strings.TrimPrefix(dir, root+"/")
			} else {
				return nil, errors.New("分享根目录已变化，请重新解析")
			}
		}
		result[i].Directory = path.Join(*name, dir)
		if result[i].Directory == "." {
			result[i].Directory = ""
		}
	}
	return result, nil
}

type shareTreeEntry struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

func shareTree(items []shareEntry, batches []shareBatch) []shareTreeEntry {
	entries := map[string]shareTreeEntry{}
	for _, item := range items {
		entries[item.Name] = shareTreeEntry{Path: item.Name, Name: item.Name, IsDir: item.IsDir}
	}
	for _, batch := range batches {
		directory := batch.Directory
		for directory != "" && directory != "." {
			entries[directory] = shareTreeEntry{Path: directory, Name: path.Base(directory), IsDir: true}
			directory = path.Dir(directory)
		}
		for _, item := range batch.Items {
			key := path.Join(batch.Directory, item.Name)
			entries[key] = shareTreeEntry{Path: key, Name: item.Name, IsDir: item.IsDir}
		}
	}
	result := []shareTreeEntry{}
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func selectedShareBatches(batches []shareBatch, tree []shareTreeEntry, excluded []string) ([]shareBatch, error) {
	known := map[string]bool{}
	for _, item := range tree {
		known[item.Path] = true
	}
	if len(excluded) > mobileShareScanLimit {
		return nil, errors.New("选择项目过多")
	}
	omitted := make(map[string]bool, len(excluded))
	for _, key := range excluded {
		if !known[key] {
			return nil, errors.New("分享目录已变化，请重新解析")
		}
		omitted[key] = true
	}
	blocked := func(key string) bool {
		for key != "" && key != "." {
			if omitted[key] {
				return true
			}
			key = path.Dir(key)
		}
		return false
	}
	result := []shareBatch{}
	for _, batch := range batches {
		if blocked(batch.Directory) {
			continue
		}
		items := []shareEntry{}
		for _, item := range batch.Items {
			if !blocked(path.Join(batch.Directory, item.Name)) {
				items = append(items, item)
			}
		}
		if len(items) > 0 || len(batch.Items) == 0 {
			batch.Items = items
			result = append(result, batch)
		}
	}
	return result, nil
}

func (a *App) planShare(ctx context.Context, s Storage, code, pass string, items []shareEntry) ([]shareBatch, error) {
	return a.planShareSession(ctx, &s, code, pass, "", items)
}

func (a *App) planShareSession(ctx context.Context, session *Storage, code, pass, token string, items []shareEntry) ([]shareBatch, error) {
	s := *session
	result := []shareBatch{}
	seen, count := map[string]bool{}, 0
	var walk func([]shareEntry, string, string, int) error
	walk = func(entries []shareEntry, directory, source string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 {
			return errors.New("分享目录层级超过32层")
		}
		count += len(entries)
		if count > shareScanLimit(s) {
			return fmt.Errorf("分享文件总数超过%d，请缩小分享范围", shareScanLimit(s))
		}
		limit := shareTransferBatchLimit
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
				nextToken, children, err := a.readShareSession(ctx, session, code, pass, childSource, token)
				if err != nil {
					return err
				}
				if s.Type == "quark" {
					token = nextToken
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
		finished := *p
		a.shareMu.Unlock()
		a.recordBrowserShare(key, s, finished, "failed", "转存未完成，请核对云盘及运行日志")
		fail(w, 400, err)
	}
	current, err := a.store.storage(s.ID)
	if err != nil || shareConfig(current) != preview.Config {
		failure(errors.New("存储配置已变化"))
		return
	}
	a.shareMu.Lock()
	p.Next++
	running := *p
	a.shareMu.Unlock()
	if err := a.recordBrowserShare(key, s, running, "running", "正在提交转存"); err != nil {
		failure(errors.New("转存记录保存失败，未提交本批次"))
		return
	}
	session := shareStorageSession(s, preview)
	directory := batch.Directory
	if preview.SaveDirectory != nil && *preview.SaveDirectory != "" {
		destination := preview.Destination
		if destination == "" {
			destination, _, err = a.uploadParent(ctx, session, parent, path.Join(*preview.SaveDirectory, ".aether-share-placeholder"))
			if err != nil {
				failure(err)
				return
			}
			a.shareMu.Lock()
			p.Destination = destination
			a.shareMu.Unlock()
		}
		parent = destination
		if directory == *preview.SaveDirectory {
			directory = ""
		} else if strings.HasPrefix(directory, *preview.SaveDirectory+"/") {
			directory = strings.TrimPrefix(directory, *preview.SaveDirectory+"/")
		} else {
			failure(errors.New("保存目录计划无效，请重新解析"))
			return
		}
	}
	batch.Directory = directory
	task, skipped, submitted, err := a.submitPlannedShareBatch(ctx, &session, preview, parent, batch)
	if err != nil {
		failure(err)
		return
	}
	a.shareMu.Lock()
	p.TaskID = task
	p.Cookie = session.Config["cookie"]
	p.Skipped += skipped
	p.Submitted += submitted
	p.Expires = time.Now().Add(10 * time.Minute)
	finished := *p
	finished.BatchBusy = false
	a.shareMu.Unlock()
	a.recordShareNotice(key, s, finished)
	a.cache.clear()
	response := map[string]any{"status": "submitted", "taskId": task, "message": "本批次已提交", "skipped": skipped, "done": index + 1, "total": len(preview.Batches)}
	if finished.SaveDirectory != nil && *finished.SaveDirectory != "" {
		response["destination"] = finished.Destination
		response["directory"] = *finished.SaveDirectory
	}
	jsonResponse(w, 200, response)
}

func (a *App) submitPlannedShareBatch(ctx context.Context, session *Storage, preview sharePreview, parent string, batch shareBatch, guards ...func() error) (string, int, int, error) {
	for _, guard := range guards {
		if err := guard(); err != nil {
			return "", 0, 0, err
		}
	}
	if batch.Directory != "" {
		var err error
		parent, _, err = a.uploadParent(ctx, *session, parent, path.Join(batch.Directory, ".aether-share-placeholder"))
		if err != nil {
			return "", 0, 0, err
		}
	}
	existing, err := a.rawList(ctx, *session, parent)
	if err != nil {
		return "", 0, 0, errors.New("转存目录不可访问")
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
	preview.SourceParent = batch.Source
	if len(filtered) == 0 {
		return "", skipped, 0, nil
	}
	for _, guard := range guards {
		if err := guard(); err != nil {
			return "", 0, 0, err
		}
	}
	task, err := submitShareSession(ctx, session, preview, parent, filtered)
	if err != nil {
		return "", skipped, 0, errors.New(err.Error() + "；请检查网盘结果，勿直接重复提交")
	}
	return task, skipped, len(filtered), nil
}
