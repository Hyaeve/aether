package app

import (
	"errors"
	"net/http"
	"strings"
)

type FavoriteCrumb struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FileFavorite struct {
	Storage string          `json:"storage"`
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	History []FavoriteCrumb `json:"history"`
	Deleted bool            `json:"deleted,omitempty"`
}

func visibleFavorites(items []FileFavorite) []FileFavorite {
	result := make([]FileFavorite, 0, len(items))
	for _, item := range items {
		if !item.Deleted {
			result = append(result, item)
		}
	}
	return result
}

func validFavoriteID(value string) bool {
	return value != "" && len(value) <= 4096 && !strings.ContainsAny(value, "\x00\r\n")
}

func normalizeFavorite(item *FileFavorite, storages []Storage) error {
	var pool *Storage
	for i := range storages {
		if storages[i].ID == item.Storage {
			pool = &storages[i]
			break
		}
	}
	if pool == nil || !validFavoriteID(item.ID) || item.ID == "/" || item.ID == rootOf(*pool) || !validFavoriteID(item.Name) || !safeName(item.Name) || len(item.Name) > 255 || len(item.History) == 0 || len(item.History) > 128 {
		return errors.New("收藏目录无效或存储池不存在")
	}
	for i, crumb := range item.History {
		if !validFavoriteID(crumb.ID) || !validFavoriteID(crumb.Name) || !safeName(crumb.Name) || len(crumb.Name) > 255 {
			return errors.New("收藏目录路径无效")
		}
		// Older browser arrays could include descendants after the target.
		if crumb.ID == item.ID {
			item.History = item.History[:i]
			break
		}
	}
	if len(item.History) == 0 {
		return errors.New("不能收藏根目录")
	}
	item.Deleted = false
	return nil
}

func (a *App) fileFavorites(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		jsonResponse(w, 200, visibleFavorites(a.store.snapshotWithLogLimit(0).FileFavorites))
		return
	}
	var input struct {
		Action string         `json:"action"`
		Items  []FileFavorite `json:"items"`
	}
	if !decode(w, r, &input) {
		return
	}
	if (input.Action != "add" && input.Action != "remove" && input.Action != "import") || len(input.Items) == 0 || len(input.Items) > 2000 || input.Action != "import" && len(input.Items) != 1 {
		fail(w, 400, errors.New("收藏操作无效"))
		return
	}
	var result []FileFavorite
	err := a.store.update(func(st *State) error {
		for _, item := range input.Items {
			if input.Action != "remove" {
				if err := normalizeFavorite(&item, st.Storages); err != nil {
					if input.Action == "import" {
						continue
					}
					return err
				}
			} else if !validFavoriteID(item.Storage) || !validFavoriteID(item.ID) {
				return errors.New("收藏目录无效")
			}
			index := -1
			for i, existing := range st.FileFavorites {
				if existing.Storage == item.Storage && existing.ID == item.ID {
					index = i
					break
				}
			}
			if input.Action == "import" && index >= 0 {
				continue
			}
			if input.Action == "remove" {
				if index < 0 {
					continue
				}
				// Keep a compact tombstone so a stale browser migration cannot undo removal.
				st.FileFavorites[index] = FileFavorite{Storage: item.Storage, ID: item.ID, Deleted: true}
			} else if index >= 0 {
				st.FileFavorites[index] = item
			} else {
				if len(st.FileFavorites) >= 2000 {
					return errors.New("收藏记录已达到2000项上限")
				}
				st.FileFavorites = append(st.FileFavorites, item)
			}
		}
		result = visibleFavorites(st.FileFavorites)
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, result)
}
