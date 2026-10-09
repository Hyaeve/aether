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
	if err := ensureOrganizeConfigFiles(configDir); err != nil {
		return err
	}
	return nil
}

func ensureOrganizeConfigFiles(configDir string) error {
	rulesDir := filepath.Join(configDir, "organize")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		return err
	}
	files := map[string][]byte{
		"organize-rules.json":    []byte("{}\n"),
		"categories.json":        []byte("{}\n"),
		"upgrade-policies.json":  []byte("{}\n"),
		"ai.json":                []byte("{}\n"),
		"recognition-rules.json": []byte("{}\n"),
	}
	for name, contents := range files {
		filename := filepath.Join(rulesDir, name)
		if info, err := os.Lstat(filename); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("配置路径不是普通文件: %s", filename)
			}
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		// Preserve legacy originals and never overwrite an existing grouped configuration.
		legacy := filepath.Join(configDir, name)
		if info, err := os.Lstat(legacy); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("旧配置路径不是普通文件: %s", legacy)
			}
			contents, err = os.ReadFile(legacy)
			if err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.Write(contents)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func migrateLegacyConfig(configDir, dataDir string) error {
	if _, err := os.Stat(filepath.Join(configDir, "state.json")); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(filepath.Join(configDir, "state.enc")); err == nil {
		// Never generate a new key for existing encrypted configuration.
		keyPath := filepath.Join(configDir, "master.key")
		if external := os.Getenv("AETHER_MASTER_KEY_FILE"); external != "" {
			keyPath = external
		}
		if _, err := os.Stat(keyPath); err != nil {
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
