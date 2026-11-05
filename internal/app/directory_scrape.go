package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type directoryScrapeContextKey struct{}
type directoryScrapeEntry struct {
	item   scrapeItem
	file   File
	parent string
}
type directoryScrapeInput struct {
	Group     bool     `json:"group"`
	StorageID string   `json:"storageId"`
	Directory string   `json:"directory"`
	IDs       []string `json:"ids"`
	TMDB      int      `json:"tmdb"`
	Kind      string   `json:"kind"`
}

func directoryScrapeKey(s Storage, dir string) string {
	return "directory:" + archiveStamp(s.ID, storageRevision(s), dir)
}
func directoryScrapeMedia(name string) bool {
	return isVideo(name) || excludedType(name, taskVideoExtensions+";rmvb") || strings.HasSuffix(strings.ToLower(name), ".strm")
}

func (a *App) directoryScrapeStorage(id string) (Storage, error) {
	s, err := a.store.storage(id)
	if err != nil || !s.Enabled || storageHealthPaused(s) {
		return Storage{}, errors.New("请选择已启用且健康的存储池")
	}
	return s, nil
}
func (a *App) directoryScrapeAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	action := r.PathValue("action")
	if action == "files" && r.Method == "GET" {
		s, err := a.directoryScrapeStorage(r.URL.Query().Get("storageId"))
		if err != nil {
			fail(w, 400, err)
			return
		}
		dir := r.URL.Query().Get("directory")
		if dir == "" {
			dir = rootOf(s)
		}
		files, err := a.rawList(r.Context(), s, dir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		key := directoryScrapeKey(s, dir)
		index := a.loadDirectoryScrapeIndex(directoryScrapeKey(s, ""))
		known := map[string]scrapeItem{}
		for _, item := range index.Items {
			if item.SourceParent == dir {
				known[item.SourceID] = item
			}
		}
		items := []scrapeItem{}
		for _, f := range files {
			if item, ok := known[f.ID]; ok && directoryScrapeSameFile(item, f, dir) {
				item.Path = f.Name
				items = append(items, item)
			}
		}
		jsonResponse(w, 200, map[string]any{"files": files, "items": items, "taskId": key, "directory": dir})
		return
	}
	if r.Method != "POST" || (action != "run" && action != "identify" && action != "match" && action != "reset") {
		http.NotFound(w, r)
		return
	}
	var in directoryScrapeInput
	if !decode(w, r, &in) {
		return
	}
	s, err := a.directoryScrapeStorage(in.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if in.Directory == "" {
		in.Directory = rootOf(s)
	}
	if len(in.IDs) > 1000 || len(in.Directory) > 4096 || strings.ContainsAny(in.Directory, "\x00\r\n") {
		fail(w, 400, errors.New("目录或选择范围无效"))
		return
	}
	if action == "match" && (len(in.IDs) != 1 || in.TMDB < 1 || (in.Kind != "tv" && in.Kind != "movie")) {
		fail(w, 400, errors.New("请指定单个媒体和有效TMDB ID"))
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	tmdb := pluginDefaults("tmdb", st.Plugins["tmdb"])
	if action != "reset" && (!tmdb.Enabled || tmdb.APIKey == "") {
		fail(w, 400, errors.New("请配置并启用TMDB"))
		return
	}
	a.scrapeMu.Lock()
	defer a.scrapeMu.Unlock()
	if a.scrapeProgress.Running || a.scrapeDispatch || len(a.scrapeQueue) > 0 {
		fail(w, 409, errors.New("刮削任务正在执行或排队，请等待完成"))
		return
	}
	key := directoryScrapeKey(s, in.Directory)
	ctx, cancel := context.WithCancel(context.WithValue(a.ctx, directoryScrapeContextKey{}, key))
	a.runMu.Lock()
	err = a.checkTaskStartScopes(ctx, Task{ID: key, Kind: "directory-scrape", Name: s.Name, StorageID: s.ID, Source: in.Directory}, s)
	if err != nil {
		a.runMu.Unlock()
		cancel()
		fail(w, 409, err)
		return
	}
	task := Task{ID: key, Kind: "directory-scrape", Name: "目录刮削 · " + s.Name, StorageID: s.ID, Source: in.Directory}
	a.directoryScrapeTask = &task
	a.running[key] = cancel
	a.runningStorage[key] = s.ID
	a.runMu.Unlock()
	a.scrapeCancel = cancel
	a.scrapeProgress = scrapeProgress{Running: true, TaskID: key, Message: "正在读取存储目录"}
	a.store.event("info", "tasks", fmt.Sprintf("目录刮削开始：任务 %q，存储池 %q，目录 %q，操作 %q", key, s.Name, in.Directory, action))
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer cancel()
		defer func() {
			a.runMu.Lock()
			delete(a.running, key)
			delete(a.runningStorage, key)
			a.directoryScrapeTask = nil
			a.runMu.Unlock()
			a.scrapeMu.Lock()
			a.scrapeProgress.Running = false
			a.scrapeCancel = nil
			a.scrapeMu.Unlock()
			a.drainScrapeQueue()
		}()
		err := a.runDirectoryScrape(ctx, s, in, action, key, tmdb, st.Plugins["proxy"])
		a.scrapeMu.Lock()
		a.scrapeProgress.Error = err != nil || a.scrapeProgress.Failed > 0
		if err != nil {
			a.scrapeProgress.Message = err.Error()
		} else {
			a.scrapeProgress.Message = "目录刮削已结束"
		}
		completed := a.scrapeProgress
		a.scrapeMu.Unlock()
		level := "info"
		if completed.Error {
			level = "warn"
		}
		a.store.event(level, "tasks", fmt.Sprintf("目录刮削结束：任务 %q，存储池 %q，已处理 %d/%d，失败 %d，待匹配 %d；%s", key, s.Name, completed.Done, completed.Total, completed.Failed, completed.Unmatched, safeStorageError(err)))
	}()
	jsonResponse(w, 202, a.scrapeProgress)
}
func (a *App) loadDirectoryScrapeIndex(key string) scrapeIndex {
	index := scrapeIndex{Items: []scrapeItem{}}
	// loadScrapeIndex intentionally filters for STRM. Directory indices may also
	// contain original video files and are read through a separate schema path.
	f, err := os.Open(a.scrapeIndexPath(key))
	if err == nil {
		defer f.Close()
		raw, readErr := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		if readErr == nil && len(raw) <= 64<<20 {
			_ = json.Unmarshal(raw, &index)
		}
	}
	if index.Items == nil {
		index.Items = []scrapeItem{}
	}
	return index
}

func directoryScrapeSameFile(item scrapeItem, f File, parent string) bool {
	return item.SourceID == f.ID && item.SourceName == f.Name && item.SourceParent == parent && item.SourceSize == f.Size && item.SourceModified.Equal(f.Modified)
}

func (a *App) scanDirectoryScrape(ctx context.Context, s Storage, in directoryScrapeInput) ([]directoryScrapeEntry, map[string]string, error) {
	entries := []directoryScrapeEntry{}
	excludedDirectories := a.scrapeConfig().Excluded
	dirs := map[string]string{".": in.Directory}
	visited := map[string]bool{}
	count := 0
	selected := map[string]bool{}
	for _, id := range in.IDs {
		selected[id] = false
	}
	var walk func(string, string, int) error
	walk = func(dir, rel string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 || visited[dir] {
			return errors.New("目录层级过深或存在循环")
		}
		visited[dir] = true
		list, err := a.rawList(ctx, s, dir)
		if err != nil {
			return err
		}
		for _, f := range list {
			count++
			if count > 100000 {
				return errors.New("扫描超过100000项，请选择更小的目录")
			}
			if !safeName(f.Name) || f.ID == "" {
				return errors.New("存储返回无效文件名称或标识")
			}
			if depth == 0 && len(selected) > 0 {
				if _, ok := selected[f.ID]; !ok {
					continue
				}
				selected[f.ID] = true
			}
			name := path.Join(rel, f.Name)
			if excluded(name, excludedDirectories) {
				continue
			}
			if f.IsDir {
				dirs[name] = f.ID
				if err := walk(f.ID, name, depth+1); err != nil {
					return err
				}
				continue
			}
			if directoryScrapeMedia(f.Name) {
				item := recognizeSTRM(f.Name)
				item.Path = name
				entries = append(entries, directoryScrapeEntry{item, f, dir})
			}
		}
		return nil
	}
	err := walk(in.Directory, ".", 0)
	if err != nil {
		return nil, nil, err
	}
	for _, found := range selected {
		if !found {
			return nil, nil, errors.New("所选文件已变化，请刷新后重试")
		}
	}
	if len(entries) > 10000 {
		return nil, nil, errors.New("单次刮削最多10000个媒体，请选择子目录")
	}
	return entries, dirs, nil
}

func (a *App) directoryScrapeOutput(ctx context.Context, s Storage, dirs map[string]string, check func() error) scrapeOutput {
	parent := func(name string) (string, string, error) {
		if !fs.ValidPath(name) || name == "." || !safeName(path.Base(name)) {
			return "", "", errors.New("元数据文件名无效")
		}
		dir, ok := dirs[path.Dir(name)]
		if !ok {
			return "", "", errors.New("元数据目录不在本次扫描范围")
		}
		return dir, path.Base(name), nil
	}
	exists := func(name string) (bool, error) {
		dir, base, err := parent(name)
		if err != nil {
			return false, err
		}
		f, err := a.cloudByName(ctx, s, dir, base)
		if f != nil && f.IsDir {
			return false, errors.New("元数据名称与目录冲突")
		}
		return f != nil, err
	}
	return scrapeOutput{exists: exists, write: func(name string, data []byte, missing bool) error {
		if err := check(); err != nil {
			return err
		}
		dir, base, err := parent(name)
		if err != nil {
			return err
		}
		if len(data) > 20<<20 {
			return errors.New("刮削文件过大")
		}
		if s.Type == "local" {
			a.runMu.Lock()
			defer a.runMu.Unlock()
			if err := check(); err != nil {
				return err
			}
			if err := a.checkTaskFileNodes(ctx, []taskFileNode{{s, dir, base}}); err != nil {
				return err
			}
			root, err := os.OpenRoot(s.Config["root"])
			if err != nil {
				return err
			}
			defer root.Close()
			rel, err := relative(dir)
			if err != nil {
				return err
			}
			return writeCASOutput(root, path.Join(rel, base), data, missing)
		}
		tempDir := filepath.Join(a.transferTempDirectory(), "scrape")
		if err := os.MkdirAll(tempDir, 0700); err != nil {
			return err
		}
		temp, err := a.newTransferTemp(tempDir, "metadata-*", int64(len(data)))
		if err != nil {
			return err
		}
		defer func() { temp.Close(); os.Remove(temp.Name()) }()
		if _, err = temp.Write(data); err != nil {
			return err
		}
		if err = temp.Sync(); err != nil {
			return err
		}
		a.runMu.Lock()
		defer a.runMu.Unlock()
		if err := check(); err != nil {
			return err
		}
		if err := a.checkTaskFileNodes(ctx, []taskFileNode{{s, dir, base}}); err != nil {
			return err
		}
		old, err := a.cloudByName(ctx, s, dir, base)
		if err != nil {
			return err
		}
		if old != nil {
			if old.IsDir {
				return errors.New("元数据名称与目录冲突")
			}
			if missing {
				return nil
			}
		}
		defer a.cache.clear()
		return a.publishCloudFile(ctx, s, dir, base, temp.Name())
	}}
}
func (a *App) runDirectoryScrape(ctx context.Context, s Storage, in directoryScrapeInput, action, key string, tmdb, proxy PluginConfig) (runErr error) {
	ctx = a.tmdbContext(ctx)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	entries, dirs, err := a.scanDirectoryScrape(ctx, s, in)
	if err != nil {
		return err
	}
	if action == "match" {
		if !in.Group && (len(in.IDs) != 1 || len(entries) != 1 || entries[0].file.ID != in.IDs[0]) {
			return errors.New("手动识别仅允许选择单个媒体文件")
		}
		if in.Group {
			found := false
			for rel, id := range dirs {
				if rel != "." && len(in.IDs) == 1 && id == in.IDs[0] {
					found = true
				}
			}
			if !found || len(entries) == 0 {
				return errors.New("手动识别目录范围已变化")
			}
		}
	}
	if action == "reset" {
		current, err := a.directoryScrapeStorage(s.ID)
		if err != nil || storageRevision(current) != storageRevision(s) {
			return errors.New("存储配置已变化，未重置")
		}
		cacheKey := directoryScrapeKey(s, "")
		index := a.loadDirectoryScrapeIndex(cacheKey)
		ids := map[string]bool{}
		for _, entry := range entries {
			ids[entry.file.ID] = true
		}
		kept := make([]scrapeItem, 0, len(index.Items))
		for _, item := range index.Items {
			if !ids[item.SourceID] {
				kept = append(kept, item)
			}
		}
		index.Items, index.ScannedAt, index.Covers = kept, time.Time{}, nil
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := a.saveScrapeIndex(cacheKey, index); err != nil {
			return err
		}
		a.scrapeMu.Lock()
		a.scrapeProgress.Done = len(entries)
		a.scrapeProgress.Total = len(entries)
		a.scrapeMu.Unlock()
		return nil
	}
	client, closeIdle, err := pluginClient(proxy)
	if err != nil {
		return err
	}
	defer closeIdle()
	cfg := a.scrapeConfig()
	if action == "match" {
		cfg.WriteMode = "overwrite"
	}
	cacheKey := directoryScrapeKey(s, "")
	index := a.loadDirectoryScrapeIndex(cacheKey)
	index.Root = s.ID
	index.ScannedAt = time.Now()
	positions := map[string]int{}
	for i, item := range index.Items {
		positions[item.SourceID] = i
	}
	dirty := false
	defer func() {
		if dirty {
			if len(index.Items) > 100000 {
				index.Items = index.Items[len(index.Items)-100000:]
			}
			runErr = errors.Join(runErr, a.saveScrapeIndex(cacheKey, index))
		}
	}()
	a.scrapeMu.Lock()
	a.scrapeProgress.Total = len(entries)
	a.scrapeMu.Unlock()
	for _, entry := range entries {
		check := func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			current, err := a.directoryScrapeStorage(s.ID)
			if err != nil || storageRevision(current) != storageRevision(s) {
				return errors.New("存储配置已变化，刮削停止")
			}
			list, err := a.rawList(ctx, s, entry.parent)
			if err != nil {
				return err
			}
			for _, f := range list {
				if f.ID == entry.file.ID && f.Name == entry.file.Name && !f.IsDir && f.Size == entry.file.Size && f.Modified.Equal(entry.file.Modified) {
					return nil
				}
			}
			return errors.New("原媒体文件已变化，刮削停止")
		}
		if err := check(); err != nil {
			return err
		}
		item := entry.item
		item.SourceID, item.SourceParent, item.SourceSize, item.SourceModified = entry.file.ID, entry.parent, entry.file.Size, entry.file.Modified
		item.SourceName = entry.file.Name
		if i, ok := positions[entry.file.ID]; ok && directoryScrapeSameFile(index.Items[i], entry.file, entry.parent) {
			previous := index.Items[i]
			item.TMDB, item.Kind, item.Manual = previous.TMDB, previous.Kind, previous.Manual
		}
		if action == "match" {
			item.TMDB = in.TMDB
			item.Kind = in.Kind
			item.Manual = true
		}
		record := archiveRecord{archiveEntry: archiveEntry{ID: archiveStamp(key, item.Path), Category: "scrape", TaskID: key, TaskName: "目录刮削 · " + s.Name, Trigger: "manual", Action: action, Status: "running", StartedAt: time.Now(), Path: item.Path, Filename: entry.file.Name, Folder: scrapeWorkDir(item.Path), Title: item.Title, Kind: item.Kind}, GroupID: archiveStamp(key, scrapeWorkKey(item))}
		if err := a.archives.put(record); err != nil {
			return err
		}
		if action == "identify" {
			_, err = resolveScrapeMedia(ctx, client, tmdb, cfg, &item)
		} else {
			err = scrapeOneTo(ctx, a.directoryScrapeOutput(ctx, s, dirs, check), client, tmdb, cfg, &item)
		}
		if err != nil {
			item.Status = "error"
			item.Message = err.Error()
			a.store.event("warn", "tasks", fmt.Sprintf("目录刮削文件失败：存储池 %q，文件 %q：%s", s.Name, entry.file.Name, safeStorageError(err)))
		}
		if archiveErr := a.finishScrapeArchive(record, item, err); archiveErr != nil {
			return archiveErr
		}
		if i, ok := positions[item.SourceID]; ok {
			index.Items[i] = item
		} else {
			positions[item.SourceID] = len(index.Items)
			index.Items = append(index.Items, item)
		}
		dirty = true
		a.scrapeMu.Lock()
		a.scrapeProgress.Done++
		a.scrapeProgress.Message = item.Title
		if err != nil {
			a.scrapeProgress.Failed++
		}
		if item.Status == "miss" || item.Status == "doubt" {
			a.scrapeProgress.Unmatched++
		}
		a.scrapeMu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
