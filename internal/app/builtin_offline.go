package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Downloads run in-process; completed files use the existing storage upload path.
func (a *App) builtinOffline(w http.ResponseWriter, r *http.Request, s Storage, parent string, urls []string, torrent bool) {
	if !a.offlineBatches.CompareAndSwap(0, 1) {
		fail(w, 409, errors.New("已有内置下载批次执行中，请等待完成"))
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			a.offlineBatches.Store(0)
		}
	}()
	if (!torrent && (len(urls) == 0 || len(urls) > 50)) || (torrent && (len(r.MultipartForm.File["torrents"]) == 0 || len(r.MultipartForm.File["torrents"]) > 20)) {
		fail(w, 400, errors.New("每批支持 1–50 个链接或 1–20 个种子"))
		return
	}
	for _, raw := range urls {
		if err := validateBuiltinURL(raw); err != nil {
			fail(w, 400, err)
			return
		}
	}
	base := filepath.Join(a.dataDir, "cache", "offline")
	if err := os.MkdirAll(base, 0700); err != nil {
		fail(w, 500, err)
		return
	}
	type job struct {
		dir, input, name string
		torrent          bool
	}
	jobs := []job{}
	cleanup := func() {
		for _, j := range jobs {
			_ = os.RemoveAll(j.dir)
		}
	}
	if torrent {
		for _, header := range r.MultipartForm.File["torrents"] {
			if !strings.HasSuffix(strings.ToLower(header.Filename), ".torrent") || header.Size <= 0 || header.Size > 8<<20 {
				cleanup()
				fail(w, 400, errors.New("仅支持 8MB 以内的种子文件"))
				return
			}
			dir, err := os.MkdirTemp(base, "job-")
			if err != nil {
				cleanup()
				fail(w, 500, err)
				return
			}
			j := job{dir, filepath.Join(dir, "input.torrent"), header.Filename, true}
			jobs = append(jobs, j)
			in, err := header.Open()
			if err != nil {
				cleanup()
				fail(w, 400, err)
				return
			}
			out, err := os.OpenFile(j.input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				in.Close()
				cleanup()
				fail(w, 500, err)
				return
			}
			_, err = io.Copy(out, io.LimitReader(in, (8<<20)+1))
			in.Close()
			closeErr := out.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				cleanup()
				fail(w, 500, err)
				return
			}
		}
	} else {
		for _, raw := range urls {
			dir, err := os.MkdirTemp(base, "job-")
			if err != nil {
				cleanup()
				fail(w, 500, err)
				return
			}
			jobs = append(jobs, job{dir, strings.TrimSpace(raw), "链接下载", false})
		}
	}
	results := make([]offlineResult, 0, len(jobs))
	for _, j := range jobs {
		results = append(results, offlineResult{Name: j.name, Success: true, Message: "已排队，执行结果见系统日志"})
	}
	a.wg.Add(1)
	accepted = true
	go func() {
		defer a.wg.Done()
		defer a.offlineBatches.Store(0)
		for _, j := range jobs {
			ctx, cancel := context.WithTimeout(a.ctx, 24*time.Hour)
			err := a.runBuiltinDownload(ctx, s, parent, j.dir, j.input, j.torrent)
			cancel()
			if err != nil {
				// Do not log input URLs: they may contain credentials or signed queries.
				a.store.event("error", "files", fmt.Sprintf("内置下载 %s 失败：%v；暂存目录保留于 %s", filepath.Base(j.dir), err, j.dir))
			} else {
				_ = os.RemoveAll(j.dir)
				a.store.event("info", "files", "内置下载 "+filepath.Base(j.dir)+" 已上传到 "+s.Name)
			}
		}
	}()
	jsonResponse(w, 200, results)
}

func (a *App) runBuiltinDownload(ctx context.Context, s Storage, parent, dir, input string, torrent bool) error {
	output := filepath.Join(dir, "files")
	if err := os.MkdirAll(output, 0700); err != nil {
		return err
	}
	var err error
	if torrent || strings.HasPrefix(input, "magnet:") {
		err = downloadBuiltinTorrent(ctx, output, input, torrent)
	} else {
		err = downloadBuiltinHTTP(ctx, output, input)
	}
	if err != nil {
		return err
	}
	return a.publishOfflineDirectory(ctx, s, parent, output)
}

func validateBuiltinURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("下载链接无效")
	}
	if u.Scheme == "magnet" {
		return validateBuiltinMagnet(raw)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("内置下载支持 HTTP、HTTPS、磁力和 BT 种子，不支持 FTP 或 ED2K 下载")
	}
	return nil
}

func (a *App) publishOfflineDirectory(ctx context.Context, s Storage, parent, output string) error {
	count := 0
	err := filepath.WalkDir(output, func(name string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("下载结果包含符号链接")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("下载结果不是普通文件")
		}
		if strings.HasSuffix(name, ".aria2") {
			return errors.New("下载结果尚未完成")
		}
		rel, err := filepath.Rel(output, name)
		if err != nil {
			return err
		}
		if err := a.storeUploadedFile(ctx, s, parent, filepath.ToSlash(rel), name); err != nil {
			return err
		}
		count++
		return nil
	})
	if err == nil && count == 0 {
		return errors.New("下载器没有生成可上传的文件")
	}
	return err
}
