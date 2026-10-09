package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type searchCrumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type fileSearchResult struct {
	File
	URL    string        `json:"url,omitempty"`
	Parent string        `json:"parent"`
	Trail  []searchCrumb `json:"trail"`
}

func (a *App) fileSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 512 {
		fail(w, 400, errors.New("请输入不超过512字节的搜索名称"))
		return
	}
	s, err := a.store.storage(r.URL.Query().Get("storage"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	results, err := a.searchFiles(ctx, s, r.URL.Query().Get("path"), strings.ToLower(query))
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, results)
}

func (a *App) searchFiles(ctx context.Context, s Storage, dir, query string) ([]fileSearchResult, error) {
	if dir == "" || dir == "/" {
		dir = rootOf(s)
	}
	out := []fileSearchResult{}
	seen := map[string]bool{}
	count := 0
	var walk func(string, []searchCrumb, int) error
	walk = func(parent string, trail []searchCrumb, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 || seen[parent] {
			return errors.New("搜索遇到目录循环或层级超过128")
		}
		seen[parent] = true
		children, err := a.listFiles(ctx, s, parent, 0, false)
		if err != nil {
			return err
		}
		for _, f := range children {
			if err := ctx.Err(); err != nil {
				return err
			}
			count++
			if count > 100000 {
				return errors.New("单次深度搜索超过100000个项目，请缩小目录范围")
			}
			if strings.Contains(strings.ToLower(f.Name), query) {
				if len(out) >= 10000 {
					return errors.New("匹配结果超过10000项，请缩小搜索范围")
				}
				result := fileSearchResult{File: f, Parent: parent, Trail: append([]searchCrumb{}, trail...)}
				if !f.IsDir {
					result.URL = a.streamURL(s.ID, f.ID, f.PickCode)
				}
				out = append(out, result)
			}
			if f.IsDir {
				next := append(append([]searchCrumb{}, trail...), searchCrumb{ID: parent, Name: f.Name})
				if err := walk(f.ID, next, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(dir, nil, 0); err != nil {
		return nil, err
	}
	return out, nil
}
