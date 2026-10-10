package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

type backupEntry struct {
	source   Storage
	file     File
	relative string
	parent   string
}

func backupPrefixes(sources []BackupLocation, st State) []string {
	result := make([]string, len(sources))
	if len(sources) == 1 {
		return result
	}
	used := map[string]bool{}
	for i, loc := range sources {
		s, _ := backupStorage(st, loc.StorageID)
		name := path.Base(strings.TrimRight(loc.Label, "/"))
		if !safeName(name) || name == "." || backupDir(s, loc.Path) == rootOf(s) {
			name = s.Name
		}
		if !safeName(name) {
			name = fmt.Sprintf("源%d", i+1)
		}
		unique := name
		for n := 2; used[strings.ToLower(unique)]; n++ {
			unique = fmt.Sprintf("%s-%d", name, n)
		}
		used[strings.ToLower(unique)] = true
		result[i] = unique
	}
	return result
}

func (a *App) backupProgress(rule *BackupRule, message string) error {
	return a.backupUpdate(rule.ID, func(r *BackupRule) {
		r.Scanned, r.Copied, r.Skipped = rule.Scanned, rule.Copied, rule.Skipped
		r.Deleted = rule.Deleted
		r.Phase, r.Total, r.Processed, r.Message = rule.Phase, rule.Total, rule.Processed, message
	})
}

func (a *App) executeBackup(ctx context.Context, rule *BackupRule) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	st := a.store.snapshotWithLogLimit(0)
	if err := validateBackup(rule, st); err != nil {
		return err
	}
	if rule.SyncMode == "two_way" {
		return a.executeBackupSync(ctx, rule, st)
	}
	sources, locations := backupLocations(*rule)
	filters, err := compileBackupFilters(rule.Filters)
	if err != nil {
		return err
	}
	targets := make([]Storage, len(locations))
	for i, loc := range locations {
		targets[i], err = backupStorage(st, loc.StorageID)
		if err != nil {
			return err
		}
	}
	prefixes := backupPrefixes(sources, st)
	entries := []backupEntry{}
	visited := 0
	last := time.Time{}
	rule.Phase = "scan"
	// Complete all source scans and containment checks before any target writes.
	for i, loc := range sources {
		source, err := backupStorage(st, loc.StorageID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		var scan func(string, string, int, bool) error
		scan = func(dir, rel string, depth int, accepted bool) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if depth > 128 || seen[dir] {
				return errors.New("备份目录过深或存在循环")
			}
			for j, target := range targets {
				if backupSameTree(source, target) && dir == backupDir(target, locations[j].Path) {
					return errors.New("备份目标位于源目录内")
				}
			}
			seen[dir] = true
			files, err := a.rawList(ctx, source, dir)
			if err != nil {
				return err
			}
			for _, f := range files {
				if source.Type == "local" && strings.HasPrefix(f.Name, ".aether-") {
					continue
				}
				visited++
				if visited > 100000 {
					return errors.New("单次备份最多扫描100000项，请拆分规则")
				}
				if !safeName(f.Name) {
					return errors.New("源文件名称无效")
				}
				relative := path.Join(rel, f.Name)
				allowed := accepted && backupFilterAccept(filters, f, relative)
				if f.IsDir {
					// Even excluded branches are traversed to verify opaque-ID containment.
					if err := scan(f.ID, relative, depth+1, allowed); err != nil {
						return err
					}
					if allowed {
						entries = append(entries, backupEntry{source, f, path.Join(prefixes[i], relative), dir})
					}
				} else {
					rule.Scanned++
					if !allowed || !backupAccept(*rule, f) {
						rule.Skipped++
					} else {
						entries = append(entries, backupEntry{source, f, path.Join(prefixes[i], relative), dir})
						rule.Total += len(targets)
					}
				}
				if time.Since(last) >= time.Second {
					if err := a.backupProgress(rule, "正在扫描："+relative); err != nil {
						return err
					}
					last = time.Now()
				}
			}
			return nil
		}
		if err := scan(backupDir(source, loc.Path), "", 0, true); err != nil {
			return err
		}
		if prefixes[i] != "" {
			entries = append(entries, backupEntry{source, File{IsDir: true}, prefixes[i], ""})
		}
	}
	// Check the reverse direction for remote trees, where paths may be opaque IDs.
	for j, target := range targets {
		roots := map[string]bool{}
		for _, loc := range sources {
			source, _ := backupStorage(st, loc.StorageID)
			if source.Type != "local" && backupSameTree(source, target) {
				roots[backupDir(source, loc.Path)] = true
			}
		}
		if len(roots) == 0 {
			continue
		}
		seen := map[string]bool{}
		var check func(string, int) error
		check = func(dir string, depth int) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if depth > 128 || seen[dir] {
				return errors.New("目标目录过深或存在循环")
			}
			if roots[dir] {
				return errors.New("备份源位于目标目录内")
			}
			seen[dir] = true
			files, err := a.rawList(ctx, target, dir)
			if err != nil {
				return err
			}
			for _, f := range files {
				visited++
				if visited > 100000 {
					return errors.New("备份范围过大，请拆分规则")
				}
				if f.IsDir {
					if err := check(f.ID, depth+1); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := check(backupDir(target, locations[j].Path), 0); err != nil {
			return err
		}
	}
	rule.Phase = "copy"
	if err := a.backupProgress(rule, "正在复制文件"); err != nil {
		return err
	}
	defer a.cache.clear()
	candidates := []backupCleanupEntry{}
	for _, item := range entries {
		copiedAll := true
		fingerprint := ""
		destinations := []backupCleanupTarget{}
		for j, target := range targets {
			if err := ctx.Err(); err != nil {
				return err
			}
			current := a.store.snapshotWithLogLimit(0)
			for _, original := range []Storage{item.source, target} {
				s, err := backupStorage(current, original.ID)
				if err != nil {
					return err
				}
				if !sameBackupStorage(original, s) {
					return errors.New("备份存储配置已变更，请重新执行")
				}
			}
			if item.file.IsDir {
				if _, _, err := a.uploadParent(ctx, target, locations[j].Path, path.Join(item.relative, ".aether-directory")); err != nil {
					return err
				}
				continue
			}
			parent, name, err := a.uploadParent(ctx, target, locations[j].Path, item.relative)
			if err != nil {
				return err
			}
			existing, err := a.cloudByName(ctx, target, parent, name)
			if err != nil {
				return err
			}
			if existing != nil && existing.IsDir {
				return errors.New("目标同名项是文件夹：" + item.relative)
			}
			if existing != nil && rule.Replace == "skip" {
				copiedAll = false
				rule.Skipped++
			} else {
				digest := ""
				if err := a.copyBackupFile(ctx, item.source, target, item.file, parent, name, rule.Replace, &digest); err != nil {
					return fmt.Errorf("备份 %s：%w", item.relative, err)
				}
				if fingerprint != "" && fingerprint != digest {
					return errors.New("源文件在多个目标复制期间变化，未清理源文件")
				}
				fingerprint = digest
				destinations = append(destinations, backupCleanupTarget{target, parent, name})
				rule.Copied++
			}
			rule.Processed++
			if time.Since(last) >= time.Second || rule.Processed == rule.Total {
				if err := a.backupProgress(rule, "正在备份："+item.relative); err != nil {
					return err
				}
				last = time.Now()
			}
		}
		if !item.file.IsDir && copiedAll && fingerprint != "" {
			candidates = append(candidates, backupCleanupEntry{item, fingerprint, destinations})
		}
	}
	if rule.CompletionRule != "" && rule.CompletionRule != "keep" {
		if err := a.cleanupBackup(ctx, rule, candidates, entries); err != nil {
			return err
		}
	}
	if rule.DeletionRule != "keep" {
		return a.deleteOneWayExtras(ctx, rule, st, entries)
	}
	return nil
}
