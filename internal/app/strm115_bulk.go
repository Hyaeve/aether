package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

// Read-only bulk enumeration. It returns IDs and pickcodes, unlike a text export.
// A partial/changing subtree never becomes a deletion plan; callers retry via DFS.
func (a *App) strm115BulkPlan(ctx context.Context, s Storage, source string, checks map[string]strm115Inventory) (map[string][]File, error) {
	root := source
	if root == "" || root == "/" {
		root = rootOf(s)
	}
	if root == "0" {
		// The root has no pickcode. Enumerate its immediate children once, then
		// use bulk subtree pages for each top-level directory instead of full DFS.
		var files []File
		var err error
		if checks != nil {
			files, err = a.list115STRMDirectory(ctx, s, source)
		} else {
			files, err = a.rawList(ctx, s, source)
		}
		if err != nil {
			return nil, err
		}
		plan := map[string][]File{source: files}
		count := len(files)
		if count > 100000 {
			return nil, errTaskProgressLimit
		}
		for _, f := range files {
			if f.ID == "" || f.ID == root || !safeName(f.Name) {
				return nil, errors.New("115根目录条目身份无效")
			}
			if !f.IsDir {
				continue
			}
			child, err := a.strm115BulkPlan(ctx, s, f.ID, checks)
			if err != nil {
				return nil, err
			}
			for key, entries := range child {
				if _, duplicate := plan[key]; duplicate {
					return nil, errors.New("115根目录子树重复")
				}
				count += len(entries)
				if count > 100000 {
					return nil, errTaskProgressLimit
				}
				plan[key] = entries
			}
		}
		return plan, ctx.Err()
	}
	c, err := client115(ctx, s)
	if err != nil {
		return nil, err
	}
	if err = a.waitAPI(ctx, s.ID); err != nil {
		return nil, err
	}
	info, err := c.GetFile(root)
	if err != nil || info == nil || !info.IsDirectory || info.FileID != root || !pickcodePath.MatchString(info.PickCode) {
		return nil, errors.New("115目录身份或目录pickcode不可用")
	}
	if checks != nil {
		want, err := a.stat115Inventory(ctx, s, c, root, info.PickCode)
		if err != nil {
			return nil, fmt.Errorf("115目录 %.32q 独立统计不可用：%w", root, err)
		}
		checks[source] = want
	}
	read := func(address string, query url.Values, out any) error {
		return a.read115Inventory(ctx, s, c, address, query, out)
	}
	type node struct{ name, parent string }
	dirs := map[string]node{}
	folderAPI := "https://webapi.115.com/files/downfolders"
	for page := 1; ; page++ {
		if page > 21 {
			return nil, errTaskProgressLimit
		}
		var result struct {
			State bool `json:"state"`
			Data  struct {
				List *[]struct {
					ID     json.Number `json:"fid"`
					Name   string      `json:"fn"`
					Parent json.Number `json:"pid"`
				} `json:"list"`
				Next *bool `json:"has_next_page"`
			} `json:"data"`
		}
		query := url.Values{"pickcode": {info.PickCode}, "page": {strconv.Itoa(page)}, "per_page": {"5000"}}
		err = read(folderAPI, query, &result)
		if err == nil && (!result.State || result.Data.List == nil || result.Data.Next == nil) {
			err = errors.New("115目录表不完整")
		}
		if err != nil && page == 1 && ctx.Err() == nil {
			folderAPI = "https://proapi.115.com/app/2.0/chrome/downfolders"
			result.State, result.Data.List, result.Data.Next = false, nil, nil
			err = read(folderAPI, query, &result)
		}
		if err != nil {
			return nil, err
		}
		if !result.State || result.Data.List == nil || result.Data.Next == nil {
			return nil, errors.New("115目录表不完整")
		}
		for _, d := range *result.Data.List {
			id, parent := d.ID.String(), d.Parent.String()
			if id == root {
				continue
			}
			if id == "" || parent == "" || id == parent || !safeName(d.Name) {
				reason := "目录ID或父目录ID缺失"
				if id != "" && id == parent {
					reason = "目录与父目录ID相同"
				} else if !safeName(d.Name) {
					reason = "目录名称不适用于本地生成路径"
				}
				return nil, fmt.Errorf("115目录表条目无效（第%d页，目录ID %.32q，父目录ID %.32q，名称 %.160q）：%s", page, id, parent, d.Name, reason)
			}
			if _, exists := dirs[id]; exists {
				return nil, errors.New("115目录表分页重复")
			}
			dirs[id] = node{d.Name, parent}
		}
		if len(dirs) > 100000 {
			return nil, errTaskProgressLimit
		}
		if !*result.Data.Next {
			break
		}
		if len(*result.Data.List) == 0 {
			return nil, errors.New("115目录表分页中断")
		}
	}
	plan := map[string][]File{source: {}}
	for id, d := range dirs {
		cursor := id
		for depth := 0; cursor != root; depth++ {
			n, ok := dirs[cursor]
			if !ok || depth >= 128 {
				return nil, errors.New("115目录表祖先缺失或循环")
			}
			cursor = n.parent
		}
		parent := d.parent
		if parent == root {
			parent = source
		}
		plan[parent] = append(plan[parent], File{ID: id, Name: d.name, IsDir: true})
		if _, ok := plan[id]; !ok {
			plan[id] = []File{}
		}
	}
	seen := map[string]bool{}
	total := -1
	for offset := 0; ; {
		var result struct {
			State  bool               `json:"state"`
			CID    json.RawMessage    `json:"cid"`
			Count  json.Number        `json:"count"`
			Offset *int               `json:"offset"`
			Data   *[]driver.FileInfo `json:"data"`
		}
		if err = read("https://webapi.115.com/files", url.Values{"aid": {"1"}, "cid": {root}, "cur": {"0"}, "show_dir": {"0"}, "o": {"user_ptime"}, "asc": {"1"}, "offset": {strconv.Itoa(offset)}, "limit": {"1150"}, "format": {"json"}}, &result); err != nil {
			return nil, err
		}
		count, e := result.Count.Int64()
		if !result.State || lifeScalar(result.CID) != root || e != nil || count < 0 || result.Data == nil || result.Offset != nil && *result.Offset != offset {
			return nil, errors.New("115递归文件清单身份或分页无效")
		}
		if count > 100000 || count+int64(len(dirs)) > 100000 {
			return nil, errTaskProgressLimit
		}
		if total < 0 {
			if want, ok := checks[source]; ok {
				if err := want.verify(root, count, int64(len(dirs))); err != nil {
					return nil, err
				}
			}
			total = int(count)
		} else if total != int(count) {
			return nil, errors.New("115递归文件清单在扫描中变化")
		}
		if len(*result.Data) > 1150 || offset+len(*result.Data) > total {
			return nil, errors.New("115递归文件清单数量不符")
		}
		for _, raw := range *result.Data {
			f := (&driver.File{}).From(&raw)
			parent := f.ParentID
			if parent == root {
				parent = source
			}
			_, known := plan[parent]
			_, directoryID := dirs[f.FileID]
			if f.IsDirectory || f.FileID == "" || f.FileID == root || directoryID || seen[f.FileID] || !known || !safeName(f.Name) || !pickcodePath.MatchString(f.PickCode) {
				return nil, errors.New("115递归文件清单条目重复或归属不明")
			}
			seen[f.FileID] = true
			plan[parent] = append(plan[parent], File{ID: f.FileID, Name: f.Name, Size: f.Size, PickCode: f.PickCode, Modified: f.UpdateTime, Created: f.CreateTime})
		}
		offset += len(*result.Data)
		if offset == total {
			break
		}
		if len(*result.Data) == 0 {
			return nil, errors.New("115递归文件清单分页中断")
		}
	}
	if len(seen) != total {
		return nil, errors.New("115递归文件清单不完整")
	}
	return plan, ctx.Err()
}

func bulk115STRM(t Task, s Storage) bool {
	return s.Type == "115" && t.Kind == "strm" && strings.TrimSpace(t.Source) != ""
}
