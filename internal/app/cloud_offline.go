package app

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type offlineResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func offline115(ctx context.Context, s Storage, endpoint string, form url.Values, out any) error {
	var response struct {
		State bool            `json:"state"`
		Data  json.RawMessage `json:"data"`
	}
	if err := uploadAPI(ctx, s, "POST", "https://proapi.115.com/open/offline/"+endpoint, form, &response); err != nil {
		return err
	}
	if !response.State {
		return errors.New("115 拒绝离线任务，请检查授权和云下载权益")
	}
	if out != nil {
		return json.Unmarshal(response.Data, out)
	}
	return nil
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
		var added []struct {
			State    bool   `json:"state"`
			URL      string `json:"url"`
			InfoHash string `json:"info_hash"`
			Message  string `json:"message"`
		}
		err = offline115(ctx, s, "add_task_urls", url.Values{"urls": {strings.Join(input.URLs, "\n")}, "wp_path_id": {input.Parent}}, &added)
		if err != nil {
			fail(w, 400, err)
			return
		}
		for _, raw := range input.URLs {
			item := offlineResult{Name: raw, Message: "上游未返回此链接的提交结果"}
			for _, result := range added {
				if result.URL == raw {
					item.Success = result.State && result.InfoHash != ""
					item.Message = result.Message
					break
				}
			}
			results = append(results, item)
		}
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
	temp, err := os.CreateTemp("", "aether-seed-*.torrent")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	size, err := io.Copy(temp, io.LimitReader(source, (8<<20)+1))
	if err != nil || size > 8<<20 {
		return errors.New("种子内容过大或读取失败")
	}
	sha, err := uploadHash(ctx, temp, 0, size, sha1.New())
	if err != nil {
		return err
	}
	// Keep uploaded seed files in a dedicated directory; never remove user seeds.
	folder := ""
	children, err := a.rawList(ctx, s, parent)
	if err != nil {
		return err
	}
	for _, f := range children {
		if f.Name == "Aether种子" && f.IsDir {
			folder = f.ID
		}
	}
	if folder == "" {
		if err := a.createDirectory(ctx, s, parent, "Aether种子"); err != nil {
			return err
		}
		children, err = a.rawList(ctx, s, parent)
		if err != nil {
			return err
		}
		for _, f := range children {
			if f.Name == "Aether种子" && f.IsDir {
				folder = f.ID
			}
		}
	}
	if folder == "" {
		return errors.New("未获取种子目录 ID")
	}
	name := id() + ".torrent"
	if err := a.upload115(ctx, s, folder, name, temp, size); err != nil {
		return err
	}
	pick := ""
	for attempt := 0; attempt < 4 && pick == ""; attempt++ {
		children, err = a.rawList(ctx, s, folder)
		if err != nil {
			return err
		}
		for _, f := range children {
			if f.Name == name {
				pick = f.PickCode
			}
		}
		if pick == "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
	if pick == "" {
		return errors.New("种子已上传，但暂未获取 pick_code，请在网盘检查种子目录")
	}
	var parsed struct {
		InfoHash string     `json:"info_hash"`
		Name     string     `json:"torrent_name"`
		Files    []struct{} `json:"torrent_filelist"`
	}
	if err := offline115(ctx, s, "torrent", url.Values{"torrent_sha1": {strings.ToUpper(sha)}, "pick_code": {pick}}, &parsed); err != nil {
		return err
	}
	if parsed.InfoHash == "" || len(parsed.Files) == 0 {
		return errors.New("115 未解析出有效种子文件")
	}
	wanted := make([]string, len(parsed.Files))
	for i := range wanted {
		wanted[i] = strconv.Itoa(i)
	}
	saveName := strings.TrimSuffix(parsed.Name, ".torrent")
	if !safeName(saveName) {
		saveName = "离线下载"
	}
	return offline115(ctx, s, "add_task_bt", url.Values{"info_hash": {parsed.InfoHash}, "wanted": {strings.Join(wanted, ",")}, "save_path": {saveName}, "torrent_sha1": {strings.ToUpper(sha)}, "pick_code": {pick}, "wp_path_id": {parent}}, nil)
}
