package app

import (
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type DAVGrant struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StorageID string `json:"storageId"`
	Directory string `json:"directory"`
}

type DAVUser struct {
	ID       string     `json:"id"`
	Username string     `json:"username"`
	Password string     `json:"password,omitempty"`
	Enabled  bool       `json:"enabled"`
	Grants   []DAVGrant `json:"grants"`
}

type davGrantsKey struct{}

func (a *App) davUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == "PATCH" {
		var input struct {
			Enabled bool `json:"enabled"`
		}
		if !decode(w, r, &input) {
			return
		}
		err := a.store.update(func(st *State) error {
			for i := range st.DAVUsers {
				if st.DAVUsers[i].ID == r.PathValue("id") {
					st.DAVUsers[i].Enabled = input.Enabled
					return nil
				}
			}
			return errors.New("WebDAV 用户不存在")
		})
		if err != nil {
			fail(w, 404, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == "GET" {
		users := a.store.snapshot().DAVUsers
		if users == nil {
			users = []DAVUser{}
		}
		for i := range users {
			users[i].Password = ""
		}
		jsonResponse(w, 200, users)
		return
	}
	if r.Method != "POST" && r.Method != "PUT" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	var user DAVUser
	if r.Method != "DELETE" && !decode(w, r, &user) {
		return
	}
	uid := r.PathValue("id")
	if r.Method == "POST" {
		uid = id()
	}
	if r.Method != "DELETE" {
		user.Username = strings.TrimSpace(user.Username)
		if user.Username == "" || len(user.Username) > 150 || strings.ContainsAny(user.Username, ":\r\n") ||
			len(user.Password) > 72 || (r.Method == "POST" && user.Password == "") || len(user.Grants) > 100 {
			fail(w, 400, errors.New("账号、密码或授权目录数量无效（最多 100 个目录）"))
			return
		}
		if user.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
			if err != nil {
				fail(w, 400, err)
				return
			}
			user.Password = string(hash)
		}
		seen := map[string]bool{}
		for i := range user.Grants {
			g := &user.Grants[i]
			g.Name = strings.TrimSpace(g.Name)
			if !safeName(g.Name) || seen[strings.ToLower(g.Name)] {
				fail(w, 400, errors.New("目录显示名称无效或重复"))
				return
			}
			seen[strings.ToLower(g.Name)] = true
			s, err := a.store.storage(g.StorageID)
			if err != nil {
				fail(w, 400, err)
				return
			}
			if g.Directory == "" || g.Directory == "/" {
				g.Directory = rootOf(s)
			}
			if _, err := a.rawList(r.Context(), s, g.Directory); err != nil {
				fail(w, 400, errors.New("授权目录无法读取，请检查存储连接和目录"))
				return
			}
			g.ID = id()
		}
	}
	err := a.store.update(func(st *State) error {
		if r.Method != "DELETE" {
			if strings.EqualFold(st.Username, user.Username) {
				return errors.New("不能与管理员账号同名")
			}
			for _, existing := range st.DAVUsers {
				if existing.ID != uid && strings.EqualFold(existing.Username, user.Username) {
					return errors.New("WebDAV 账号已存在")
				}
			}
			for _, g := range user.Grants {
				found := false
				for _, s := range st.Storages {
					if s.ID == g.StorageID && s.Enabled {
						found = true
					}
				}
				if !found {
					return errors.New("授权存储不存在或已停用")
				}
			}
		}
		user.ID = uid
		if r.Method == "POST" {
			st.DAVUsers = append(st.DAVUsers, user)
			return nil
		}
		for i, existing := range st.DAVUsers {
			if existing.ID != uid {
				continue
			}
			if r.Method == "DELETE" {
				st.DAVUsers = append(st.DAVUsers[:i], st.DAVUsers[i+1:]...)
				return nil
			}
			if user.Password == "" {
				user.Password = existing.Password
			}
			// Keep stable mount IDs when an authorization remains unchanged.
			for j, g := range user.Grants {
				for _, old := range existing.Grants {
					if g.StorageID == old.StorageID && g.Directory == old.Directory {
						user.Grants[j].ID = old.ID
						break
					}
				}
			}
			st.DAVUsers[i] = user
			return nil
		}
		return errors.New("WebDAV 用户不存在")
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"id": uid})
}
