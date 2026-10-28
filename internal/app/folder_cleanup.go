package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"
)

type folderCleanupRule struct {
	ID        string        `json:"id"`
	StorageID string        `json:"storageId"`
	Directory string        `json:"directory"`
	Label     string        `json:"label"`
	Cron      string        `json:"cron"`
	Days      int           `json:"days"`
	Trail     []searchCrumb `json:"trail"`
}
type folderCleanupSettings struct {
	Enabled bool                `json:"enabled"`
	Rules   []folderCleanupRule `json:"rules"`
}
type folderCleanupReceipt struct {
	Done bool      `json:"done"`
	Time time.Time `json:"time"`
}

func validateFolderCleanup(value folderCleanupSettings, st State, legacy bool) error {
	if len(value.Rules) > 100 {
		return errors.New("最多添加100个清理文件夹")
	}
	seen := map[string]bool{}
	for _, rule := range value.Rules {
		if rule.ID == "" || len(rule.ID) > 128 || seen[rule.ID] || rule.Days < 0 || rule.Days > 3650 || len(rule.Cron) > 128 {
			return errors.New("文件夹清理规则无效，清理阈值须为0至3650天")
		}
		seen[rule.ID] = true
		key := "directory:" + rule.StorageID + ":" + rule.Directory
		if seen[key] {
			return errors.New("同一文件夹不能重复添加清理规则")
		}
		seen[key] = true
		var s Storage
		for _, pool := range st.Storages {
			if pool.ID == rule.StorageID {
				s = pool
			}
		}
		if !simulcastID(rule.Directory) || rule.Directory == "0" {
			return errors.New("请选择115存储的子文件夹，不能清理根目录")
		}
		// Storage can be edited independently after a rule was saved. Keep the
		// old rule readable at startup; strict execution validation pauses it.
		if !legacy && (s.Type != "115" || rule.Directory == rootOf(s)) {
			return errors.New("请选择115存储的子文件夹，不能清理根目录")
		}
		if !legacy && s.Config["deleteMode"] == "permanent" {
			return errors.New("115文件夹清理仅支持移入回收站，请先调整存储删除模式")
		}
		if len(rule.Trail) < 2 || len(rule.Trail) > 129 || rule.Trail[0].ID != "0" || rule.Trail[len(rule.Trail)-1].ID != rule.Directory {
			return errors.New("清理文件夹身份不完整，请重新选择")
		}
		parents := map[string]bool{}
		for _, parent := range rule.Trail {
			if !simulcastID(parent.ID) || parents[parent.ID] || parent.ID != "0" && !safeName(parent.Name) {
				return errors.New("清理文件夹路径无效，请重新选择")
			}
			parents[parent.ID] = true
		}
		for _, other := range value.Rules {
			if other.ID != rule.ID && other.StorageID == rule.StorageID && parents[other.Directory] {
				return errors.New("清理文件夹不能互相包含，请保留其中一条规则")
			}
		}
		parser := cronParser.Parse
		if legacy {
			parser = rawCronParser.Parse
		}
		if _, err := parser(rule.Cron); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) folderCleanupAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		jsonResponse(w, 200, a.store.snapshotWithLogLimit(0).FolderCleanup)
		return
	}
	if r.Method != http.MethodPut {
		w.WriteHeader(405)
		return
	}
	var input struct {
		folderCleanupSettings
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &input) {
		return
	}
	before := a.store.snapshotWithLogLimit(0)
	changed := !reflect.DeepEqual(before.FolderCleanup.Rules, input.Rules)
	if (changed || input.Enabled && !before.FolderCleanup.Enabled) && !input.Confirmed {
		fail(w, 400, errors.New("请二次确认文件夹清理范围和阈值"))
		return
	}
	// Resolve cloud identities before taking the configuration lock.
	for i := range input.Rules {
		rule := &input.Rules[i]
		if _, err := cronParser.Parse(rule.Cron); err != nil {
			fail(w, 400, err)
			return
		}
		if rule.ID == "" {
			rule.ID = id()
		}
		s, err := backupStorage(before, rule.StorageID)
		if err != nil || s.Type != "115" || rule.Directory == "/" || rule.Directory == "0" || rule.Directory == rootOf(s) {
			fail(w, 400, errors.New("请选择115存储的子文件夹"))
			return
		}
		if changed || input.Enabled && !before.FolderCleanup.Enabled {
			trail, err := a.cloud115DirectoryTrail(r.Context(), s, rule.Directory)
			if err != nil {
				fail(w, 400, errors.New("无法确认清理文件夹的完整身份，请重新选择"))
				return
			}
			found := rootOf(s) == "0" || rootOf(s) == "/"
			for _, parent := range trail {
				if parent.ID == rootOf(s) {
					found = true
				}
			}
			if !found {
				fail(w, 400, errors.New("清理文件夹不在存储范围内"))
				return
			}
			rule.Trail = trail
			names := []string{}
			for _, parent := range trail {
				if parent.ID != "0" {
					names = append(names, parent.Name)
				}
			}
			rule.Label = "/" + strings.Join(names, "/")
		}
	}
	if err := validateFolderCleanup(input.folderCleanupSettings, before, false); err != nil {
		fail(w, 400, err)
		return
	}
	err := a.store.update(func(st *State) error {
		if rapid115Stamp(st.FolderCleanup) != rapid115Stamp(before.FolderCleanup) {
			return errors.New("清理配置已变化，请重新打开")
		}
		for _, rule := range input.Rules {
			old, _ := backupStorage(before, rule.StorageID)
			current, e := backupStorage(*st, rule.StorageID)
			if e != nil || !sameBackupStorage(old, current) {
				return errors.New("存储配置已变化，请重新选择")
			}
		}
		st.FolderCleanup = input.folderCleanupSettings
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	a.store.event("info", "tasks", "115文件夹清理配置已保存")
	jsonResponse(w, 200, input.folderCleanupSettings)
}

type folderCleanupEntry struct {
	Parent string
	File   File
}
type folderCleanupOps struct {
	List   func(context.Context, string) ([]File, error)
	Check  func(context.Context, string) error
	Delete func(context.Context, folderCleanupEntry) error
}

func cleanFolderEntries(ctx context.Context, rule folderCleanupRule, now time.Time, ops folderCleanupOps) (int, error) {
	files, dirs := []folderCleanupEntry{}, []folderCleanupEntry{}
	seen := map[string]bool{rule.Directory: true}
	count := 0
	var scan func(string, int) error
	scan = func(parent string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 64 {
			return errors.New("清理目录层级超过64层")
		}
		if err := ops.Check(ctx, parent); err != nil {
			return err
		}
		items, err := ops.List(ctx, parent)
		if err != nil {
			return errors.New("读取清理目录失败，未提交后续清理")
		}
		for _, file := range items {
			count++
			if count > 10000 {
				return errors.New("清理目录超过10000项，请缩小范围")
			}
			if file.ID == "" || !safeName(file.Name) || seen[file.ID] {
				return errors.New("清理目录身份重复或无效")
			}
			seen[file.ID] = true
			entry := folderCleanupEntry{parent, file}
			if file.IsDir {
				dirs = append(dirs, entry)
				if err := scan(file.ID, depth+1); err != nil {
					return err
				}
			} else {
				at := file.Created
				if at.IsZero() || at.Unix() <= 0 {
					at = file.Modified
				}
				if rule.Days == 0 || !at.IsZero() && at.Unix() > 0 && at.Before(now.Add(-time.Duration(rule.Days)*24*time.Hour)) {
					files = append(files, entry)
				}
			}
		}
		return nil
	}
	// Complete enumeration must succeed before the first destructive request.
	if err := scan(rule.Directory, 0); err != nil {
		return 0, err
	}
	done := 0
	remove := func(entry folderCleanupEntry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := ops.Check(ctx, entry.Parent); err != nil {
			return err
		}
		items, err := ops.List(ctx, entry.Parent)
		if err != nil {
			return errors.New("清理前目录核验失败")
		}
		found := false
		for _, file := range items {
			same := reflect.DeepEqual(file, entry.File)
			if entry.File.IsDir {
				same = file.ID == entry.File.ID && file.IsDir && file.Name == entry.File.Name
			}
			if same {
				found = true
				break
			}
		}
		if !found {
			return errors.New("清理前文件身份或内容信息已变化，已停止")
		}
		if err := ops.Delete(ctx, entry); err != nil {
			return err
		}
		done++
		return nil
	}
	for _, entry := range files {
		if err := remove(entry); err != nil {
			return done, err
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		entry := dirs[i]
		items, err := ops.List(ctx, entry.File.ID)
		if err != nil {
			return done, errors.New("空目录核验失败")
		}
		if len(items) == 0 {
			if err := remove(entry); err != nil {
				return done, err
			}
		}
	}
	return done, nil
}

func (a *App) runFolderCleanup(ctx context.Context, rule folderCleanupRule) (int, error) {
	if !a.folderCleanupMu.TryLock() {
		return 0, errors.New("已有文件夹清理正在执行")
	}
	defer a.folderCleanupMu.Unlock()
	st := a.store.snapshotWithLogLimit(0)
	s, err := backupStorage(st, rule.StorageID)
	if err != nil {
		return 0, err
	}
	if err := validateFolderCleanup(folderCleanupSettings{Rules: []folderCleanupRule{rule}}, st, false); err != nil {
		return 0, err
	}
	account, err := account115(s)
	if err != nil {
		return 0, err
	}
	saved := false
	for _, current := range st.FolderCleanup.Rules {
		if reflect.DeepEqual(current, rule) {
			saved = true
			break
		}
	}
	if !st.FolderCleanup.Enabled || !saved {
		return 0, errors.New("文件夹清理规则已停用或变化，未执行")
	}
	check := func(ctx context.Context, parent string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := a.store.snapshotWithLogLimit(0)
		pool, e := backupStorage(current, s.ID)
		if e != nil || !sameBackupStorage(pool, s) || !current.FolderCleanup.Enabled || rapid115Stamp(current.FolderCleanup) != rapid115Stamp(st.FolderCleanup) {
			return errors.New("文件夹清理或存储配置已变化，已停止")
		}
		if err := storageAuthBlocked(pool, time.Now()); err != nil {
			return err
		}
		trail, e := a.cloud115DirectoryTrail(ctx, s, parent)
		if e != nil || len(trail) < len(rule.Trail) || !reflect.DeepEqual(trail[:min(len(trail), len(rule.Trail))], rule.Trail) {
			return errors.New("清理目录或祖先路径已变化，已停止")
		}
		return nil
	}
	remove := func(ctx context.Context, entry folderCleanupEntry) error {
		if err := check(ctx, entry.Parent); err != nil {
			return err
		}
		identity := entry
		if identity.File.IsDir {
			identity.File = File{ID: entry.File.ID, Name: entry.File.Name, IsDir: true}
		}
		key := rapid115Stamp([]any{account, rule.Directory, identity})
		claimed := false
		err := a.store.update(func(st *State) error {
			if st.FolderCleanupReceipts == nil {
				st.FolderCleanupReceipts = map[string]folderCleanupReceipt{}
			}
			if _, exists := st.FolderCleanupReceipts[key]; exists {
				return nil
			}
			for k, v := range st.FolderCleanupReceipts {
				if v.Done && time.Since(v.Time) > 30*24*time.Hour {
					delete(st.FolderCleanupReceipts, k)
				}
			}
			if len(st.FolderCleanupReceipts) >= 20000 {
				return errors.New("清理回执已达上限，请先核对历史记录")
			}
			st.FolderCleanupReceipts[key] = folderCleanupReceipt{Time: time.Now()}
			claimed = true
			return nil
		})
		if err != nil {
			return err
		}
		if !claimed {
			return errors.New("该项目存在先前清理回执，请人工核对；未重复删除")
		}
		if err := check(ctx, entry.Parent); err != nil {
			return err
		}
		err = a.cloudFileAction(ctx, s, s, fileActionRequest{Source: entry.Parent, IDs: []string{entry.File.ID}, Action: "delete", emptyOnly: entry.File.IsDir})
		if err != nil {
			if errors.Is(err, errEmptyDirectoryUnconfirmed) {
				return err
			}
			return errors.New("115清理回执未确认，已保留写入意图，未自动重试")
		}
		if err := a.store.update(func(st *State) error {
			st.FolderCleanupReceipts[key] = folderCleanupReceipt{Done: true, Time: time.Now()}
			return nil
		}); err != nil {
			return err
		}
		a.cache.clear()
		a.store.event("info", "tasks", fmt.Sprintf("115文件夹清理：%s · %s · %s，已移入回收站", s.Name, rule.Label, entry.File.Name))
		return nil
	}
	return cleanFolderEntries(ctx, rule, time.Now(), folderCleanupOps{List: func(ctx context.Context, parent string) ([]File, error) { return a.rawList(ctx, s, parent) }, Check: check, Delete: remove})
}

func (a *App) folderCleanupMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	revision := ""
	next := map[string]time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			settings := a.store.snapshotWithLogLimit(0).FolderCleanup
			key := rapid115Stamp(settings)
			if key != revision {
				revision = key
				next = map[string]time.Time{}
			}
			if !settings.Enabled {
				continue
			}
			for _, rule := range settings.Rules {
				schedule, err := cronParser.Parse(rule.Cron)
				if err != nil {
					continue
				}
				due := next[rule.ID]
				if due.IsZero() {
					next[rule.ID] = schedule.Next(now)
					continue
				}
				if now.Before(due) {
					continue
				}
				next[rule.ID] = schedule.Next(now)
				ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
				count, err := a.runFolderCleanup(ctx, rule)
				cancel()
				level, message := "info", "完成"
				if err != nil {
					level, message = "warn", err.Error()
				}
				a.store.event(level, "tasks", fmt.Sprintf("115文件夹计划清理：%s，阈值%d天，已处理%d项：%s", rule.Label, rule.Days, count, message))
			}
		}
	}
}
