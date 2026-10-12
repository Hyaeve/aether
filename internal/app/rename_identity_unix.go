//go:build !windows

package app

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func renameLocalIdentity(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("无法核验文件身份")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
