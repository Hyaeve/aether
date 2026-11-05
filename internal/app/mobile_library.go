package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mobileLibrarySettings struct {
	ScrapeEnabled      bool     `json:"scrapeEnabled"`
	StorageIDs         []string `json:"storageIds"`
	UpdateDays         *int     `json:"updateDays,omitempty"`
	Directory          string   `json:"directory"`
	MediaExtensions    string   `json:"mediaExtensions"`
	MetadataExtensions string   `json:"metadataExtensions"`
	PlaybackMode       string   `json:"playbackMode"`
	InvalidDays        int      `json:"invalidDays,omitempty"`
	RetentionHours     int      `json:"retentionHours,omitempty"`
	RotationHours      int      `json:"rotationHours,omitempty"`
}

type mobileLibraryRuntime struct {
	Results   map[string]quarkLibraryResult     `json:"results,omitempty"`
	Files     map[string]quarkLibraryFile       `json:"files,omitempty"`
	Roots     map[string]quarkLibraryOwnership  `json:"roots,omitempty"`
	Temporary map[string]mobileLibraryTemporary `json:"temporary,omitempty"`
}

func initMobileLibrary(st *State) {
	if st.MobileLibraryRuntime.Temporary == nil {
		st.MobileLibraryRuntime.Temporary = map[string]mobileLibraryTemporary{}
	}
	if st.MobileLibraryRuntime.Results == nil {
		st.MobileLibraryRuntime.Results = map[string]quarkLibraryResult{}
	}
	if st.MobileLibraryRuntime.Files == nil {
		st.MobileLibraryRuntime.Files = map[string]quarkLibraryFile{}
	}
	if st.MobileLibraryRuntime.Roots == nil {
		st.MobileLibraryRuntime.Roots = map[string]quarkLibraryOwnership{}
	}
}

func effectiveMobileLibrary(st State) mobileLibrarySettings {
	cfg := mobileLibrarySettings{StorageIDs: []string{}, PlaybackMode: "share", InvalidDays: 15, RetentionHours: 12, RotationHours: 24,
		MediaExtensions: taskVideoExtensions + ";" + taskAudioExtensions, MetadataExtensions: "nfo;ass;srt;ssa;sub;jpg;jpeg;png;webp"}
	if st.MobileLibrary != nil {
		cfg = *st.MobileLibrary
	}
	if cfg.PlaybackMode == "redirect" || cfg.PlaybackMode == "" {
		cfg.PlaybackMode = "share"
	}
	if cfg.InvalidDays == 0 {
		cfg.InvalidDays = 15
	}
	if cfg.RetentionHours == 0 {
		cfg.RetentionHours = 12
	}
	if cfg.RotationHours == 0 {
		cfg.RotationHours = 24
	}
	days := libraryUpdateDays(cfg.UpdateDays)
	cfg.UpdateDays = &days
	cfg.StorageIDs = append([]string{}, cfg.StorageIDs...)
	for _, s := range st.Storages {
		if nativeMobile(s) && s.Enabled {
			ids := []string{s.ID}
			for _, sid := range cfg.StorageIDs {
				if sid != s.ID {
					ids = append(ids, sid)
				}
			}
			cfg.StorageIDs = ids
			break
		}
	}
	return cfg
}

func mobileLibraryCardAt(st State, sid string) (quarkLibraryCard, bool) {
	for _, card := range st.MobileLibraryCards {
		if card.ID == sid {
			return card, true
		}
	}
	return quarkLibraryCard{}, false
}

func validateMobileLibraryCard(card *quarkLibraryCard, legacy ...bool) error {
	provider, code, pass, err := parseShareLink(card.Secret, "")
	if err != nil || provider != "mobile" {
		return errors.New("请输入有效的移动云盘官方分享链接和提取码")
	}
	card.Secret = mobileLibrarySecret(code, pass)
	if card.CascadeCleanup == "" {
		card.CascadeCleanup = "off"
	}
	return validateShareLibraryFields(card, legacy...)
}

func validateShareLibraryOutputs(st State) error {
	all := append(append([]quarkLibraryCard{}, st.QuarkLibraryCards...), st.MobileLibraryCards...)
	all = append(all, st.CASLibraryCards...)
	all = append(all, st.Library115Cards...)
	for i, card := range all {
		for _, previous := range all[:i] {
			if quarkLibraryOutputsOverlap(card, previous) {
				return errors.New("分享链库的生成目录不能互相包含或重叠")
			}
		}
	}
	return nil
}

func validateMobileLibrary(st State, legacy ...bool) error {
	if cfg := st.MobileLibrary; cfg != nil {
		if err := validateLibraryUpdateDays(cfg.UpdateDays); err != nil {
			return err
		}
		if cfg.PlaybackMode != "redirect" && cfg.PlaybackMode != "share" && cfg.PlaybackMode != "transfer" && cfg.PlaybackMode != "relay" && cfg.PlaybackMode != "" {
			return errors.New("移动链库播放模式须为以太中继、分享直链或转存直链")
		}
		if cfg.InvalidDays < 0 || cfg.InvalidDays > 3650 || cfg.RetentionHours < 0 || cfg.RetentionHours > 8760 || cfg.RotationHours < 0 || cfg.RotationHours > 8760 {
			return errors.New("移动链库失效保留须为1–3650天，保活期及轮换时间须为1–8760 h")
		}
		if len(cfg.StorageIDs) > 32 {
			return errors.New("移动链库最多绑定32个原生存储")
		}
		seen := map[string]bool{}
		for _, sid := range cfg.StorageIDs {
			if sid == "" || seen[sid] {
				return errors.New("移动链库存储重复或无效")
			}
			seen[sid] = true
			for _, s := range st.Storages {
				if s.ID == sid && !nativeMobile(s) {
					return errors.New("移动链库只能绑定原生移动云盘")
				}
			}
		}
		if cfg.Directory != "" && (!filepath.IsAbs(cfg.Directory) || len(cfg.Directory) > 4096 || strings.ContainsAny(cfg.Directory, "\x00\r\n")) {
			return errors.New("请选择容器内绝对生成位置")
		}
		for _, extensions := range []string{cfg.MediaExtensions, cfg.MetadataExtensions} {
			if _, err := normalizeExtensions(extensions); err != nil {
				return err
			}
		}
	}
	if len(st.MobileLibraryCards) > 200 {
		return errors.New("移动链库最多200条分享记录，每页最多20条")
	}
	seen, shares := map[string]bool{}, map[string]bool{}
	for _, card := range st.MobileLibraryCards {
		if card.ID == "" || seen[card.ID] || card.DirectoryName == "" {
			return errors.New("移动分享标识重复或目录名称为空")
		}
		seen[card.ID] = true
		if err := validateMobileLibraryCard(&card, legacy...); err != nil {
			return err
		}
		_, code, _, _ := parseShareLink(card.Secret, "")
		if shares[code] {
			return errors.New("同一移动分享只能添加一条链库记录")
		}
		shares[code] = true
	}
	return validateShareLibraryOutputs(st)
}

func (a *App) mobileLibraryStorages(st State) []Storage {
	var result []Storage
	for _, sid := range effectiveMobileLibrary(st).StorageIDs {
		for _, s := range st.Storages {
			if s.ID == sid && nativeMobile(s) && s.Enabled && storageAuthBlocked(s, time.Now()) == nil {
				if _, _, err := mobileAccount(s); err == nil {
					result = append(result, s)
				}
			}
		}
	}
	return result
}

func (a *App) mobileLibraryStorage(st State) (Storage, error) {
	if available := a.mobileLibraryStorages(st); len(available) > 0 {
		return available[0], nil
	}
	return Storage{}, errors.New("请在移动链库设置中绑定已授权且健康的原生移动云盘")
}

func (a *App) mobileLibraryCheck(card quarkLibraryCard, storage *Storage) error {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	st := a.store.state
	if current, ok := mobileLibraryCardAt(st, card.ID); !ok || current != card || !current.Enabled {
		return errors.New("移动链库配置已改变或停用，执行已停止")
	}
	if storage != nil {
		for _, current := range a.mobileLibraryStorages(st) {
			if current.ID == storage.ID && storageRevision(current) == storageRevision(*storage) {
				return nil
			}
		}
		return errors.New("移动链库存储授权或绑定已改变，执行已停止")
	}
	return nil
}

func (a *App) mobileLibraryResolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	storage, err := a.mobileLibraryStorage(a.store.snapshotWithLogLimit(0))
	if err != nil {
		fail(w, 400, err)
		return
	}
	share, err := a.mobileLibraryShareInfo(r.Context(), storage, in.URL)
	if err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]string{"name": share.Name})
}

func revokeMobileLibraryCard(st *State, sid string) {
	delete(st.MobileLibraryRuntime.Results, sid)
	delete(st.MobileLibraryRuntime.Roots, sid)
	for key, file := range st.MobileLibraryRuntime.Files {
		if file.CardID == sid {
			delete(st.MobileLibraryRuntime.Files, key)
		}
	}
}

func (a *App) mobileLibraryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	st := a.store.snapshotWithLogLimit(0)
	if r.Method == http.MethodGet {
		cards := []map[string]any{}
		counts := map[string]int{"strm": 0, "metadata": 0}
		for _, card := range st.MobileLibraryCards {
			raw, _ := json.Marshal(card)
			item := map[string]any{}
			_ = json.Unmarshal(raw, &item)
			delete(item, "secret")
			item["url"], item["result"] = card.Secret, st.MobileLibraryRuntime.Results[card.ID]
			cards = append(cards, item)
		}
		for _, file := range st.MobileLibraryRuntime.Files {
			if _, exists := mobileLibraryCardAt(st, file.CardID); exists && file.Published {
				if file.Media {
					counts["strm"]++
				} else {
					counts["metadata"]++
				}
			}
		}
		jsonResponse(w, 200, map[string]any{"settings": effectiveMobileLibrary(st), "cards": cards, "stats": counts})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.WriteHeader(405)
		return
	}
	if !a.mobileLibraryMu.TryLock() {
		fail(w, 409, errors.New("移动链库正在生成或刮削，请稍后重试"))
		return
	}
	locked := true
	defer func() {
		if locked {
			a.mobileLibraryMu.Unlock()
		}
	}()
	if r.PathValue("id") == "settings" {
		if r.Method != http.MethodPut {
			w.WriteHeader(405)
			return
		}
		var cfg mobileLibrarySettings
		if !decode(w, r, &cfg) {
			return
		}
		cfg.Directory = strings.TrimSpace(cfg.Directory)
		if cfg.Directory != "" {
			root, err := openStrmReplaceRoot(cfg.Directory)
			if err != nil {
				fail(w, 400, errors.New("生成位置不存在、不可访问或包含符号链接"))
				return
			}
			root.Close()
			cfg.Directory = filepath.Clean(cfg.Directory)
		}
		err := a.store.update(func(st *State) error {
			st.MobileLibrary = &cfg
			cfg = effectiveMobileLibrary(*st)
			st.MobileLibrary = &cfg
			return validateMobileLibrary(*st)
		})
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, cfg)
		return
	}
	sid := r.PathValue("id")
	old, exists := mobileLibraryCardAt(st, sid)
	if r.Method != http.MethodPost && !exists {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		err := a.store.update(func(st *State) error {
			for i, card := range st.MobileLibraryCards {
				if card.ID == sid {
					st.MobileLibraryCards = append(st.MobileLibraryCards[:i], st.MobileLibraryCards[i+1:]...)
					break
				}
			}
			revokeMobileLibraryCard(st, sid)
			return nil
		})
		if err != nil {
			fail(w, 500, err)
			return
		}
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	var in struct {
		ID                 string  `json:"id"`
		URL                string  `json:"url"`
		Name               string  `json:"name"`
		Cron               string  `json:"cron"`
		Directory          string  `json:"directory"`
		DirectoryName      string  `json:"directoryName"`
		MediaExtensions    string  `json:"mediaExtensions"`
		MetadataExtensions string  `json:"metadataExtensions"`
		CascadeCleanup     string  `json:"cascadeCleanup"`
		ScrapeEnabled      *bool   `json:"scrapeEnabled"`
		ExcludeDirs        *string `json:"excludeDirs"`
		ExcludeFiles       *string `json:"excludeFiles"`
		ExcludeTypes       *string `json:"excludeTypes"`
		Enabled            bool    `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if r.Method == http.MethodPost {
		sid = id()
	}
	card := quarkLibraryCard{ID: sid, Secret: in.URL, Name: in.Name, Cron: in.Cron, Directory: in.Directory, DirectoryName: in.DirectoryName,
		MediaExtensions: in.MediaExtensions, MetadataExtensions: in.MetadataExtensions, CascadeCleanup: in.CascadeCleanup, Enabled: in.Enabled,
		ScrapeEnabled: old.ScrapeEnabled, ExcludeDirs: old.ExcludeDirs, ExcludeFiles: old.ExcludeFiles, ExcludeTypes: old.ExcludeTypes}
	if in.ScrapeEnabled != nil {
		card.ScrapeEnabled = *in.ScrapeEnabled
	}
	for _, pair := range []struct {
		value *string
		field *string
	}{{in.ExcludeDirs, &card.ExcludeDirs}, {in.ExcludeFiles, &card.ExcludeFiles}, {in.ExcludeTypes, &card.ExcludeTypes}} {
		if pair.value != nil {
			*pair.field = *pair.value
		}
	}
	setLibrarySchedule(&card, old, time.Now())
	if card.Enabled && strings.TrimSpace(card.Directory) == "" {
		card.Directory = effectiveMobileLibrary(st).Directory
	}
	if err := validateMobileLibraryCard(&card); err != nil {
		fail(w, 400, err)
		return
	}
	if exists {
		if saved, err := a.saveShareLibraryMetadata(r.Context(), "mobile", old, card); saved {
			if err != nil {
				if errors.Is(err, errShareLibraryDirectory) {
					jsonResponse(w, 409, map[string]string{"error": err.Error(), "field": "directoryName"})
				} else {
					fail(w, 409, err)
				}
			} else {
				jsonResponse(w, 200, map[string]string{"id": sid, "message": "链接配置已保存"})
			}
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	storage, err := a.mobileLibraryStorage(st)
	if err != nil && card.Enabled {
		fail(w, 400, err)
		return
	}
	card.ShareName = old.ShareName
	if storage.ID != "" {
		share, err := a.mobileLibraryShareInfo(ctx, storage, card.Secret)
		if err != nil {
			fail(w, 400, err)
			return
		}
		card.Secret, card.ShareName = mobileLibrarySecret(share.Code, share.Pass), share.Name
	} else if card.ShareName == "" || old.Secret != card.Secret {
		_, code, _, _ := parseShareLink(card.Secret, "")
		card.ShareName = "移动分享-" + quarkLibraryStamp(code)[:8]
	}
	if card.Name == "" {
		card.Name = card.ShareName
	}
	if card.DirectoryName == "" {
		card.DirectoryName = card.ShareName
	}
	if err := validateMobileLibraryCard(&card); err != nil || card.DirectoryName == "" {
		fail(w, 400, errors.New("分享原名称不能作为目录名，请填写合法的目录名称"))
		return
	}
	if err := shareLibraryDirectoryAvailable(st, card, old); err != nil {
		jsonResponse(w, 409, map[string]string{"error": err.Error(), "field": "directoryName"})
		return
	}
	projection := st
	projection.MobileLibraryCards = append([]quarkLibraryCard{}, st.MobileLibraryCards...)
	if exists {
		for i := range projection.MobileLibraryCards {
			if projection.MobileLibraryCards[i].ID == sid {
				projection.MobileLibraryCards[i] = card
			}
		}
	} else {
		projection.MobileLibraryCards = append(projection.MobileLibraryCards, card)
	}
	if err := validateMobileLibrary(projection); err != nil {
		fail(w, 400, err)
		return
	}
	if exists && card.Directory != "" && quarkLibraryRoot(old) != quarkLibraryRoot(card) {
		target, err := openStrmReplaceRoot(quarkLibraryRoot(card))
		if err == nil {
			if _, markerErr := target.Lstat(quarkLibraryMarker); !os.IsNotExist(markerErr) {
				target.Close()
				fail(w, 400, errors.New("迁移目标包含归属标记，原目录保留"))
				return
			}
			entries, scanErr := scanQuarkLibraryDisk(ctx, target)
			target.Close()
			if scanErr != nil {
				fail(w, 400, errors.New("迁移目标不可完整核验，原目录保留"))
				return
			}
			for _, entry := range entries {
				if !entry.info.IsDir() {
					fail(w, 400, errors.New("迁移目标包含已有文件或归属标记，原目录保留"))
					return
				}
			}
		} else if !os.IsNotExist(err) {
			fail(w, 400, errors.New("迁移目标不可访问或包含符号链接，原目录保留"))
			return
		}
	}
	migration, err := a.prepareShareLibraryMove(ctx, old, card, st.MobileLibraryRuntime.Files, st.MobileLibraryRuntime.Roots[old.ID])
	if err != nil {
		fail(w, 400, err)
		return
	}
	err = a.store.update(func(st *State) error {
		if err := shareLibraryNamesAvailable(*st, card); err != nil {
			return err
		}
		if exists {
			if current, ok := mobileLibraryCardAt(*st, sid); !ok || current != old {
				return errors.New("移动分享记录已改变，请重新打开")
			}
			for i := range st.MobileLibraryCards {
				if st.MobileLibraryCards[i].ID == sid {
					st.MobileLibraryCards[i] = card
				}
			}
		} else {
			st.MobileLibraryCards = append(st.MobileLibraryCards, card)
		}
		initMobileLibrary(st)
		updateShareLibraryRenamedRoot(st.MobileLibraryRuntime.Roots, old, card)
		st.MobileLibraryRuntime.Results[sid] = quarkLibraryResult{Status: "idle", Message: "等待执行", NextRun: quarkLibraryNext(card, time.Now())}
		return validateMobileLibrary(*st)
	})
	if err != nil {
		migration(false)
		if errors.Is(err, errShareLibraryDirectory) {
			jsonResponse(w, 409, map[string]string{"error": err.Error(), "field": "directoryName"})
			return
		}
		fail(w, 400, err)
		return
	}
	migration(true)
	if card.Enabled {
		if err := a.startMobileLibraryLocked(card); err != nil {
			fail(w, 500, err)
			return
		}
		locked = false
		jsonResponse(w, 202, map[string]string{"id": sid, "message": "移动分享已保存，已在后台开始生成"})
		return
	}
	jsonResponse(w, 200, map[string]string{"id": sid})
}

func (a *App) startMobileLibraryLocked(card quarkLibraryCard) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if err := a.mobileLibraryCheck(card, nil); err != nil {
		return err
	}
	if err := a.store.update(func(st *State) error {
		initMobileLibrary(st)
		result := st.MobileLibraryRuntime.Results[card.ID]
		result.Status, result.Message = "running", "已在后台开始生成"
		st.MobileLibraryRuntime.Results[card.ID] = result
		return nil
	}); err != nil {
		return err
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.mobileLibraryMu.Unlock()
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()
		_, _ = a.runMobileLibrary(ctx, card)
	}()
	return nil
}

func (a *App) mobileLibraryRunAPI(w http.ResponseWriter, r *http.Request) {
	if !a.mobileLibraryMu.TryLock() {
		fail(w, 409, errors.New("移动链库正在生成或刮削，请稍后重试"))
		return
	}
	card, ok := mobileLibraryCardAt(a.store.snapshotWithLogLimit(0), r.PathValue("id"))
	if !ok || !card.Enabled {
		a.mobileLibraryMu.Unlock()
		fail(w, 400, errors.New("移动分享不存在或未启用"))
		return
	}
	if err := a.startMobileLibraryLocked(card); err != nil {
		a.mobileLibraryMu.Unlock()
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 202, map[string]string{"message": "已在后台开始生成"})
}

func (a *App) mobileLibraryMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			a.mobileLibraryTick(now)
		}
	}
}

func (a *App) mobileLibraryTick(now time.Time) {
	a.cleanupMobileLibraryTemporary(now)
	if !a.mobileLibraryMu.TryLock() {
		return
	}
	keep := false
	defer func() {
		if !keep {
			a.mobileLibraryMu.Unlock()
		}
	}()
	st := a.store.snapshotWithLogLimit(0)
	expiryCtx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	a.mobileLibraryExpiryTick(expiryCtx, st, now)
	cancel()
	st = a.store.snapshotWithLogLimit(0)
	for _, card := range st.MobileLibraryCards {
		if !card.Enabled || card.Cron == "" {
			continue
		}
		if _, err := parseLibraryCron(card.Cron); err != nil {
			continue
		}
		result := st.MobileLibraryRuntime.Results[card.ID]
		if result.NextRun.IsZero() {
			_ = a.store.update(func(st *State) error {
				initMobileLibrary(st)
				result.NextRun = quarkLibraryNext(card, now)
				st.MobileLibraryRuntime.Results[card.ID] = result
				return nil
			})
			continue
		}
		if !result.NextRun.After(now) {
			if err := a.startMobileLibraryLocked(card); err == nil {
				keep = true
			}
			return
		}
	}
}

func mobileLibraryFailure(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "移动链库执行超时，已生成文件保留；请检查网络、目录大小及存储调用间隔"
	}
	if errors.Is(err, context.Canceled) {
		return "移动链库执行已取消，已生成文件保留"
	}
	return fmt.Sprint(err)
}
