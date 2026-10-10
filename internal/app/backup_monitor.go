package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Like Nestify, local filesystem monitoring compares metadata every five seconds.
func backupFingerprint(ctx context.Context, r BackupRule, st State) (string, error) {
	h := sha256.New()
	count := 0
	sources, targets := backupLocations(r)
	if r.SyncMode == "two_way" {
		sources = append(append([]BackupLocation{}, sources...), targets...)
	}
	for _, loc := range sources {
		s, err := backupStorage(st, loc.StorageID)
		if err != nil {
			return "", err
		}
		if s.Type != "local" {
			return "", fmt.Errorf("监听仅支持本地目录")
		}
		rel, err := relative(loc.Path)
		if err != nil {
			return "", err
		}
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s:%s:%v;", s.ID, rel, s.Config)
		err = fs.WalkDir(root.FS(), rel, func(p string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			count++
			if count > 100000 {
				return fmt.Errorf("监听目录超过100000项")
			}
			if strings.HasPrefix(d.Name(), ".aether-") {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				fmt.Fprintf(h, "dir:%s;", p)
			} else {
				fmt.Fprintf(h, "%s:%d:%d;", p, info.Size(), info.ModTime().UnixNano())
			}
			return nil
		})
		root.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) backupMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	type observation struct{ baseline, pending string }
	seen := map[string]observation{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			st := a.store.snapshotWithLogLimit(0)
			active := map[string]bool{}
			for _, r := range st.BackupRules {
				if !r.Enabled || !r.MonitorEnabled {
					continue
				}
				active[r.ID] = true
				ctx, cancel := context.WithTimeout(a.ctx, 4*time.Second)
				digest, err := backupFingerprint(ctx, r, st)
				cancel()
				if err != nil {
					continue
				}
				old, exists := seen[r.ID]
				if !exists {
					seen[r.ID] = observation{digest, ""}
					continue
				}
				if r.Status == "running" {
					continue
				}
				if digest == old.baseline {
					seen[r.ID] = observation{digest, ""}
					continue
				}
				if digest != old.pending {
					old.pending = digest
					seen[r.ID] = old
					continue
				}
				if err := a.startBackupScheduled(r.ID); err == nil {
					seen[r.ID] = observation{digest, ""}
				}
			}
			for id := range seen {
				if !active[id] {
					delete(seen, id)
				}
			}
		}
	}
}
