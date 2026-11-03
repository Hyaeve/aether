package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const defaultSTRMCascadeExtensions = "jpg;jpeg;png;webp;nfo;srt;ass;ssa;sub;vtt"

type strmLifeSource struct {
	Relative  string `json:"relative"`
	Parent    string `json:"parent"`
	Name      string `json:"name"`
	Directory bool   `json:"directory"`
}

type strmLifeCursor struct {
	ID      string `json:"id,omitempty"`
	Since   int64  `json:"since,omitempty"`
	Started int64  `json:"started,omitempty"`
}

type strmLifeEvent struct {
	ID, FileID, Parent, Name, Pick string
	Type                           int
	Directory                      bool
	Time                           int64
}

type strmLifeWorkKey struct{}

func recordSTRMLifeWork(ctx context.Context) error {
	if mark, ok := ctx.Value(strmLifeWorkKey{}).(func() error); ok {
		return mark()
	}
	return nil
}

// Old tree tasks retain full-update behavior through the ordinary bulk reader.
// No text-tree export or separate tree generation strategy remains.
func normalizeSTRMSource(t *Task) {
	if t.STRMSource == "tree" {
		t.STRMSource, t.Mode = "scan", strmUpdate
	}
}

func validateSTRMSource(t Task, s Storage, _ bool) error {
	normalizeSTRMSource(&t)
	if t.DeleteThreshold != nil && *t.DeleteThreshold < 0 {
		return errors.New("删除阈值须为非负整数")
	}
	if t.STRMSource != "" && t.STRMSource != "scan" && t.STRMSource != "life" {
		return errors.New("115生成方式须为增量更新、查漏补缺或生活事件")
	}
	if t.STRMSource != "" && s.Type != "115" {
		return errors.New("115生成策略仅适用于115存储")
	}
	if t.CascadeExtensions != "" {
		if _, err := normalizeExtensions(t.CascadeExtensions); err != nil {
			return err
		}
		for _, ext := range []string{"strm", "cas", "ed2k"} {
			if excludedType("file."+ext, t.CascadeExtensions) {
				return errors.New("级联删除扩展名不能包含STRM、CAS或ED2K")
			}
		}
	}
	return nil
}

func lifeScalar(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func lifeIDCompare(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

func (a *App) pullSTRMLife(ctx context.Context, s Storage, cursor strmLifeCursor, until time.Time) ([]strmLifeEvent, strmLifeCursor, error) {
	next, events, seen := cursor, []strmLifeEvent{}, map[string]bool{}
	offset := 0
	for page := 0; page < 50; page++ {
		if err := a.waitAPI(ctx, s.ID); err != nil {
			return nil, cursor, err
		}
		var response struct {
			Data struct {
				Count json.RawMessage              `json:"count"`
				List  []map[string]json.RawMessage `json:"list"`
			} `json:"data"`
		}
		address := "https://webapi.115.com/behavior/detail?" + url.Values{"type": {""}, "limit": {"1000"}, "offset": {strconv.Itoa(offset)}}.Encode()
		if err := shareRequest(ctx, s, http.MethodGet, address, nil, &response); err != nil {
			return nil, cursor, fmt.Errorf("读取115生活事件失败，请检查115 App生活事件开关及账号授权：%w", err)
		}
		if response.Data.List == nil {
			return nil, cursor, errors.New("115生活事件响应缺少完整列表，未推进游标")
		}
		count, err := strconv.Atoi(lifeScalar(response.Data.Count))
		if err != nil || count < 0 {
			return nil, cursor, errors.New("115生活事件总量无效，未推进游标")
		}
		stop := false
		for _, row := range response.Data.List {
			event := strmLifeEvent{ID: lifeScalar(row["id"]), FileID: lifeScalar(row["file_id"]), Parent: lifeScalar(row["parent_id"]), Name: lifeScalar(row["file_name"]), Pick: lifeScalar(row["pick_code"])}
			event.Type, _ = strconv.Atoi(lifeScalar(row["type"]))
			event.Time, _ = strconv.ParseInt(lifeScalar(row["update_time"]), 10, 64)
			event.Directory = lifeScalar(row["file_category"]) == "0"
			if event.ID == "" || event.Time <= 0 {
				return nil, cursor, errors.New("115生活事件标识或时间缺失，未推进游标")
			}
			if event.Time > until.Unix() {
				continue
			}
			if event.Time < cursor.Since || cursor.ID != "" && lifeIDCompare(event.ID, cursor.ID) <= 0 {
				stop = true
				continue
			}
			if lifeIDCompare(event.ID, next.ID) > 0 {
				next.ID = event.ID
			}
			if seen[event.ID] {
				continue
			}
			seen[event.ID] = true
			switch event.Type {
			case 1, 2, 5, 6, 14, 17, 18, 20, 22, 23, 24:
				if event.FileID == "" || event.Parent == "" || !safeName(event.Name) {
					return nil, cursor, errors.New("115生活事件文件身份无效，未推进游标")
				}
				events = append(events, event)
			}
		}
		offset += len(response.Data.List)
		if stop || offset >= count {
			slices.SortFunc(events, func(a, b strmLifeEvent) int { return lifeIDCompare(a.ID, b.ID) })
			return events, next, nil
		}
		if len(response.Data.List) == 0 {
			return nil, cursor, errors.New("115生活事件分页提前结束，未推进游标")
		}
	}
	return nil, cursor, errors.New("115生活事件超过50页，未截断或推进游标；请先全量同步")
}

func (a *App) strmLifeRelative(ctx context.Context, s Storage, task Task, parent, name string) (string, bool, error) {
	trail, err := a.cloud115DirectoryTrail(ctx, s, parent)
	if err != nil {
		return "", false, err
	}
	source := task.Source
	if source == "/" || source == "" {
		source = rootOf(s)
	}
	for i, node := range trail {
		if node.ID != source {
			continue
		}
		parts := []string{}
		for _, child := range trail[i+1:] {
			parts = append(parts, child.Name)
		}
		parts = append(parts, name)
		relative := path.Join(parts...)
		if !quarkLibraryRelative(relative) {
			return "", false, errors.New("生活事件相对路径无效")
		}
		return relative, true, nil
	}
	return "", false, nil
}

func (a *App) strmFileURL(s Storage, f File) (string, error) {
	switch s.Type {
	case "local":
		return filepath.Join(s.Config["root"], filepath.FromSlash(f.ID)), nil
	case "openlist":
		return openlistSTRM(s, f.ID, a.currentSTRMSettings().Content == "encoded")
	case "webdav":
		u, err := url.Parse(strings.TrimRight(s.Config["address"], "/"))
		if err != nil || u.Host == "" {
			return "", errors.New("WebDAV地址无效")
		}
		return u.Scheme + "://" + u.Host + strmContentPath(path.Join(u.Path, s.Config["root"], f.ID), a.currentSTRMSettings().Content == "encoded"), nil
	case "115", "quark":
		return a.publicSTRMURL(s, f)
	default:
		return a.generatedStreamURL(streamClaim{Storage: s.ID, File: f.ID, Pick: f.PickCode}), nil
	}
}

func (h *strmTaskSync) lifeOutput(source strmLifeSource) string {
	if taskMedia(h.task, source.Name) {
		return taskOutputName(h.task, h.storage, source.Relative, ".strm")
	}
	return source.Relative
}

func (a *App) executeSTRMLife(ctx context.Context, task Task, s Storage) (count int, runErr error) {
	if s.Type != "115" || task.STRMSource != "life" {
		return 0, errors.New("生活事件仅支持115存储")
	}
	base, prefix, err := a.outputLocation(task.Target)
	if err != nil {
		return 0, err
	}
	if task.Source != "" && task.Source != "/" && task.Source != rootOf(s) {
		if !safeName(task.SourceLabel) {
			return 0, errors.New("请重新选择115源目录并确认名称")
		}
		prefix = path.Join(prefix, task.SourceLabel)
	}
	h, err := a.beginSTRMSync(task, s, base, prefix)
	if err != nil {
		return 0, fmt.Errorf("生活事件任务登记不可用：%w", err)
	}
	if h == nil {
		return 0, errors.New("生活事件任务登记不可用")
	}
	if h.manifest.Sources == nil {
		h.manifest.Sources = map[string]strmLifeSource{}
	}
	if h.manifest.Life.Since == 0 || !task.STRMLifeSince.IsZero() && h.manifest.Life.Started != task.STRMLifeSince.Unix() {
		h.manifest.Life = strmLifeCursor{}
		h.manifest.Life.Since = task.STRMLifeSince.Unix()
		if task.STRMLifeSince.IsZero() {
			h.manifest.Life.Since = time.Now().Unix()
		}
		h.manifest.Life.Started = h.manifest.Life.Since
	}
	beforeSources := maps.Clone(h.manifest.Sources)
	beforeCursor := h.manifest.Life
	h.sourcesBefore, h.lifeBefore, h.sourcesPending = beforeSources, beforeCursor, true
	defer func() {
		if h.root != nil {
			h.root.Close()
		}
		if runErr != nil {
			h.manifest.Sources = beforeSources
			h.manifest.Life = beforeCursor
		}
		h.sourcesPending = false
		runErr = errors.Join(runErr, h.save())
	}()
	events, next, err := a.pullSTRMLife(ctx, s, h.manifest.Life, time.Now())
	if err != nil {
		return 0, err
	}
	latest := map[string]string{}
	for _, event := range events {
		latest[event.FileID] = event.ID
	}
	events = slices.DeleteFunc(events, func(event strmLifeEvent) bool { return latest[event.FileID] != event.ID })
	deletedDirectories := map[string]string{}
	rootDeleted := false
	for _, event := range events {
		if event.Type == 22 {
			rootDeleted = rootDeleted || event.FileID == task.Source
			if source, known := h.manifest.Sources[event.FileID]; known && source.Directory {
				deletedDirectories[event.FileID] = source.Relative
			}
		}
	}
	// A folder deletion covers earlier child changes; its parent remains verifiable.
	events = slices.DeleteFunc(events, func(event strmLifeEvent) bool {
		if rootDeleted {
			return event.FileID != task.Source
		}
		parent, known := h.manifest.Sources[event.Parent]
		for id, relative := range deletedDirectories {
			if event.FileID != id && (event.Parent == id || known && (parent.Relative == relative || strings.HasPrefix(parent.Relative, relative+"/"))) {
				return true
			}
		}
		return false
	})
	if err := os.MkdirAll(base, 0755); err != nil {
		return 0, err
	}
	root, err := openStrmReplaceRoot(base)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	stale := map[string]bool{}
	removeSource := func(id string) {
		old, ok := h.manifest.Sources[id]
		if !ok {
			return
		}
		for key, item := range h.manifest.Sources {
			if key == id || old.Directory && strings.HasPrefix(item.Relative, old.Relative+"/") {
				if !item.Directory {
					stale[h.lifeOutput(item)] = true
				}
				delete(h.manifest.Sources, key)
			}
		}
	}
	visited := map[string]bool{}
	scanned := 0
	var publish func(File, string, string, int) error
	publish = func(f File, parent, relative string, depth int) error {
		if depth > 128 || scanned >= 100000 {
			return errors.New("生活事件目录展开超过安全范围，未推进游标")
		}
		scanned++
		if !safeName(f.Name) || excluded(path.Dir(relative), task.ExcludeDirs) || excluded(f.Name, task.ExcludeFiles) || excludedType(f.Name, task.ExcludeTypes) {
			return nil
		}
		if f.IsDir {
			if excluded(relative, task.ExcludeDirs) {
				return nil
			}
			if visited[f.ID] {
				return errors.New("生活事件目录出现循环")
			}
			visited[f.ID] = true
			if _, known := h.manifest.Sources[f.ID]; !known && len(h.manifest.Sources) >= 100000 {
				return errors.New("生活事件来源登记达到100000项，未推进游标")
			}
			h.manifest.Sources[f.ID] = strmLifeSource{Relative: relative, Parent: parent, Name: f.Name, Directory: true}
			children, err := a.rawList(ctx, s, f.ID)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := publish(child, f.ID, path.Join(relative, child.Name), depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		source := strmLifeSource{Relative: relative, Parent: parent, Name: f.Name}
		if !taskMedia(task, f.Name) && !excludedType(f.Name, task.MetadataExtensions) {
			return nil
		}
		if _, known := h.manifest.Sources[f.ID]; !known && len(h.manifest.Sources) >= 100000 {
			return errors.New("生活事件来源登记达到100000项，未推进游标")
		}
		name := h.lifeOutput(source)
		for id, other := range h.manifest.Sources {
			if id != f.ID && !other.Directory && (taskMedia(task, other.Name) || excludedType(other.Name, task.MetadataExtensions)) && strings.EqualFold(h.lifeOutput(other), name) {
				return errors.New("生活事件输出名称与其他源文件冲突，请全量同步核对")
			}
		}
		var data []byte
		var err error
		if taskMedia(task, f.Name) {
			var link string
			link, err = a.strmFileURL(s, f)
			data = []byte(link + "\n")
		} else {
			data, err = a.readSTRMMetadata(ctx, s, f)
		}
		if err != nil {
			return err
		}
		changed, err := publishSTRMOutput(ctx, h, root, path.Join(prefix, name), data, false)
		if err != nil {
			return err
		}
		if changed {
			count++
		}
		h.manifest.Sources[f.ID] = source
		delete(stale, name)
		return nil
	}
	for index, event := range events {
		if err := h.check(ctx); err != nil {
			return count, err
		}
		if event.Type == 22 {
			if _, known := h.manifest.Sources[event.FileID]; !known && event.FileID != task.Source {
				continue
			}
			files, err := a.rawList(ctx, s, event.Parent)
			if err != nil {
				return count, fmt.Errorf("核验生活事件删除失败，未清理：%w", err)
			}
			if slices.ContainsFunc(files, func(f File) bool { return f.ID == event.FileID }) {
				return count, errors.New("删除事件的文件仍存在，未清理或推进游标")
			}
			if h.lifeDeleteParents == nil {
				h.lifeDeleteParents = map[string]string{}
			}
			h.lifeDeleteParents[event.FileID] = event.Parent
			if event.FileID == task.Source {
				for name := range h.manifest.Files {
					stale[name] = true
				}
				clear(h.manifest.Sources)
			} else {
				removeSource(event.FileID)
			}
		} else {
			relative, inside, err := a.strmLifeRelative(ctx, s, task, event.Parent, event.Name)
			if err != nil {
				return count, fmt.Errorf("核验生活事件目录范围失败：%w", err)
			}
			if !inside {
				if _, known := h.manifest.Sources[event.FileID]; known {
					items, err := a.rawList(ctx, s, event.Parent)
					if err != nil {
						return count, err
					}
					found := slices.ContainsFunc(items, func(f File) bool { return f.ID == event.FileID })
					if !found {
						return count, errors.New("移出源目录的事件当前身份不可核验，未清理")
					}
					removeSource(event.FileID)
				}
			} else {
				files, err := a.rawList(ctx, s, event.Parent)
				if err != nil {
					return count, err
				}
				var current *File
				for _, f := range files {
					if f.ID == event.FileID {
						copy := f
						current = &copy
						break
					}
				}
				if current == nil {
					return count, errors.New("生活事件项目当前身份不可核验，请稍后重试或全量同步")
				}
				// Use the fresh item name, not an older event's rename payload.
				relative = path.Join(path.Dir(relative), current.Name)
				if excluded(path.Dir(relative), task.ExcludeDirs) || excluded(current.Name, task.ExcludeFiles) || excludedType(current.Name, task.ExcludeTypes) || current.IsDir && excluded(relative, task.ExcludeDirs) {
					removeSource(event.FileID)
					continue
				}
				if old, ok := h.manifest.Sources[event.FileID]; ok && old.Relative != relative {
					removeSource(event.FileID)
				}
				clear(visited)
				if err := publish(*current, event.Parent, relative, 0); err != nil {
					return count, err
				}
			}
		}
		a.taskUpdate(task.ID, func(t *Task) {
			t.ProgressKnown = true
			t.ProgressDone = index + 1
			t.ProgressTotal = len(events)
			t.Processed = count
			t.Message = fmt.Sprintf("生活事件 %d/%d · %s", index+1, len(events), event.Name)
		})
	}
	names := []string{}
	for name := range stale {
		names = append(names, name)
	}
	if err := h.removeLifeOutputs(ctx, names); err != nil {
		return count, err
	}
	h.manifest.Life = next
	if len(events) > 0 {
		a.store.event("info", "tasks", fmt.Sprintf("STRM任务「%s」生活事件：%d条，新增/更新%d，清理%d", task.Name, len(events), count, h.removed))
	}
	return count, nil
}

func (a *App) latestSTRMLifeCursor(ctx context.Context, s Storage, since time.Time) (strmLifeCursor, error) {
	if err := a.waitAPI(ctx, s.ID); err != nil {
		return strmLifeCursor{}, err
	}
	var response struct {
		Data struct {
			List []map[string]json.RawMessage `json:"list"`
		} `json:"data"`
	}
	if err := shareRequest(ctx, s, http.MethodGet, "https://webapi.115.com/behavior/detail?limit=1&offset=0&type=", nil, &response); err != nil {
		return strmLifeCursor{}, fmt.Errorf("全量同步前读取生活事件基线失败：%w", err)
	}
	cursor := strmLifeCursor{Since: time.Now().Unix(), Started: since.Unix()}
	if response.Data.List == nil {
		return cursor, errors.New("生活事件响应不完整，未开始全量同步")
	}
	if len(response.Data.List) > 0 {
		cursor.ID = lifeScalar(response.Data.List[0]["id"])
		if cursor.ID == "" {
			return cursor, errors.New("生活事件基线缺少标识")
		}
	}
	return cursor, nil
}

func (h *strmTaskSync) cleanupScopes() error {
	ownOutput, err := taskLocalPath(Storage{Type: "local", Config: map[string]string{"root": h.manifest.Root}}, "/")
	if err != nil {
		return err
	}
	st := h.app.store.snapshotWithLogLimit(0)
	for _, other := range st.Tasks {
		if other.ID == h.task.ID || other.Kind == "cache" {
			continue
		}
		pool, err := h.app.store.storage(other.StorageID)
		if err != nil {
			return err
		}
		nodes, err := h.app.taskRunNodes(other, pool)
		if err != nil || len(nodes) < 2 {
			return errors.New("其他任务输出范围无法核验，未执行同步清理")
		}
		output, err := taskLocalPath(nodes[1].storage, nodes[1].id)
		if err != nil || pathOverlaps(output, ownOutput) {
			return errors.New("生成目录与其他任务重叠，未执行同步清理")
		}
	}
	for _, group := range [][]quarkLibraryCard{st.QuarkLibraryCards, st.MobileLibraryCards} {
		for _, card := range group {
			if card.Directory != "" && pathOverlaps(quarkLibraryRoot(card), ownOutput) {
				return errors.New("生成目录与分享链库重叠，未执行清理")
			}
		}
	}
	return nil
}

func (h *strmTaskSync) removeLifeOutputs(ctx context.Context, names []string) error {
	return h.deleteOutputs(ctx, names)
}
