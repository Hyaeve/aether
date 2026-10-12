package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var errStorageHealthPaused = errors.New("关联存储健康异常，暂时停止")

func account115(s Storage) (string, error) {
	c, err := credential115(s.Config["cookie"])
	if err != nil {
		return "", err
	}
	raw := strings.Split(c.UID, "_")[0]
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return "", errors.New("115 CK 缺少有效账号 ID")
		}
	}
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == 0 {
		return "", errors.New("115 CK 缺少有效账号 ID")
	}
	return strconv.FormatUint(n, 10), nil
}

func unique115Account(s Storage, storages []Storage) error {
	if s.Type != "115" {
		return nil
	}
	account, err := account115(s)
	if err != nil {
		return err
	}
	for _, other := range storages {
		if other.Type != "115" || s.ID != "" && other.ID == s.ID {
			continue
		}
		id, err := account115(other)
		if err == nil && id == account {
			return fmt.Errorf("115 账号 %s 已添加到存储池「%s」，不能重复添加", account, other.Name)
		}
	}
	return nil
}

func storageHealthPaused(s Storage) bool {
	return s.Health != nil && s.Health.Stamp == storageRevision(s) && s.Health.State != "active"
}

func dependencyPause(st State, ids ...string) string {
	for _, id := range ids {
		if id == "" {
			continue
		}
		for _, s := range st.Storages {
			if s.ID == id && storageHealthPaused(s) {
				return "存储池「" + s.Name + "」健康异常，暂时停止"
			}
		}
	}
	return ""
}
func taskHealthPause(st State, t Task) string {
	return dependencyPause(st, t.StorageID, t.ED2KBindingID, t.CASBindingID)
}
func backupHealthPause(st State, r BackupRule) string {
	sources, targets := backupLocations(r)
	ids := []string{}
	for _, loc := range append(sources, targets...) {
		ids = append(ids, loc.StorageID)
	}
	return dependencyPause(st, ids...)
}

// Cancel active work without changing the user's enable switches or schedules.
func (a *App) pauseUnhealthyWork() {
	a.runMu.Lock()
	st := a.store.snapshotWithLogLimit(0)
	for _, t := range st.Tasks {
		if taskHealthPause(st, t) != "" && a.taskHealthCancels[t.ID] != nil {
			a.taskHealthCancels[t.ID]()
		}
	}
	a.runMu.Unlock()
	a.backupMu.Lock()
	st = a.store.snapshotWithLogLimit(0)
	for _, r := range st.BackupRules {
		if backupHealthPause(st, r) != "" && a.backupHealthCancels[r.ID] != nil {
			a.backupHealthCancels[r.ID]()
		}
	}
	a.backupMu.Unlock()
}
