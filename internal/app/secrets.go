package app

import (
	"errors"
	"net/http"
)

// Only an explicit administrator action reveals one field; list APIs stay masked.
func (a *App) revealSecret(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var input struct {
			Field string `json:"field"`
		}
		if !decode(w, r, &input) {
			return
		}
		allowed := map[string]bool{"password": true, "token": true, "accessToken": true, "refreshToken": true, "cookie": true, "authorization": true}
		if kind == "links" {
			allowed = map[string]bool{"apiKey": true, "password": true}
		}
		if !allowed[input.Field] {
			fail(w, 400, errors.New("不支持显示此字段"))
			return
		}
		st := a.store.snapshot()
		if kind == "storages" {
			for _, storage := range st.Storages {
				if storage.ID == r.PathValue("id") {
					jsonResponse(w, 200, map[string]string{"value": storage.Config[input.Field]})
					return
				}
			}
		} else {
			for _, link := range st.Links {
				if link.ID == r.PathValue("id") {
					value := link.APIKey
					if input.Field == "password" {
						value = link.Password
					}
					jsonResponse(w, 200, map[string]string{"value": value})
					return
				}
			}
		}
		fail(w, 404, errors.New("配置不存在"))
	}
}
