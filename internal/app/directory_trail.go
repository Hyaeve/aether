package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Restores legacy 115 directory IDs using the existing read-only path protocol.
func (a *App) directoryTrail(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil || !s.Enabled || s.Type != "115" {
		fail(w, 400, errors.New("此存储暂无可核验的目录路径接口，请从根目录重新选择"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := backupDir(s, r.URL.Query().Get("path"))
	entries, err := a.cloud115DirectoryTrail(ctx, s, key)
	if err != nil {
		fail(w, 502, errors.New("读取完整路径失败，请从根目录重新选择"))
		return
	}
	root := rootOf(s)
	if root == "/" {
		root = "0"
	}
	start := -1
	for i, entry := range entries {
		if entry.ID == root {
			start = i
			break
		}
	}
	if start < 0 {
		fail(w, 400, errors.New("目录不在当前存储根目录内"))
		return
	}
	current, err := a.store.storage(s.ID)
	if err != nil || !current.Enabled || !sameBackupStorage(s, current) {
		fail(w, 409, errors.New("存储配置已变化，请重新选择"))
		return
	}
	entries = append([]searchCrumb{}, entries[start:]...)
	entries[0] = searchCrumb{ID: "/", Name: "根目录"}
	names := []string{}
	for _, entry := range entries[1:] {
		names = append(names, entry.Name)
	}
	jsonResponse(w, 200, map[string]any{"path": "/" + strings.Join(names, "/"), "trail": entries[:len(entries)-1], "label": entries[len(entries)-1].Name})
}

type backupAncestryKey struct{}
type backupAncestryCache map[string][]searchCrumb

func (a *App) backupPrunedSafe(ctx context.Context, s Storage, excluded string, peer Storage, directory string) error {
	if s.Type != "115" {
		return backupPrunedOverlap(s, backupScopePath(s, excluded), backupScopePath(peer, directory))
	}
	key := peer.ID + "\x00" + backupDir(peer, directory)
	cache, _ := ctx.Value(backupAncestryKey{}).(backupAncestryCache)
	trail := cache[key]
	if len(trail) == 0 {
		var err error
		trail, err = a.cloud115DirectoryTrail(ctx, peer, backupDir(peer, directory))
		if err != nil {
			return errors.New("无法核验115备份目录是否与已排除分支重叠，已停止")
		}
		if cache != nil {
			cache[key] = trail
		}
	}
	for _, entry := range trail {
		if entry.ID == excluded {
			return errors.New("备份目录相互重叠，目标位于已排除分支内")
		}
	}
	return nil
}
