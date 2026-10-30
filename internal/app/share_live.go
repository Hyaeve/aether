package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

type shareTransferRecord struct {
	ID            string    `json:"id"`
	LiveID        string    `json:"liveId,omitempty"`
	TargetID      string    `json:"targetId,omitempty"`
	StorageID     string    `json:"storageId"`
	Provider      string    `json:"provider"`
	Name          string    `json:"name"`
	Link          string    `json:"secret"`
	DirectoryName string    `json:"directoryName"`
	Source        string    `json:"source"`
	Time          time.Time `json:"time"`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
	Submitted     int       `json:"submitted"`
	Skipped       int       `json:"skipped"`
}

type shareLiveLink struct {
	ID            string    `json:"id"`
	TargetID      string    `json:"targetId"`
	StorageID     string    `json:"storageId"`
	Provider      string    `json:"provider"`
	Parent        string    `json:"parent"`
	DirectoryID   string    `json:"directoryId"`
	DirectoryName string    `json:"directoryName"`
	Name          string    `json:"name"`
	OriginalName  string    `json:"originalName,omitempty"`
	Link          string    `json:"secret"`
	Stamp         string    `json:"stamp"`
	Cron          string    `json:"cron"`
	IntervalDays  int       `json:"intervalDays"`
	LastRun       time.Time `json:"lastRun"`
	NextRun       time.Time `json:"nextRun"`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
	Intent        string    `json:"intent,omitempty"`
	Excluded      []string  `json:"excluded,omitempty"`
}

func validateShareLiveCron(expression string) error {
	if expression == "" {
		return nil
	}
	if len(expression) > 128 {
		return errors.New("Cron表达式最多128字节")
	}
	if err := checkCronInterval(expression, 12*time.Hour, false); err != nil {
		return errors.New("活链Cron相邻执行间隔至少12小时，请检查跨日间隔")
	}
	return nil
}

func shareLiveNext(link shareLiveLink, now time.Time) time.Time {
	if link.Cron != "" {
		if validateShareLiveCron(link.Cron) != nil {
			return time.Time{}
		}
		schedule, err := rawCronParser.Parse(link.Cron)
		if err == nil {
			return schedule.Next(now)
		}
	}
	if link.IntervalDays > 0 && link.IntervalDays <= 365 {
		next := now.AddDate(0, 0, link.IntervalDays)
		return time.Date(next.Year(), next.Month(), next.Day(), now.Hour(), 0, 0, 0, now.Location())
	}
	return time.Time{}
}

func sharePublicURL(provider, code string) string {
	switch provider {
	case "115":
		return "https://115.com/s/" + code
	case "mobile":
		return "https://yun.139.com/shareweb/#/w/i/" + code
	default:
		return "https://pan.quark.cn/s/" + code
	}
}

func shareRecordURL(link string) string {
	provider, code, _, err := parseShareLink(link, "")
	if err != nil {
		return ""
	}
	return sharePublicURL(provider, code)
}

func appendShareTransfer(st *State, record shareTransferRecord) {
	for i := range st.ShareTransfers {
		if st.ShareTransfers[i].ID == record.ID {
			st.ShareTransfers[i] = record
			return
		}
	}
	st.ShareTransfers = append(st.ShareTransfers, record)
	if len(st.ShareTransfers) > 1000 {
		// Never evict an unfinished write receipt.
		for i, item := range st.ShareTransfers {
			if item.Status != "running" {
				st.ShareTransfers = slices.Delete(st.ShareTransfers, i, i+1)
				break
			}
		}
	}
}

func (a *App) recordShareTransfer(record shareTransferRecord, p sharePreview) error {
	if record.Time.IsZero() {
		record.Time = time.Now()
	}
	return a.store.update(func(st *State) error {
		if (record.Provider == "mobile" || record.Provider == "quark") && (record.Status == "completed" || record.Status == "submitted") &&
			shareRootDirectory(p.Items) != "" && p.Destination != "" && record.DirectoryName != "" {
			var target *telegramTarget
			for _, value := range st.Notifications.Targets {
				parent := value.Parent
				if parent == "/" {
					for _, storage := range st.Storages {
						if storage.ID == value.StorageID {
							parent = rootOf(storage)
						}
					}
				}
				if value.StorageID == record.StorageID && value.Provider == record.Provider && parent == p.Parent && value.shares() {
					copy := value
					target = &copy
					break
				}
			}
			if target != nil {
				record.TargetID = target.ID
				key := ""
				for i := range st.ShareLiveLinks {
					link := &st.ShareLiveLinks[i]
					if link.StorageID == record.StorageID && link.Parent == p.Parent && shareRecordURL(link.Link) == shareRecordURL(record.Link) {
						key = link.ID
						break
					}
				}
				if key == "" && len(st.ShareLiveLinks) < 1000 {
					link := shareLiveLink{ID: id(), TargetID: target.ID, StorageID: record.StorageID, Provider: record.Provider,
						Parent: p.Parent, DirectoryID: p.Destination, DirectoryName: record.DirectoryName, Name: record.Name,
						Link: record.Link, OriginalName: shareRootDirectory(p.Items), Excluded: append([]string(nil), p.Excluded...), Stamp: telegramDigest(p.Config), IntervalDays: target.updateDays(),
						LastRun: record.Time, Status: record.Status, Message: record.Message}
					link.NextRun = shareLiveNext(link, record.Time)
					st.ShareLiveLinks = append(st.ShareLiveLinks, link)
					key = link.ID
				}
				record.LiveID = key
			}
		}
		appendShareTransfer(st, record)
		return nil
	})
}

func (a *App) shareTransferHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	records := a.store.snapshotWithLogLimit(0).ShareTransfers
	items := make([]map[string]any, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		items = append(items, map[string]any{"id": record.ID, "provider": record.Provider, "storageId": record.StorageID, "targetId": record.TargetID,
			"name": record.Name, "url": shareRecordURL(record.Link), "directoryName": record.DirectoryName, "source": record.Source,
			"time": record.Time, "status": record.Status, "message": record.Message, "submitted": record.Submitted, "skipped": record.Skipped})
	}
	jsonResponse(w, 200, items)
}

func (a *App) shareLiveGuard(link shareLiveLink) (Storage, error) {
	st := a.store.snapshotWithLogLimit(0)
	for _, s := range st.Storages {
		if s.ID != link.StorageID || s.Type != link.Provider || !s.Enabled || telegramDigest(shareConfig(s)) != link.Stamp {
			continue
		}
		if err := storageAuthBlocked(s, time.Now()); err != nil {
			return Storage{}, err
		}
		for _, target := range st.Notifications.Targets {
			parent := target.Parent
			if parent == "/" {
				parent = rootOf(s)
			}
			if target.ID == link.TargetID && target.StorageID == s.ID && target.Provider == s.Type && parent == link.Parent && target.shares() {
				return s, nil
			}
		}
	}
	return Storage{}, errors.New("存储凭据或转存目录配置已变化，未执行写操作")
}

func (a *App) verifyShareLiveDirectory(ctx context.Context, s Storage, link shareLiveLink) error {
	if link.DirectoryID == "" || link.DirectoryID == rootOf(s) || link.DirectoryID == link.Parent || !validShareDirectory(link.DirectoryName) || link.DirectoryName == "" {
		return errors.New("未确认活链保存目录身份")
	}
	files, err := a.rawList(ctx, s, link.Parent)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.ID == link.DirectoryID && f.Name == link.DirectoryName && f.IsDir {
			return nil
		}
	}
	return errors.New("活链保存目录已移动、改名或移除，停止操作")
}

func (a *App) shareLiveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		items := []map[string]any{}
		for _, link := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
			if target := r.URL.Query().Get("target"); target != "" && target != link.TargetID {
				continue
			}
			items = append(items, map[string]any{"id": link.ID, "targetId": link.TargetID, "provider": link.Provider, "name": link.Name,
				"url": link.Link, "directoryName": link.DirectoryName, "cron": link.Cron, "intervalDays": link.IntervalDays,
				"lastRun": link.LastRun, "nextRun": link.NextRun, "status": link.Status, "message": link.Message})
		}
		jsonResponse(w, 200, items)
		return
	}
	key := r.PathValue("id")
	if r.PathValue("action") == "run" {
		if err := a.startShareLive(key, "manual"); err != nil {
			fail(w, 409, err)
		} else {
			jsonResponse(w, 202, map[string]bool{"ok": true})
		}
		return
	}
	if !a.shareLiveMu.TryLock() {
		fail(w, 409, errors.New("有活链操作正在执行，请稍后重试"))
		return
	}
	defer a.shareLiveMu.Unlock()
	var link shareLiveLink
	for _, current := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
		if current.ID == key {
			link = current
		}
	}
	if link.ID == "" {
		fail(w, 404, errors.New("活链记录不存在"))
		return
	}
	if r.PathValue("action") == "remove" {
		err := a.store.update(func(st *State) error {
			st.ShareLiveLinks = slices.DeleteFunc(st.ShareLiveLinks, func(item shareLiveLink) bool { return item.ID == key })
			return nil
		})
		if err != nil {
			fail(w, 500, err)
		} else {
			jsonResponse(w, 200, map[string]bool{"ok": true})
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if r.Method == http.MethodPut {
		var input struct{ URL, Name, Cron, DirectoryName string }
		if !decode(w, r, &input) {
			return
		}
		provider, _, _, err := parseShareLink(input.URL, "")
		if err != nil || provider != link.Provider || len(input.Name) > 300 || !validShareDirectory(input.DirectoryName) || input.DirectoryName == "" {
			fail(w, 400, errors.New("分享链接、名称或目录名称无效"))
			return
		}
		if err := validateShareLiveCron(strings.TrimSpace(input.Cron)); err != nil {
			fail(w, 400, err)
			return
		}
		s, err := a.shareLiveGuard(link)
		if err == nil && link.Intent != "" {
			err = errors.New("上次写入结果未确认，请核对云盘后手动执行，不可改名")
		}
		if err == nil && input.DirectoryName != link.DirectoryName {
			for _, other := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
				if other.ID != key && other.StorageID == link.StorageID && other.DirectoryID == link.DirectoryID {
					err = errors.New("此目录被其他活链共用，不可单独改名")
				}
			}
		}
		if err == nil && input.DirectoryName != link.DirectoryName {
			err = a.verifyShareLiveDirectory(ctx, s, link)
			if err == nil {
				err = a.setShareLiveIntent(key, "rename:"+input.DirectoryName)
			}
			if err == nil {
				_, err = a.shareLiveGuard(link)
			}
			if err == nil {
				err = a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "rename", Source: link.Parent, IDs: []string{link.DirectoryID}, Name: input.DirectoryName})
			}
			if err == nil {
				renamed := link
				renamed.DirectoryName = input.DirectoryName
				err = a.verifyShareLiveDirectory(ctx, s, renamed)
			}
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
		link.Link, link.Name, link.Cron, link.DirectoryName = input.URL, strings.TrimSpace(input.Name), strings.TrimSpace(input.Cron), input.DirectoryName
		if link.Name == "" {
			link.Name = link.OriginalName
			if link.Name == "" {
				link.Name = input.DirectoryName
			}
		}
		link.IntervalDays, link.Intent = 0, ""
		link.NextRun = shareLiveNext(link, time.Now())
		err = a.store.update(func(st *State) error {
			for i := range st.ShareLiveLinks {
				if st.ShareLiveLinks[i].ID == key {
					st.ShareLiveLinks[i] = link
				}
			}
			for i := range st.ShareTransfers {
				if st.ShareTransfers[i].LiveID == key {
					st.ShareTransfers[i].DirectoryName = link.DirectoryName
					st.ShareTransfers[i].Name = link.Name
				}
			}
			return nil
		})
		if err != nil {
			fail(w, 500, err)
		} else {
			a.cache.clear()
			jsonResponse(w, 200, map[string]bool{"ok": true})
		}
		return
	}
	if r.PathValue("action") != "delete" {
		http.NotFound(w, r)
		return
	}
	var input struct{ Confirm bool }
	if !decode(w, r, &input) || !input.Confirm {
		fail(w, 400, errors.New("删除文件须明确二次确认"))
		return
	}
	s, err := a.shareLiveGuard(link)
	if err == nil && link.Intent != "" {
		err = errors.New("上次写入结果未确认，不重复提交删除")
	}
	if err == nil {
		for _, other := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
			if other.ID != key && other.StorageID == link.StorageID && other.DirectoryID == link.DirectoryID {
				err = errors.New("此目录还被其他活链记录使用，不能一并删除")
			}
		}
	}
	if err == nil {
		err = a.verifyShareLiveDirectory(ctx, s, link)
	}
	if err == nil {
		err = a.setShareLiveIntent(key, "delete")
	}
	if err == nil {
		_, err = a.shareLiveGuard(link)
	}
	if err == nil {
		err = a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "delete", Source: link.Parent, IDs: []string{link.DirectoryID}})
	}
	if err == nil {
		var files []File
		files, err = a.rawList(ctx, s, link.Parent)
		if err == nil {
			for _, file := range files {
				if file.ID == link.DirectoryID {
					err = errors.New("删除已提交，云盘尚未确认目录移除；请核对云盘，勿重复提交")
				}
			}
		}
	}
	if err != nil {
		fail(w, 409, err)
		return
	}
	err = a.store.update(func(st *State) error {
		st.ShareLiveLinks = slices.DeleteFunc(st.ShareLiveLinks, func(item shareLiveLink) bool { return item.ID == key })
		return nil
	})
	a.cache.clear()
	if err != nil {
		fail(w, 500, err)
	} else {
		jsonResponse(w, 200, map[string]bool{"ok": true})
	}
}

func (a *App) setShareLiveIntent(key, intent string) error {
	return a.store.update(func(st *State) error {
		for i := range st.ShareLiveLinks {
			if st.ShareLiveLinks[i].ID == key {
				st.ShareLiveLinks[i].Intent = intent
				return nil
			}
		}
		return errors.New("活链记录已移除")
	})
}

func (a *App) startShareLive(key, source string) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if !a.shareLiveMu.TryLock() {
		return errors.New("活链更新正在执行")
	}
	var link shareLiveLink
	for _, current := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
		if current.ID == key {
			link = current
		}
	}
	if link.ID == "" || link.Status == "running" || link.Intent != "" {
		a.shareLiveMu.Unlock()
		return errors.New("活链不存在、正在执行或上次写入未确认，请核对云盘")
	}
	if _, err := a.shareLiveGuard(link); err != nil {
		_ = a.store.update(func(st *State) error {
			for i := range st.ShareLiveLinks {
				if st.ShareLiveLinks[i].ID == link.ID {
					st.ShareLiveLinks[i].Status, st.ShareLiveLinks[i].Message, st.ShareLiveLinks[i].NextRun = "failed", err.Error(), time.Time{}
				}
			}
			return nil
		})
		a.shareLiveMu.Unlock()
		return err
	}
	record := shareTransferRecord{ID: id(), LiveID: link.ID, TargetID: link.TargetID, StorageID: link.StorageID, Provider: link.Provider,
		Name: link.Name, Link: link.Link, DirectoryName: link.DirectoryName, Source: source, Time: time.Now(), Status: "running", Message: "正在扫描分享"}
	if err := a.store.update(func(st *State) error {
		for i := range st.ShareLiveLinks {
			if st.ShareLiveLinks[i].ID == key {
				st.ShareLiveLinks[i].Status, st.ShareLiveLinks[i].Message = "running", "正在扫描分享"
			}
		}
		appendShareTransfer(st, record)
		return nil
	}); err != nil {
		a.shareLiveMu.Unlock()
		return err
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.shareLiveMu.Unlock()
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
		defer cancel()
		a.executeShareLive(ctx, link, record)
	}()
	return nil
}

func (a *App) executeShareLive(ctx context.Context, link shareLiveLink, record shareTransferRecord) {
	s, err := a.shareLiveGuard(link)
	if err == nil {
		err = a.verifyShareLiveDirectory(ctx, s, link)
	}
	var batches []shareBatch
	var preview sharePreview
	if err == nil {
		provider, code, pass, parseErr := parseShareLink(link.Link, "")
		if parseErr != nil || provider != s.Type {
			err = errors.New("分享链接无效")
		} else {
			token, items, readErr := a.readShareSession(ctx, &s, code, pass, "root", "")
			err = readErr
			if err == nil && shareRootDirectory(items) == "" {
				err = errors.New("活链更新仅支持文件夹分享，分享根结构已变化")
			}
			if err == nil {
				batches, err = a.planShareSession(ctx, &s, code, pass, token, items)
			}
			if err == nil && len(link.Excluded) > 0 {
				tree := shareTree(items, batches)
				known := map[string]bool{}
				for _, entry := range tree {
					known[entry.Path] = true
				}
				filter := []string{}
				for _, entry := range link.Excluded {
					if known[entry] {
						filter = append(filter, entry)
					}
				}
				batches, err = selectedShareBatches(batches, tree, filter)
			}
			if err == nil {
				empty := ""
				batches, err = shareDirectoryBatches(items, batches, &empty)
			}
			preview = sharePreview{Code: code, Pass: pass, Token: token}
		}
	}
	guard := func() error {
		_, err := a.shareLiveGuard(link)
		return err
	}
	for i, batch := range batches {
		if err != nil {
			break
		}
		err = guard()
		if err == nil {
			err = a.setShareLiveIntent(link.ID, fmt.Sprintf("batch:%s:%d", record.ID, i))
		}
		if err != nil {
			break
		}
		task, skipped, submitted, submitErr := a.submitPlannedShareBatch(ctx, &s, preview, link.DirectoryID, batch, guard)
		record.Submitted += submitted
		record.Skipped += skipped
		err = submitErr
		if err == nil && s.Type == "quark" && task != "" {
			err = waitShareQuark(ctx, &s, task, guard)
		}
		if err == nil {
			err = a.setShareLiveIntent(link.ID, "")
		}
		a.cache.clear()
	}
	record.Status = "completed"
	if record.Submitted > 0 && link.Provider == "mobile" {
		record.Status = "submitted"
	}
	record.Message = fmt.Sprintf("新增提交%d个文件，跳过%d个文件", record.Submitted, record.Skipped)
	if err != nil {
		record.Status, record.Message = "failed", "活链更新未完成，请核对云盘及运行日志"
		a.store.event("error", "notifications", fmt.Sprintf("[活链:%s] %s", link.ID, transferFailureReason(err)))
	}
	if saveErr := a.store.update(func(st *State) error {
		for i := range st.ShareLiveLinks {
			if st.ShareLiveLinks[i].ID == link.ID {
				current := &st.ShareLiveLinks[i]
				current.Status, current.Message, current.LastRun = record.Status, record.Message, record.Time
				current.NextRun = shareLiveNext(*current, record.Time)
				if current.Intent != "" {
					current.NextRun = time.Time{}
				}
			}
		}
		appendShareTransfer(st, record)
		return nil
	}); saveErr != nil {
		a.store.event("error", "notifications", "活链结果保存失败，请核对云盘；不自动重提")
	}
}

func waitShareQuark(ctx context.Context, session *Storage, task string, guard func() error) error {
	for range 120 {
		if err := guard(); err != nil {
			return err
		}
		var result struct {
			Data struct{ Status int } `json:"data"`
		}
		if err := shareSessionRequest(ctx, session, "GET", "https://drive.quark.cn/1/clouddrive/task?pr=ucpro&fr=pc&retry_index=0&task_id="+url.QueryEscape(task), nil, &result); err != nil {
			return err
		}
		if result.Data.Status == 2 {
			return nil
		}
		if result.Data.Status == 3 || !telegramPause(ctx, 2*time.Second) {
			return errors.New("夸克转存未确认完成")
		}
	}
	return errors.New("夸克转存确认超时，请核对云盘")
}

func (a *App) shareLiveMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			for _, link := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
				if link.Status != "running" && link.Intent == "" && !link.NextRun.IsZero() && !now.Before(link.NextRun) {
					_ = a.startShareLive(link.ID, "cron")
				}
			}
		}
	}
}

func shareSavedName(p sharePreview) string {
	if p.SaveDirectory != nil && *p.SaveDirectory != "" {
		return *p.SaveDirectory
	}
	return shareRootDirectory(p.Items)
}

func (a *App) resolveShareDestination(ctx context.Context, s Storage, p *sharePreview) {
	name := shareSavedName(*p)
	if name == "" || p.Parent == "" || p.Destination != "" || (s.Type != "mobile" && s.Type != "quark") {
		return
	}
	matched := false
	for _, target := range a.store.snapshotWithLogLimit(0).Notifications.Targets {
		parent := target.Parent
		if parent == "/" {
			parent = rootOf(s)
		}
		if target.StorageID == s.ID && target.Provider == s.Type && parent == p.Parent && target.shares() {
			matched = true
			break
		}
	}
	if !matched {
		return
	}
	files, err := a.rawList(ctx, s, p.Parent)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.IsDir && f.Name == name {
			p.Destination = f.ID
			return
		}
	}
}
