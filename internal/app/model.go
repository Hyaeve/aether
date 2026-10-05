package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Storage struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Enabled   bool              `json:"enabled"`
	CacheTTL  int               `json:"cacheTTL"`
	Config    map[string]string `json:"config"`
	Status    string            `json:"status"`
	LastError string            `json:"lastError,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

type Task struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Kind           string    `json:"kind"`
	StorageID      string    `json:"storageId"`
	Source         string    `json:"source"`
	Target         string    `json:"target"`
	Mode           string    `json:"mode"`
	APIInterval    int       `json:"apiInterval"`
	Cron           string    `json:"cron"`
	Depth          int       `json:"depth"`
	Interval       int       `json:"interval"`
	CacheTTL       int       `json:"cacheTTL"`
	RetentionHours int       `json:"retentionHours,omitempty"`
	ExcludeDirs    string    `json:"excludeDirs"`
	ExcludeFiles   string    `json:"excludeFiles"`
	ExcludeTypes   string    `json:"excludeTypes"`
	Enabled        bool      `json:"enabled"`
	Status         string    `json:"status"`
	Message        string    `json:"message"`
	Processed      int       `json:"processed"`
	LastRun        time.Time `json:"lastRun"`
	NextRun        time.Time `json:"nextRun"`
}

type Settings struct {
	LogDays          int    `json:"logDays"`
	LogMaxEntries    int    `json:"logMaxEntries"`
	SessionDays      int    `json:"sessionDays"`
	CacheEnabled     bool   `json:"cacheEnabled"`
	CacheTTL         int    `json:"cacheTTL"`
	CacheMaxItems    int    `json:"cacheMaxItems"`
	CacheMemoryMB    int    `json:"cacheMemoryMB"`
	CachePersist     bool   `json:"cachePersist"`
	SnapshotInterval int    `json:"snapshotInterval"`
	WebDAVCache      bool   `json:"webdavCache"`
	WebDAVEnabled    bool   `json:"webdavEnabled"`
	PublicURL        string `json:"publicURL"`
}

type LogEntry struct {
	Module  string    `json:"module"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type State struct {
	Links        []MediaLink    `json:"links,omitempty"`
	DAVUsers     []DAVUser      `json:"davUsers,omitempty"`
	CASTemporary []CASTemporary `json:"casTemporary,omitempty"`
	Storages     []Storage      `json:"storages"`
	Tasks        []Task         `json:"tasks"`
	Settings     Settings       `json:"settings"`
	Username     string         `json:"username"`
	Password     string         `json:"password"`
	SignKey      string         `json:"signKey"`
	Logs         []LogEntry     `json:"logs"`
}

type Store struct {
	logDir string
	mu     sync.RWMutex
	state  State
	dir    string
	aead   cipher.AEAD
}

func id() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		err = os.WriteFile(keyPath, key, 0600)
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	s := &Store{dir: dir, aead: aead}
	s.state = State{
		Storages: []Storage{}, Tasks: []Task{}, Logs: []LogEntry{}, SignKey: id(),
		Settings: Settings{LogDays: 15, LogMaxEntries: 20000, SessionDays: 7, CacheEnabled: true, CacheTTL: 30, CacheMaxItems: 10000, CacheMemoryMB: 128, CachePersist: true, SnapshotInterval: 10, WebDAVCache: true, PublicURL: defaultPublicURL()},
	}
	data, err := os.ReadFile(filepath.Join(dir, "state.enc"))
	if err == nil {
		if len(data) < aead.NonceSize() {
			return nil, errors.New("invalid state file")
		}
		plain, e := aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], nil)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(plain, &s.state); e != nil {
			return nil, e
		}
		for i := range s.state.Tasks {
			if s.state.Tasks[i].Status == "running" {
				s.state.Tasks[i].Status = "interrupted"
				s.state.Tasks[i].Message = "服务重启，任务已中断"
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.state.Settings.PublicURL == "http://localhost:15151" {
		s.state.Settings.PublicURL = defaultPublicURL()
	}
	return s, s.saveLocked()
}

func atomicWrite(name string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(name), ".aether-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, name)
}

func (s *Store) saveLocked() error {
	persisted := s.state
	if s.logDir != "" {
		persisted.Logs = nil
	}
	plain, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir, "state.enc"), s.aead.Seal(nonce, nonce, plain, nil))
}

func (s *Store) snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, _ := json.Marshal(s.state)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *Store) update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := json.Marshal(s.state)
	if err := fn(&s.state); err != nil {
		_ = json.Unmarshal(before, &s.state)
		return err
	}
	if err := s.saveLocked(); err != nil {
		_ = json.Unmarshal(before, &s.state)
		return err
	}
	return nil
}

func (s *Store) log(level, message string) {
	s.event(level, "system", message)
}

func (s *Store) storage(storageID string) (Storage, error) {
	for _, v := range s.snapshot().Storages {
		if v.ID == storageID && v.Enabled {
			return v, nil
		}
	}
	return Storage{}, errors.New("存储池不存在或已停用")
}
