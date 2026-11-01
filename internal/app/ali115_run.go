package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

type ali115PlanFile struct {
	File aliFile
	Path string
}

func ali115Plan(ctx context.Context, s *aliSession, share aliShare, token, extensions string) ([]ali115PlanFile, string, error) {
	var info struct {
		Name string `json:"share_name"`
	}
	if err := aliRequest(ctx, "POST", aliAPI+"/adrive/v3/share_link/get_share_by_anonymous?share_id="+url.QueryEscape(share.Code), map[string]string{"share_id": share.Code}, nil, "", &info); err != nil {
		return nil, "", err
	}
	name := info.Name
	if !ali115SafeName(name) {
		name = share.Code
	}
	root, err := s.list(ctx, share.Folder, share.Code, token)
	if err != nil {
		return nil, "", err
	}
	prefix := name
	if len(root) == 1 && root[0].Type == "folder" && share.Folder == "root" {
		prefix = ""
	}
	allowed := map[string]bool{}
	for _, ext := range strings.Split(extensions, ";") {
		allowed[ext] = true
	}
	files := []ali115PlanFile{}
	seen, paths := map[string]bool{}, map[string]bool{}
	count := 0
	var walk func([]aliFile, string, int) error
	walk = func(items []aliFile, parent string, depth int) error {
		if depth > 64 {
			return errors.New("分享目录超过64层，已停止")
		}
		for _, f := range items {
			count++
			if count > 20000 || seen[f.ID] {
				return errors.New("分享超过20000项或出现重复文件身份，已停止")
			}
			seen[f.ID] = true
			rel := path.Join(parent, f.Name)
			key := strings.ToLower(rel)
			if paths[key] {
				return errors.New("分享中存在重名路径，未执行转存")
			}
			paths[key] = true
			if f.Type == "folder" {
				children, err := s.list(ctx, f.ID, share.Code, token)
				if err != nil {
					return err
				}
				if err = walk(children, rel, depth+1); err != nil {
					return err
				}
			} else if f.Size > 0 && allowed[strings.TrimPrefix(strings.ToLower(path.Ext(f.Name)), ".")] {
				files = append(files, ali115PlanFile{File: f, Path: rel})
			}
		}
		return nil
	}
	if err = walk(root, prefix, 0); err != nil {
		return nil, "", err
	}
	return files, name, nil
}

type ali115Run struct {
	a       *App
	job     ali115Receipt
	session *aliSession
	storage Storage
	client  *driver.Pan115Client
	guard   func() error
}

func (h *ali115Run) check(ctx context.Context) error {
	if ctx.Err() != nil {
		return errors.New("阿里云盘秒传已中断，请核对未确认结果")
	}
	if err := h.a.ali115Current(h.job); err != nil {
		return err
	}
	if h.guard != nil {
		if err := h.guard(); err != nil {
			return err
		}
	}
	if h.session != nil && time.Now().After(h.session.Until) {
		session, err := h.a.aliAccount(ctx)
		if err != nil {
			return err
		}
		h.session = session
	}
	return nil
}
func (h *ali115Run) receiver(ctx context.Context) error {
	trail, err := h.a.cloud115DirectoryTrail(ctx, h.storage, h.job.Target.Directory)
	if err != nil {
		return errors.New("115接收目录无法核验，已停止")
	}
	inside := false
	names := []string{}
	for _, c := range trail {
		if c.ID == rootOf(h.storage) || rootOf(h.storage) == "/" && c.ID == "0" {
			inside = true
			continue
		}
		if inside {
			names = append(names, c.Name)
		}
	}
	if !inside || "/"+strings.Join(names, "/") != h.job.Target.Path {
		return errors.New("115接收目录路径已变化，请重新添加")
	}
	return nil
}

func (h *ali115Run) underReceiver(ctx context.Context, parent string) error {
	trail, err := h.a.cloud115DirectoryTrail(ctx, h.storage, parent)
	if err != nil {
		return errors.New("115接收子目录无法核验")
	}
	for _, c := range trail {
		if c.ID == h.job.Target.Directory {
			return h.receiver(ctx)
		}
	}
	return errors.New("115接收子目录已移出所选目录，未写入")
}
func (h *ali115Run) intent(ctx context.Context, operation string) error {
	if err := h.check(ctx); err != nil {
		return err
	}
	if err := h.receiver(ctx); err != nil {
		return err
	}
	if err := h.a.ali115Update(h.job.ID, func(r *ali115Receipt) { r.Intent = operation; r.Message = operation }); err != nil {
		return err
	}
	return h.check(ctx)
}
func (h *ali115Run) accepted(temp *ali115Temp) error {
	return h.a.ali115Update(h.job.ID, func(r *ali115Receipt) {
		r.Intent = ""
		if temp != nil {
			r.Temporary = append(r.Temporary, *temp)
		}
	})
}
func (h *ali115Run) create(ctx context.Context, parent, name string, track bool) (string, error) {
	if err := h.intent(ctx, "创建阿里临时目录 "+name); err != nil {
		return "", err
	}
	key, err := h.session.create(ctx, parent, name)
	if err != nil {
		return "", err
	}
	items, err := h.session.list(ctx, parent, "", "")
	if err != nil {
		return "", err
	}
	matches, identity := 0, false
	for _, f := range items {
		if strings.EqualFold(f.Name, name) {
			matches++
			identity = f.ID == key && f.Name == name && f.Type == "folder"
		}
	}
	if matches != 1 || !identity {
		return "", errors.New("阿里新建临时目录读回身份不符，未继续转存")
	}
	var temp *ali115Temp
	if track {
		temp = &ali115Temp{ID: key, Parent: parent, Name: name, Type: "folder"}
	}
	return key, h.accepted(temp)
}
func (h *ali115Run) ownTemp(ctx context.Context, temp ali115Temp) error {
	if err := h.check(ctx); err != nil {
		return err
	}
	var receipt ali115Receipt
	for _, r := range h.a.store.snapshotWithLogLimit(0).Ali115Receipts {
		if r.ID == h.job.ID {
			receipt = r
		}
	}
	if receipt.ID == "" {
		return errors.New("临时文件归属回执丢失，未清理")
	}
	registered := false
	for _, saved := range receipt.Temporary {
		if saved == temp {
			registered = true
			break
		}
	}
	if !registered {
		return errors.New("临时文件未经本任务登记，未清理")
	}
	visited := map[string]bool{}
	for current := temp; ; {
		if visited[current.ID] {
			return errors.New("临时文件归属循环，未清理")
		}
		visited[current.ID] = true
		items, err := h.session.list(ctx, current.Parent, "", "")
		if err != nil {
			return err
		}
		matches := 0
		for _, f := range items {
			if f.ID == current.ID && f.Name == current.Name && f.Type == current.Type && (f.Type == "folder" || f.Size == current.Size && strings.EqualFold(f.SHA1, current.SHA1)) {
				matches++
			}
		}
		if matches != 1 {
			return errors.New("阿里临时文件身份已改变，未清理")
		}
		found := false
		for _, parent := range receipt.Temporary {
			if parent.ID == current.Parent && parent.Type == "folder" {
				current = parent
				found = true
				break
			}
		}
		if !found {
			roots, err := h.session.list(ctx, "root", "", "")
			if err != nil {
				return err
			}
			for _, f := range roots {
				if f.ID == current.Parent && f.Name == "Aether" && f.Type == "folder" && current.Name == "rapid-"+h.job.ID {
					return nil
				}
			}
			return errors.New("阿里临时任务目录已移出/Aether，未清理")
		}
	}
}
func (h *ali115Run) remove(ctx context.Context, temp ali115Temp) error {
	if err := h.ownTemp(ctx, temp); err != nil {
		return err
	}
	if temp.Type == "folder" {
		items, err := h.session.list(ctx, temp.ID, "", "")
		if err != nil {
			return err
		}
		if len(items) != 0 {
			return errors.New("临时目录存在未登记内容，未清理")
		}
	}
	if err := h.intent(ctx, "清理阿里临时文件 "+temp.Name); err != nil {
		return err
	}
	if err := aliRequest(ctx, "POST", aliAPI+"/v3/file/delete", map[string]string{"drive_id": h.session.Drive, "file_id": temp.ID}, h.session, "", nil); err != nil {
		return err
	}
	items, err := h.session.list(ctx, temp.Parent, "", "")
	if err != nil {
		return err
	}
	for _, f := range items {
		if f.ID == temp.ID {
			return errors.New("阿里临时文件删除未确认，未重复删除")
		}
	}
	return h.a.ali115Update(h.job.ID, func(r *ali115Receipt) {
		r.Intent = ""
		kept := []ali115Temp{}
		for _, v := range r.Temporary {
			if v.ID != temp.ID {
				kept = append(kept, v)
			}
		}
		r.Temporary = kept
	})
}
func (h *ali115Run) stage(ctx context.Context, share aliShare, token, parent string, source aliFile) (ali115Temp, error) {
	if err := h.intent(ctx, "转存阿里分享文件 "+source.Name); err != nil {
		return ali115Temp{}, err
	}
	var out struct {
		Responses []struct {
			ID     string `json:"id"`
			Status int    `json:"status"`
			Body   struct {
				ID    string `json:"file_id"`
				Async string `json:"async_task_id"`
			} `json:"body"`
		} `json:"responses"`
	}
	body := map[string]any{"resource": "file", "requests": []any{map[string]any{"id": "0", "method": "POST", "url": "/file/copy", "headers": map[string]string{"Content-Type": "application/json"}, "body": map[string]any{"file_id": source.ID, "share_id": share.Code, "auto_rename": false, "to_parent_file_id": parent, "to_drive_id": h.session.Drive}}}}
	if err := aliRequest(ctx, "POST", aliAPI+"/adrive/v2/batch", body, h.session, token, &out); err != nil {
		return ali115Temp{}, err
	}
	if len(out.Responses) != 1 || out.Responses[0].ID != "0" || out.Responses[0].Status < 200 || out.Responses[0].Status >= 300 || out.Responses[0].Body.ID == "" && out.Responses[0].Body.Async == "" {
		return ali115Temp{}, errors.New("阿里转存结果未确认；请检查容量与临时目录，未重试")
	}
	for attempt := 0; attempt < 30; attempt++ {
		if err := h.check(ctx); err != nil {
			return ali115Temp{}, err
		}
		items, err := h.session.list(ctx, parent, "", "")
		if err != nil {
			return ali115Temp{}, err
		}
		if len(items) > 1 {
			return ali115Temp{}, errors.New("阿里临时目录出现额外文件，未继续秒传或清理")
		}
		if len(items) == 1 {
			f := items[0]
			if f.Name != source.Name || f.Type != "file" || f.Size != source.Size || !simulcastSHA1(f.SHA1) || out.Responses[0].Body.ID != "" && out.Responses[0].Body.ID != f.ID {
				return ali115Temp{}, errors.New("阿里副本身份或SHA1无效，已保留临时文件")
			}
			temp := ali115Temp{ID: f.ID, Parent: parent, Name: f.Name, Type: f.Type, Size: f.Size, SHA1: f.SHA1}
			return temp, h.accepted(&temp)
		}
		if !telegramPause(ctx, time.Second) {
			return ali115Temp{}, errors.New("阿里转存等待已中断")
		}
	}
	return ali115Temp{}, errors.New("阿里转存尚未可见，已保留临时目录；未重试")
}
func (h *ali115Run) checksum(ctx context.Context, temp ali115Temp, spec string) (string, error) {
	start, end, err := aliRange(spec, temp.Size)
	if err != nil {
		return "", err
	}
	if err = h.ownTemp(ctx, temp); err != nil {
		return "", err
	}
	var out struct {
		URL string `json:"url"`
	}
	if err = aliRequest(ctx, "POST", aliAPI+"/v2/file/get_download_url", map[string]any{"drive_id": h.session.Drive, "file_id": temp.ID, "expire_sec": 600}, h.session, "", &out); err != nil {
		return "", err
	}
	u, err := url.Parse(out.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !(strings.HasSuffix(u.Hostname(), ".aliyuncs.com") || strings.HasSuffix(u.Hostname(), ".aliyundrive.net") || strings.HasSuffix(u.Hostname(), ".alipan.com")) {
		return "", errors.New("阿里分片下载地址无效")
	}
	return read115RenameRange(ctx, Download{URL: out.URL, Headers: http.Header{"Referer": {"https://www.alipan.com/"}}}, temp.Size, start, end)
}
func (h *ali115Run) targetParent(ctx context.Context, relative string) (string, error) {
	parent := h.job.Target.Directory
	for _, name := range strings.Split(path.Dir(relative), "/") {
		if name == "." || name == "" {
			continue
		}
		if err := h.check(ctx); err != nil {
			return "", err
		}
		files, err := h.a.simulcast.list(ctx, h.storage, h.client, parent)
		if err != nil {
			return "", errors.New("无法核验115目录")
		}
		found := ""
		for _, f := range files {
			if strings.EqualFold(f.Name, name) {
				if found != "" || f.Name != name || !f.IsDirectory {
					return "", errors.New("115目录名称冲突，未覆盖")
				}
				found = f.FileID
			}
		}
		if found == "" {
			if err := h.underReceiver(ctx, parent); err != nil {
				return "", err
			}
			if err := h.intent(ctx, "创建115子目录 "+name); err != nil {
				return "", err
			}
			if err = h.a.waitAPI(ctx, h.storage.ID); err != nil {
				return "", err
			}
			found, err = h.client.Mkdir(parent, name)
			if err != nil || !simulcastID(found) || found == "0" {
				return "", errors.New("115新建目录结果未确认，未重试")
			}
			files, err = h.a.simulcast.list(ctx, h.storage, h.client, parent)
			if err != nil {
				return "", err
			}
			valid := false
			for _, f := range files {
				if f.FileID == found && f.ParentID == parent && f.Name == name && f.IsDirectory {
					valid = true
				}
			}
			if !valid {
				return "", errors.New("115新建目录身份未确认")
			}
			if err = h.accepted(nil); err != nil {
				return "", err
			}
		}
		parent = found
	}
	return parent, nil
}
func (h *ali115Run) upload(ctx context.Context, temp ali115Temp, parent string) (bool, error) {
	return h.uploadWith(ctx, temp, parent, func() (string, bool, error) {
		return h.a.submit115Hash(ctx, h.storage, h.client, &driver.File{Name: temp.Name, Size: temp.Size, Sha1: temp.SHA1, ParentID: parent}, temp.Name, func(spec string) (string, error) { return h.checksum(ctx, temp, spec) })
	})
}

func (h *ali115Run) uploadWith(ctx context.Context, temp ali115Temp, parent string, submit func() (string, bool, error)) (bool, error) {
	if err := h.check(ctx); err != nil {
		return false, err
	}
	files, err := h.a.simulcast.list(ctx, h.storage, h.client, parent)
	if err != nil {
		return false, errors.New("115目标读取失败")
	}
	previous := map[string]bool{}
	exists := false
	for _, f := range files {
		previous[f.FileID] = true
		if strings.EqualFold(f.Name, temp.Name) {
			if exists || f.Name != temp.Name || f.IsDirectory || f.Size != temp.Size || !strings.EqualFold(f.Sha1, temp.SHA1) {
				return false, errors.New("115已有同名文件与源文件不符，未覆盖")
			}
			exists = true
		}
	}
	if exists {
		return true, nil
	}
	if err = h.intent(ctx, "向115秒传 "+temp.Name); err != nil {
		return false, err
	}
	if err = h.underReceiver(ctx, parent); err != nil {
		return false, err
	}
	pick, noWrite, err := submit()
	if err != nil {
		if noWrite {
			if e := h.accepted(nil); e != nil {
				return false, e
			}
		}
		return false, errors.New("115秒传未完成，已保留阿里临时副本；请检查115权限或秒传记录，未重试")
	}
	if pick == "" {
		return false, errors.New("115未返回新文件凭据，临时副本保留")
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err = h.check(ctx); err != nil {
			return false, err
		}
		files, err = h.a.simulcast.list(ctx, h.storage, h.client, parent)
		if err != nil {
			return false, errors.New("115结果无法读回，临时副本保留")
		}
		matches := 0
		var found *driver.File
		for _, f := range files {
			if strings.EqualFold(f.Name, temp.Name) {
				matches++
				if !previous[f.FileID] && f.Name == temp.Name && f.ParentID == parent && f.PickCode == pick && !f.IsDirectory && f.Size == temp.Size && strings.EqualFold(f.Sha1, temp.SHA1) {
					found = f
				}
			}
		}
		if matches > 1 || matches == 1 && found == nil {
			return false, errors.New("115结果重名或身份不符，临时副本保留")
		}
		if found != nil {
			if err = h.a.waitAPI(ctx, h.storage.ID); err != nil {
				return false, err
			}
			confirmed, e := h.client.GetFile(found.FileID)
			if e != nil || !same115RenameFile(found, confirmed) {
				return false, errors.New("115文件二次核验失败，临时副本保留")
			}
			return false, h.accepted(nil)
		}
		if !telegramPause(ctx, 250*time.Millisecond) {
			break
		}
	}
	return false, errors.New("115文件尚未可见，临时副本保留；未重试")
}
func (a *App) executeAli115(ctx context.Context, job ali115Receipt, guard func() error) error {
	h := &ali115Run{a: a, job: job, guard: guard}
	if err := h.check(ctx); err != nil {
		return err
	}
	s, err := a.store.storage(job.Target.StorageID)
	if err != nil {
		return err
	}
	h.storage = s
	a.runMu.Lock()
	release, err := a.reserveTaskFileNodes(ctx, []taskFileNode{{storage: s, id: job.Target.Directory}})
	a.runMu.Unlock()
	if err != nil {
		return err
	}
	defer release()
	if err = h.receiver(ctx); err != nil {
		return err
	}
	h.session, err = a.aliAccount(ctx)
	if err != nil {
		return err
	}
	h.client, err = client115(ctx, s)
	if err != nil {
		return errors.New("115账号初始化失败")
	}
	share, err := parseAliShare(job.Link)
	if err != nil {
		return err
	}
	var token struct {
		Token string `json:"share_token"`
	}
	if err = aliRequest(ctx, "POST", aliAPI+"/v2/share_link/get_share_token", map[string]string{"share_id": share.Code, "share_pwd": share.Password}, nil, "", &token); err != nil {
		return err
	}
	if token.Token == "" {
		return errors.New("阿里分享访问失败，请核对提取码")
	}
	files, name, err := ali115Plan(ctx, h.session, share, token.Token, job.Extensions)
	if err != nil {
		return err
	}
	storedCount := 0
	for _, r := range a.store.snapshotWithLogLimit(0).Ali115Receipts {
		storedCount += len(r.Files)
	}
	if storedCount+len(files) > 20000 {
		return errors.New("秒传文件记录累计超过20000项保护范围，未转存")
	}
	if err = a.ali115Update(job.ID, func(r *ali115Receipt) { r.Total, r.Name = len(files), name }); err != nil {
		return err
	}
	if len(files) == 0 {
		return nil
	}
	roots, err := h.session.list(ctx, "root", "", "")
	if err != nil {
		return err
	}
	root := ""
	for _, f := range roots {
		if strings.EqualFold(f.Name, "Aether") {
			if root != "" || f.Name != "Aether" || f.Type != "folder" {
				return errors.New("阿里/Aether名称冲突，未转存")
			}
			root = f.ID
		}
	}
	if root == "" {
		root, err = h.create(ctx, "root", "Aether", false)
		if err != nil {
			return err
		}
	}
	jobName := "rapid-" + job.ID
	jobDir, err := h.create(ctx, root, jobName, true)
	if err != nil {
		return err
	}
	for index, f := range files {
		if err = h.check(ctx); err != nil {
			return err
		}
		parent, err := h.targetParent(ctx, f.Path)
		if err != nil {
			return err
		}
		folderName := strconv.Itoa(index + 1)
		folder, err := h.create(ctx, jobDir, folderName, true)
		if err != nil {
			return err
		}
		temp, err := h.stage(ctx, share, token.Token, folder, f.File)
		exists := false
		if err == nil {
			exists, err = h.upload(ctx, temp, parent)
		}
		status, message := "success", "秒传成功"
		if exists {
			status, message = "skipped", "目标文件已存在"
		}
		if err != nil {
			status, message = "failed", err.Error()
		}
		if saveErr := a.ali115Update(job.ID, func(r *ali115Receipt) {
			r.Files = append(r.Files, ali115FileResult{Name: f.Path, Status: status, Message: message})
			if err != nil {
				r.Failed++
			} else if exists {
				r.Skipped++
			} else {
				r.Success++
			}
		}); saveErr != nil {
			return saveErr
		}
		if err != nil {
			return fmt.Errorf("%s：%w；阿里临时副本保留在/Aether/%s", f.File.Name, err, jobName)
		}
		if err = h.remove(ctx, temp); err != nil {
			return fmt.Errorf("115结果已核验，阿里副本清理未确认：%w", err)
		}
		if err = h.remove(ctx, ali115Temp{ID: folder, Parent: jobDir, Name: folderName, Type: "folder"}); err != nil {
			return err
		}
	}
	if err = h.remove(ctx, ali115Temp{ID: jobDir, Parent: root, Name: jobName, Type: "folder"}); err != nil {
		return err
	}
	a.cache.clear()
	a.invalidateTransferPaths(s)
	return nil
}
