package app

import (
	"errors"
	"net/http"
)

type shareTransferDirectory struct {
	Parent  string          `json:"parent"`
	Label   string          `json:"label"`
	History []FavoriteCrumb `json:"history"`
}

type shareTransferPreferences struct {
	Last        map[string]string                 `json:"last"`
	Directories map[string]shareTransferDirectory `json:"directories"`
}

func shareDestinationStorage(s Storage) bool {
	return s.Enabled && (s.Type == "115" || s.Type == "quark" || nativeMobile(s))
}

func visibleSharePreferences(st State) shareTransferPreferences {
	result := shareTransferPreferences{Last: map[string]string{}, Directories: map[string]shareTransferDirectory{}}
	if st.ShareTargets == nil {
		return result
	}
	for _, s := range st.Storages {
		if !shareDestinationStorage(s) {
			continue
		}
		if st.ShareTargets.Last[s.Type] == s.ID {
			result.Last[s.Type] = s.ID
		}
		if directory, ok := st.ShareTargets.Directories[s.ID]; ok {
			result.Directories[s.ID] = directory
		}
	}
	return result
}

func (a *App) sharePreferences(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		jsonResponse(w, 200, visibleSharePreferences(a.store.snapshotWithLogLimit(0)))
		return
	}
	var input struct {
		StorageID string `json:"storageId"`
		shareTransferDirectory
	}
	if !decode(w, r, &input) {
		return
	}
	if !validFavoriteID(input.Parent) || !validFavoriteID(input.Label) || !safeName(input.Label) || len(input.Label) > 255 || len(input.History) > 128 {
		fail(w, 400, errors.New("转存目录记忆无效"))
		return
	}
	for _, crumb := range input.History {
		if !validFavoriteID(crumb.ID) || !validFavoriteID(crumb.Name) || !safeName(crumb.Name) || len(crumb.Name) > 255 {
			fail(w, 400, errors.New("转存目录路径记忆无效"))
			return
		}
	}
	err := a.store.update(func(st *State) error {
		for _, s := range st.Storages {
			if s.ID != input.StorageID {
				continue
			}
			if !shareDestinationStorage(s) {
				return errors.New("请选择支持分享转存的已启用存储池")
			}
			preferences := visibleSharePreferences(*st)
			preferences.Last[s.Type] = s.ID
			preferences.Directories[s.ID] = input.shareTransferDirectory
			st.ShareTargets = &preferences
			return nil
		}
		return errors.New("转存存储池不存在")
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
