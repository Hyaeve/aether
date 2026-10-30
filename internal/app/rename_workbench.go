package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type renameRuleSet struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Rules []renameRule `json:"rules"`
}

type renameItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	NewName string `json:"newName"`
	IsDir   bool   `json:"isDir"`
	Error   string `json:"error,omitempty"`
}

type renameRequest struct {
	StorageID   string       `json:"storageId"`
	Source      string       `json:"source"`
	SourceLabel string       `json:"sourceLabel,omitempty"`
	IDs         []string     `json:"ids"`
	Rules       []renameRule `json:"rules"`
	Expected    []renameItem `json:"expected"`
}

func renamePlan(files []File, input renameRequest) ([]renameItem, error) {
	if len(input.IDs) == 0 || len(input.IDs) > 20000 {
		return nil, errors.New("请选择 1–20000 个项目")
	}
	rules, err := compileRenameRules(input.Rules)
	if err != nil {
		return nil, err
	}
	byID := map[string]File{}
	occupied := map[string]string{}
	for _, f := range files {
		byID[f.ID] = f
		occupied[strings.ToLower(f.Name)] = f.ID
	}
	seen, targets := map[string]bool{}, map[string]int{}
	items := make([]renameItem, 0, len(input.IDs))
	for _, id := range input.IDs {
		f, ok := byID[id]
		if !ok || seen[id] || f.Name == ".aether-trash" || f.Name == "." || f.Name == ".." {
			return nil, errors.New("目录已变化或重复选择，请刷新后重试")
		}
		seen[id] = true
		name, err := rules.apply(f.Name, len(items), f.IsDir)
		item := renameItem{ID: id, Name: f.Name, NewName: name, IsDir: f.IsDir}
		if err != nil {
			item.Error = err.Error()
		} else {
			key := strings.ToLower(name)
			if other, exists := occupied[key]; exists && other != id {
				item.Error = "名称已存在，不覆盖已有项目"
			}
			if prev, exists := targets[key]; exists {
				item.Error = "多个项目生成相同名称"
				items[prev].Error = item.Error
			}
			targets[key] = len(items)
		}
		items = append(items, item)
	}
	return items, nil
}

func (a *App) renameWorkbench(w http.ResponseWriter, r *http.Request) {
	var input renameRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(w, 400, errors.New("重命名请求格式错误或超过 16MB"))
		return
	}
	if len(input.SourceLabel) > 4096 || len(input.Source) > 4096 {
		fail(w, 400, errors.New("目录路径过长"))
		return
	}
	s, err := a.store.storage(input.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	execute := r.URL.Path == "/api/files/rename"
	if execute {
		a.runMu.Lock()
		defer a.runMu.Unlock()
	}
	if input.Source == "" || input.Source == "/" {
		input.Source = rootOf(s)
	}
	files, err := a.listFiles(r.Context(), s, input.Source, 0, execute)
	if err != nil {
		fail(w, 400, err)
		return
	}
	items, err := renamePlan(files, input)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !execute {
		jsonResponse(w, 200, items)
		return
	}
	if len(input.Expected) != len(items) {
		fail(w, 409, errors.New("请先预览最新规则"))
		return
	}
	for i, item := range items {
		expected := input.Expected[i]
		if item.Error != "" || expected.ID != item.ID || expected.Name != item.Name || expected.NewName != item.NewName {
			fail(w, 409, errors.New("预览已失效或名称冲突，请重新预览"))
			return
		}
	}
	nodes := make([]taskFileNode, 0, len(items)*2)
	for _, item := range items {
		nodes = append(nodes, taskFileNode{storage: s, id: item.ID}, taskFileNode{s, input.Source, item.NewName})
	}
	if err := a.checkTaskFileNodes(r.Context(), nodes); err != nil {
		fail(w, 409, err)
		return
	}
	var root *os.Root
	if s.Type == "local" {
		root, err = os.OpenRoot(s.Config["root"])
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer root.Close()
	}
	done := 0
	renamedItems := []map[string]string{}
	history := []RenameRecord{}
	defer a.cache.clear()
	for _, item := range items {
		var renamed File
		if item.Name == item.NewName {
			continue
		}
		if err = r.Context().Err(); err != nil {
			break
		}
		identity := ""
		if s.Type == "webdav" || s.Type == "openlist" {
			for _, f := range files {
				if f.ID == item.ID {
					identity, err = a.pathRenameIdentity(r.Context(), s, input.Source, f)
					break
				}
			}
			if err != nil {
				break
			}
		}
		if root != nil {
			var from string
			from, err = relative(item.ID)
			if err == nil {
				identity, err = localRenameIdentity(root, from)
			}
			if err == nil {
				to := path.Join(path.Dir(from), item.NewName)
				if _, check := root.Lstat(to); !os.IsNotExist(check) {
					err = errors.New("目标已存在或不可访问")
				} else {
					err = root.Rename(from, to)
				}
			}
		} else {
			err = a.cloudFileAction(r.Context(), s, s, fileActionRequest{Source: input.Source, Action: "rename", IDs: []string{item.ID}, Name: item.NewName, renamed: &renamed})
		}
		if err != nil {
			break
		}
		done++
		newID := item.ID
		if root != nil {
			newID = path.Join(path.Dir(item.ID), item.NewName)
		} else if renamed.ID != "" {
			newID = renamed.ID
		} else if s.Type == "webdav" || s.Type == "openlist" {
			newID = path.Join(path.Dir(item.ID), item.NewName)
		}
		renamedItems = append(renamedItems, map[string]string{"id": item.ID, "newId": newID, "name": item.NewName, "pickCode": renamed.PickCode})
		record := makeRenameRecord(s, item, identity)
		if renamed.ID != "" {
			record.FileID = renamed.ID
			record.previousID = item.ID
		}
		history = append(history, record)
	}
	if len(history) > 0 {
		if historyErr := a.rememberRenames(s, input, history); historyErr != nil {
			err = errors.New("已重命名，但历史保存失败，请核对实际名称")
		}
	}
	message := fmt.Sprintf("批量重命名完成 %d 项", done)
	if err != nil {
		a.store.event("error", "files", message+"，后续失败")
		jsonResponse(w, 200, map[string]any{"processed": done, "renamed": renamedItems, "error": message + "：" + err.Error()})
		return
	}
	a.store.event("info", "files", message)
	jsonResponse(w, 200, map[string]any{"processed": done, "renamed": renamedItems})
}

func (a *App) renameRuleSets(w http.ResponseWriter, r *http.Request) {
	var input renameRuleSet
	if r.Method != "GET" && !decode(w, r, &input) {
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	filename := filepath.Join(a.store.dir, "organize", "rename-rules.json")
	sets := []renameRuleSet{}
	if stat, err := os.Lstat(filename); err == nil && !stat.Mode().IsRegular() {
		fail(w, 400, errors.New("规则集配置不是普通文件"))
		return
	}
	data, err := os.ReadFile(filename)
	if err == nil {
		err = json.Unmarshal(data, &sets)
	}
	if err != nil && !os.IsNotExist(err) {
		fail(w, 400, errors.New("规则集配置读取失败"))
		return
	}
	switch r.Method {
	case "GET":
		jsonResponse(w, 200, sets)
		return
	case "POST":
		input.Name = strings.TrimSpace(input.Name)
		if input.Name == "" || len(input.Name) > 180 || len(sets) >= 100 {
			fail(w, 400, errors.New("请输入规则集名称，最多保存 100 个规则集"))
			return
		}
		if _, err := compileRenameRules(input.Rules); err != nil {
			fail(w, 400, err)
			return
		}
		input.ID = id()
		sets = append(sets, input)
	case "DELETE":
		found := false
		for i, set := range sets {
			if set.ID == input.ID {
				sets = append(sets[:i], sets[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			fail(w, 404, errors.New("规则集不存在"))
			return
		}
	default:
		w.WriteHeader(405)
		return
	}
	data, err = json.MarshalIndent(sets, "", "  ")
	if err == nil {
		err = atomicWrite(filename, data)
	}
	if err != nil {
		fail(w, 500, errors.New("规则集保存失败"))
		return
	}
	jsonResponse(w, 200, sets)
}
