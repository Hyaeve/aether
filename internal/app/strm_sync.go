package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const strmUpdate = "incremental_update"
const strmMissing = "incremental_missing"

func normalizeSTRMMode(mode string) (string, error) {
	switch mode {
	case "", "full", strmUpdate:
		return strmUpdate, nil
	case "incremental", strmMissing:
		return strmMissing, nil
	default:
		return "", errors.New("STRM生成方式须为增量更新或查漏补缺")
	}
}

type strmSyncManifest struct {
	Root           string                    `json:"root"`
	RootIdentity   string                    `json:"rootIdentity,omitempty"`
	Scope          string                    `json:"scope"`
	Token          string                    `json:"secret"`
	Files          map[string]string         `json:"files"`
	Sources        map[string]strmLifeSource `json:"sources,omitempty"`
	Life           strmLifeCursor            `json:"life,omitempty"`
	PendingCascade []string                  `json:"pendingCascade,omitempty"`
}

type strmTaskSync struct {
	mu                        sync.Mutex
	app                       *App
	task                      Task
	storage                   Storage
	module, file, prefix      string
	configStamp, storageStamp string
	manifest                  strmSyncManifest
	wanted                    map[string]bool
	root                      *os.Root
	rootInfo                  os.FileInfo
	created, updated, removed int
	trackingFull              bool
	sourcesBefore             map[string]strmLifeSource
	lifeAfterScan             *strmLifeCursor
	sourcesPending            bool
	lifeBefore                strmLifeCursor
	lifeDeleteParents         map[string]string
}

func strmTaskConfig(t Task) string {
	normalizeSTRMSource(&t)
	if mode, err := normalizeSTRMMode(t.Mode); err == nil {
		t.Mode = mode
	}
	if t.STRMSource == "life" {
		t.Mode = strmUpdate
	}
	retained := "iso"
	if t.RetainedExtensions != nil {
		retained = *t.RetainedExtensions
	}
	return archiveStamp(t.ID, t.StorageID, t.Source, t.SourceLabel, t.Target, t.Mode, t.MediaExtensions, t.MetadataExtensions,
		t.ExcludeDirs, t.ExcludeFiles, t.ExcludeTypes, retained, t.STRMSource, fmt.Sprint(t.CascadeDelete), t.CascadeExtensions, t.STRMLifeSince.UTC().Format(time.RFC3339Nano), fmt.Sprint(strmDeleteThreshold(t)))
}

func strmOwnerName(taskID string) string {
	return ".aether-strm-" + archiveStamp(taskID)[:16] + ".owner"
}

func (a *App) beginSTRMSync(t Task, s Storage, base, prefix string) (*strmTaskSync, error) {
	if t.Kind != "strm" || t.ID == "" {
		return nil, nil
	}
	var stored *Task
	for _, current := range a.store.snapshotWithLogLimit(0).Tasks {
		if current.ID == t.ID {
			copy := current
			stored = &copy
			break
		}
	}
	if stored == nil {
		return nil, nil
	}
	expected := t
	expected.Mode = stored.Mode // A confirmed reset temporarily changes only the execution mode.
	if strmTaskConfig(expected) != strmTaskConfig(*stored) {
		return nil, errors.New("STRM任务配置已改变，未开始同步")
	}
	root, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(prefix)))
	if err != nil {
		return nil, err
	}
	key := archiveStamp(t.ID)
	h := &strmTaskSync{app: a, task: t, storage: s, prefix: path.Clean(prefix),
		module: "runtime/strm-outputs/" + key, file: filepath.Join(a.dataDir, "runtime", "strm-outputs", key+".json"),
		configStamp: strmTaskConfig(*stored), storageStamp: storageRevision(s), wanted: map[string]bool{}}
	h.manifest = strmSyncManifest{Root: root, Scope: archiveStamp(s.ID, t.Source, h.storageStamp), Token: id(), Files: map[string]string{}}
	f, err := os.Open(h.file)
	if os.IsNotExist(err) {
		return h, nil
	}
	if err != nil {
		return nil, errors.New("无法读取STRM产物登记，未执行同步")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	f.Close()
	var previous strmSyncManifest
	if err != nil || len(raw) > 32<<20 || a.store.decodeJSONConfig(h.module, raw, &previous) != nil {
		return nil, errors.New("STRM产物登记完整性校验失败，未执行同步")
	}
	if len(previous.Files) > 100000 || previous.Token == "" {
		return nil, errors.New("STRM产物登记无效")
	}
	if len(previous.Sources) > 100000 || len(previous.PendingCascade) > 100000 {
		return nil, errors.New("STRM来源登记超过安全范围")
	}
	for _, source := range previous.Sources {
		if !quarkLibraryRelative(source.Relative) || !safeName(source.Name) {
			return nil, errors.New("STRM来源登记路径无效")
		}
	}
	for _, name := range previous.PendingCascade {
		if !quarkLibraryRelative(name) {
			return nil, errors.New("STRM级联清理登记路径无效")
		}
	}
	for name, digest := range previous.Files {
		if !quarkLibraryRelative(name) || len(digest) != 64 {
			return nil, errors.New("STRM产物登记路径或摘要无效")
		}
	}
	if previous.Root == root && previous.Scope == h.manifest.Scope {
		h.manifest = previous
		h.manifest.Files = maps.Clone(previous.Files)
		if h.manifest.Files == nil {
			h.manifest.Files = map[string]string{}
		}
	} else if previous.Root == root {
		h.manifest.Token = previous.Token
		h.manifest.RootIdentity = previous.RootIdentity
	}
	if h.manifest.Sources == nil {
		h.manifest.Sources = map[string]strmLifeSource{}
	}
	if t.resetGeneration {
		h.manifest.RootIdentity = ""
		h.manifest.Files = map[string]string{}
		h.manifest.Sources = map[string]strmLifeSource{}
		h.manifest.PendingCascade = nil
	}
	h.sourcesBefore = maps.Clone(h.manifest.Sources)
	return h, nil
}

func (h *strmTaskSync) rememberSource(f File, parent, relative string) {
	if h.storage.Type != "115" || f.ID == "" {
		return
	}
	if h.manifest.Sources == nil {
		h.manifest.Sources = map[string]strmLifeSource{}
	}
	if len(h.manifest.Sources) >= 100000 {
		return
	}
	if h.task.Source != "" && h.task.Source != "/" && h.task.Source != rootOf(h.storage) {
		relative = strings.TrimPrefix(relative, h.task.SourceLabel+"/")
	}
	h.manifest.Sources[f.ID] = strmLifeSource{Relative: relative, Parent: parent, Name: f.Name, Directory: f.IsDir}
}

func (h *strmTaskSync) relative(filename string) (string, error) {
	name := filename
	if h.prefix != "." && h.prefix != "" {
		if !strings.HasPrefix(filename, h.prefix+"/") {
			return "", errors.New("STRM产物不属于本任务输出范围")
		}
		name = strings.TrimPrefix(filename, h.prefix+"/")
	}
	if !quarkLibraryRelative(name) {
		return "", errors.New("STRM产物路径无效")
	}
	return name, nil
}

func (h *strmTaskSync) observe(filename string) error {
	name, err := h.relative(filename)
	if err == nil {
		h.wanted[name] = true
	}
	return err
}

func (h *strmTaskSync) check(ctx context.Context) error {
	return h.checkRoot(ctx, h.manifest.RootIdentity)
}

func (h *strmTaskSync) checkRoot(ctx context.Context, expectedIdentity string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.app.store.mu.RLock()
	found := false
	for _, task := range h.app.store.state.Tasks {
		if task.ID == h.task.ID && strmTaskConfig(task) == h.configStamp {
			found = true
		}
	}
	storageFound := false
	for _, current := range h.app.store.state.Storages {
		if current.ID == h.storage.ID && storageRevision(current) == h.storageStamp {
			storageFound = true
		}
	}
	h.app.store.mu.RUnlock()
	if !found || !storageFound {
		return errors.New("STRM任务或存储配置已改变，同步停止")
	}
	if h.root != nil {
		live, err := openStrmReplaceRoot(h.manifest.Root)
		if err != nil {
			return errors.New("STRM生成目录已移动或替换")
		}
		defer live.Close()
		info, err := live.Stat(".")
		identity, identityErr := strmRootIdentity(live)
		if err != nil || !os.SameFile(h.rootInfo, info) || identityErr != nil || identity != expectedIdentity {
			return errors.New("STRM生成目录身份已改变")
		}
	}
	return nil
}

func strmReadOwner(root *os.Root, name string) (string, error) {
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("STRM目录归属标记缺失或不是普通文件")
	}
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 128))
	return string(raw), err
}

func (h *strmTaskSync) ensure(ctx context.Context, base *os.Root) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.check(ctx); err != nil {
		return err
	}
	if h.root != nil {
		return nil
	}
	var err error
	if base == nil {
		h.root, err = openStrmReplaceRoot(h.manifest.Root)
	} else {
		h.root, err = virtualOutputRoot(base, h.prefix)
	}
	if err != nil {
		return err
	}
	h.rootInfo, err = h.root.Stat(".")
	if err != nil {
		return err
	}
	return h.bindRootIdentity(ctx)
}

func strmOutputDigest(root *os.Root, filename string) (string, os.FileInfo, error) {
	parent, err := quarkLibraryParent(root, filename)
	if err != nil {
		return "", nil, err
	}
	defer parent.Close()
	name := path.Base(filename)
	before, err := parent.Lstat(name)
	if err != nil {
		return "", nil, err
	}
	if !before.Mode().IsRegular() {
		return "", nil, errors.New("STRM产物目标不是普通文件")
	}
	f, err := parent.Open(name)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(before, info) {
		return "", nil, errors.New("读取STRM产物期间文件已改变")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, (64<<20)+1))
	if err != nil || n > 64<<20 {
		return "", nil, errors.New("STRM产物读取失败或超过64MiB")
	}
	return hex.EncodeToString(hash.Sum(nil)), before, nil
}

func (h *strmTaskSync) save() error {
	manifest := h.manifest
	if h.sourcesPending {
		manifest.Sources, manifest.Life = h.sourcesBefore, h.lifeBefore
	}
	raw, err := h.app.store.encodeJSONConfig(h.module, manifest)
	if err != nil {
		return err
	}
	if len(raw) > 32<<20 {
		return errors.New("STRM产物登记超过32MiB")
	}
	if err := os.MkdirAll(filepath.Dir(h.file), 0700); err != nil {
		return err
	}
	return durableConfigWrite(h.file, raw)
}

// Only written files become owned. Existing identical or unrelated files are not adopted.
func publishSTRMOutput(ctx context.Context, h *strmTaskSync, root *os.Root, filename string, data []byte, missing bool) (bool, error) {
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	before, identity, err := strmOutputDigest(root, filename)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if exists && (missing || before == digest) {
		return false, nil
	}
	relative := ""
	track := true
	if h != nil {
		relative, err = h.relative(filename)
		if err != nil {
			return false, err
		}
		h.mu.Lock()
		old := h.manifest.Files[relative]
		h.mu.Unlock()
		if exists && old != "" && old != before {
			h.app.store.event("warn", "tasks", "STRM任务「"+h.task.Name+"」保留外部修改的产物："+relative)
			return false, nil
		}
		if err := recordSTRMLifeWork(ctx); err != nil {
			return false, err
		}
		if err := h.ensure(ctx, root); err != nil {
			return false, err
		}
	}
	if err := recordSTRMLifeWork(ctx); err != nil {
		return false, err
	}
	parent, err := virtualOutputRoot(root, path.Dir(filename))
	if err != nil {
		return false, err
	}
	defer parent.Close()
	temp := ".aether-strm-" + id()
	f, err := parent.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer parent.Remove(temp)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err = errors.Join(err, closeErr, ctx.Err()); err != nil {
		return false, err
	}
	if h != nil {
		if err := h.check(ctx); err != nil {
			return false, err
		}
	}
	live, err := quarkLibraryParent(root, filename)
	if err != nil {
		return false, err
	}
	left, leftErr := parent.Stat(".")
	right, rightErr := live.Stat(".")
	live.Close()
	if leftErr != nil || rightErr != nil || !os.SameFile(left, right) {
		return false, errors.New("STRM产物目录在发布前已改变")
	}
	if exists {
		current, currentID, err := strmOutputDigest(root, filename)
		if err != nil || current != before || !os.SameFile(identity, currentID) {
			return false, errors.New("STRM产物在发布前已改变，未覆盖")
		}
		err = parent.Rename(temp, path.Base(filename))
	} else {
		err = parent.Link(temp, path.Base(filename))
	}
	if err != nil {
		return false, err
	}
	if h != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, known := h.manifest.Files[relative]; !known && len(h.manifest.Files) >= 100000 {
			track = false
			if !h.trackingFull {
				h.trackingFull = true
				h.app.store.event("warn", "tasks", "STRM任务「"+h.task.Name+"」产物登记达到100000项，继续生成；超出部分不自动清理")
			}
		}
		if track {
			h.manifest.Files[relative] = digest
		}
		if exists {
			h.updated++
		} else {
			h.created++
		}
	}
	return true, nil
}

func (h *strmTaskSync) freshOutputs(ctx context.Context) (map[string]bool, error) {
	sourceRel := ""
	if h.task.Source != "" && h.task.Source != "/" && h.task.Source != rootOf(h.storage) {
		sourceRel = h.task.SourceLabel
		if h.storage.Type == "local" || h.storage.Type == "openlist" || h.storage.Type == "webdav" {
			sourceRel = path.Base(strings.TrimRight(h.task.Source, "/"))
		}
	}
	plan, _, err := h.app.scanTaskProgress(ctx, h.task, h.storage, sourceRel, true)
	if err != nil {
		return nil, err
	}
	_, target, err := h.app.outputLocation(h.task.Target)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	visited := map[string]bool{}
	var walk func(string, string) error
	walk = func(dir, rel string) error {
		if visited[dir] {
			return errors.New("源目录重新核验发现循环，未执行清理")
		}
		visited[dir] = true
		for _, f := range plan[dir] {
			child := path.Join(rel, f.Name)
			if !safeName(f.Name) {
				continue
			}
			if f.IsDir {
				if !excluded(child, h.task.ExcludeDirs) {
					if err := walk(f.ID, child); err != nil {
						return err
					}
				}
				continue
			}
			if excluded(f.Name, h.task.ExcludeFiles) || excludedType(f.Name, h.task.ExcludeTypes) {
				continue
			}
			name := path.Join(target, child)
			if taskMedia(h.task, f.Name) {
				name = path.Join(target, taskOutputName(h.task, h.storage, child, ".strm"))
			} else if !excludedType(f.Name, h.task.MetadataExtensions) {
				continue
			}
			relative, err := h.relative(name)
			if err != nil {
				return err
			}
			wanted[relative] = true
		}
		return nil
	}
	err = walk(h.task.Source, sourceRel)
	return wanted, err
}

func (h *strmTaskSync) finish(ctx context.Context, completed bool) error {
	defer func() {
		if h.root != nil {
			h.root.Close()
		}
	}()
	defer func() {
		h.app.store.event("info", "tasks", fmt.Sprintf("STRM任务「%s」：新增 %d，更新 %d，清理 %d；未登记或外改文件保留", h.task.Name, h.created, h.updated, h.removed))
	}()
	deferSave := func(err error) error {
		if err != nil && h.storage.Type == "115" {
			h.manifest.Sources = maps.Clone(h.sourcesBefore)
			h.manifest.Life = h.lifeBefore
		}
		return errors.Join(err, h.save())
	}
	legacyMarker := false
	if completed && h.root == nil && len(h.manifest.Files) == 0 {
		_, err := os.Lstat(filepath.Join(h.manifest.Root, strmOwnerName(h.task.ID)))
		legacyMarker = err == nil || !os.IsNotExist(err)
	}
	if completed && (len(h.manifest.Files) > 0 || legacyMarker) {
		if err := h.ensure(ctx, nil); err != nil && !os.IsNotExist(err) {
			return deferSave(err)
		}
	}
	if !completed || h.task.Mode != strmUpdate {
		if !completed {
			h.manifest.Sources = maps.Clone(h.sourcesBefore)
		}
		return deferSave(nil)
	}
	h.sourcesPending = false
	stale := []string{}
	for name := range h.manifest.Files {
		if !h.wanted[name] {
			stale = append(stale, name)
		}
	}
	if len(stale) == 0 {
		err := h.deleteOutputs(ctx, nil)
		if err == nil && h.lifeAfterScan != nil {
			h.manifest.Life = *h.lifeAfterScan
		}
		return deferSave(err)
	}
	if err := h.cleanupScopes(); err != nil {
		return deferSave(err)
	}
	ownOutput, err := taskLocalPath(Storage{Type: "local", Config: map[string]string{"root": h.manifest.Root}}, "/")
	if err != nil {
		return deferSave(err)
	}
	for _, other := range h.app.store.snapshotWithLogLimit(0).Tasks {
		if other.ID == h.task.ID || other.Kind == "cache" {
			continue
		}
		pool, err := h.app.store.storage(other.StorageID)
		if err != nil {
			return deferSave(err)
		}
		nodes, err := h.app.taskRunNodes(other, pool)
		if err != nil || len(nodes) < 2 {
			return deferSave(errors.New("其他任务输出范围无法核验，未执行同步清理"))
		}
		output, err := taskLocalPath(nodes[1].storage, nodes[1].id)
		if err != nil || pathOverlaps(output, ownOutput) {
			return deferSave(errors.New("生成目录与其他任务重叠，未执行同步清理"))
		}
	}
	// Never infer a source deletion solely from a directory-cache snapshot.
	fresh, err := h.freshOutputs(ctx)
	if err != nil {
		return deferSave(fmt.Errorf("源目录重新核验失败，未清理：%w", err))
	}
	removedNames := []string{}
	for _, name := range stale {
		if fresh[name] {
			continue
		}
		removedNames = append(removedNames, name)
	}
	err = h.deleteOutputs(ctx, removedNames)
	if err == nil && h.lifeAfterScan != nil {
		h.manifest.Life = *h.lifeAfterScan
	}
	return deferSave(err)
}

func (a *App) readSTRMMetadata(ctx context.Context, s Storage, f File) ([]byte, error) {
	download, err := a.download(ctx, s, f.ID, f.PickCode)
	if err != nil {
		return nil, err
	}
	var input io.ReadCloser
	if s.Type == "local" {
		source, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return nil, err
		}
		input, err = source.Open(download.Local)
		source.Close()
		if err != nil {
			return nil, err
		}
	} else {
		input = &davFile{ctx: ctx, info: davInfo{f}, download: download}
	}
	defer input.Close()
	body, err := io.ReadAll(io.LimitReader(&cancelReader{ctx, input}, (64<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 64<<20 || f.Size >= 0 && int64(len(body)) != f.Size {
		return nil, errors.New("STRM元数据超过64MiB或下载不完整，未发布")
	}
	return body, nil
}
