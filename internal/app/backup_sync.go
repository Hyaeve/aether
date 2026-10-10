package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type syncFile struct {
	backupEntry
	digest string
}
type syncReplica struct {
	location BackupLocation
	storage  Storage
	files    map[string]syncFile
	dirs     map[string]File
}
type syncBaseline struct {
	Revision   string              `json:"revision"`
	Files      []map[string]string `json:"files"`
	Scans      int                 `json:"scans"`
	FullScanAt time.Time           `json:"fullScanAt"`
	Metadata   []map[string]string `json:"metadata"`
	Dirs       []map[string]bool   `json:"dirs"`
	History    []syncHistory       `json:"history"`
}
type syncHistory struct {
	Side    int       `json:"side"`
	Name    string    `json:"name"`
	Digest  string    `json:"digest"`
	Created time.Time `json:"created"`
}
type syncOperation struct {
	from      int
	to        int
	name      string
	file      syncFile
	remove    bool
	directory bool
}

func syncRevision(rule BackupRule, st State) string {
	sources, targets := backupLocations(rule)
	configs := []Storage{}
	for _, loc := range append(append([]BackupLocation{}, sources...), targets...) {
		s, _ := backupStorage(st, loc.StorageID)
		configs = append(configs, Storage{ID: s.ID, Type: s.Type, Config: s.Config})
	}
	raw, _ := json.Marshal([]any{sources, targets, configs, rule.Filters, rule.Extensions, rule.Exclude, rule.MinSize, rule.MaxSize})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (a *App) syncStatePath(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(a.dataDir, "runtime", "backup-sync", hex.EncodeToString(sum[:])+".json")
}
func syncInternal(name string) bool { return strings.HasPrefix(name, ".aether-") }

func (a *App) scanSyncReplica(ctx context.Context, loc BackupLocation, s Storage, rule BackupRule, roots map[string]bool, visited *int, cached ...syncBaseline) (syncReplica, error) {
	rep := syncReplica{location: loc, storage: s, files: map[string]syncFile{}, dirs: map[string]File{}}
	filters, err := compileBackupFilters(rule.Filters)
	if err != nil {
		return rep, err
	}
	seen := map[string]bool{}
	var scan func(string, string, int, bool) error
	scan = func(dir, rel string, depth int, accept bool) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 || seen[dir] {
			return errors.New("同步目录过深或存在循环")
		}
		if depth > 0 && roots[dir] {
			return errors.New("同步目录相互重叠")
		}
		seen[dir] = true
		files, err := a.rawList(ctx, s, dir)
		if err != nil {
			return err
		}
		for _, f := range files {
			if syncInternal(f.Name) {
				continue
			}
			*visited++
			if *visited > 100000 {
				return errors.New("单次同步最多扫描100000项，请拆分规则")
			}
			if !safeName(f.Name) {
				return errors.New("同步文件名称无效")
			}
			name := path.Join(rel, f.Name)
			allowed := accept && backupFilterAccept(filters, f, name)
			if f.IsDir {
				if allowed {
					rep.dirs[name] = f
				}
				if err := scan(f.ID, name, depth+1, allowed); err != nil {
					return err
				}
			} else if allowed && backupAccept(rule, f) {
				if f.Size < 0 || f.Size > 100<<30 {
					return errors.New("同步单文件上限100GiB")
				}
				digest := ""
				if s.Type != "local" && len(cached) > 0 && len(cached[0].Files) > 0 && len(cached[0].Metadata) > 0 && !f.Modified.IsZero() && cached[0].Metadata[0][name] == syncMetadata(f) {
					digest = cached[0].Files[0][name]
				}
				var err error
				if digest == "" {
					digest, err = a.backupFileDigest(ctx, s, dir, f.Name)
				}
				if err != nil {
					return fmt.Errorf("校验 %s：%w", name, err)
				}
				rep.files[name] = syncFile{backupEntry{s, f, name, dir}, digest}
				rule.Scanned++
			}
		}
		return nil
	}
	err = scan(backupDir(s, loc.Path), "", 0, true)
	return rep, err
}
func syncMetadata(f File) string { return fmt.Sprintf("%d:%d", f.Size, f.Modified.UnixNano()) }

func chooseSyncVersion(rule BackupRule, candidates []int, replicas []syncReplica, name string) int {
	chosen := candidates[0]
	if rule.ConflictRule == "newest" {
		for _, i := range candidates[1:] {
			if replicas[i].files[name].file.Modified.After(replicas[chosen].files[name].file.Modified) {
				chosen = i
			}
		}
	}
	return chosen
}

func planSync(rule BackupRule, replicas []syncReplica, prior syncBaseline, sourceCount int) ([]syncOperation, error) {
	names := map[string]bool{}
	for _, rep := range replicas {
		for name := range rep.files {
			names[name] = true
		}
	}
	for _, files := range prior.Files {
		for name := range files {
			names[name] = true
		}
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	operations := []syncOperation{}
	for _, name := range ordered {
		present, changed := []int{}, []int{}
		deleted := false
		for i, rep := range replicas {
			old := ""
			if i < len(prior.Files) {
				old = prior.Files[i][name]
			}
			f, ok := rep.files[name]
			if ok {
				present = append(present, i)
				if old == "" || old != f.digest {
					changed = append(changed, i)
				}
			} else if old != "" {
				deleted = true
			}
		}
		if len(present) == 0 {
			continue
		}
		if rule.TargetOnly == "keep" {
			inSource := false
			for _, i := range present {
				inSource = inSource || i < sourceCount
			}
			previousSource := false
			for i := 0; i < sourceCount && i < len(prior.Files); i++ {
				previousSource = previousSource || prior.Files[i][name] != ""
			}
			if !inSource && !previousSource {
				continue
			}
		}
		if deleted && len(changed) == 0 && rule.SyncDelete && rule.DeletionRule != "keep" {
			for _, i := range present {
				operations = append(operations, syncOperation{to: i, name: name, file: replicas[i].files[name], remove: true})
			}
			continue
		}
		candidates := changed
		if len(candidates) == 0 {
			candidates = present
		}
		chosen := chooseSyncVersion(rule, candidates, replicas, name)
		winner := replicas[chosen].files[name]
		if rule.ConflictRule == "keep_both" {
			variants := map[string]bool{winner.digest: true}
			for _, i := range candidates {
				f := replicas[i].files[name]
				if variants[f.digest] {
					continue
				}
				variants[f.digest] = true
				ext := path.Ext(name)
				conflict := strings.TrimSuffix(name, ext) + " (" + rule.ConflictMarker + " " + f.digest[:12] + ")" + ext
				for j, rep := range replicas {
					if existing, ok := rep.files[conflict]; ok {
						if existing.digest != f.digest {
							return nil, errors.New("冲突副本名称已被其他内容占用")
						}
						continue
					}
					if _, ok := rep.dirs[conflict]; ok {
						return nil, errors.New("冲突副本名称被文件夹占用")
					}
					operations = append(operations, syncOperation{from: i, to: j, name: conflict, file: f})
				}
			}
		}
		for j, rep := range replicas {
			if _, ok := rep.dirs[name]; ok {
				return nil, errors.New("同步存在同名文件与文件夹冲突：" + name)
			}
			if current, ok := rep.files[name]; ok && current.digest == winner.digest {
				continue
			}
			operations = append(operations, syncOperation{from: chosen, to: j, name: name, file: winner})
		}
	}
	return operations, nil
}

func syncPlanToken(operations []syncOperation, replicas []syncReplica) string {
	values := []string{}
	for _, op := range operations {
		if op.remove {
			values = append(values, fmt.Sprintf("%d:%s:%s", op.to, op.name, op.file.digest))
		}
	}
	for i, rep := range replicas {
		keys := []string{}
		for key := range rep.files {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			values = append(values, fmt.Sprintf("%d:%s:%s", i, key, rep.files[key].digest))
		}
		dirs := []string{}
		for name := range rep.dirs {
			dirs = append(dirs, name)
		}
		sort.Strings(dirs)
		for _, name := range dirs {
			values = append(values, fmt.Sprintf("dir:%d:%s", i, name))
		}
	}
	raw, _ := json.Marshal(values)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func planSyncDirectories(rule BackupRule, replicas []syncReplica, prior syncBaseline, sourceCount int, files []syncOperation) (map[string]bool, []syncOperation, error) {
	dirs := map[string]bool{}
	for _, rep := range replicas {
		for name := range rep.dirs {
			dirs[name] = true
		}
	}
	deletes := []syncOperation{}
	for name := range dirs {
		deleted, changed, inSource, oldSource := false, false, false, false
		for i, rep := range replicas {
			_, present := rep.dirs[name]
			old := i < len(prior.Dirs) && prior.Dirs[i][name]
			deleted = deleted || old && !present
			changed = changed || present && !old
			inSource = inSource || i < sourceCount && present
			oldSource = oldSource || i < sourceCount && old
			for child, f := range rep.files {
				if strings.HasPrefix(child, name+"/") && (i >= len(prior.Files) || prior.Files[i][child] != f.digest) {
					changed = true
				}
			}
			if _, ok := rep.files[name]; ok {
				return nil, nil, errors.New("同步目录与文件名称冲突：" + name)
			}
		}
		for _, op := range files {
			if !op.remove && strings.HasPrefix(op.name, name+"/") {
				changed = true
			}
		}
		if rule.TargetOnly == "keep" && !inSource && !oldSource {
			delete(dirs, name)
			continue
		}
		if deleted && !changed && rule.SyncDelete && rule.DeletionRule != "keep" {
			delete(dirs, name)
			for i, rep := range replicas {
				if _, ok := rep.dirs[name]; ok {
					deletes = append(deletes, syncOperation{to: i, name: name, directory: true, remove: true})
				}
			}
		}
	}
	sort.Slice(deletes, func(i, j int) bool {
		if len(deletes[i].name) != len(deletes[j].name) {
			return len(deletes[i].name) > len(deletes[j].name)
		}
		if deletes[i].name != deletes[j].name {
			return deletes[i].name < deletes[j].name
		}
		return deletes[i].to < deletes[j].to
	})
	return dirs, deletes, nil
}

func (a *App) protectSyncDeletes(rule *BackupRule, operations []syncOperation, replicas []syncReplica) error {
	counts := make([]int, len(replicas))
	for _, op := range operations {
		if op.remove {
			counts[op.to]++
		}
	}
	protected := false
	for i, count := range counts {
		if count > 20 && count*100 > (len(replicas[i].files)+len(replicas[i].dirs))*rule.DeleteLimit {
			protected = true
		}
	}
	if !protected {
		return nil
	}
	token := syncPlanToken(operations, replicas)
	if rule.deleteApproval == token {
		return nil
	}
	if err := a.backupUpdate(rule.ID, func(r *BackupRule) { r.SyncPending = token }); err != nil {
		return err
	}
	return errors.New("删除数量超过保护上限，已暂停；请确认同步删除后重新扫描执行")
}

func (a *App) checkSyncStorage(replicas []syncReplica) error {
	for _, rep := range replicas {
		current, err := a.store.storage(rep.storage.ID)
		if err != nil || !current.Enabled || !sameBackupStorage(current, rep.storage) {
			return errors.New("同步存储配置变化，停止执行")
		}
	}
	return nil
}

func (a *App) syncMarker(ctx context.Context, rep syncReplica, owner string, create bool) error {
	fs := mountFS{app: a, config: MountConfig{StorageID: rep.storage.ID, Source: rep.location.Path}}
	f, err := fs.OpenFile(ctx, "/.aether-sync-owner", os.O_RDONLY, 0)
	if err == nil {
		data, e := io.ReadAll(io.LimitReader(f, 1025))
		f.Close()
		if e != nil || string(data) != owner {
			return errors.New("目录已由其他同步规则或实例占用")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !create {
		return nil
	}
	flag := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	if rep.storage.Type != "local" {
		flag |= os.O_TRUNC
		ctx = context.WithValue(ctx, mountPutLength{}, int64(len(owner)))
	}
	f, err = fs.OpenFile(ctx, "/.aether-sync-owner", flag, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(owner))
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (a *App) preserveSyncVersion(ctx context.Context, rep syncReplica, file syncFile, rule BackupRule) (string, error) {
	name := path.Join(".aether-history", rule.ID, time.Now().UTC().Format("20060102T150405.000000000"), file.relative)
	parent, base, err := a.uploadParent(ctx, rep.storage, rep.location.Path, name)
	if err != nil {
		return "", err
	}
	var digest string
	if err = a.copyBackupFile(ctx, rep.storage, rep.storage, file.file, parent, base, "skip", &digest); err != nil {
		return "", err
	}
	if digest != file.digest {
		return "", errors.New("覆盖前原文件已变化，停止同步")
	}
	return name, nil
}

func (a *App) removeSyncFile(ctx context.Context, rep syncReplica, op syncOperation, rule BackupRule) error {
	current, err := a.backupFileDigest(ctx, rep.storage, op.file.parent, op.file.file.Name)
	if err != nil || current != op.file.digest {
		return errors.New("删除前文件已变化，保留文件并停止同步")
	}
	if rule.DeletionRule == "trash" {
		parent, name, err := a.uploadParent(ctx, rep.storage, rep.location.Path, path.Join(".aether-trash", rule.ID, time.Now().UTC().Format("20060102T150405.000000000"), op.name))
		if err != nil {
			return err
		}
		var digest string
		if err = a.copyBackupFile(ctx, rep.storage, rep.storage, op.file.file, parent, name, "skip", &digest); err != nil {
			return err
		}
		if digest != op.file.digest {
			return errors.New("回收副本校验失败，取消删除")
		}
	}
	current, err = a.backupFileDigest(ctx, rep.storage, op.file.parent, op.file.file.Name)
	if err != nil || current != op.file.digest {
		return errors.New("删除提交前文件已变化，保留文件")
	}
	return a.removeSyncEntry(ctx, rep, op.name, false, rule.DeletionRule)
}

func (a *App) removeSyncEntry(ctx context.Context, rep syncReplica, name string, directory bool, policy string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.checkSyncStorage([]syncReplica{rep}); err != nil {
		return err
	}
	if rep.storage.Type == "local" {
		root, err := os.OpenRoot(rep.storage.Config["root"])
		if err != nil {
			return err
		}
		defer root.Close()
		rel, err := relative(path.Join(rep.location.Path, name))
		if err != nil || rel == "." {
			return os.ErrPermission
		}
		// Remove only a single entry; unknown or filtered directory contents are never removed recursively.
		if directory {
			f, err := root.Open(rel)
			if err != nil {
				return err
			}
			entries, err := f.Readdirnames(1)
			f.Close()
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			if len(entries) != 0 {
				return nil
			}
		}
		return root.Remove(rel)
	}
	parent, base, err := a.uploadParent(ctx, rep.storage, rep.location.Path, name)
	if err != nil {
		return err
	}
	f, err := a.cloudByName(ctx, rep.storage, parent, base)
	if err != nil || f == nil {
		return os.ErrNotExist
	}
	if f.IsDir != directory {
		return errors.New("删除目标类型已变化")
	}
	if directory {
		items, err := a.rawList(ctx, rep.storage, f.ID)
		if err != nil {
			return err
		}
		if len(items) != 0 {
			return nil
		}
	}
	s := rep.storage
	s.Config = maps.Clone(s.Config)
	if policy == "delete" {
		s.Config["deleteMode"] = "permanent"
	} else {
		s.Config["deleteMode"] = "trash"
	}
	return a.cloudFileAction(ctx, s, s, fileActionRequest{Source: parent, IDs: []string{f.ID}, Action: "delete"})
}

func (a *App) executeBackupSync(ctx context.Context, rule *BackupRule, st State) error {
	sources, targets := backupLocations(*rule)
	locations := append(append([]BackupLocation{}, sources...), targets...)
	revision := syncRevision(*rule, st)
	prior := syncBaseline{}
	if data, err := os.ReadFile(a.syncStatePath(rule.ID)); err == nil {
		if err = a.store.decodeJSONConfig("runtime/backup-sync/"+rule.ID, data, &prior); err != nil {
			return errors.New("同步基线损坏，已停止以避免错误删除")
		}
		if prior.Revision != revision || len(prior.Files) != len(locations) {
			prior = syncBaseline{}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	replicas := []syncReplica{}
	visited := 0
	full := rule.forceFullSync || len(prior.Files) == 0 || rule.FullScan && (rule.FullScanEvery > 0 && prior.Scans >= rule.FullScanEvery || rule.FullScanHours > 0 && time.Since(prior.FullScanAt) >= time.Duration(rule.FullScanHours)*time.Hour)
	for i, loc := range locations {
		s, err := backupStorage(st, loc.StorageID)
		if err != nil {
			return err
		}
		roots := map[string]bool{}
		for _, other := range locations {
			t, _ := backupStorage(st, other.StorageID)
			if backupSameTree(s, t) {
				roots[backupDir(t, other.Path)] = true
			}
		}
		cache := syncBaseline{}
		if !full && i < len(prior.Files) && i < len(prior.Metadata) {
			cache.Files = []map[string]string{prior.Files[i]}
			cache.Metadata = []map[string]string{prior.Metadata[i]}
		}
		rep, err := a.scanSyncReplica(ctx, loc, s, *rule, roots, &visited, cache)
		if err != nil {
			return err
		}
		replicas = append(replicas, rep)
	}
	operations, err := planSync(*rule, replicas, prior, len(sources))
	if err != nil {
		return err
	}
	dirs, dirDeletes, err := planSyncDirectories(*rule, replicas, prior, len(sources), operations)
	if err != nil {
		return err
	}
	operations = append(operations, dirDeletes...)
	if rule.DeletionRule == "delete" {
		for _, op := range operations {
			if op.remove && (replicas[op.to].storage.Type == "115" || replicas[op.to].storage.Type == "quark") {
				return errors.New("115/夸克不支持永久删除，请选择保留或移至回收目录")
			}
		}
	}
	if err = a.protectSyncDeletes(rule, operations, replicas); err != nil {
		return err
	}
	if err = a.checkSyncStorage(replicas); err != nil {
		return err
	}
	if rule.SyncMarker {
		owner := a.store.configMAC("backup-sync-owner", []byte(rule.ID))
		for _, rep := range replicas {
			if err = a.syncMarker(ctx, rep, hex.EncodeToString(owner), false); err != nil {
				return err
			}
		}
		for _, rep := range replicas {
			if err = a.syncMarker(ctx, rep, hex.EncodeToString(owner), true); err != nil {
				return err
			}
		}
	}
	rule.Phase = "copy"
	rule.Total = len(operations)
	if err = a.backupProgress(rule, "正在双向同步"); err != nil {
		return err
	}
	defer a.cache.clear()
	// Stage every source version before any write so cyclic changes cannot overwrite a later source.
	stageDir, err := os.MkdirTemp(filepath.Join(a.dataDir), ".aether-sync-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	staged := map[string]syncFile{}
	var stageBytes int64
	history := append([]syncHistory{}, prior.History...)
	for _, op := range operations {
		if op.remove {
			continue
		}
		key := op.file.digest
		if _, ok := staged[key]; ok {
			continue
		}
		stageBytes += op.file.file.Size
		if stageBytes > 100<<30 {
			return errors.New("单次同步暂存超过100GiB，请拆分规则")
		}
		fs := mountFS{app: a, config: MountConfig{StorageID: op.file.source.ID, Source: op.file.parent}}
		input, err := fs.OpenFile(ctx, "/"+op.file.file.Name, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		name := filepath.Join(stageDir, key)
		out, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return err
		}
		h := sha256.New()
		n, e := io.Copy(io.MultiWriter(out, h), io.LimitReader(&cancelReader{ctx, input}, op.file.file.Size+1))
		input.Close()
		closeErr := out.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if n != op.file.file.Size || hex.EncodeToString(h.Sum(nil)) != key {
			return errors.New("同步源文件已变化，停止发布")
		}
		staged[key] = syncFile{backupEntry{Storage{ID: "", Type: "local", Config: map[string]string{"root": stageDir}}, File{ID: "/" + key, Name: key, Size: n}, op.name, "/"}, key}
	}
	// Preserve empty directories without deleting filtered or unknown content.
	for name := range dirs {
		for _, rep := range replicas {
			if _, ok := rep.files[name]; ok {
				return errors.New("同步目录与文件名称冲突")
			}
			if _, _, err = a.uploadParent(ctx, rep.storage, rep.location.Path, path.Join(name, ".aether-directory")); err != nil {
				return err
			}
		}
	}
	for _, op := range operations {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = a.checkSyncStorage(replicas); err != nil {
			return err
		}
		rep := replicas[op.to]
		if op.remove {
			if op.directory {
				err = a.removeSyncEntry(ctx, rep, op.name, true, rule.DeletionRule)
			} else {
				err = a.removeSyncFile(ctx, rep, op, *rule)
			}
			if err == nil {
				rule.Deleted++
			}
		} else {
			parent, name, e := a.uploadParent(ctx, rep.storage, rep.location.Path, op.name)
			if e != nil {
				return e
			}
			old, e := a.cloudByName(ctx, rep.storage, parent, name)
			if e != nil {
				return e
			}
			if existing, ok := rep.files[op.name]; ok {
				if old == nil {
					return errors.New("同步目标在扫描后消失，停止覆盖")
				}
				digest, e := a.backupFileDigest(ctx, rep.storage, parent, name)
				if e != nil || digest != existing.digest {
					return errors.New("同步目标在扫描后变化，停止覆盖")
				}
				var historyName string
				if historyName, err = a.preserveSyncVersion(ctx, rep, existing, *rule); err != nil {
					return err
				}
				history = append(history, syncHistory{op.to, historyName, existing.digest, time.Now()})
			} else if old != nil {
				return errors.New("同步目标在扫描后出现，停止覆盖")
			}
			// Use the existing publisher with a registered source revision guard.
			stage := staged[op.file.digest]
			expected := ""
			if old, ok := rep.files[op.name]; ok {
				expected = old.digest
			}
			err = a.publishSyncStaged(ctx, rep.storage, parent, name, filepath.Join(stageDir, stage.file.Name), op.file.digest, expected)
			if err == nil {
				rule.Copied++
			}
		}
		if err != nil {
			return err
		}
		rule.Processed++
		if err = a.backupProgress(rule, "正在同步："+op.name); err != nil {
			return err
		}
	}
	// Re-scan after publication; failures never advance the deletion baseline.
	final := syncBaseline{Revision: revision, Scans: prior.Scans + 1, FullScanAt: prior.FullScanAt, History: history}
	if full {
		final.Scans = 0
		final.FullScanAt = time.Now()
	}
	visited = 0
	for i, rep := range replicas {
		next, e := a.scanSyncReplica(ctx, rep.location, rep.storage, *rule, map[string]bool{}, &visited)
		if e != nil {
			return e
		}
		files := map[string]string{}
		metadata := map[string]string{}
		for name, f := range next.files {
			files[name] = f.digest
			metadata[name] = syncMetadata(f.file)
		}
		expected := map[string]string{}
		for name, f := range rep.files {
			expected[name] = f.digest
		}
		for _, op := range operations {
			if op.to == i && !op.directory {
				if op.remove {
					delete(expected, op.name)
				} else {
					expected[op.name] = op.file.digest
				}
			}
		}
		if !maps.Equal(expected, files) {
			return errors.New("同步期间文件再次变化，未更新删除基线，请重新扫描")
		}
		dirNames := map[string]bool{}
		for name := range next.dirs {
			dirNames[name] = true
		}
		final.Dirs = append(final.Dirs, dirNames)
		final.Files = append(final.Files, files)
		final.Metadata = append(final.Metadata, metadata)
	}
	if rule.HistoryDays > 0 {
		retained := []syncHistory{}
		for _, entry := range final.History {
			if entry.Side >= len(replicas) || entry.Side < 0 || time.Since(entry.Created) < time.Duration(rule.HistoryDays)*24*time.Hour {
				retained = append(retained, entry)
				continue
			}
			rep := replicas[entry.Side]
			fs := mountFS{app: a, config: MountConfig{StorageID: rep.storage.ID, Source: rep.location.Path}}
			parent, err := fs.cloudParent(ctx, rep.storage, rep.location.Path, entry.Name)
			if rep.storage.Type == "local" {
				parent = path.Join(rep.location.Path, path.Dir(entry.Name))
				err = nil
			}
			if err != nil {
				retained = append(retained, entry)
				continue
			}
			digest, err := a.backupFileDigest(ctx, rep.storage, parent, path.Base(entry.Name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || digest != entry.Digest {
				retained = append(retained, entry)
				continue
			}
			if err = a.removeSyncEntry(ctx, rep, entry.Name, false, "delete"); err != nil {
				retained = append(retained, entry)
			}
		}
		final.History = retained
	}
	raw, err := a.store.encodeJSONConfig("runtime/backup-sync/"+rule.ID, final)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(a.syncStatePath(rule.ID)), 0700); err != nil {
		return err
	}
	if err = durableConfigWrite(a.syncStatePath(rule.ID), raw); err != nil {
		return err
	}
	return a.backupUpdate(rule.ID, func(r *BackupRule) { r.SyncPending = "" })
}

func (a *App) publishSyncStaged(ctx context.Context, target Storage, parent, name, staged, digest, expected string) error {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if len(a.running) != 0 {
		return errors.New("有任务正在执行，停止同步发布")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := a.store.storage(target.ID)
	if err != nil || !current.Enabled || !sameBackupStorage(current, target) {
		return errors.New("目标存储配置变化，停止同步发布")
	}
	old, err := a.cloudByName(ctx, target, parent, name)
	if err != nil {
		return err
	}
	if expected == "" && old != nil || expected != "" && (old == nil || old.IsDir) {
		return errors.New("发布前同步目标已变化")
	}
	if expected != "" {
		actual, err := a.backupFileDigest(ctx, target, parent, name)
		if err != nil || actual != expected {
			return errors.New("发布前同步目标内容已变化")
		}
	}
	if target.Type == "local" {
		root, err := os.OpenRoot(target.Config["root"])
		if err != nil {
			return err
		}
		defer root.Close()
		rel, err := relative(parent)
		if err != nil {
			return err
		}
		temp := path.Join(rel, ".aether-sync-"+id())
		mode := os.FileMode(0600)
		if info, err := root.Stat(path.Join(rel, name)); err == nil {
			mode = info.Mode().Perm()
		}
		out, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		defer root.Remove(temp)
		in, err := os.Open(staged)
		if err != nil {
			out.Close()
			return err
		}
		_, err = io.Copy(out, &cancelReader{ctx, in})
		in.Close()
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if expected != "" {
			actual, err := a.backupFileDigest(ctx, target, parent, name)
			if err != nil || actual != expected {
				return errors.New("原子发布前同步目标已变化")
			}
		} else if _, err := root.Stat(path.Join(rel, name)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("原子发布前目标已出现")
		}
		if err = root.Rename(temp, path.Join(rel, name)); err != nil {
			return err
		}
	} else if err := a.publishCloudFile(ctx, target, parent, name, staged); err != nil {
		return err
	}
	actual, err := a.backupFileDigest(ctx, target, parent, name)
	if err != nil || actual != digest {
		return errors.New("同步目标内容校验失败")
	}
	return nil
}

func (a *App) deleteOneWayExtras(ctx context.Context, rule *BackupRule, st State, entries []backupEntry) error {
	_, targets := backupLocations(*rule)
	allowed := map[string]bool{}
	for _, entry := range entries {
		allowed[entry.relative] = true
	}
	visited := 0
	replicas := []syncReplica{}
	ops := []syncOperation{}
	for _, loc := range targets {
		s, err := backupStorage(st, loc.StorageID)
		if err != nil {
			return err
		}
		rep, err := a.scanSyncReplica(ctx, loc, s, *rule, map[string]bool{}, &visited)
		if err != nil {
			return err
		}
		for name, f := range rep.files {
			if !allowed[name] {
				ops = append(ops, syncOperation{to: len(replicas), name: name, file: f, remove: true})
			}
		}
		replicas = append(replicas, rep)
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].to != ops[j].to {
			return ops[i].to < ops[j].to
		}
		return ops[i].name < ops[j].name
	})
	if rule.DeletionRule == "delete" {
		for _, op := range ops {
			if replicas[op.to].storage.Type == "115" || replicas[op.to].storage.Type == "quark" {
				return errors.New("115/夸克不支持永久删除，请选择保留或移至回收目录")
			}
		}
	}
	if err := a.protectSyncDeletes(rule, ops, replicas); err != nil {
		return err
	}
	for _, op := range ops {
		rep := replicas[op.to]
		var err error
		if err = a.checkSyncStorage([]syncReplica{rep}); err != nil {
			return err
		}
		if err = a.removeSyncFile(ctx, rep, op, *rule); err != nil {
			return err
		}
		rule.Deleted++
	}
	return a.backupUpdate(rule.ID, func(r *BackupRule) { r.SyncPending = "" })
}
