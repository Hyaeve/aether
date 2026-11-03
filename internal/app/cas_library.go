package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type casLibrarySettings struct {
	Directory       string `json:"directory"`
	MediaExtensions string `json:"mediaExtensions"`
}
type casLibraryRuntime struct {
	Results map[string]quarkLibraryResult `json:"results,omitempty"`
}

func effectiveCASLibrary(st State) casLibrarySettings {
	if st.CASLibrary != nil {
		return *st.CASLibrary
	}
	return casLibrarySettings{MediaExtensions: taskVideoExtensions + ";" + taskAudioExtensions}
}
func initCASLibrary(st *State) {
	if st.CASLibraryRuntime.Results == nil {
		st.CASLibraryRuntime.Results = map[string]quarkLibraryResult{}
	}
}
func casLibraryCardAt(st State, key string) (quarkLibraryCard, bool) {
	for _, c := range st.CASLibraryCards {
		if c.ID == key {
			return c, true
		}
	}
	return quarkLibraryCard{}, false
}
func validateCASLibrary(st State) error {
	cfg := effectiveCASLibrary(st)
	if cfg.Directory != "" && (!filepath.IsAbs(cfg.Directory) || strings.ContainsAny(cfg.Directory, "\x00\r\n") || len(cfg.Directory) > 4096) {
		return errors.New("请选择CAS链库绝对生成位置")
	}
	if _, err := normalizeExtensions(cfg.MediaExtensions); err != nil {
		return err
	}
	if len(st.CASLibraryCards) > 200 {
		return errors.New("CAS链库最多200条分享记录")
	}
	seen, ids := map[string]bool{}, map[string]bool{}
	for _, card := range st.CASLibraryCards {
		provider, code, _, err := parseShareLink(card.Secret, "")
		if err != nil || provider != "mobile" {
			return errors.New("CAS链库只接受移动云盘分享链接")
		}
		if ids[card.ID] || !shareCodePattern.MatchString(card.ID) || seen[code] || card.DirectoryName == "" {
			return errors.New("CAS分享重复或标识/目录名称无效")
		}
		ids[card.ID], seen[code] = true, true
		if err := validateShareLibraryFields(&card); err != nil {
			return err
		}
	}
	return validateShareLibraryOutputs(st)
}
func (a *App) casLibraryTitle(ctx context.Context, secret string) (string, error) {
	provider, code, _, err := parseShareLink(secret, "")
	if err != nil || provider != "mobile" {
		return "", errors.New("/cas 只接受移动云盘分享链接")
	}
	var data struct {
		Response struct {
			Links []struct {
				Name string `json:"lkName"`
			} `json:"outLinkGeneral"`
		} `json:"getOutLinkGeneralResp"`
	}
	if err := mobileGenerationShareJSON(ctx, "getOutLinkGeneral", map[string]any{"getOutLinkGeneralReq": map[string]any{"linkID": code, "isPasswd": 1}}, &data); err != nil {
		return "", err
	}
	if len(data.Response.Links) != 1 {
		return "", errors.New("移动分享信息不完整")
	}
	title := strings.TrimSpace(data.Response.Links[0].Name)
	if title == "" {
		title = "移动分享-" + quarkLibraryStamp(code)[:8]
	}
	if len(title) > 255 {
		return "", errors.New("分享名称过长")
	}
	return title, nil
}
func (a *App) casLibraryResolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	title, err := a.casLibraryTitle(ctx, in.URL)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"name": title})
}
func (a *App) casLibraryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	key := r.PathValue("id")
	st := a.store.snapshotWithLogLimit(0)
	if r.Method == "GET" && key == "" {
		cards := []map[string]any{}
		total := 0
		for _, c := range st.CASLibraryCards {
			result := st.CASLibraryRuntime.Results[c.ID]
			total += result.Generated + result.Skipped
			cards = append(cards, map[string]any{"id": c.ID, "url": c.Secret, "name": c.Name, "shareName": c.ShareName, "directory": c.Directory, "directoryName": c.DirectoryName, "cron": c.Cron, "enabled": c.Enabled, "mediaExtensions": c.MediaExtensions, "excludeDirs": c.ExcludeDirs, "excludeFiles": c.ExcludeFiles, "excludeTypes": c.ExcludeTypes, "result": result})
		}
		jsonResponse(w, 200, map[string]any{"cards": cards, "settings": effectiveCASLibrary(st), "stats": map[string]int{"strm": total}})
		return
	}
	if !a.casLibraryMu.TryLock() {
		fail(w, 409, errors.New("CAS链库正在生成，请稍后修改"))
		return
	}
	defer a.casLibraryMu.Unlock()
	if key == "settings" && r.Method == "PUT" {
		var cfg casLibrarySettings
		if !decode(w, r, &cfg) {
			return
		}
		cfg.Directory = strings.TrimSpace(cfg.Directory)
		err := a.store.update(func(st *State) error { st.CASLibrary = &cfg; return validateCASLibrary(*st) })
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, cfg)
		return
	}
	old, exists := casLibraryCardAt(st, key)
	if r.Method == "DELETE" && exists {
		err := a.store.update(func(st *State) error {
			out := []quarkLibraryCard{}
			for _, c := range st.CASLibraryCards {
				if c.ID != key {
					out = append(out, c)
				}
			}
			st.CASLibraryCards = out
			delete(st.CASLibraryRuntime.Results, key)
			return nil
		})
		if err != nil {
			fail(w, 500, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" && r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	if r.Method == "PUT" && !exists {
		fail(w, 404, errors.New("CAS分享不存在"))
		return
	}
	var in struct {
		quarkLibraryCard
		URL string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	card := in.quarkLibraryCard
	if exists {
		card.ID = old.ID
	} else {
		card.ID = id()
	}
	card.Secret = in.URL
	card.CascadeCleanup = "off"
	card.MetadataExtensions = ""
	card.ScrapeEnabled = false
	provider, code, pass, err := parseShareLink(card.Secret, "")
	if err != nil || provider != "mobile" {
		fail(w, 400, errors.New("CAS链库只接受移动云盘分享链接"))
		return
	}
	card.Secret = mobileLibrarySecret(code, pass)
	if err := validateShareLibraryFields(&card); err != nil {
		fail(w, 400, err)
		return
	}
	if exists {
		if saved, err := a.saveShareLibraryMetadata("cas", old, card); saved {
			if err != nil {
				fail(w, 409, err)
			} else {
				jsonResponse(w, 200, map[string]string{"id": card.ID, "message": "CAS链接配置已保存"})
			}
			return
		}
	}
	if err := shareLibraryDirectoryAvailable(st, card, old); err != nil {
		jsonResponse(w, 409, map[string]string{"error": err.Error(), "field": "directoryName"})
		return
	}
	err = a.store.update(func(st *State) error {
		if err := shareLibraryNamesAvailable(*st, card); err != nil {
			return err
		}
		if exists {
			for i := range st.CASLibraryCards {
				if st.CASLibraryCards[i].ID == key {
					st.CASLibraryCards[i] = card
				}
			}
		} else {
			st.CASLibraryCards = append(st.CASLibraryCards, card)
		}
		initCASLibrary(st)
		st.CASLibraryRuntime.Results[card.ID] = quarkLibraryResult{Status: "idle", Message: "等待执行", NextRun: quarkLibraryNext(card, time.Now())}
		return validateCASLibrary(*st)
	})
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"id": card.ID, "message": "CAS链接已保存，可点击立即生成"})
}

func casLibraryUncertain(st State, card quarkLibraryCard) bool {
	for _, j := range st.GenerationJobs {
		if j.LibrarySource == casLibrarySourceStamp(card.Secret) && generationHasTemporary(j) {
			return true
		}
	}
	return false
}
func casLibrarySourceStamp(secret string) string {
	provider, code, _, _ := parseShareLink(secret, "")
	return quarkLibraryStamp(provider + ":" + code)
}
func (a *App) runCASLibrary(ctx context.Context, card quarkLibraryCard, external func() error) (result quarkLibraryResult, runErr error) {
	st := a.store.snapshotWithLogLimit(0)
	cfg := effectiveCASLibrary(st)
	cfgStamp := telegramDigest(cfg)
	result = quarkLibraryResult{Status: "running", Message: "正在读取移动分享CAS信息", LastRun: time.Now(), NextRun: quarkLibraryNext(card, time.Now())}
	if !card.Enabled || card.Directory == "" {
		return result, errors.New("请设置CAS生成位置并启用链接")
	}
	if casLibraryUncertain(st, card) {
		return result, errors.New("上次临时转存或清理未确认，请核对生成队列中的回执；未重复提交")
	}
	if err := a.store.update(func(st *State) error { initCASLibrary(st); st.CASLibraryRuntime.Results[card.ID] = result; return nil }); err != nil {
		return result, err
	}
	defer func() {
		if runErr != nil {
			result.Status, result.Message = "failed", fmt.Sprint(runErr)
		} else {
			result.Status, result.Message = "completed", fmt.Sprintf("生成CAS STRM %d条，跳过%d个已有文件", result.Generated, result.Skipped)
		}
		_ = a.store.update(func(st *State) error {
			if current, ok := casLibraryCardAt(*st, card.ID); ok && current == card {
				st.CASLibraryRuntime.Results[card.ID] = result
			}
			return nil
		})
		a.store.event(map[bool]string{true: "warn", false: "info"}[runErr != nil], "virtual", "CAS链库「"+card.Name+"」："+result.Message)
	}()
	for _, protected := range []string{a.store.dir, a.dataDir} {
		if protected != "" && (backupPathsOverlap(quarkLibraryRoot(card), protected) || backupPathsOverlap(protected, quarkLibraryRoot(card))) {
			return result, errors.New("CAS生成目录不能覆盖配置或运行数据目录")
		}
	}
	release, err := a.reserveTaskFileNodes(ctx, []taskFileNode{{storage: Storage{Type: "local", Config: map[string]string{"root": card.Directory}}, id: card.DirectoryName}})
	if err != nil {
		return result, err
	}
	defer release()
	parent, err := openStrmReplaceRoot(card.Directory)
	if err != nil {
		return result, err
	}
	defer parent.Close()
	root, err := virtualOutputRoot(parent, card.DirectoryName)
	if err != nil {
		return result, err
	}
	defer root.Close()
	identity, err := root.Stat(".")
	if err != nil {
		return result, err
	}
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if external != nil {
			if err := external(); err != nil {
				return err
			}
		}
		now := a.store.snapshotWithLogLimit(0)
		current, ok := casLibraryCardAt(now, card.ID)
		if !ok || current != card || telegramDigest(effectiveCASLibrary(now)) != cfgStamp {
			return errors.New("CAS链库配置已变化，操作停止")
		}
		fresh, err := openStrmReplaceRoot(quarkLibraryRoot(card))
		if err != nil {
			return err
		}
		defer fresh.Close()
		info, err := fresh.Stat(".")
		if err != nil || !os.SameFile(identity, info) {
			return errors.New("CAS生成目录身份已改变")
		}
		return nil
	}
	if err := check(); err != nil {
		return result, err
	}
	entries, err := a.parseMobileGenerationLinks(ctx, card.Secret)
	if err != nil {
		return result, err
	}
	media := card.MediaExtensions
	if media == "" {
		media = cfg.MediaExtensions
	}
	wanted := []GenerationEntry{}
	seen := map[string]bool{}
	for _, entry := range entries {
		source := strings.TrimSuffix(entry.Path, ".cas.strm")
		if !taskMedia(Task{MediaExtensions: media}, source) || quarkLibraryExcluded(card, path.Dir(source), path.Base(source)) {
			continue
		}
		if entry.Error != "" {
			return result, errors.New(entry.Error)
		}
		if !quarkLibraryRelative(entry.Path) || seen[strings.ToLower(entry.Path)] {
			return result, errors.New("CAS输出路径无效或重复")
		}
		seen[strings.ToLower(entry.Path)] = true
		if info, e := root.Lstat(entry.Path); e == nil {
			if !info.Mode().IsRegular() {
				return result, errors.New("CAS目标不是普通文件")
			}
			result.Skipped++
			continue
		} else if !os.IsNotExist(e) {
			return result, e
		}
		wanted = append(wanted, entry)
	}
	if len(wanted) == 0 {
		return result, nil
	}
	if err := check(); err != nil {
		return result, err
	}
	key := id()
	manifest := generationManifest{User: st.Username, Kind: "cas", Entries: wanted, Created: time.Now()}
	for _, entry := range wanted {
		if entry.Mobile != nil {
			s, stamp, e := a.selectGenerationMobile(time.Now())
			if e != nil {
				return result, e
			}
			manifest.Mobile = &generationMobilePlan{StorageID: s.ID, Revision: storageRevision(s), Pool: stamp, Directory: "cas-import-" + key}
			break
		}
	}
	if err := a.writeGenerationManifest(key, manifest); err != nil {
		return result, err
	}
	a.generationMu.Lock()
	defer a.generationMu.Unlock()
	if err := check(); err != nil {
		return result, err
	}
	job := GenerationJob{ID: id(), Preview: key, Kind: "cas", Name: "CAS链库 · " + card.Name, Target: quarkLibraryRoot(card), Total: len(wanted), Status: "running", Created: time.Now(), LibrarySource: casLibrarySourceStamp(card.Secret)}
	if err := a.store.update(func(st *State) error {
		if len(st.GenerationJobs) >= 100 {
			cut := -1
			for i, j := range st.GenerationJobs {
				if j.Status != "running" && j.Status != "queued" && !generationHasTemporary(j) {
					cut = i
					break
				}
			}
			if cut < 0 {
				return errors.New("生成记录已满，请核对未确认临时资源")
			}
			st.GenerationJobs = append(st.GenerationJobs[:cut], st.GenerationJobs[cut+1:]...)
		}
		st.GenerationJobs = append(st.GenerationJobs, job)
		return nil
	}); err != nil {
		return result, err
	}
	workCtx := context.WithValue(ctx, generationWorkGuardKey{}, check)
	a.executeGeneration(workCtx, job, manifest, quarkLibraryRoot(card), ".")
	for _, j := range a.store.snapshotWithLogLimit(0).GenerationJobs {
		if j.ID == job.ID {
			result.Generated = j.Success
			if j.Status != "success" {
				return result, errors.New(j.Message)
			}
			break
		}
	}
	return result, nil
}
func (a *App) startCASLibraryLocked(card quarkLibraryCard) error {
	if !card.Enabled || casLibraryUncertain(a.store.snapshotWithLogLimit(0), card) {
		return errors.New("CAS链接未启用或上次临时操作未确认")
	}
	if err := a.store.update(func(st *State) error {
		initCASLibrary(st)
		r := st.CASLibraryRuntime.Results[card.ID]
		r.Status, r.Message = "running", "正在后台生成CAS"
		st.CASLibraryRuntime.Results[card.ID] = r
		return nil
	}); err != nil {
		return err
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.casLibraryMu.Unlock()
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
		defer cancel()
		_, _ = a.runCASLibrary(ctx, card, nil)
	}()
	return nil
}
func (a *App) casLibraryRunAPI(w http.ResponseWriter, r *http.Request) {
	if !a.casLibraryMu.TryLock() {
		fail(w, 409, errors.New("CAS链库正在生成"))
		return
	}
	card, ok := casLibraryCardAt(a.store.snapshotWithLogLimit(0), r.PathValue("id"))
	if !ok {
		a.casLibraryMu.Unlock()
		fail(w, 404, errors.New("CAS链接不存在"))
		return
	}
	if err := a.startCASLibraryLocked(card); err != nil {
		a.casLibraryMu.Unlock()
		fail(w, 409, err)
		return
	}
	jsonResponse(w, 202, map[string]string{"message": "已在后台开始生成CAS"})
}
func (a *App) casLibraryMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			a.casLibraryTick(now)
		}
	}
}
func (a *App) casLibraryTick(now time.Time) {
	if !a.casLibraryMu.TryLock() {
		return
	}
	keep := false
	defer func() {
		if !keep {
			a.casLibraryMu.Unlock()
		}
	}()
	st := a.store.snapshotWithLogLimit(0)
	for _, card := range st.CASLibraryCards {
		if !card.Enabled || card.Cron == "" || casLibraryUncertain(st, card) {
			continue
		}
		if _, err := cronParser.Parse(card.Cron); err != nil {
			continue
		}
		result := st.CASLibraryRuntime.Results[card.ID]
		if result.NextRun.IsZero() {
			_ = a.store.update(func(st *State) error {
				initCASLibrary(st)
				result.NextRun = quarkLibraryNext(card, now)
				st.CASLibraryRuntime.Results[card.ID] = result
				return nil
			})
			continue
		}
		if !result.NextRun.After(now) {
			keep = a.startCASLibraryLocked(card) == nil
			return
		}
	}
}

type generationWorkGuardKey struct{}

func generationWorkCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if check, ok := ctx.Value(generationWorkGuardKey{}).(func() error); ok {
		return check()
	}
	return nil
}
