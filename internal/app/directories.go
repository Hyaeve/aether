package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func prepareDirectories(configDir, dataDir string) error {
	for _, dir := range []string{configDir, dataDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	configAbs, err := filepath.Abs(configDir)
	if err != nil {
		return err
	}
	dataAbs, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if configAbs != dataAbs {
		if err := migrateLegacyConfig(configDir, dataDir); err != nil {
			return err
		}
	}
	// These directories reserve ownership for future configuration modules.
	for _, name := range []string{"organize-rules", "categories", "upgrade-policies", "ai", "recognition-rules"} {
		if err := os.MkdirAll(filepath.Join(configDir, name), 0700); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyConfig(configDir, dataDir string) error {
	if _, err := os.Stat(filepath.Join(configDir, "state.enc")); err == nil {
		// Never generate a new key for existing encrypted configuration.
		if _, err := os.Stat(filepath.Join(configDir, "master.key")); err != nil {
			return fmt.Errorf("配置已存在但密钥不可读取: %w", err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	state, err := os.ReadFile(filepath.Join(dataDir, "state.enc"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	key, err := os.ReadFile(filepath.Join(dataDir, "master.key"))
	if err != nil {
		return fmt.Errorf("旧配置迁移缺少密钥: %w", err)
	}
	target := filepath.Join(configDir, "master.key")
	existing, err := os.ReadFile(target)
	if err == nil {
		if !bytes.Equal(existing, key) {
			return fmt.Errorf("配置目录已有不同密钥，已停止迁移以避免覆盖")
		}
	} else if os.IsNotExist(err) {
		if err := atomicWrite(target, key); err != nil {
			return err
		}
	} else {
		return err
	}
	// Write the key first; a restart can safely resume before writing the state.
	return atomicWrite(filepath.Join(configDir, "state.enc"), state)
}
