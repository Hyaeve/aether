package app

import (
	"errors"
	"net/http"
)

type FileVisit struct {
	Storage string          `json:"storage"`
	ID      string          `json:"id"`
	History []FavoriteCrumb `json:"history"`
}

func normalizeVisit(v *FileVisit, storages []Storage) error {
	if len(v.History) < 2 {
		return errors.New("仅记录根目录下至少两级的目录")
	}
	f := FileFavorite{Storage: v.Storage, ID: v.ID, Name: v.History[len(v.History)-1].Name, History: v.History}
	if err := normalizeFavorite(&f, storages); err != nil {
		return err
	}
	if len(f.History) < 2 {
		return errors.New("仅记录根目录下至少两级的目录")
	}
	v.History = f.History
	return nil
}

func visitAncestor(a, b FileVisit) bool {
	if a.Storage != b.Storage {
		return false
	}
	for _, crumb := range b.History {
		if crumb.ID == a.ID {
			return true
		}
	}
	return false
}

func rememberFileVisit(previous []FileVisit, next FileVisit) []FileVisit {
	result := make([]FileVisit, 0, 6)
	for _, item := range previous {
		if visitAncestor(next, item) {
			result = append(result, item)
		}
	}
	if len(result) == 0 {
		result = append(result, next)
	}
	for _, item := range previous {
		if item.Storage == next.Storage && item.ID == next.ID || visitAncestor(item, next) || visitAncestor(next, item) {
			continue
		}
		result = append(result, item)
	}
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

func (a *App) fileVisits(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Action string      `json:"action"`
		Items  []FileVisit `json:"items"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Action != "visit" && input.Action != "import" || len(input.Items) == 0 || len(input.Items) > 5 || input.Action == "visit" && len(input.Items) != 1 {
		fail(w, 400, errors.New("访问记录操作无效"))
		return
	}
	var result []FileVisit
	err := a.store.update(func(st *State) error {
		if input.Action == "visit" {
			v := input.Items[0]
			if err := normalizeVisit(&v, st.Storages); err != nil {
				return err
			}
			st.FileVisits = rememberFileVisit(st.FileVisits, v)
		} else {
			// Legacy browser entries are older than the current shared history.
			merged := []FileVisit{}
			for i := len(input.Items) - 1; i >= 0; i-- {
				v := input.Items[i]
				if normalizeVisit(&v, st.Storages) == nil {
					merged = rememberFileVisit(merged, v)
				}
			}
			for i := len(st.FileVisits) - 1; i >= 0; i-- {
				merged = rememberFileVisit(merged, st.FileVisits[i])
			}
			st.FileVisits = merged
		}
		result = append([]FileVisit{}, st.FileVisits...)
		return nil
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, result)
}
