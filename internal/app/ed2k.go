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
	if s.Type == "115" {
		if f.IsDir || f.Size <= 0 || f.Size > 1<<40 {
			return ED2KInfo{}, fmt.Errorf("ED2K 源文件大小无效")
		}
		download, err := a.download(ctx, s, f.ID, f.PickCode)
		if err != nil {
			return ED2KInfo{}, err
		}
		in := &davFile{ctx: ctx, info: davInfo{f}, download: download}
		defer in.Close()
		return ed2kRemoteInfo(ctx, f, in)
	}
	if s.Type != "local" {
		return ED2KInfo{}, fmt.Errorf("ED2K 计算仅支持本地或 115 存储")
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

func ed2kRemoteInfo(ctx context.Context, f File, reader io.Reader) (ED2KInfo, error) {
	// Read one extra byte to reject stale sizes instead of publishing an invalid hash.
	limited := &io.LimitedReader{R: &cancelReader{ctx, reader}, N: f.Size + 1}
	hash, err := ed2kHash(limited)
	if err != nil {
		return ED2KInfo{}, err
	}
	if limited.N != 1 {
		return ED2KInfo{}, fmt.Errorf("ED2K 读取大小与源文件不一致，请刷新目录后重试")
	}
	return ED2KInfo{Name: f.Name, Size: f.Size, Hash: hash}, nil
}
