package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
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
	if s.Type != "local" {
		return nil, fmt.Errorf("ED2K 计算目前仅支持本地存储")
	}
	rel, err := relative(f.ID)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return nil, err
	}
	defer root.Close()
	in, err := root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	before, err := in.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("不是普通文件")
	}
	hash, err := ed2kHash(&cancelReader{ctx, in})
	if err != nil {
		return nil, err
	}
	after, err := in.Stat()
	if err != nil {
		return nil, err
	}
	if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("计算期间文件发生变化")
	}
	return []byte(fmt.Sprintf("ed2k://|file|%s|%d|%s|/\n", url.PathEscape(f.Name), after.Size(), hash)), nil
}
