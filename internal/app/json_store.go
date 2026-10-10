package app

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

func configTree(raw []byte) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err := decoder.Decode(&value)
	return value, err
}

func syncConfigDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func durableConfigWrite(name string, data []byte) error {
	if err := atomicWrite(name, data); err != nil {
		return err
	}
	return syncConfigDirectory(filepath.Dir(name))
}

var jsonModules = []string{"storage/storage", "storage/settings", "task/strm", "task/cas", "task/ed2k", "task/cache", "task/other", "task/automation", "link/links", "tool/config", "tool/tmdb", "tool/proxy", "tool/ai", "tool/emby", "tool/quark-takeover", "tool/115-simulcast", "file/webdav", "file/mount", "transfer/backup", "state"}

type jsonConfig struct {
	Version int             `json:"version"`
	Data    json.RawMessage `json:"data"`
	Auth    []byte          `json:"auth"`
}

func (s *Store) configMAC(module string, data []byte) []byte {
	// Derive an authentication key without reusing AES as an HMAC key.
	h := hmac.New(sha256.New, s.macKey)
	h.Write([]byte(module + "\x00"))
	h.Write(data)
	return h.Sum(nil)
}

func secretJSONField(key string) bool {
	switch strings.ToLower(key) {
	case "password", "token", "accesstoken", "refreshtoken", "cookie", "ck", "authorization", "apikey", "signkey", "directorypassword", "clientsecret", "secret":
		return true
	}
	return false
}

func credentialURL(key, text string) bool {
	if key != "address" && key != "apiURL" && key != "imageURL" && key != "publicURL" && key != "broker" {
		return false
	}
	u, err := url.Parse(text)
	return err == nil && (u.User != nil || u.RawQuery != "")
}

func jsonSegment(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "~", "~0"), "/", "~1")
}

// AAD uses module, record ID (or map key), and field path; credentials cannot
// be moved between records or modules, even when the same master key is used.
func (s *Store) transformSecrets(value any, module, path string, decrypt bool) (any, error) {
	switch v := value.(type) {
	case []any:
		for i, item := range v {
			identity := strconv.Itoa(i)
			if record, ok := item.(map[string]any); ok {
				if id, ok := record["id"].(string); ok && id != "" {
					identity = id
				}
			}
			next, err := s.transformSecrets(item, module, path+"/"+jsonSegment(identity), decrypt)
			if err != nil {
				return nil, err
			}
			v[i] = next
		}
	case map[string]any:
		for key, item := range v {
			field := path + "/" + jsonSegment(key)
			text, _ := item.(string)
			_, envelope := item.(map[string]any)
			urlField := key == "address" || key == "apiURL" || key == "imageURL" || key == "publicURL" || key == "broker"
			hashField := key == "password" && (module == "state" || module == "file/webdav")
			if !hashField && (secretJSONField(key) || credentialURL(key, text) || decrypt && urlField && envelope) {
				if decrypt {
					if envelope, ok := item.(map[string]any); ok {
						encoded, ok := envelope["encrypted"].(string)
						if !ok || envelope["algorithm"] != "AES-256-GCM" {
							return nil, errors.New("密钥字段格式无效")
						}
						data, err := base64.StdEncoding.DecodeString(encoded)
						if err != nil || len(data) < s.aead.NonceSize() {
							return nil, errors.New("密钥字段密文无效")
						}
						plain, err := s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], []byte(module+"\x00"+field))
						if err != nil {
							return nil, errors.New("密钥字段校验失败")
						}
						v[key] = string(plain)
						continue
					}
					if text, ok := item.(string); !ok || text != "" {
						return nil, errors.New("密钥字段缺少加密封装")
					}
				} else if text, ok := item.(string); ok && text != "" {
					// URLs are normally readable, but may contain credentials or signed queries.
					nonce := make([]byte, s.aead.NonceSize())
					if _, err := rand.Read(nonce); err != nil {
						return nil, err
					}
					v[key] = map[string]any{"algorithm": "AES-256-GCM", "encrypted": base64.StdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(text), []byte(module+"\x00"+field)))}
					continue
				}
			}
			next, err := s.transformSecrets(item, module, field, decrypt)
			if err != nil {
				return nil, err
			}
			v[key] = next
		}
	}
	return value, nil
}

func (s *Store) encodeJSONConfig(module string, value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	tree, err := configTree(raw)
	if err != nil {
		return nil, err
	}
	tree, err = s.transformSecrets(tree, module, "", false)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(tree)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(jsonConfig{1, data, s.configMAC(module, data)}, "", "  ")
}

func (s *Store) decodeJSONConfig(module string, raw []byte, target any) error {
	var doc jsonConfig
	if json.Unmarshal(raw, &doc) != nil || doc.Version != 1 {
		return errors.New("JSON 配置格式无效")
	}
	tree, err := configTree(doc.Data)
	if err != nil {
		return err
	}
	canonical, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	if !hmac.Equal(doc.Auth, s.configMAC(module, canonical)) {
		return fmt.Errorf("模块 %s 完整性校验失败", module)
	}
	tree, err = s.transformSecrets(tree, module, "", true)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, target)
}

func (s *Store) jsonValues() map[string]any {
	v := s.moduleValues()
	v["storage/storage"], v["storage/settings"] = s.state.Storages, v["storage/storage"].(storageModule).Settings
	plugins := map[string]PluginConfig{}
	for name, config := range s.state.Plugins {
		if name != "tmdb" && name != "proxy" && name != "ai" && name != "emby" {
			plugins[name] = config
		}
	}
	v["tool/config"] = plugins
	for _, name := range []string{"tmdb", "proxy", "ai", "emby"} {
		v["tool/"+name] = s.state.Plugins[name]
	}
	v["tool/quark-takeover"] = toolSettings{QuarkTV: s.state.QuarkTV, Enabled: s.state.QuarkTVEnabled}
	v["tool/115-simulcast"] = s.state.Simulcast
	p := s.state
	p.Storages, p.Tasks, p.Automations, p.Links, p.Mounts, p.DAVUsers, p.BackupRules = nil, nil, nil, nil, nil, nil, nil
	p.Plugins, p.QuarkTV, p.Simulcast, p.QuarkTVEnabled = nil, nil, nil, nil
	p.Settings, p.Modules, p.ToolsRevision, p.ModuleVersion = Settings{}, nil, "", 3
	p.TaskOrder = make([]string, 0, len(s.state.Tasks))
	for _, t := range s.state.Tasks {
		p.TaskOrder = append(p.TaskOrder, t.ID)
	}
	if s.logDir != "" {
		p.Logs = nil
	}
	v["state"] = p
	if s.state.RuntimeSeparated {
		stripRuntimeConfig(v)
	}
	return v
}

func (s *Store) jsonPath(module string) (string, error) {
	valid := false
	for _, key := range jsonModules {
		if module == key {
			valid = true
			break
		}
	}
	if !valid {
		return "", errors.New("非法模块路径")
	}
	name := filepath.Join(s.dir, filepath.FromSlash(module)+".json")
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return "", err
	}
	if info, err := os.Lstat(filepath.Dir(name)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("模块目录不是普通目录")
	}
	if info, err := os.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return "", errors.New("模块配置不是普通文件")
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return name, nil
}

// An authenticated undo journal is durable before any module is replaced.
// Its removal is the commit point; startup restores an interrupted transaction.
func (s *Store) recoverJSONTransaction() error {
	name := filepath.Join(s.dir, ".config-transaction.enc")
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("配置恢复日志不是普通文件")
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if len(raw) < s.aead.NonceSize() {
		return errors.New("配置恢复日志损坏")
	}
	plain, err := s.aead.Open(nil, raw[:s.aead.NonceSize()], raw[s.aead.NonceSize():], []byte("config-transaction-v1"))
	if err != nil {
		return err
	}
	var old map[string][]byte
	if err := json.Unmarshal(plain, &old); err != nil {
		return err
	}
	for key, data := range old {
		file, err := s.jsonPath(key)
		if err != nil {
			return err
		}
		if data == nil {
			err = os.Remove(file)
			if os.IsNotExist(err) {
				err = nil
			}
		} else {
			err = durableConfigWrite(file, data)
		}
		if err != nil {
			return err
		}
		if err := syncConfigDirectory(filepath.Dir(file)); err != nil {
			return err
		}
	}
	if err := os.Remove(name); err != nil {
		return err
	}
	return syncConfigDirectory(s.dir)
}

func (s *Store) saveJSONModules() error {
	if err := s.saveRuntimeLocked(); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(s.dir, ".config-transaction.enc")); err == nil {
		if err := s.recoverJSONTransaction(); err != nil {
			return err
		}
		s.jsonCache = nil
	} else if !os.IsNotExist(err) {
		return err
	}
	values, changes, old := s.jsonValues(), map[string][]byte{}, map[string][]byte{}
	cache := map[string][]byte{}
	for _, key := range jsonModules {
		name, err := s.jsonPath(key)
		if err != nil {
			return err
		}
		after, err := json.Marshal(values[key])
		if err != nil {
			return err
		}
		canonical, err := configTree(after)
		if err != nil {
			return err
		}
		after, err = json.Marshal(canonical)
		if err != nil {
			return err
		}
		cache[key] = after
		if bytes.Equal(s.jsonCache[key], after) {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		// Compare decoded values so unchanged credentials retain their nonce and file.
		var prior json.RawMessage
		if err == nil {
			if err := s.decodeJSONConfig(key, raw, &prior); err != nil {
				return err
			}
		}
		var before []byte
		if prior != nil {
			tree, err := configTree(prior)
			if err != nil {
				return err
			}
			before, err = json.Marshal(tree)
			if err != nil {
				return err
			}
		}
		if string(before) == string(after) && raw != nil {
			continue
		}
		encoded, err := s.encodeJSONConfig(key, values[key])
		if err != nil {
			return err
		}
		changes[key], old[key] = encoded, raw
	}
	if len(changes) == 0 {
		s.jsonCache = cache
		return nil
	}
	journal, err := json.Marshal(old)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	name := filepath.Join(s.dir, ".config-transaction.enc")
	if err := durableConfigWrite(name, s.aead.Seal(nonce, nonce, journal, []byte("config-transaction-v1"))); err != nil {
		return err
	}
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		file, err := s.jsonPath(key)
		if err == nil {
			err = durableConfigWrite(file, changes[key])
		}
		if err != nil {
			if recovery := s.recoverJSONTransaction(); recovery != nil {
				return fmt.Errorf("保存失败：%v；恢复失败：%w", err, recovery)
			}
			return err
		}
	}
	if err := os.Remove(name); err != nil {
		if recovery := s.recoverJSONTransaction(); recovery != nil {
			return recovery
		}
		return err
	}
	s.state.ModuleVersion, s.state.ToolsRevision, s.state.Modules = 3, "", nil
	s.jsonCache = cache
	// Module data and directory entries are durable before the journal removal.
	// If final directory sync fails, the complete new state is still published.
	_ = syncConfigDirectory(s.dir)
	return nil
}

func (s *Store) readJSONModules() error {
	name, err := s.jsonPath("state")
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if err := s.decodeJSONConfig("state", raw, &s.state); err != nil {
		return err
	}
	if s.state.ModuleVersion != 3 {
		return errors.New("不支持的配置版本")
	}
	s.state.Tasks = nil
	for _, key := range jsonModules {
		if key == "state" {
			continue
		}
		name, err := s.jsonPath(key)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		var tasks []Task
		var dav davModule
		var tools toolSettings
		var plugin PluginConfig
		var target any
		switch key {
		case "storage/storage":
			target = &s.state.Storages
		case "storage/settings":
			target = &s.state.Settings
		case "file/webdav":
			target = &dav
		case "file/mount":
			target = &s.state.Mounts
		case "link/links":
			target = &s.state.Links
		case "task/automation":
			target = &s.state.Automations
		case "transfer/backup":
			target = &s.state.BackupRules
		case "tool/config":
			target = &s.state.Plugins
		case "tool/quark-takeover":
			target = &tools
		case "tool/115-simulcast":
			target = &s.state.Simulcast
		case "tool/tmdb", "tool/proxy", "tool/ai", "tool/emby":
			target = &plugin
		default:
			target = &tasks
		}
		if err := s.decodeJSONConfig(key, raw, target); err != nil {
			return err
		}
		switch key {
		case "file/webdav":
			s.state.DAVUsers = dav.Users
			s.state.Settings.WebDAVEnabled, s.state.Settings.WebDAVCache = dav.Enabled, dav.Cache
		case "tool/quark-takeover":
			s.state.QuarkTV, s.state.QuarkTVEnabled = tools.QuarkTV, tools.Enabled
		case "tool/tmdb", "tool/proxy", "tool/ai", "tool/emby":
			if s.state.Plugins == nil {
				s.state.Plugins = map[string]PluginConfig{}
			}
			s.state.Plugins[strings.TrimPrefix(key, "tool/")] = plugin
		default:
			s.state.Tasks = append(s.state.Tasks, tasks...)
		}
	}
	byID := map[string]Task{}
	for _, task := range s.state.Tasks {
		if _, exists := byID[task.ID]; exists {
			return errors.New("任务 ID 重复")
		}
		byID[task.ID] = task
	}
	ordered := make([]Task, 0, len(byID))
	for _, id := range s.state.TaskOrder {
		task, ok := byID[id]
		if !ok {
			return errors.New("任务排序索引不完整")
		}
		ordered = append(ordered, task)
		delete(byID, id)
	}
	if len(byID) != 0 {
		return errors.New("任务排序索引不完整")
	}
	s.state.Tasks = ordered
	s.jsonCache = map[string][]byte{}
	for key, value := range s.jsonValues() {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		tree, err := configTree(raw)
		if err != nil {
			return err
		}
		s.jsonCache[key], err = json.Marshal(tree)
		if err != nil {
			return err
		}
	}
	return nil
}
