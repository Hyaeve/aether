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

var errCASMissingHash = errors.New("云端目录接口未返回有效文件哈希，无法生成 CAS")

func (a *App) casBinding(t Task, source Storage) (Storage, error) {
	bindingID := t.CASBindingID
	if bindingID == "" && casStorage(source) {
		bindingID = source.ID
	}
	binding, err := a.store.storage(bindingID)
	if err != nil || !binding.Enabled || !casStorage(binding) {
		return Storage{}, errors.New("请选择已启用的移动或天翼存储作为 CAS 绑定账号")
	}
	if source.Type != "local" && source.Type != binding.Type {
		return Storage{}, errors.New("源目录仅允许本地存储或与绑定账号同类型的云存储")
	}
	return binding, nil
}

func casForBinding(info CASInfo, binding Storage) (CASInfo, error) {
	info.Provider = binding.Type
	return info, validateCASFor(binding, info)
}

func (a *App) generateCASInfo(ctx context.Context, s Storage, f File) (CASInfo, error) {
	info := CASInfo{Provider: s.Type, Name: f.Name, Size: f.Size, SHA256: strings.ToLower(f.SHA256), MD5: strings.ToLower(f.MD5)}
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if s.Type != "local" {
		if !casStorage(s) {
			return info, errors.New("不支持此存储生成 CAS")
		}
		value, size := info.SHA256, 32
		if nativeTianyi(s) {
			value, size = info.MD5, 16
		}
		hash, err := hex.DecodeString(value)
		if err != nil || len(hash) != size {
			return info, errCASMissingHash
		}
		if err := validateCASFor(s, info); err != nil {
			return info, err
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
	sha, md, slice, aggregate := sha256.New(), md5.New(), md5.New(), md5.New()
	const sliceSize = int64(10 << 20)
	var sliceBytes int64
	sliceCount := 0
	finishSlice := func() {
		if sliceCount > 0 {
			aggregate.Write([]byte("\n"))
		}
		aggregate.Write([]byte(strings.ToUpper(hex.EncodeToString(slice.Sum(nil)))))
		sliceCount++
		slice.Reset()
		sliceBytes = 0
	}
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
			for chunk := buffer[:n]; len(chunk) > 0; {
				take := min(int64(len(chunk)), sliceSize-sliceBytes)
				slice.Write(chunk[:take])
				sliceBytes += take
				chunk = chunk[take:]
				if sliceBytes == sliceSize {
					finishSlice()
				}
			}
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
	if sliceBytes > 0 {
		finishSlice()
	}
	info.SliceSize, info.SliceMD5 = sliceSize, strings.ToUpper(info.MD5)
	if sliceCount > 1 {
		info.SliceMD5 = strings.ToUpper(hex.EncodeToString(aggregate.Sum(nil)))
	}
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
