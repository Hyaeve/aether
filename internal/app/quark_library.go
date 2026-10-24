package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

type quarkLibrarySettings struct {
	StorageIDs         []string `json:"storageIds"`
	InvalidDays        int      `json:"invalidDays"`
	RetentionHours     int      `json:"retentionHours"`
	MediaExtensions    string   `json:"mediaExtensions"`
	MetadataExtensions string   `json:"metadataExtensions"`
}

type quarkLibraryCard struct {
	ID                 string `json:"id"`
	Secret             string `json:"secret"`
	Name               string `json:"name"`
	ShareName          string `json:"shareName"`
	Cron               string `json:"cron"`
	Directory          string `json:"directory"`
	DirectoryName      string `json:"directoryName"`
	MediaExtensions    string `json:"mediaExtensions"`
	MetadataExtensions string `json:"metadataExtensions"`
	Enabled            bool   `json:"enabled"`
}

type quarkLibraryResult struct {
	Status       string    `json:"status"`
	Message      string    `json:"message"`
	LastRun      time.Time `json:"lastRun"`
	NextRun      time.Time `json:"nextRun"`
	InvalidSince time.Time `json:"invalidSince"`
	Generated    int       `json:"generated"`
	Metadata     int       `json:"metadata"`
	Skipped      int       `json:"skipped"`
}

// Manifests authorize only resources actually published by this deployment.
type quarkLibraryFile struct {
	Key          string `json:"key"`
	CardID       string `json:"cardId"`
	ShareStamp   string `json:"shareStamp"`
	SourceID     string `json:"sourceId"`
	SourceParent string `json:"sourceParent"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	Relative     string `json:"relative"`
	Digest       string `json:"digest"`
	Media        bool   `json:"media"`
	Published    bool   `json:"published"`
	Stage        string `json:"stage,omitempty"`
}

type quarkLibraryTemporary struct {
	Key        string    `json:"key"`
	StorageID  string    `json:"storageId"`
	Revision   string    `json:"revision"`
	Folder     string    `json:"folder"`
	FolderName string    `json:"folderName,omitempty"`
	Parent     string    `json:"parent,omitempty"`
	FileID     string    `json:"fileId"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Task       string    `json:"task"`
	Attempted  bool      `json:"attempted"`
	Deleting   bool      `json:"deleting,omitempty"`
	LastUsed   time.Time `json:"lastUsed"`
}

type quarkLibraryRuntime struct {
	Results   map[string]quarkLibraryResult    `json:"results,omitempty"`
	Files     map[string]quarkLibraryFile      `json:"files,omitempty"`
	Temporary map[string]quarkLibraryTemporary `json:"temporary,omitempty"`
}

func initQuarkLibraryRuntime(st *State) {
	if st.QuarkLibraryRuntime.Results == nil {
		st.QuarkLibraryRuntime.Results = map[string]quarkLibraryResult{}
	}
	if st.QuarkLibraryRuntime.Files == nil {
		st.QuarkLibraryRuntime.Files = map[string]quarkLibraryFile{}
	}
	if st.QuarkLibraryRuntime.Temporary == nil {
		st.QuarkLibraryRuntime.Temporary = map[string]quarkLibraryTemporary{}
	}
}

func effectiveQuarkLibrary(st State) quarkLibrarySettings {
	if st.QuarkLibrary != nil {
		return *st.QuarkLibrary
	}
	cfg := quarkLibrarySettings{StorageIDs: []string{}, InvalidDays: 15, RetentionHours: 12,
		MediaExtensions: taskVideoExtensions + ";" + taskAudioExtensions, MetadataExtensions: "nfo;ass;srt;ssa;sub;jpg;jpeg;png;webp"}
	for _, s := range st.Storages {
		if s.Type == "quark" && s.Enabled {
			cfg.StorageIDs = []string{s.ID}
			break
		}
	}
	return cfg
}

func validateQuarkLibrary(st State) error {
	if cfg := st.QuarkLibrary; cfg != nil {
		if cfg.InvalidDays < 1 || cfg.InvalidDays > 3650 || cfg.RetentionHours < 1 || cfg.RetentionHours > 8760 || len(cfg.StorageIDs) > 32 {
			return errors.New("夸克链库保留天数须为1–3650，临时保活须为1–8760 h，最多32个存储")
		}
		seen := map[string]bool{}
		for _, sid := range cfg.StorageIDs {
			if sid == "" || seen[sid] {
				return errors.New("夸克链库存储重复或无效")
			}
			seen[sid] = true
			for _, storage := range st.Storages {
				if storage.ID == sid && storage.Type != "quark" {
					return errors.New("夸克链库只能绑定夸克存储")
				}
			}
		}
		for _, value := range []string{cfg.MediaExtensions, cfg.MetadataExtensions} {
			if _, err := normalizeExtensions(value); err != nil {
				return err
			}
		}
	}
	if len(st.QuarkLibraryCards) > 200 {
		return errors.New("夸克链库最多200张链接卡片")
	}
	seen := map[string]bool{}
	shares := map[string]bool{}
	for i, card := range st.QuarkLibraryCards {
		if card.ID == "" || seen[card.ID] {
			return errors.New("夸克链库卡片标识重复或为空")
		}
		seen[card.ID] = true
		if card.DirectoryName == "" {
			return errors.New("夸克链库目录名称不能为空")
		}
		if err := validateQuarkLibraryCard(&card); err != nil {
			return err
		}
		_, code, _, _ := parseShareLink(card.Secret, "")
		if shares[code] {
			return errors.New("同一个夸克分享只能添加一张卡片")
		}
		shares[code] = true
		for _, other := range st.QuarkLibraryCards[:i] {
			if backupPathsOverlap(quarkLibraryRoot(card), quarkLibraryRoot(other)) || backupPathsOverlap(quarkLibraryRoot(other), quarkLibraryRoot(card)) {
				return errors.New("夸克链库生成目录不能互相包含")
			}
		}
	}
	return nil
}

func validateQuarkLibraryCard(card *quarkLibraryCard) error {
	provider, code, pass, err := parseShareLink(card.Secret, "")
	if err != nil || provider != "quark" {
		return errors.New("请输入有效的夸克分享链接和提取码")
	}
	card.Secret = "https://pan.quark.cn/s/" + code
	if pass != "" {
		card.Secret += "?pwd=" + url.QueryEscape(pass)
	}
	card.Name, card.DirectoryName = strings.TrimSpace(card.Name), strings.TrimSpace(card.DirectoryName)
	card.Cron = strings.TrimSpace(card.Cron)
	if len(card.Name) > 255 || len(card.Secret) > 4096 || !validShareDirectory(card.DirectoryName) {
		return errors.New("分享名称过长或目录名称不合规范")
	}
	if !filepath.IsAbs(card.Directory) || len(card.Directory) > 4096 || strings.ContainsAny(card.Directory, "\x00\r\n") {
		return errors.New("请选择容器内绝对生成位置")
	}
	card.Directory = filepath.Clean(card.Directory)
	if card.Cron != "" {
		if _, err := cronParser.Parse(card.Cron); err != nil {
			return errors.New("Cron表达式无效，须为五段表达式")
		}
	}
	card.MediaExtensions, err = normalizeExtensions(card.MediaExtensions)
	if err != nil {
		return fmt.Errorf("媒体扩展名：%w", err)
	}
	card.MetadataExtensions, err = normalizeExtensions(card.MetadataExtensions)
	if err != nil {
		return fmt.Errorf("元数据扩展名：%w", err)
	}
	return nil
}

func quarkLibraryStamp(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func quarkLibraryCardAt(st State, sid string) (quarkLibraryCard, bool) {
	for _, card := range st.QuarkLibraryCards {
		if card.ID == sid {
			return card, true
		}
	}
	return quarkLibraryCard{}, false
}

func revokeQuarkLibraryCard(st *State, sid string) {
	delete(st.QuarkLibraryRuntime.Results, sid)
	for key, file := range st.QuarkLibraryRuntime.Files {
		if file.CardID == sid {
			delete(st.QuarkLibraryRuntime.Files, key)
		}
	}
}

func (a *App) quarkLibraryCheckCard(card quarkLibraryCard) error {
	a.store.mu.RLock()
	current, ok := quarkLibraryCardAt(a.store.state, card.ID)
	a.store.mu.RUnlock()
	if !ok || current != card {
		return errors.New("夸克链库卡片配置已改变，执行停止")
	}
	return nil
}

func (a *App) quarkLibraryResolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &in) {
		return
	}
	provider, _, _, err := parseShareLink(in.URL, "")
	if err != nil || provider != "quark" {
		fail(w, 400, errors.New("请输入有效的夸克分享链接"))
		return
	}
	s, err := a.quarkLibraryStorage(a.store.snapshotWithLogLimit(0))
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	title, _, _, err := a.quarkLibraryRead(ctx, &s, in.URL, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]string{"name": title})
}

func (a *App) quarkLibraryStorage(st State) (Storage, error) {
	cfg := effectiveQuarkLibrary(st)
	for _, sid := range cfg.StorageIDs {
		for _, s := range st.Storages {
			if s.ID == sid && s.Type == "quark" && s.Enabled && storageAuthBlocked(s, time.Now()) == nil {
				return s, nil
			}
		}
	}
	return Storage{}, errors.New("请在夸克链库设置中启用健康的夸克存储")
}

func (a *App) quarkLibraryAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		st := a.store.snapshotWithLogLimit(0)
		cards := []map[string]any{}
		mediaCount, metadataCount := 0, 0
		for _, f := range st.QuarkLibraryRuntime.Files {
			if _, exists := quarkLibraryCardAt(st, f.CardID); exists && f.Published {
				if f.Media {
					mediaCount++
				} else {
					metadataCount++
				}
			}
		}
		for _, card := range st.QuarkLibraryCards {
			raw, _ := json.Marshal(card)
			var item map[string]any
			_ = json.Unmarshal(raw, &item)
			delete(item, "secret")
			item["url"], item["result"] = card.Secret, st.QuarkLibraryRuntime.Results[card.ID]
			cards = append(cards, item)
		}
		jsonResponse(w, 200, map[string]any{"settings": effectiveQuarkLibrary(st), "cards": cards, "stats": map[string]int{"strm": mediaCount, "metadata": metadataCount}})
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodDelete {
		w.WriteHeader(405)
		return
	}
	// UI requests cannot queue unbounded operations behind a cloud refresh.
	if !a.quarkLibraryMu.TryLock() {
		fail(w, 409, errors.New("夸克链库正在执行，请稍后重试"))
		return
	}
	defer a.quarkLibraryMu.Unlock()
	if r.PathValue("id") == "settings" {
		if r.Method != http.MethodPut {
			w.WriteHeader(405)
			return
		}
		var cfg quarkLibrarySettings
		if !decode(w, r, &cfg) {
			return
		}
		err := a.store.update(func(st *State) error {
			st.QuarkLibrary = &cfg
			return validateQuarkLibrary(*st)
		})
		if err != nil {
			fail(w, 400, err)
			return
		}
		a.store.event("info", "virtual", "夸克链库设置已更新")
		jsonResponse(w, 200, cfg)
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	sid := r.PathValue("id")
	old, exists := quarkLibraryCardAt(st, sid)
	if r.Method != http.MethodPost && !exists {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		err := a.store.update(func(st *State) error {
			for i, card := range st.QuarkLibraryCards {
				if card.ID == sid {
					st.QuarkLibraryCards = append(st.QuarkLibraryCards[:i], st.QuarkLibraryCards[i+1:]...)
					break
				}
			}
			revokeQuarkLibraryCard(st, sid)
			// Generated files remain on disk; public grants are revoked with the card.
			return nil
		})
		if err != nil {
			fail(w, 500, err)
			return
		}
		a.store.event("info", "virtual", "夸克链库移除卡片："+old.Name+"；本地生成文件保留")
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	var in struct {
		ID                 string `json:"id"`
		URL                string `json:"url"`
		Name               string `json:"name"`
		Cron               string `json:"cron"`
		Directory          string `json:"directory"`
		DirectoryName      string `json:"directoryName"`
		MediaExtensions    string `json:"mediaExtensions"`
		MetadataExtensions string `json:"metadataExtensions"`
		Enabled            bool   `json:"enabled"`
	}
	if !decode(w, r, &in) {
		return
	}
	if r.Method == http.MethodPost {
		sid = id()
	}
	card := quarkLibraryCard{ID: sid, Secret: in.URL, Name: in.Name, Cron: in.Cron, Directory: in.Directory, DirectoryName: in.DirectoryName,
		MediaExtensions: in.MediaExtensions, MetadataExtensions: in.MetadataExtensions, Enabled: in.Enabled}
	if err := validateQuarkLibraryCard(&card); err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, err := a.quarkLibraryStorage(st)
	if err != nil {
		fail(w, 400, err)
		return
	}
	title, _, _, err := a.quarkLibraryRead(ctx, &s, card.Secret, false)
	if err != nil {
		fail(w, 400, err)
		return
	}
	card.ShareName = title
	if card.Name == "" {
		card.Name = title
	}
	if card.DirectoryName == "" {
		card.DirectoryName = title
	}
	if !validShareDirectory(card.DirectoryName) || card.DirectoryName == "" {
		fail(w, 400, errors.New("分享原名称不能作为目录名，请填写目录名称"))
		return
	}
	for _, other := range st.QuarkLibraryCards {
		if other.ID != sid && (backupPathsOverlap(quarkLibraryRoot(other), quarkLibraryRoot(card)) || backupPathsOverlap(quarkLibraryRoot(card), quarkLibraryRoot(other)) || quarkLibraryStamp(other.Secret) == quarkLibraryStamp(card.Secret)) {
			fail(w, 400, errors.New("该分享已存在或生成目录互相包含，请编辑已有卡片"))
			return
		}
	}
	migration, err := a.prepareQuarkLibraryMove(ctx, old, card, st)
	if err != nil {
		fail(w, 400, err)
		return
	}
	err = a.store.update(func(st *State) error {
		if exists {
			current, ok := quarkLibraryCardAt(*st, sid)
			if !ok || current != old {
				return errors.New("卡片已被其他配置操作修改，请重新打开")
			}
		}
		for _, other := range st.QuarkLibraryCards {
			if other.ID != sid && (filepath.Join(other.Directory, other.DirectoryName) == filepath.Join(card.Directory, card.DirectoryName) || quarkLibraryStamp(other.Secret) == quarkLibraryStamp(card.Secret)) {
				return errors.New("该分享或生成目录已存在，请编辑已有卡片")
			}
		}
		if exists {
			for i := range st.QuarkLibraryCards {
				if st.QuarkLibraryCards[i].ID == sid {
					st.QuarkLibraryCards[i] = card
				}
			}
		} else {
			st.QuarkLibraryCards = append(st.QuarkLibraryCards, card)
		}
		initQuarkLibraryRuntime(st)
		result := st.QuarkLibraryRuntime.Results[sid]
		result.InvalidSince = time.Time{}
		result.Status, result.Message = "idle", "等待执行"
		result.NextRun = quarkLibraryNext(card, time.Now())
		st.QuarkLibraryRuntime.Results[sid] = result
		return validateQuarkLibrary(*st)
	})
	if err != nil {
		migration(false)
		fail(w, 400, err)
		return
	}
	migration(true)
	a.store.event("info", "virtual", "夸克链库保存卡片："+card.Name)
	response := map[string]any{"id": sid}
	if card.Enabled {
		result, err := a.runQuarkLibrary(ctx, card)
		response["result"] = result
		if err != nil {
			response["generationError"] = err.Error()
		}
	}
	jsonResponse(w, 200, response)
}

func quarkLibraryNext(card quarkLibraryCard, now time.Time) time.Time {
	if card.Enabled && card.Cron != "" {
		if schedule, err := cronParser.Parse(card.Cron); err == nil {
			return schedule.Next(now)
		}
	}
	return time.Time{}
}

func (a *App) quarkLibraryRunAPI(w http.ResponseWriter, r *http.Request) {
	if !a.quarkLibraryMu.TryLock() {
		fail(w, 409, errors.New("夸克链库正在执行，请稍后重试"))
		return
	}
	defer a.quarkLibraryMu.Unlock()
	card, ok := quarkLibraryCardAt(a.store.snapshotWithLogLimit(0), r.PathValue("id"))
	if !ok || !card.Enabled {
		fail(w, 400, errors.New("卡片不存在或已停用"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := a.runQuarkLibrary(ctx, card)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, result)
}

func (a *App) quarkLibraryMonitor() {
	defer a.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case now := <-ticker.C:
			a.quarkLibraryTick(now)
		}
	}
}

func (a *App) quarkLibraryTick(now time.Time) {
	if a.quarkLibraryMu.TryLock() {
		st := a.store.snapshotWithLogLimit(0)
		for _, card := range st.QuarkLibraryCards {
			result := st.QuarkLibraryRuntime.Results[card.ID]
			invalidCheck := !result.InvalidSince.IsZero() && !result.LastRun.Add(time.Hour).After(now)
			if !card.Enabled || !invalidCheck && (card.Cron == "" || result.NextRun.After(now)) {
				continue
			}
			ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
			_, _ = a.runQuarkLibrary(ctx, card)
			cancel()
			if a.ctx.Err() != nil {
				break
			}
		}
		a.quarkLibraryMu.Unlock()
	}
	a.cleanupQuarkLibraryTemporary(now)
}
