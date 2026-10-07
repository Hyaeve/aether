package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

type offlineResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (a *App) cloudOffline(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	var input struct {
		StorageID string   `json:"storageId"`
		Parent    string   `json:"parent"`
		URLs      []string `json:"urls"`
	}
	torrent := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
	if torrent {
		r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			if r.MultipartForm != nil {
				r.MultipartForm.RemoveAll()
			}
			fail(w, 400, errors.New("种子总大小不能超过 64MB"))
			return
		}
		defer r.MultipartForm.RemoveAll()
		input.StorageID, input.Parent = r.FormValue("storageId"), r.FormValue("parent")
	} else if !decode(w, r, &input) {
		return
	}
	s, err := a.store.storage(input.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if input.Parent == "" || input.Parent == "/" {
		input.Parent = rootOf(s)
	}
	if _, err := a.rawList(ctx, s, input.Parent); err != nil {
		fail(w, 400, errors.New("离线目录不可访问"))
		return
	}
	if torrent && r.FormValue("mode") == "cas" {
		a.importCAS(w, r, ctx, s, input.Parent)
		return
	}
	if s.Type != "115" {
		a.builtinOffline(w, r, s, input.Parent, input.URLs, torrent)
		return
	}
	results := []offlineResult{}
	if !torrent {
		if len(input.URLs) == 0 || len(input.URLs) > 50 {
			fail(w, 400, errors.New("每批支持 1–50 个链接"))
			return
		}
		for i, raw := range input.URLs {
			raw = strings.TrimSpace(raw)
			scheme, _, ok := strings.Cut(raw, ":")
			if !ok || len(raw) > 8192 || strings.ContainsAny(raw, "\r\n") || !map[string]bool{"http": true, "https": true, "ftp": true, "magnet": true, "ed2k": true}[strings.ToLower(scheme)] {
				fail(w, 400, errors.New("链接须为 HTTP、HTTPS、FTP、磁力或电驴地址"))
				return
			}
			input.URLs[i] = raw
		}
		results = submit115URIs(ctx, s, input.Parent, input.URLs)
	} else {
		files := r.MultipartForm.File["torrents"]
		if len(files) == 0 || len(files) > 20 {
			fail(w, 400, errors.New("每批支持 1–20 个种子"))
			return
		}
		for _, header := range files {
			if !strings.HasSuffix(strings.ToLower(header.Filename), ".torrent") || header.Size <= 0 || header.Size > 8<<20 {
				fail(w, 400, errors.New("仅支持 8MB 以内的 .torrent 文件"))
				return
			}
		}
		for _, header := range files {
			source, err := header.Open()
			if err != nil {
				results = append(results, offlineResult{Name: header.Filename, Message: "种子读取失败"})
				continue
			}
			err = a.submit115Torrent(ctx, s, input.Parent, source)
			source.Close()
			item := offlineResult{Name: header.Filename, Success: err == nil, Message: "BT 任务已提交"}
			if err != nil {
				item.Message = err.Error()
			}
			results = append(results, item)
		}
	}
	a.cache.clear()
	a.store.event("info", "files", fmt.Sprintf("提交 115 离线下载，返回 %d 项结果", len(results)))
	jsonResponse(w, 200, results)
}

func (a *App) submit115Torrent(ctx context.Context, s Storage, parent string, source io.Reader) error {
	meta, err := metainfo.Load(io.LimitReader(source, 8<<20))
	if err != nil {
		return errors.New("无效的种子文件")
	}
	info, err := meta.UnmarshalInfo()
	if err != nil || !info.HasV1() || info.PieceLength <= 0 || len(info.Pieces) == 0 {
		return errors.New("115 云下载需要有效的 v1 或混合种子")
	}
	if info.Private != nil && *info.Private {
		return errors.New("115 CK 驱动暂不支持私有种子，请使用官方客户端提交")
	}
	// 115driver accepts magnet URIs; preserve the torrent hash and trackers.
	results := submit115URIs(ctx, s, parent, []string{meta.Magnet(nil, &info).String()})
	if !results[0].Success {
		return errors.New(results[0].Message)
	}
	return nil
}
