package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type savedSessions struct {
	Account string               `json:"account"`
	Tokens  map[string]time.Time `json:"tokens"`
}

func sessionKey(token string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) }

func (a *App) sessionAccount() string {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	return sessionKey(a.store.state.Username + "\x00" + a.store.state.Password)
}

func (a *App) restoreSessions() error {
	data, err := os.ReadFile(filepath.Join(a.dataDir, "runtime", "sessions.enc"))
	legacy := false
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(a.store.dir, "sessions.enc"))
		legacy = err == nil
	}
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	n := a.store.aead.NonceSize()
	if len(data) < n {
		return fmt.Errorf("invalid session file")
	}
	plain, err := a.store.aead.Open(nil, data[:n], data[n:], []byte("aether-sessions"))
	if err != nil {
		return err
	}
	var saved savedSessions
	if err := json.Unmarshal(plain, &saved); err != nil {
		return err
	}
	if saved.Account == a.sessionAccount() {
		for key, expiry := range saved.Tokens {
			if time.Now().Before(expiry) {
				a.sessions[key] = expiry
			}
		}
	}
	if legacy {
		return a.saveSessions()
	}
	return nil
}

// Caller holds sessionMu. Only token digests are persisted, bound to the account hash.
func (a *App) saveSessions() error {
	plain, err := json.Marshal(savedSessions{a.sessionAccount(), a.sessions})
	if err != nil {
		return err
	}
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	data := a.store.aead.Seal(nonce, nonce, plain, []byte("aether-sessions"))
	dir := filepath.Join(a.dataDir, "runtime")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "sessions.enc"), data)
}
