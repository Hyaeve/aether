package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type mountPutLength struct{}
type mountPutFailure struct{}

type mountRequestBody struct {
	io.ReadCloser
	err error
}

func (b *mountRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		b.err = err
	}
	return n, err
}

// The bridge stages bytes without acknowledging a successful PUT until the
// provider confirms publication. Failed files remain private for recovery.
type cloudWriteFile struct {
	*os.File
	app          *App
	ctx          context.Context
	storage      Storage
	parent, name string
	expected     int64
	closed       bool
}

func (f *cloudWriteFile) Stat() (os.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return davInfo{File{Name: f.name, Size: info.Size(), Modified: info.ModTime()}}, nil
}

func (f *cloudWriteFile) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	info, err := f.File.Stat()
	if err == nil {
		err = f.File.Sync()
	}
	closeErr := f.File.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.ctx.Err()
	}
	if body, ok := f.ctx.Value(mountPutFailure{}).(*mountRequestBody); ok && body.err != nil {
		err = body.err
	}
	if err == nil && f.expected >= 0 && info.Size() != f.expected {
		err = errors.New("上传内容不完整")
	}
	if err == nil {
		f.app.runMu.Lock()
		defer f.app.runMu.Unlock()
		current, e := f.app.store.storage(f.storage.ID)
		if e != nil {
			err = e
		} else {
			oldConfig, _ := json.Marshal(f.storage.Config)
			newConfig, _ := json.Marshal(current.Config)
			if string(oldConfig) != string(newConfig) {
				err = errors.New("存储配置已变化，写入已停止")
			} else {
				err = f.app.publishCloudFile(f.ctx, current, f.parent, f.name, f.File.Name())
			}
		}
	}
	f.app.cache.clear()
	if err != nil {
		f.app.store.event("error", "storage", "云端写入失败，暂存保留于 "+f.File.Name()+"："+err.Error())
		return err
	}
	_ = os.Remove(f.File.Name())
	f.app.uploaded.Add(info.Size())
	f.app.store.event("info", "storage", f.storage.Name+" 写入文件："+f.name)
	return nil
}

func (d mountFS) cloudParent(ctx context.Context, s Storage, source, rel string) (string, error) {
	ctx, name := mountReadContext(ctx, s, source, path.Dir(rel))
	_, f, err := (davFS{d.app}).resolve(ctx, name)
	if err != nil {
		return "", err
	}
	if !f.IsDir {
		return "", os.ErrNotExist
	}
	return f.ID, nil
}

func (d mountFS) openCloudWrite(ctx context.Context, s Storage, source, rel string, flag int) (*cloudWriteFile, error) {
	if rel == "." || !safeName(path.Base(rel)) {
		return nil, os.ErrPermission
	}
	parent, err := d.cloudParent(ctx, s, source, rel)
	if err != nil {
		return nil, err
	}
	if flag&os.O_TRUNC == 0 {
		return nil, errors.New("云盘写入需要完整文件提交")
	}
	dir := filepath.Join(d.app.dataDir, "cache", "mount-upload")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, "pending-*")
	if err != nil {
		return nil, err
	}
	expected := int64(-1)
	if n, ok := ctx.Value(mountPutLength{}).(int64); ok {
		expected = n
	}
	return &cloudWriteFile{File: f, app: d.app, ctx: ctx, storage: s, parent: parent, name: path.Base(rel), expected: expected}, nil
}

func (a *App) cloudByName(ctx context.Context, s Storage, parent, name string) (*File, error) {
	files, err := a.rawList(ctx, s, parent)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Name == name {
			return &f, nil
		}
	}
	return nil, nil
}

func (a *App) publishCloudFile(ctx context.Context, s Storage, parent, name, local string) error {
	if !safeName(name) {
		return os.ErrPermission
	}
	old, err := a.cloudByName(ctx, s, parent, name)
	if err != nil {
		return err
	}
	if old != nil && old.IsDir {
		return errors.New("目标是目录")
	}
	tempName := ".aether-upload-" + id() + path.Ext(name)
	if err := a.uploadCloud(ctx, s, parent, tempName, local); err != nil {
		return err
	}
	var uploaded *File
	for i := 0; i < 6; i++ {
		uploaded, err = a.cloudByName(ctx, s, parent, tempName)
		if err != nil {
			return err
		}
		if uploaded != nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	if uploaded == nil {
		return errors.New("上游未确认上传结果；暂存文件保留")
	}
	info, err := os.Stat(local)
	if err != nil {
		return err
	}
	if uploaded.IsDir || uploaded.Size != info.Size() {
		return errors.New("上游文件大小与暂存不一致，停止替换原文件")
	}
	apply := func(f *File, action, newName string) error {
		return a.cloudFileAction(ctx, s, s, fileActionRequest{Source: parent, IDs: []string{f.ID}, Action: action, Name: newName})
	}
	backupName := ".aether-previous-" + id()
	if old != nil {
		if err := apply(old, "rename", backupName); err != nil {
			return fmt.Errorf("新文件已上传但旧文件改名失败：%w", err)
		}
		old, err = a.cloudByName(ctx, s, parent, backupName)
		if err != nil || old == nil {
			return errors.New("旧文件已改名但无法确认，保留两份文件等待处理")
		}
	}
	if err := apply(uploaded, "rename", name); err != nil {
		if old != nil {
			_ = apply(old, "rename", name)
		}
		return fmt.Errorf("发布新文件失败，云端临时文件保留：%w", err)
	}
	if old != nil {
		if err := apply(old, "delete", ""); err != nil {
			a.store.event("warn", "storage", s.Name+" 新文件已发布，旧文件保留为 "+backupName)
		}
	}
	return nil
}

// Upload endpoints must not redirect credentials or write bodies to another host.
func writeHTTP(ctx context.Context, method, address string, headers http.Header, body io.Reader, size int64) ([]byte, http.Header, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, nil, errors.New("写入地址无效")
	}
	req.Header = headers.Clone()
	req.ContentLength = size
	client := &http.Client{Transport: apiClient.Transport, Timeout: 30 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, errors.New("云端写入网络请求失败")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("云端写入返回 HTTP %d", resp.StatusCode)
	}
	return raw, resp.Header, nil
}

func uploadAPI(ctx context.Context, s Storage, method, address string, body, out any) error {
	h := cloudHeaders(s)
	var data []byte
	if values, ok := body.(url.Values); ok {
		data = []byte(values.Encode())
		h.Set("Content-Type", "application/x-www-form-urlencoded")
	} else if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
		h.Set("Content-Type", "application/json")
	}
	raw, _, err := writeHTTP(ctx, method, address, h, strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func (a *App) uploadCloud(ctx context.Context, s Storage, parent, name, local string) error {
	file, err := os.Open(local)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	switch s.Type {
	case "webdav":
		address, err := davURL(s, path.Join(parent, name))
		if err != nil {
			return err
		}
		req, _ := http.NewRequest("PUT", address, nil)
		req.SetBasicAuth(s.Config["username"], s.Config["password"])
		_, _, err = writeHTTP(ctx, "PUT", address, req.Header, file, info.Size())
		return err
	case "openlist":
		h := http.Header{"Authorization": {s.Config["token"]}, "File-Path": {url.PathEscape(path.Join("/", s.Config["root"], parent, name))}, "As-Task": {"false"}, "Content-Type": {"application/octet-stream"}}
		raw, _, err := writeHTTP(ctx, "PUT", strings.TrimRight(s.Config["address"], "/")+"/api/fs/put", h, file, info.Size())
		if err != nil {
			return err
		}
		var result struct{ Code int }
		if json.Unmarshal(raw, &result) != nil || result.Code != 200 {
			return errors.New("OpenList 未确认上传成功")
		}
		return nil
	case "mobile":
		return a.uploadMobile(ctx, s, parent, name, file, info.Size())
	case "tianyi":
		return a.uploadTianyi(ctx, s, parent, name, file, info.Size())
	case "115":
		return a.upload115(ctx, s, parent, name, file, info.Size())
	case "quark":
		return a.uploadQuark(ctx, s, parent, name, file, info.Size())
	default:
		return errors.New("此存储不支持上传")
	}
}

func (a *App) cloudMkdir(ctx context.Context, s Storage, parent, name string) error {
	if !safeName(name) {
		return os.ErrPermission
	}
	found, err := a.cloudByName(ctx, s, parent, name)
	if err != nil {
		return err
	}
	if found != nil {
		return os.ErrExist
	}
	switch s.Type {
	case "webdav":
		address, err := davURL(s, path.Join(parent, name))
		if err != nil {
			return err
		}
		req, _ := http.NewRequest("MKCOL", address, nil)
		req.SetBasicAuth(s.Config["username"], s.Config["password"])
		_, _, err = writeHTTP(ctx, "MKCOL", address, req.Header, nil, 0)
		return err
	case "openlist":
		return openlistWriteJSON(ctx, s, "mkdir", map[string]any{"path": path.Join("/", s.Config["root"], parent, name)})
	case "mobile":
		host, err := a.mobileHost(ctx, s)
		if err != nil {
			return err
		}
		return a.mobilePost(ctx, s, host+"/file/create", map[string]any{"parentFileId": parent, "name": name, "type": "folder", "fileRenameMode": "refuse"}, false, nil)
	case "tianyi":
		var out map[string]any
		return a.tianyiRequest(ctx, s, "POST", tianyiAPI+"/createFolder.action", url.Values{"parentFolderId": {parent}, "folderName": {name}, "relativePath": {""}}, &out)
	case "115":
		c, err := client115(ctx, s)
		if err != nil {
			return err
		}
		_, err = c.Mkdir(parent, name)
		return err
	case "quark":
		return quarkWriteJSON(ctx, s, "/file", map[string]any{"pdir_fid": parent, "file_name": name, "dir_path": "", "dir_init_lock": false}, nil)
	}
	return errors.New("此存储不支持创建目录")
}

func openlistWriteJSON(ctx context.Context, s Storage, action string, body any) error {
	var response struct{ Code int }
	err := requestJSON(ctx, "POST", strings.TrimRight(s.Config["address"], "/")+"/api/fs/"+action, http.Header{"Authorization": {s.Config["token"]}}, body, &response)
	if err != nil {
		return err
	}
	if response.Code != 200 {
		return errors.New("OpenList 操作未成功")
	}
	return nil
}
