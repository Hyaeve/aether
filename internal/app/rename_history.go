package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type RenameRecord struct {
	ID         string    `json:"id"`
	FileID     string    `json:"fileId"`
	Name       string    `json:"name"`
	NewName    string    `json:"newName"`
	IsDir      bool      `json:"isDir"`
	Modified   time.Time `json:"modified"`
	Identity   string    `json:"identity,omitempty"`
	Status     string    `json:"status,omitempty"`
	Error      string    `json:"error,omitempty"`
	previousID string
}
type RenameDirectory struct {
	ID        string         `json:"id"`
	StorageID string         `json:"storageId"`
	Source    string         `json:"source"`
	PathLabel string         `json:"pathLabel"`
	Revision  string         `json:"revision,omitempty"`
	Items     []RenameRecord `json:"items"`
}

func localRenameIdentity(root *os.Root, name string) (string, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("不支持恢复符号链接")
	}
	f, err := root.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	identity, err := renameLocalIdentity(f)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		identity += ":" + info.ModTime().UTC().Format(time.RFC3339Nano) + ":" + fmt.Sprint(info.Size())
	}
	return identity, nil
}

func makeRenameRecord(s Storage, item renameItem, identity string) RenameRecord {
	fileID := item.ID
	if s.Type == "local" || s.Type == "webdav" || s.Type == "openlist" {
		fileID = path.Join(path.Dir(item.ID), item.NewName)
		if strings.HasPrefix(item.ID, "/") {
			fileID = "/" + strings.TrimPrefix(fileID, "/")
		}
	}
	return RenameRecord{ID: id(), FileID: fileID, Name: item.Name, NewName: item.NewName, IsDir: item.IsDir, Modified: time.Now(), Identity: identity}
}

func (a *App) pathRenameIdentity(ctx context.Context, s Storage, parent string, f File) (string, error) {
	if f.IsDir {
		if f.Created.IsZero() {
			return "", nil
		}
		return "directory:" + f.Created.UTC().Format(time.RFC3339Nano), nil
	}
	digest, err := a.backupFileDigest(ctx, s, parent, f.Name)
	if err != nil {
		return "", err
	}
	value := fmt.Sprintf("%s:%d:%s:%s", digest, f.Size, f.Created.UTC().Format(time.RFC3339Nano), f.Modified.UTC().Format(time.RFC3339Nano))
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:]), nil
}

func (a *App) rememberRenames(s Storage, input renameRequest, records []RenameRecord) error {
	return a.store.update(func(st *State) error {
		directory := RenameDirectory{ID: id(), StorageID: s.ID, Source: input.Source, PathLabel: input.SourceLabel, Revision: storageRevision(s), Items: []RenameRecord{}}
		var rest []RenameDirectory
		for _, d := range st.RenameHistory {
			if d.StorageID == s.ID && d.Source == input.Source {
				if d.Revision == storageRevision(s) {
					directory = d
				}
			} else {
				rest = append(rest, d)
			}
		}
		if input.SourceLabel != "" {
			directory.PathLabel = input.SourceLabel
		}
		replaced := map[string]bool{}
		for _, record := range records {
			replaced[record.FileID] = true
			if record.previousID != "" {
				replaced[record.previousID] = true
			}
			if s.Type == "local" || s.Type == "webdav" || s.Type == "openlist" {
				replaced[path.Join(path.Dir(record.FileID), record.Name)] = true
			}
			if record.Identity != "" {
				replaced["identity:"+record.Identity] = true
			}
		}
		var entries []RenameRecord
		for _, prior := range directory.Items {
			if !replaced[prior.FileID] && !replaced["identity:"+prior.Identity] {
				entries = append(entries, prior)
			}
		}
		directory.Items = append(records, entries...)
		if len(directory.Items) > 20000 {
			directory.Items = directory.Items[:20000]
		}
		st.RenameHistory = append([]RenameDirectory{directory}, rest...)
		if len(st.RenameHistory) > 5 {
			st.RenameHistory = st.RenameHistory[:5]
		}
		return nil
	})
}

func (a *App) renameHistory(w http.ResponseWriter, r *http.Request) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if err := a.store.update(func(st *State) error {
		for i := range st.RenameHistory {
			items := []RenameRecord{}
			for _, item := range st.RenameHistory[i].Items {
				if item.Status == "" {
					items = append(items, item)
				}
			}
			st.RenameHistory[i].Items = items
		}
		return nil
	}); err != nil {
		fail(w, 500, err)
		return
	}
	directories := a.store.snapshotWithLogLimit(0).RenameHistory
	if directories == nil {
		directories = []RenameDirectory{}
	}
	for i := range directories {
		directories[i].Revision = ""
		for j := range directories[i].Items {
			directories[i].Items[j].Identity = ""
		}
	}
	jsonResponse(w, 200, directories)
}

func (a *App) restoreRenameRecord(ctx context.Context, s Storage, directory RenameDirectory, item RenameRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	currentStorage, err := a.store.storage(s.ID)
	if err != nil || storageRevision(currentStorage) != storageRevision(s) {
		return errors.New("存储配置已变化，不能恢复")
	}
	if directory.Revision != storageRevision(s) {
		return errors.New("存储配置已变化，不能恢复")
	}
	if !safeName(item.Name) || !safeName(item.NewName) {
		return errors.New("历史名称无效")
	}
	if s.Type == "local" {
		root, err := os.OpenRoot(s.Config["root"])
		if err != nil {
			return err
		}
		defer root.Close()
		from, err := relative(item.FileID)
		if err != nil {
			return err
		}
		source, err := relative(directory.Source)
		if err != nil || path.Dir(from) != path.Clean(source) || path.Base(from) != item.NewName {
			return errors.New("历史路径已失效")
		}
		info, err := root.Lstat(from)
		if err != nil || info.IsDir() != item.IsDir {
			return errors.New("项目已改名、移动或删除，无法恢复")
		}
		identity, err := localRenameIdentity(root, from)
		if err != nil {
			return err
		}
		if identity != item.Identity || identity == "" {
			return errors.New("文件身份已变化，无法恢复")
		}
		to := path.Join(path.Dir(from), item.Name)
		if _, err := root.Lstat(to); !os.IsNotExist(err) {
			return errors.New("原名称已存在或不可访问")
		}
		return root.Rename(from, to)
	}
	files, err := a.rawList(ctx, s, directory.Source)
	if err != nil {
		return err
	}
	found := false
	var current File
	for _, f := range files {
		if strings.EqualFold(f.Name, item.Name) && f.ID != item.FileID {
			return errors.New("原名称已被占用，不会覆盖")
		}
		if f.ID == item.FileID && f.Name == item.NewName && f.IsDir == item.IsDir {
			found = true
			current = f
		}
	}
	if !found {
		return errors.New("项目已改名、移动或删除，无法恢复")
	}
	if s.Type == "webdav" || s.Type == "openlist" {
		identity, err := a.pathRenameIdentity(ctx, s, directory.Source, current)
		if err != nil || identity == "" || identity != item.Identity {
			return errors.New("文件身份已变化或上游未提供可核验的目录身份，无法安全恢复")
		}
	}
	return a.cloudFileAction(ctx, s, s, fileActionRequest{Source: directory.Source, Action: "rename", IDs: []string{item.FileID}, Name: item.Name})
}

func (a *App) renameRestore(w http.ResponseWriter, r *http.Request) {
	var input struct {
		DirectoryID string   `json:"directoryId"`
		IDs         []string `json:"ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.IDs) == 0 || len(input.IDs) > 20000 {
		fail(w, 400, errors.New("请选择1至20000项"))
		return
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	var directory RenameDirectory
	for _, d := range a.store.snapshotWithLogLimit(0).RenameHistory {
		if d.ID == input.DirectoryID {
			directory = d
			break
		}
	}
	if directory.ID == "" {
		fail(w, 404, errors.New("历史目录已失效"))
		return
	}
	wanted := map[string]bool{}
	for _, sid := range input.IDs {
		if wanted[sid] {
			fail(w, 400, errors.New("重复选择"))
			return
		}
		wanted[sid] = true
	}
	for _, item := range directory.Items {
		delete(wanted, item.ID)
	}
	if len(wanted) > 0 {
		fail(w, 409, errors.New("历史条目已失效"))
		return
	}
	s, storageErr := a.store.storage(directory.StorageID)
	if storageErr == nil {
		nodes := []taskFileNode{}
		selectedIDs := map[string]bool{}
		for _, selected := range input.IDs {
			selectedIDs[selected] = true
		}
		for _, item := range directory.Items {
			if selectedIDs[item.ID] && item.Status == "" {
				nodes = append(nodes, taskFileNode{storage: s, id: item.FileID})
				nodes = append(nodes, taskFileNode{s, directory.Source, item.Name})
			}
		}
		if err := a.checkTaskFileNodes(r.Context(), nodes); err != nil {
			fail(w, 409, err)
			return
		}
	}
	results := []RenameRecord{}
	records := map[string]RenameRecord{}
	for _, record := range directory.Items {
		records[record.ID] = record
	}
	defer a.cache.clear()
	for _, sid := range input.IDs {
		item := records[sid]
		if item.Status != "" {
			results = append(results, item)
			continue
		}
		err := storageErr
		if err == nil {
			err = a.restoreRenameRecord(r.Context(), s, directory, item)
		}
		item.Status = "success"
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
		}
		records[sid] = item
		results = append(results, item)
	}
	if err := a.store.update(func(st *State) error {
		for i := range st.RenameHistory {
			if st.RenameHistory[i].ID == directory.ID {
				for j, item := range st.RenameHistory[i].Items {
					st.RenameHistory[i].Items[j] = records[item.ID]
				}
			}
		}
		return nil
	}); err != nil {
		fail(w, 500, errors.New("恢复结果保存失败，请刷新核对实际名称"))
		return
	}
	for i := range results {
		results[i].Identity = ""
	}
	a.store.event("info", "files", "历史重命名恢复完成")
	jsonResponse(w, 200, results)
}
