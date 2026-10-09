package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type toolSettings struct {
	Simulcast map[string]SimulcastConfig `json:"simulcast,omitempty"`
	Plugins   map[string]PluginConfig    `json:"plugins"`
	QuarkTV   map[string]QuarkTVBinding  `json:"quarkTV"`
	Enabled   *bool                      `json:"quarkTVEnabled"`
}

// The encrypted main state commits a content-addressed tools snapshot so a
// failed write cannot publish half of a configuration update.
func (s *Store) initTools(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.modular {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if revision := s.state.ToolsRevision; revision != "" {
		if raw, err := hex.DecodeString(revision); err != nil || len(raw) != 32 {
			return errors.New("invalid tools revision")
		}
		data, err := os.ReadFile(filepath.Join(dir, revision+".enc"))
		if err != nil {
			return err
		}
		if len(data) < s.aead.NonceSize() {
			return errors.New("invalid tools snapshot")
		}
		plain, err := s.aead.Open(nil, data[:s.aead.NonceSize()], data[s.aead.NonceSize():], nil)
		if err != nil {
			return err
		}
		var tools toolSettings
		if err := json.Unmarshal(plain, &tools); err != nil {
			return err
		}
		s.state.Plugins, s.state.QuarkTV, s.state.QuarkTVEnabled = tools.Plugins, tools.QuarkTV, tools.Enabled
		s.state.Simulcast = tools.Simulcast
	}
	s.toolsDir = dir
	return s.saveLocked()
}

func (s *Store) writeToolsLocked() (string, error) {
	plain, err := json.Marshal(toolSettings{Simulcast: s.state.Simulcast, Plugins: s.state.Plugins, QuarkTV: s.state.QuarkTV, Enabled: s.state.QuarkTVEnabled})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(plain)
	revision := hex.EncodeToString(hash[:])
	name := filepath.Join(s.toolsDir, revision+".enc")
	if _, err := os.Stat(name); err == nil {
		return revision, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return revision, atomicWrite(name, s.aead.Seal(nonce, nonce, plain, nil))
}

func (s *Store) cleanToolsLocked(previous, current string) {
	if previous == current {
		return
	}
	entries, _ := os.ReadDir(s.toolsDir)
	for _, entry := range entries {
		name := entry.Name()
		if len(name) == 68 && filepath.Ext(name) == ".enc" && name != previous+".enc" && name != current+".enc" {
			if _, err := hex.DecodeString(name[:64]); err == nil {
				_ = os.Remove(filepath.Join(s.toolsDir, name))
			}
		}
	}
}

func (s *Store) plugin(kind string) PluginConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Plugins[kind]
}
