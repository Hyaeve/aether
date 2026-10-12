package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type ED2KPoolConfig struct {
	StorageIDs      []string `json:"storageIds"`
	RotationMinutes int      `json:"rotationMinutes"`
	RetentionHours  int      `json:"retentionHours"`
	Directory       string   `json:"directory"`
}
type ED2KTemporary struct {
	Key       string    `json:"key"`
	StorageID string    `json:"storageId"`
	Revision  string    `json:"revision"`
	Folder    string    `json:"folder"`
	FileID    string    `json:"fileId"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	LastUsed  time.Time `json:"lastUsed"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func normalizedED2KPool(p ED2KPoolConfig) ED2KPoolConfig {
	if p.RotationMinutes == 0 {
		p.RotationMinutes = 1440
	}
	if p.RetentionHours == 0 {
		p.RetentionHours = 24
	}
	if p.Directory == "" {
		p.Directory = "/最近接收/Aether"
	}
	if p.StorageIDs == nil {
		p.StorageIDs = []string{}
	}
	return p
}
func requiredED2KPool(p ED2KPoolConfig, st State) ED2KPoolConfig {
	p = normalizedED2KPool(p)
	for _, s := range st.Storages {
		if s.Enabled && s.Type == "115" {
			ids := []string{s.ID}
			for _, sid := range p.StorageIDs {
				if sid != s.ID {
					ids = append(ids, sid)
				}
			}
			p.StorageIDs = ids
			break
		}
	}
	return p
}
func validED2KInfo(i ED2KInfo) error {
	h, e := hex.DecodeString(i.Hash)
	if e != nil || len(h) != 16 || !safeName(i.Name) || strings.ContainsAny(i.Name, "\r\n|\x00") || i.Size <= 0 || i.Size > 1<<40 {
		return errors.New("ED2K 文件名、大小或32位哈希无效")
	}
	return nil
}
func ed2kRevision(s Storage) string {
	b, _ := json.Marshal([]any{s.ID, s.Type, s.Config})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func validateED2KPoolShape(p ED2KPoolConfig) error {
	if p.RotationMinutes < 1 || p.RotationMinutes > 1440 || p.RetentionHours < 1 || p.RetentionHours > 8760 || len(p.StorageIDs) > 32 {
		return errors.New("ED2K 轮换或保活设置超出范围")
	}
	if !strings.HasPrefix(p.Directory, "/") || !extractName(strings.TrimPrefix(p.Directory, "/")) || path.Base(p.Directory) != "Aether" {
		return errors.New("秒传目录须为以 / 开头、以 /Aether 结尾的绝对路径")
	}
	seen := map[string]bool{}
	for _, sid := range p.StorageIDs {
		if sid == "" || seen[sid] {
			return errors.New("ED2K 绑定账号无效或重复")
		}
		seen[sid] = true
	}
	return nil
}
func validateED2KPool(p ED2KPoolConfig, st State) error {
	if err := validateED2KPoolShape(p); err != nil {
		return err
	}
	for _, sid := range p.StorageIDs {
		found := false
		for _, s := range st.Storages {
			if s.ID == sid && s.Enabled && s.Type == "115" {
				found = true
			}
		}
		if !found {
			return errors.New("请选择有效且不重复的115存储池")
		}
	}
	return nil
}
func (a *App) ed2kPoolSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		st := a.store.snapshotWithLogLimit(0)
		jsonResponse(w, 200, requiredED2KPool(st.ED2KPool, st))
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	var p ED2KPoolConfig
	if !decode(w, r, &p) {
		return
	}
	p = normalizedED2KPool(p)
	p.Directory = strings.TrimRight(strings.TrimSpace(p.Directory), "/")
	if path.Base(p.Directory) != "Aether" {
		p.Directory += "/Aether"
	}
	if err := a.acquireCAS(r.Context()); err != nil {
		fail(w, 408, err)
		return
	}
	defer func() { <-a.casGate }()
	if err := a.store.update(func(st *State) error {
		p = requiredED2KPool(p, *st)
		if e := validateED2KPool(p, *st); e != nil {
			return e
		}
		st.ED2KPool, st.ED2KPoolVersion = p, 1
		return nil
	}); err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, p)
}
func (a *App) selectED2KStorage(info ED2KInfo, now time.Time) (Storage, int, error) {
	if e := validED2KInfo(info); e != nil {
		return Storage{}, 0, e
	}
	st := a.store.snapshotWithLogLimit(0)
	p := requiredED2KPool(st.ED2KPool, st)
	if err := validateED2KPoolShape(p); err != nil {
		return Storage{}, 0, err
	}
	available := []Storage{}
	for _, sid := range p.StorageIDs {
		for _, s := range st.Storages {
			if s.ID == sid && s.Enabled && s.Type == "115" && storageAuthBlocked(s, now) == nil {
				available = append(available, s)
			}
		}
	}
	for _, item := range st.ED2KTemporary {
		if item.Name == info.Name && item.Size == info.Size && now.Before(item.ExpiresAt) {
			for _, s := range available {
				if item.StorageID == s.ID && item.Revision == ed2kRevision(s) && item.Key == ed2kPoolKey(s, info) {
					return s, p.RetentionHours, nil
				}
			}
		}
	}
	if len(available) == 0 {
		return Storage{}, 0, errors.New("请添加并启用115存储池")
	}
	return available[(now.Unix()/int64(p.RotationMinutes*60))%int64(len(available))], p.RetentionHours, nil
}
func ed2kPoolKey(s Storage, i ED2KInfo) string {
	b, _ := json.Marshal([]any{ed2kRevision(s), i.Name, i.Size, strings.ToLower(i.Hash)})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (a *App) ed2kStatus(w http.ResponseWriter, r *http.Request) {
	st := a.store.snapshotWithLogLimit(0)
	for i := range st.ED2KTemporary {
		st.ED2KTemporary[i].Revision = ""
	}
	jsonResponse(w, 200, map[string]any{"items": st.ED2KTemporary, "stats": st.ED2KStats})
}
func (a *App) ed2kCleanup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Keys []string `json:"keys"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Keys) == 0 || len(in.Keys) > 1000 {
		fail(w, 400, errors.New("请选择1至1000项"))
		return
	}
	n, e := a.cleanupED2K(r.Context(), in.Keys)
	if e != nil {
		jsonResponse(w, 200, map[string]any{"removed": n, "error": e.Error()})
		return
	}
	jsonResponse(w, 200, map[string]int{"removed": n})
}
func (a *App) cleanupED2K(ctx context.Context, keys []string) (int, error) {
	if e := a.acquireCAS(ctx); e != nil {
		return 0, e
	}
	defer func() { <-a.casGate }()
	selected := map[string]bool{}
	for _, k := range keys {
		if selected[k] {
			return 0, errors.New("重复选择")
		}
		selected[k] = true
	}
	items := a.store.snapshotWithLogLimit(0).ED2KTemporary
	known := map[string]bool{}
	for _, i := range items {
		known[i.Key] = true
	}
	for k := range selected {
		if !known[k] {
			return 0, errors.New("所选文件已失效，请刷新")
		}
	}
	n := 0
	for _, item := range items {
		if keys != nil && !selected[item.Key] || keys == nil && time.Now().Before(item.ExpiresAt) {
			continue
		}
		s, e := a.store.storage(item.StorageID)
		if e != nil || ed2kRevision(s) != item.Revision {
			return n, errors.New("存储配置已改变，拒绝清理旧账号文件")
		}
		files, e := a.list115(ctx, s, item.Folder)
		if e != nil {
			return n, e
		}
		found := false
		for _, f := range files {
			if f.ID == item.FileID {
				if f.IsDir || f.Name != item.Name || f.Size != item.Size {
					return n, errors.New("秒传文件身份已改变，拒绝删除")
				}
				found = true
			}
		}
		if found {
			if e = a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "delete", IDs: []string{item.FileID}}); e != nil {
				return n, e
			}
		}
		if e = os.Remove(a.ed2kRecordPath(item.Key)); e != nil && !os.IsNotExist(e) {
			return n, e
		}
		if e = a.store.update(func(st *State) error {
			for j, i := range st.ED2KTemporary {
				if i.Key == item.Key {
					st.ED2KTemporary = append(st.ED2KTemporary[:j], st.ED2KTemporary[j+1:]...)
					break
				}
			}
			return nil
		}); e != nil {
			return n, e
		}
		n++
	}
	return n, nil
}
