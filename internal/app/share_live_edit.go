package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

type shareLiveActionKey struct{}
type shareLiveInput struct {
	URL           string `json:"url"`
	Name          string `json:"name"`
	Cron          string `json:"cron"`
	DirectoryName string `json:"directoryName"`
	TargetID      string `json:"targetId"`
	Confirm       bool   `json:"confirm"`
}

func (a *App) resolveShareLive(w http.ResponseWriter, r *http.Request) {
	var input shareLiveInput
	if !decode(w, r, &input) {
		return
	}
	provider, code, pass, err := parseShareLink(input.URL, "")
	if err != nil || provider != "mobile" && provider != "quark" {
		fail(w, 400, errors.New("请输入移动或夸克文件夹分享"))
		return
	}
	_, s, err := a.shareLiveTarget(input.TargetID, provider)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_, items, err := a.readShareSessionPages(ctx, &s, code, pass, "root", "", true)
	if err != nil {
		fail(w, 400, err)
		return
	}
	name := shareRootDirectory(items)
	if name == "" {
		fail(w, 400, errors.New("活动链接仅支持文件夹分享"))
		return
	}
	jsonResponse(w, 200, map[string]string{"name": name})
}

func (a *App) initializeShareLiveDirectory(ctx context.Context, s Storage, link *shareLiveLink) error {
	if _, err := a.shareLiveGuard(*link); err != nil {
		return err
	}
	files, err := a.rawList(ctx, s, link.Parent)
	if err != nil {
		return err
	}
	for _, f := range files {
		if strings.EqualFold(f.Name, link.DirectoryName) {
			return errors.New("保存目录已存在，未接管；请修改目录名称")
		}
	}
	if err := a.setShareLiveIntent(link.ID, "mkdir:"+link.DirectoryName); err != nil {
		return err
	}
	if err := a.createDirectory(ctx, s, link.Parent, link.DirectoryName); err != nil {
		return err
	}
	files, err = a.rawList(ctx, s, link.Parent)
	if err != nil {
		return errors.New("创建目录已提交但未确认，请核对云盘，勿重复提交")
	}
	for _, f := range files {
		if !f.IsDir || f.Name != link.DirectoryName {
			continue
		}
		link.DirectoryID = f.ID
		link.Intent = ""
		return a.store.update(func(st *State) error {
			for i := range st.ShareLiveLinks {
				if st.ShareLiveLinks[i].ID == link.ID {
					st.ShareLiveLinks[i].DirectoryID = f.ID
					st.ShareLiveLinks[i].Intent = ""
					return nil
				}
			}
			return errors.New("活链已移除，目录创建结果未登记")
		})
	}
	return errors.New("创建目录结果未确认，请核对云盘，不重复创建")
}

func (a *App) shareLiveTarget(key, provider string) (telegramTarget, Storage, error) {
	st := a.store.snapshotWithLogLimit(0)
	for _, target := range st.Notifications.Targets {
		if target.ID != key || target.Provider != provider || !target.shares() {
			continue
		}
		s, err := a.store.storage(target.StorageID)
		if err != nil || !s.Enabled || s.Type != provider {
			return telegramTarget{}, Storage{}, errors.New("转存目录的存储不可用")
		}
		if provider == "mobile" && !nativeMobile(s) {
			return telegramTarget{}, Storage{}, errors.New("请选择原生移动存储")
		}
		if err := storageAuthBlocked(s, time.Now()); err != nil {
			return telegramTarget{}, Storage{}, err
		}
		if target.Parent == "/" {
			target.Parent = rootOf(s)
		}
		return target, s, nil
	}
	return telegramTarget{}, Storage{}, errors.New("请选择该分享类型允许的转存目录")
}

func (a *App) createShareLive(w http.ResponseWriter, r *http.Request) {
	var input shareLiveInput
	if !decode(w, r, &input) {
		return
	}
	provider, code, pass, err := parseShareLink(input.URL, "")
	if err != nil || provider != "quark" && provider != "mobile" {
		fail(w, 400, errors.New("活动链接仅支持移动或夸克文件夹分享"))
		return
	}
	if err := validateShareLiveCron(strings.TrimSpace(input.Cron)); err != nil {
		fail(w, 400, err)
		return
	}
	if !a.shareLiveMu.TryLock() {
		fail(w, 409, errors.New("有活链操作正在执行"))
		return
	}
	defer a.shareLiveMu.Unlock()
	target, s, err := a.shareLiveTarget(input.TargetID, provider)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	_, items, err := a.readShareSession(ctx, &s, code, pass, "root", "")
	name := shareRootDirectory(items)
	if err != nil || name == "" {
		fail(w, 400, errors.New("无法读取文件夹分享名称，请核对链接和授权"))
		return
	}
	directory := strings.TrimSpace(input.DirectoryName)
	if directory == "" {
		directory = name
	}
	if !validShareDirectory(directory) || directory == "" {
		fail(w, 400, errors.New("请输入合法保存目录名称"))
		return
	}
	link := shareLiveLink{ID: id(), TargetID: target.ID, StorageID: s.ID, Provider: provider, Parent: target.Parent, DirectoryName: directory,
		Name: name, OriginalName: name, Link: input.URL, Stamp: telegramDigest(shareConfig(s)), Cron: strings.TrimSpace(input.Cron), Status: "idle", Message: "等待执行"}
	link.NextRun = shareLiveNext(link, time.Now())
	err = a.store.update(func(st *State) error {
		if len(st.ShareLiveLinks) >= 1000 {
			return errors.New("活动链接最多1000条")
		}
		for _, other := range st.ShareLiveLinks {
			if other.StorageID == link.StorageID && other.Parent == link.Parent && (shareRecordURL(other.Link) == shareRecordURL(link.Link) || strings.EqualFold(other.DirectoryName, link.DirectoryName)) {
				return errors.New("该分享或保存目录已存在，请编辑已有活动链接")
			}
		}
		st.ShareLiveLinks = append(st.ShareLiveLinks, link)
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	jsonResponse(w, 201, map[string]any{"id": link.ID, "name": name})
}

func (a *App) editShareLive(w http.ResponseWriter, r *http.Request, link shareLiveLink) {
	var input shareLiveInput
	if !decode(w, r, &input) {
		return
	}
	provider, _, _, err := parseShareLink(input.URL, "")
	if err != nil || provider != link.Provider || !validShareDirectory(input.DirectoryName) || input.DirectoryName == "" {
		fail(w, 400, errors.New("分享链接或目录名称无效"))
		return
	}
	if input.URL == link.Link && input.Name != "" && input.Name != link.Name {
		fail(w, 400, errors.New("分享名称来自原分享，不可手动修改"))
		return
	}
	if err := validateShareLiveCron(strings.TrimSpace(input.Cron)); err != nil {
		fail(w, 400, err)
		return
	}
	if link.Intent != "" || link.Status == "running" {
		fail(w, 409, errors.New("活链正在执行或上次写入未确认，请先核对云盘"))
		return
	}
	s, err := a.shareLiveGuard(link)
	if err != nil && link.Status == "paused" {
		_, current, targetErr := a.shareLiveTarget(link.TargetID, link.Provider)
		if targetErr == nil && current.ID == link.StorageID && telegramDigest(shareConfig(current)) == link.Stamp {
			s = current
			err = nil
		}
	}
	if err != nil {
		fail(w, 409, err)
		return
	}
	if input.TargetID == "" {
		input.TargetID = link.TargetID
	}
	target, destination, err := a.shareLiveTarget(input.TargetID, link.Provider)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if destination.ID != s.ID {
		fail(w, 400, errors.New("已转存目录迁移仅支持原存储池，请选择该存储池的转存目录"))
		return
	}
	if input.URL != link.Link {
		_, code, pass, _ := parseShareLink(input.URL, "")
		_, items, readErr := a.readShareSession(r.Context(), &s, code, pass, "root", "")
		name := shareRootDirectory(items)
		if readErr != nil || name == "" {
			fail(w, 400, errors.New("新分享名称或根目录不可核验，未修改"))
			return
		}
		link.Name, link.OriginalName = name, name
	}
	migrating := target.Parent != link.Parent || input.DirectoryName != link.DirectoryName
	for _, other := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
		if other.ID != link.ID && other.StorageID == link.StorageID && other.Parent == target.Parent && strings.EqualFold(other.DirectoryName, input.DirectoryName) {
			fail(w, 409, errors.New("目标保存目录被其他活链使用，未执行改名或迁移"))
			return
		}
	}
	if migrating && link.DirectoryID != "" {
		if !input.Confirm {
			fail(w, 400, errors.New("请确认移动或改名已转存的目录及其内容"))
			return
		}
		for _, other := range a.store.snapshotWithLogLimit(0).ShareLiveLinks {
			if other.ID != link.ID && other.StorageID == link.StorageID && other.DirectoryID == link.DirectoryID {
				fail(w, 409, errors.New("此目录被其他活链共用，不可单独迁移"))
				return
			}
		}
		a.runMu.Lock()
		release, reserveErr := a.reserveTaskFileNodes(r.Context(), []taskFileNode{
			{storage: s, id: link.DirectoryID},
			{storage: s, id: target.Parent, child: input.DirectoryName},
		})
		a.runMu.Unlock()
		if reserveErr != nil {
			fail(w, 409, reserveErr)
			return
		}
		defer release()
		if err := a.verifyShareLiveDirectory(r.Context(), s, link); err != nil {
			fail(w, 409, err)
			return
		}
		if err := a.setShareLiveIntent(link.ID, "relocate:"+target.Parent+":"+input.DirectoryName); err != nil {
			fail(w, 500, err)
			return
		}
		ctx := context.WithValue(r.Context(), shareLiveActionKey{}, link.ID)
		if input.DirectoryName != link.DirectoryName {
			err = a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "rename", Source: link.Parent, IDs: []string{link.DirectoryID}, Name: input.DirectoryName})
			if err == nil {
				link.DirectoryName = input.DirectoryName
				err = a.verifyShareLiveDirectory(ctx, s, link)
			}
		}
		if err == nil && target.Parent != link.Parent {
			err = a.cloudFileAction(ctx, s, s, fileActionRequest{Action: "move", Source: link.Parent, IDs: []string{link.DirectoryID}, TargetStorage: s.ID, Target: target.Parent})
			if err == nil {
				link.Parent = target.Parent
				err = a.verifyShareLiveDirectory(ctx, s, link)
			}
		}
		if err != nil {
			fail(w, 409, err)
			return
		}
	}
	oldCursor := link.IncrementalCursor
	sourceChanged := shareRecordURL(input.URL) != shareRecordURL(link.Link)
	link.TargetID, link.Parent = target.ID, target.Parent
	link.Link, link.Cron, link.DirectoryName = input.URL, strings.TrimSpace(input.Cron), input.DirectoryName
	if sourceChanged {
		link.IncrementalCursor, err = a.stageShareLiveCheckpoint(link, map[string]bool{}, time.Now())
		if err != nil {
			fail(w, 500, err)
			return
		}
		link.LastSuccess = time.Time{}
	}
	link.IntervalDays, link.Intent = 0, ""
	link.NextRun = shareLiveNext(link, time.Now())
	err = a.store.update(func(st *State) error {
		for _, other := range st.ShareLiveLinks {
			if other.ID != link.ID && other.StorageID == link.StorageID && other.Parent == link.Parent && strings.EqualFold(other.DirectoryName, link.DirectoryName) {
				return errors.New("目标保存目录被其他活链使用")
			}
		}
		for i := range st.ShareLiveLinks {
			if st.ShareLiveLinks[i].ID == link.ID {
				st.ShareLiveLinks[i] = link
			}
		}
		for i := range st.ShareTransfers {
			if st.ShareTransfers[i].LiveID == link.ID {
				st.ShareTransfers[i].DirectoryName = link.DirectoryName
				st.ShareTransfers[i].TargetID = link.TargetID
			}
		}
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	a.cache.clear()
	if sourceChanged {
		a.removeShareLiveCheckpoint(link, oldCursor)
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}

func (a *App) syncShareLiveDirectory(ctx context.Context, s Storage, req fileActionRequest) error {
	if req.Action != "rename" && req.Action != "move" {
		return nil
	}
	parent := req.Source
	if req.Action == "move" {
		parent = req.Target
	}
	files, err := a.rawList(ctx, s, parent)
	if err != nil {
		return errors.New("云端操作已提交，但活动链接目录回传未确认，请刷新核对")
	}
	return a.store.update(func(st *State) error {
		for i := range st.ShareLiveLinks {
			link := &st.ShareLiveLinks[i]
			if link.StorageID != s.ID {
				continue
			}
			selected := false
			for _, id := range req.IDs {
				if id == link.DirectoryID {
					selected = true
				}
			}
			if !selected {
				continue
			}
			confirmed := false
			for _, file := range files {
				if file.ID != link.DirectoryID || !file.IsDir {
					continue
				}
				if req.Action == "rename" && file.Name != req.Name {
					return errors.New("云端改名结果未确认，活动链接目录名称未更新，请刷新核对")
				}
				confirmed = true
				link.Parent, link.DirectoryName = parent, file.Name
				if req.Action == "move" {
					found := false
					for _, target := range st.Notifications.Targets {
						p := target.Parent
						if p == "/" {
							p = rootOf(s)
						}
						if target.StorageID == s.ID && target.Provider == s.Type && target.shares() && p == parent {
							link.TargetID = target.ID
							found = true
							break
						}
					}
					if !found {
						link.Status, link.Message, link.NextRun = "paused", "保存目录已手动移动，请在活动链接中选择有效转存目录", time.Time{}
					}
				}
				for j := range st.ShareTransfers {
					if st.ShareTransfers[j].LiveID == link.ID {
						st.ShareTransfers[j].DirectoryName = file.Name
						st.ShareTransfers[j].TargetID = link.TargetID
					}
				}
			}
			if !confirmed {
				return errors.New("云端目录操作已提交但身份未确认，活动链接记录未更新，请刷新核对")
			}
		}
		return nil
	})
}
