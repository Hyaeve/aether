package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

type backupEnvelope struct {
	Version int    `json:"version"`
	Salt    []byte `json:"salt"`
	Nonce   []byte `json:"nonce"`
	Data    []byte `json:"data"`
}
type configBundle struct {
	State State                      `json:"state"`
	Files map[string]json.RawMessage `json:"files"`
}

func backupCipher(password string, salt []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(pbkdf2.Key([]byte(password), salt, 600000, 32, sha256.New))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func sealBackup(data []byte, password string) ([]byte, error) {
	if password == "" || len(password) > 1024 {
		return nil, errors.New("请输入备份密码（最多1024字节）")
	}
	env := backupEnvelope{Version: 1, Salt: make([]byte, 16), Nonce: make([]byte, 12)}
	if _, err := rand.Read(env.Salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(env.Nonce); err != nil {
		return nil, err
	}
	aead, err := backupCipher(password, env.Salt)
	if err != nil {
		return nil, err
	}
	env.Data = aead.Seal(nil, env.Nonce, data, []byte("aether-backup-v1"))
	return json.Marshal(env)
}
func openBackup(data []byte, password string) ([]byte, error) {
	var env backupEnvelope
	if len(data) > 48<<20 || password == "" || len(password) > 1024 || json.Unmarshal(data, &env) != nil || env.Version != 1 || len(env.Salt) != 16 || len(env.Nonce) != 12 {
		return nil, errors.New("备份格式或密码无效")
	}
	aead, err := backupCipher(password, env.Salt)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, env.Nonce, env.Data, []byte("aether-backup-v1"))
	if err != nil {
		return nil, errors.New("密码错误或备份已损坏")
	}
	return plain, nil
}
func validateBundle(b configBundle) error {
	if b.State.Username == "" || b.State.Password == "" || b.State.SignKey == "" || len(b.Files) > 1000 {
		return errors.New("备份缺少完整账户配置或文件数量过多")
	}
	for name, data := range b.Files {
		if !fs.ValidPath(name) || strings.ContainsAny(name, `:\`) || !strings.HasSuffix(name, ".json") || !json.Valid(data) {
			return errors.New("备份包含非法配置路径或JSON")
		}
		for _, module := range jsonModules {
			if name == module+".json" {
				return errors.New("模块配置必须通过备份状态恢复，不能作为附加文件覆盖")
			}
		}
	}
	return nil
}
func (a *App) configBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	b := configBundle{State: a.store.snapshot(), Files: map[string]json.RawMessage{}}
	b.State.Logs = nil
	b.State.ToolsRevision = ""
	b.State.Modules = nil
	var total int64
	err := filepath.WalkDir(a.store.dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("配置中存在符号链接，无法导出")
		}
		if entry.IsDir() {
			if name != a.store.dir && (entry.Name() == "log" || entry.Name() == "cache") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(name) != ".json" {
			return nil
		}
		relModule, err := filepath.Rel(a.store.dir, name)
		if err != nil {
			return err
		}
		for _, module := range jsonModules {
			if filepath.ToSlash(relModule) == module+".json" {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > 16<<20 || len(b.Files) >= 1000 {
			return errors.New("配置文件超出备份限制")
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(a.store.dir, name)
		if err != nil {
			return err
		}
		b.Files[filepath.ToSlash(rel)] = raw
		return nil
	})
	if err == nil {
		err = validateBundle(b)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	raw, err := json.Marshal(b)
	if err == nil {
		raw, err = sealBackup(raw, input.Password)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="aether-config-`+time.Now().Format("20060102")+`.aether"`)
	w.Write(raw)
}
func (a *App) configImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 50<<20)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail(w, 400, errors.New("备份文件过大或格式错误"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		fail(w, 400, errors.New("请选择备份文件"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (48<<20)+1))
	if err == nil {
		data, err = openBackup(data, r.FormValue("password"))
	}
	var bundle configBundle
	if err == nil {
		err = json.Unmarshal(data, &bundle)
	}
	if err == nil {
		err = validateBundle(bundle)
	}
	if err != nil {
		fail(w, 400, err)
		return
	}
	// Pending import uses this installation's key, never plaintext credentials.
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err = rand.Read(nonce); err == nil {
		err = atomicWrite(filepath.Join(a.store.dir, "import.pending"), a.store.aead.Seal(nonce, nonce, data, []byte("config-import")))
	}
	if err != nil {
		fail(w, 500, err)
		return
	}
	a.store.event("info", "system", "加密配置备份已校验，重启后应用")
	jsonResponse(w, 200, map[string]string{"message": "导入已校验，请重启容器生效；重启后使用备份中的账号登录"})
}

// Apply before listeners and schedulers start; rollback files if any write fails.
func applyPendingConfig(s *Store) error {
	name := filepath.Join(s.dir, "import.pending")
	raw, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return errors.New("暂存配置损坏")
	}
	raw, err = s.aead.Open(nil, raw[:n], raw[n:], []byte("config-import"))
	if err != nil {
		return err
	}
	var b configBundle
	if err = json.Unmarshal(raw, &b); err != nil {
		return err
	}
	if err = validateBundle(b); err != nil {
		return err
	}
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	old := map[string][]byte{}
	written := []string{}
	rollback := func() {
		for _, key := range written {
			if old[key] == nil {
				root.Remove(key)
			} else {
				_ = atomicWrite(filepath.Join(s.dir, filepath.FromSlash(key)), old[key])
			}
		}
	}
	for key, data := range b.Files {
		for part := key; part != "."; part = path.Dir(part) {
			info, e := root.Lstat(part)
			if e == nil && info.Mode()&os.ModeSymlink != 0 {
				rollback()
				return errors.New("导入路径包含符号链接")
			}
			if e != nil && !os.IsNotExist(e) {
				rollback()
				return e
			}
		}
		previous, e := root.ReadFile(key)
		if e != nil && !os.IsNotExist(e) {
			rollback()
			return e
		}
		old[key] = previous
		if e = root.MkdirAll(path.Dir(key), 0700); e != nil {
			rollback()
			return e
		}
		written = append(written, key)
		if e = atomicWrite(filepath.Join(s.dir, filepath.FromSlash(key)), data); e != nil {
			rollback()
			return e
		}
	}
	before := s.state
	s.state = b.State
	s.state.Modules = before.Modules
	s.state.ModuleVersion = before.ModuleVersion
	for i := range s.state.BackupRules {
		s.state.BackupRules[i].Status = "idle"
		s.state.BackupRules[i].Message = ""
		s.state.BackupRules[i].NextRun = backupNext(s.state.BackupRules[i], time.Now())
	}
	for i := range s.state.Tasks {
		s.state.Tasks[i].Status = "idle"
		s.state.Tasks[i].NextRun = nextRun(s.state.Tasks[i], time.Now())
	}
	for i := range s.state.Automations {
		s.state.Automations[i].Status = "idle"
		s.state.Automations[i].Message = ""
		s.state.Automations[i].NextRun = automationNext(s.state.Automations[i], time.Now())
	}
	if err = s.saveLocked(); err != nil {
		s.state = before
		rollback()
		return err
	}
	return os.Remove(name)
}
