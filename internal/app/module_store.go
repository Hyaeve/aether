package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var moduleNames = []string{"storage/storage", "task/strm", "task/cas", "task/ed2k", "task/cache", "task/other", "task/automation", "link/links", "tool/config", "file/webdav", "file/mount", "transfer/backup"}

type storageModule struct {
	Storages []Storage `json:"storages"`
	Settings Settings  `json:"settings"`
}
type davModule struct {
	Users   []DAVUser `json:"users"`
	Enabled bool      `json:"enabled"`
	Cache   bool      `json:"cache"`
}

func (s *Store) initModules() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.modular {
		return nil
	}
	s.modular = true
	if err := s.saveLocked(); err != nil {
		s.modular = false
		return err
	}
	s.toolsDir = ""
	return nil
}
func (s *Store) moduleValues() map[string]any {
	settings := s.state.Settings
	settings.WebDAVEnabled = false
	settings.WebDAVCache = false
	v := map[string]any{
		"transfer/backup": s.state.BackupRules,
		"storage/storage": storageModule{s.state.Storages, settings},
		"task/automation": s.state.Automations,
		"link/links":      s.state.Links,
		"tool/config":     toolSettings{Simulcast: s.state.Simulcast, Plugins: s.state.Plugins, QuarkTV: s.state.QuarkTV, Enabled: s.state.QuarkTVEnabled},
		"file/webdav":     davModule{s.state.DAVUsers, s.state.Settings.WebDAVEnabled, s.state.Settings.WebDAVCache},
		"file/mount":      s.state.Mounts,
	}
	for _, kind := range []string{"strm", "cas", "ed2k", "cache", "other"} {
		tasks := []Task{}
		for _, t := range s.state.Tasks {
			known := t.Kind == "strm" || t.Kind == "cas" || t.Kind == "ed2k" || t.Kind == "cache"
			if t.Kind == kind || kind == "other" && !known {
				tasks = append(tasks, t)
			}
		}
		v["task/"+kind] = tasks
	}
	return v
}

// Immutable snapshots are written first; the encrypted root index commits them together.
func (s *Store) saveModulesLocked() error {
	refs := map[string]string{}
	values := s.moduleValues()
	for _, key := range moduleNames {
		plain, err := json.Marshal(values[key])
		if err != nil {
			return err
		}
		hash := sha256.Sum256(plain)
		rev := hex.EncodeToString(hash[:])
		refs[key] = rev
		name := filepath.Join(s.dir, filepath.FromSlash(key)+"."+rev+".enc")
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return err
		}
		if info, err := os.Lstat(filepath.Dir(name)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("模块目录不是普通目录")
		}
		if info, err := os.Lstat(name); os.IsNotExist(err) {
			nonce := make([]byte, s.aead.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			if err := atomicWrite(name, s.aead.Seal(nonce, nonce, plain, []byte(key))); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !info.Mode().IsRegular() {
			return errors.New("模块快照不是普通文件")
		}
	}
	p := s.state
	p.TaskOrder = make([]string, 0, len(s.state.Tasks))
	for _, t := range s.state.Tasks {
		p.TaskOrder = append(p.TaskOrder, t.ID)
	}
	p.Modules, p.ToolsRevision = refs, ""
	p.ModuleVersion = 2
	p.Storages, p.Tasks, p.Automations, p.Links, p.Mounts, p.DAVUsers = nil, nil, nil, nil, nil, nil
	p.Plugins, p.QuarkTV, p.QuarkTVEnabled, p.Simulcast = nil, nil, nil, nil
	p.Settings = Settings{}
	p.BackupRules = nil
	if s.logDir != "" {
		p.Logs = nil
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(s.dir, "state.enc"), s.aead.Seal(nonce, nonce, plain, nil)); err != nil {
		return err
	}
	previous := s.state.Modules
	s.state.Modules, s.state.ToolsRevision = refs, ""
	s.state.ModuleVersion = 2
	s.state.TaskOrder = p.TaskOrder
	for _, key := range moduleNames {
		if previous[key] == refs[key] {
			continue
		}
		names, _ := filepath.Glob(filepath.Join(s.dir, filepath.FromSlash(key)+".*.enc"))
		for _, name := range names {
			rev := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(name), filepath.Base(key)+"."), ".enc")
			if raw, err := hex.DecodeString(rev); err == nil && len(raw) == 32 && rev != previous[key] && rev != refs[key] {
				_ = os.Remove(name)
			}
		}
	}
	return nil
}
func (s *Store) readModulesLocked() error {
	// v0.3.9 has all original modules but no transfer module. Only that exact
	// schema may migrate; a missing module from a new schema remains an error.
	legacy := s.state.ModuleVersion < 2 && s.state.Modules["transfer/backup"] == "" && len(s.state.Modules) == len(moduleNames)-1
	if len(s.state.Modules) != len(moduleNames) && !legacy {
		return errors.New("模块配置索引不完整")
	}
	s.state.Tasks = nil
	for _, key := range moduleNames {
		if legacy && key == "transfer/backup" {
			s.state.BackupRules = nil
			continue
		}
		rev := s.state.Modules[key]
		if raw, err := hex.DecodeString(rev); err != nil || len(raw) != 32 {
			return fmt.Errorf("模块 %s 索引无效", key)
		}
		name := filepath.Join(s.dir, filepath.FromSlash(key)+"."+rev+".enc")
		if info, err := os.Lstat(filepath.Dir(name)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("模块目录不是普通目录")
		}
		if info, err := os.Lstat(name); err != nil || !info.Mode().IsRegular() {
			return errors.New("模块快照不是普通文件")
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("读取模块 %s: %w", key, err)
		}
		if len(data) < s.aead.NonceSize() {
			return errors.New("模块密文无效")
		}
		plain, err := s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], []byte(key))
		if err != nil {
			return fmt.Errorf("解密模块 %s: %w", key, err)
		}
		hash := sha256.Sum256(plain)
		if hex.EncodeToString(hash[:]) != rev {
			return errors.New("模块内容摘要不匹配")
		}
		var target any
		var storage storageModule
		var dav davModule
		var tools toolSettings
		var tasks []Task
		switch key {
		case "storage/storage":
			target = &storage
		case "file/webdav":
			target = &dav
		case "file/mount":
			target = &s.state.Mounts
		case "tool/config":
			target = &tools
		case "link/links":
			target = &s.state.Links
		case "task/automation":
			target = &s.state.Automations
		case "transfer/backup":
			target = &s.state.BackupRules
		default:
			target = &tasks
		}
		if err := json.Unmarshal(plain, target); err != nil {
			return err
		}
		switch key {
		case "storage/storage":
			s.state.Storages, s.state.Settings = storage.Storages, storage.Settings
		case "file/webdav":
			s.state.DAVUsers = dav.Users
			s.state.Settings.WebDAVEnabled, s.state.Settings.WebDAVCache = dav.Enabled, dav.Cache
		case "tool/config":
			s.state.Plugins, s.state.QuarkTV, s.state.QuarkTVEnabled, s.state.Simulcast = tools.Plugins, tools.QuarkTV, tools.Enabled, tools.Simulcast
		default:
			s.state.Tasks = append(s.state.Tasks, tasks...)
		}
	}
	if len(s.state.TaskOrder) > 0 {
		byID := map[string]Task{}
		for _, t := range s.state.Tasks {
			byID[t.ID] = t
		}
		ordered := make([]Task, 0, len(s.state.Tasks))
		for _, id := range s.state.TaskOrder {
			if t, ok := byID[id]; ok {
				ordered = append(ordered, t)
				delete(byID, id)
			}
		}
		if len(byID) > 0 || len(ordered) != len(s.state.Tasks) {
			return errors.New("任务排序索引不完整")
		}
		s.state.Tasks = ordered
	}
	return nil
}
