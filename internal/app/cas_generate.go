package app

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

func (a *App) generateCASInfo(ctx context.Context, s Storage, f File) (CASInfo, error) {
	info := CASInfo{Provider: s.Type, Name: f.Name, Size: f.Size, SHA256: strings.ToLower(f.SHA256), MD5: strings.ToLower(f.MD5)}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if s.Type != "local" {
		if !casStorage(s) {
			return info, errors.New("不支持此存储生成 CAS")
		}
		if err := validateCASFor(s, info); err != nil {
			return info, errors.New("云端目录接口未返回有效文件哈希，无法生成 CAS")
		}
		return info, nil
	}
	name, err := relative(f.ID)
	if err != nil {
		return info, err
	}
	root, err := os.OpenRoot(s.Config["root"])
	if err != nil {
		return info, err
	}
	defer root.Close()
	file, err := root.Open(name)
	if err != nil {
		return info, err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return info, err
	}
	if !before.Mode().IsRegular() {
		return info, errors.New("仅支持普通文件")
	}
	info.Size = before.Size()
	sha, md := sha256.New(), md5.New()
	buffer := make([]byte, 1<<20)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return info, err
		}
		n, err := file.Read(buffer)
		if n > 0 {
			sha.Write(buffer[:n])
			md.Write(buffer[:n])
			total += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return info, err
		}
	}
	after, err := file.Stat()
	if err != nil {
		return info, err
	}
	if total != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return info, errors.New("文件在计算哈希期间发生变化，请重新执行")
	}
	info.SHA256, info.MD5 = hex.EncodeToString(sha.Sum(nil)), hex.EncodeToString(md.Sum(nil))
	if err := ctx.Err(); err != nil {
		return info, err
	}
	return info, validateCAS(info)
}

// Publish only complete CAS files, retaining existing incremental outputs.
func writeCASOutput(root *os.Root, name string, data []byte, incremental bool) error {
	if err := root.MkdirAll(path.Dir(name), 0755); err != nil {
		return err
	}
	temp := path.Join(path.Dir(name), ".aether-cas-"+id())
	file, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if incremental {
		err = root.Link(temp, name)
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return root.Rename(temp, name)
}
