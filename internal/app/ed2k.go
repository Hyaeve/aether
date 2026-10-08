package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/md4"
)

func ed2kHash(reader io.Reader) (string, error) {
	const chunk = 9728000
	var parts []byte
	for {
		h := md4.New()
		n, err := io.CopyN(h, reader, chunk)
		if err != nil && err != io.EOF {
			return "", err
		}
		parts = append(parts, h.Sum(nil)...)
		if n < chunk {
			break
		}
	}
	if len(parts) == 16 {
		return hex.EncodeToString(parts), nil
	}
	h := md4.New()
	_, _ = h.Write(parts)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (a *App) generateED2K(ctx context.Context, s Storage, f File) ([]byte, error) {
	info, err := a.generateED2KInfo(ctx, s, f)
	if err != nil {
		return nil, err
	}
	return []byte(info.URI() + "\n"), nil
}

func (a *App) generateED2KInfo(ctx context.Context, s Storage, f File) (ED2KInfo, error) {
	if s.Type != "local" {
		return ED2KInfo{}, fmt.Errorf("ED2K 计算目前仅支持本地存储")
	}
	rel, err := relative(f.ID)
	if err != nil {
		return ED2KInfo{}, err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return ED2KInfo{}, err
	}
	defer root.Close()
	in, err := root.Open(rel)
	if err != nil {
		return ED2KInfo{}, err
	}
	defer in.Close()
	before, err := in.Stat()
	if err != nil {
		return ED2KInfo{}, err
	}
	if !before.Mode().IsRegular() {
		return ED2KInfo{}, fmt.Errorf("不是普通文件")
	}
	hash, err := ed2kHash(&cancelReader{ctx, in})
	if err != nil {
		return ED2KInfo{}, err
	}
	after, err := in.Stat()
	if err != nil {
		return ED2KInfo{}, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ED2KInfo{}, fmt.Errorf("计算期间文件发生变化")
	}
	return ED2KInfo{Name: f.Name, Size: after.Size(), Hash: hash}, nil
}
