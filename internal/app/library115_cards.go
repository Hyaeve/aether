package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

func initLibrary115(st *State) {
	if st.Library115Runtime.Results == nil {
		st.Library115Runtime.Results = map[string]quarkLibraryResult{}
	}
	if st.Library115Runtime.Files == nil {
		st.Library115Runtime.Files = map[string]quarkLibraryFile{}
	}
	if st.Library115Runtime.Roots == nil {
		st.Library115Runtime.Roots = map[string]quarkLibraryOwnership{}
	}
}
func library115CardAt(st State, key string) (quarkLibraryCard, bool) {
	for _, card := range st.Library115Cards {
		if card.ID == key {
			return card, true
		}
	}
	return quarkLibraryCard{}, false
}
func validateLibrary115(st State) error {
	if len(st.Library115Cards) > 200 {
		return errors.New("115链库最多200条分享记录")
	}
	seen, ids := map[string]bool{}, map[string]bool{}
	for _, card := range st.Library115Cards {
		provider, code, _, err := parseShareLink(card.Secret, "")
		if err != nil || provider != "115" || seen[code] || ids[card.ID] || !shareCodePattern.MatchString(card.ID) {
			return errors.New("115分享无效、重复或标识不符")
		}
		seen[code], ids[card.ID] = true, true
		if card.Cron != "" || !card.ScheduleAnchor.IsZero() {
			return errors.New("115链库不支持自动更新周期")
		}
		if card.StorageID == "" || card.DirectoryName == "" {
			return errors.New("请选择115解析存储池和保存目录")
		}
		for _, s := range st.Storages {
			if s.ID == card.StorageID && s.Type != "115" {
				return errors.New("115链库解析存储类型不符")
			}
		}
		if err := validateShareLibraryFields(&card); err != nil {
			return err
		}
	}
	return validateShareLibraryOutputs(st)
}
func (a *App) library115Check(ctx context.Context, card quarkLibraryCard) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := generationWorkCheck(ctx); err != nil {
		return err
	}
	current, ok := library115CardAt(a.store.snapshotWithLogLimit(0), card.ID)
	if !ok || current != card {
		return errors.New("115链库卡片已改变，执行停止")
	}
	return nil
}
func (a *App) library115Title(ctx context.Context, secret, sid string) (string, error) {
	s, err := a.store.storage(sid)
	if err != nil || s.Type != "115" || !s.Enabled {
		return "", errors.New("请选择可用115解析存储池")
	}
	provider, code, pass, err := parseShareLink(secret, "")
	if err != nil || provider != "115" {
		return "", errors.New("请输入115分享链接")
	}
	_, items, err := a.readShare(ctx, s, code, pass)
	if err != nil {
		return "", err
	}
	name := telegramShareName(items)
	if name == "" {
		name = "115分享-" + quarkLibraryStamp(code)[:8]
	}
	return name, nil
}

func (a *App) library115API(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		st := a.store.snapshotWithLogLimit(0)
		cards := []map[string]any{}
		count := 0
		for _, card := range st.Library115Cards {
			raw, _ := json.Marshal(card)
			var item map[string]any
			_ = json.Unmarshal(raw, &item)
			delete(item, "secret")
			delete(item, "cron")
			delete(item, "scheduleAnchor")
			item["url"], item["result"] = card.Secret, st.Library115Runtime.Results[card.ID]
			cards = append(cards, item)
		}
		for _, f := range st.Library115Runtime.Files {
			if f.Published {
				count++
			}
		}
		cfg := virtual115Defaults{}
		if st.Virtual115 != nil {
			cfg = *st.Virtual115
		}
		jsonResponse(w, 200, map[string]any{"cards": cards, "settings": cfg, "stats": map[string]int{"strm": count}})
		return
	}
	if !a.library115Mu.TryLock() {
		fail(w, 409, errors.New("115链库正在执行，请稍后重试"))
		return
	}
	locked := true
	defer func() {
		if locked {
			a.library115Mu.Unlock()
		}
	}()
	st := a.store.snapshotWithLogLimit(0)
	key := r.PathValue("id")
	old, exists := library115CardAt(st, key)
	if r.Method == "DELETE" {
		if !exists {
			fail(w, 404, errors.New("115链接不存在"))
			return
		}
		err := a.store.update(func(st *State) error {
			for i, c := range st.Library115Cards {
				if c.ID == key {
					st.Library115Cards = append(st.Library115Cards[:i], st.Library115Cards[i+1:]...)
					break
				}
			}
			delete(st.Library115Runtime.Results, key)
			delete(st.Library115Runtime.Roots, key)
			for k, f := range st.Library115Runtime.Files {
				if f.CardID == key {
					delete(st.Library115Runtime.Files, k)
				}
			}
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
		fail(w, 404, errors.New("115链接不存在"))
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
	if strings.TrimSpace(card.Cron) != "" {
		fail(w, 400, errors.New("115链库不支持自动更新周期"))
		return
	}
	card.ID = key
	if !exists {
		card.ID = id()
	}
	card.Secret = in.URL
	card.Cron = ""
	card.ScheduleAnchor = time.Time{}
	if card.StorageID == "" && st.Virtual115 != nil {
		card.StorageID = st.Virtual115.StorageID
	}
	provider, code, pass, err := parseShareLink(card.Secret, "")
	if err != nil || provider != "115" {
		fail(w, 400, errors.New("请输入115分享链接"))
		return
	}
	card.Secret = "https://115.com/s/" + code
	if pass != "" {
		card.Secret += "?password=" + pass
	}
	if err := validateShareLibraryFields(&card); err != nil {
		fail(w, 400, err)
		return
	}
	for _, c := range st.Library115Cards {
		_, existing, _, _ := parseShareLink(c.Secret, "")
		if c.ID != card.ID && existing == code {
			fail(w, 409, errors.New("该115分享已存在，请编辑原卡片"))
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if exists {
		if saved, err := a.saveShareLibraryMetadata(r.Context(), "115", old, card); saved {
			if err != nil {
				if errors.Is(err, errShareLibraryDirectory) {
					jsonResponse(w, 409, map[string]string{"error": err.Error(), "field": "directoryName"})
				} else {
					fail(w, 409, err)
				}
			} else {
				jsonResponse(w, 200, map[string]string{"id": card.ID, "message": "115链接配置已保存"})
			}
			return
		}
	}
	title, err := a.library115Title(ctx, card.Secret, card.StorageID)
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
	if err := shareLibraryDirectoryAvailable(st, card, old); err != nil {
		jsonResponse(w, 409, map[string]string{"error": err.Error(), "field": "directoryName"})
		return
	}
	migration, err := a.prepareShareLibraryMove(ctx, old, card, st.Library115Runtime.Files, st.Library115Runtime.Roots[old.ID])
	if err != nil {
		fail(w, 400, err)
		return
	}

	err = a.store.update(func(st *State) error {
		if exists {
			current, ok := library115CardAt(*st, key)
			if !ok || current != old {
				return errors.New("115链接已改变，请重新打开")
			}
			for i, c := range st.Library115Cards {
				if c.ID == key {
					st.Library115Cards[i] = card
				}
			}
		} else {
			st.Library115Cards = append(st.Library115Cards, card)
		}
		initLibrary115(st)
		updateShareLibraryRenamedRoot(st.Library115Runtime.Roots, old, card)
		return validateLibrary115(*st)
	})
	if err != nil {
		migration(false)
		fail(w, 409, err)
		return
	}
	migration(true)
	if card.Enabled {
		if err := a.startLibrary115Locked(card); err != nil {
			fail(w, 500, err)
			return
		}
		locked = false
		jsonResponse(w, 202, map[string]string{"id": card.ID, "message": "115链接已保存，已加入执行队列"})
		return
	}
	jsonResponse(w, 200, map[string]string{"id": card.ID, "message": "115链接已保存"})
}
func (a *App) library115Resolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL       string `json:"url"`
		StorageID string `json:"storageId"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.StorageID == "" {
		if cfg := a.store.snapshotWithLogLimit(0).Virtual115; cfg != nil {
			in.StorageID = cfg.StorageID
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	name, err := a.library115Title(ctx, in.URL, in.StorageID)
	if err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, map[string]string{"name": name})
}
func (a *App) library115RunAPI(w http.ResponseWriter, r *http.Request) {
	if !a.library115Mu.TryLock() {
		fail(w, 409, errors.New("115链库正在执行"))
		return
	}
	card, ok := library115CardAt(a.store.snapshotWithLogLimit(0), r.PathValue("id"))
	if !ok || !card.Enabled {
		a.library115Mu.Unlock()
		fail(w, 400, errors.New("115链接不存在或未启用"))
		return
	}
	if err := a.startLibrary115Locked(card); err != nil {
		a.library115Mu.Unlock()
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 202, map[string]string{"message": "已加入115链库执行队列"})
}
func (a *App) startLibrary115Locked(card quarkLibraryCard) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	if err := a.store.update(func(st *State) error {
		initLibrary115(st)
		st.Library115Runtime.Results[card.ID] = quarkLibraryResult{Status: "running", Message: "已加入执行队列"}
		return nil
	}); err != nil {
		return err
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer a.library115Mu.Unlock()
		ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
		defer cancel()
		_, _ = a.runLibrary115(ctx, card)
	}()
	return nil
}

// 115 shares are snapshots: explicit execution adds missing outputs; no live monitor.
func (a *App) runLibrary115(ctx context.Context, card quarkLibraryCard) (result quarkLibraryResult, runErr error) {
	result = quarkLibraryResult{Status: "running", LastRun: time.Now()}
	defer func() {
		if runErr != nil {
			result.Status, result.Message = "failed", runErr.Error()
		} else {
			result.Status = "completed"
			result.Message = fmt.Sprintf("新增STRM %d 条，跳过 %d 个", result.Generated, result.Skipped)
		}
		saveErr := a.store.update(func(st *State) error {
			current, ok := library115CardAt(*st, card.ID)
			if !ok || current != card {
				return errors.New("115卡片已改变")
			}
			initLibrary115(st)
			st.Library115Runtime.Results[card.ID] = result
			return nil
		})
		if saveErr != nil {
			runErr = errors.Join(runErr, saveErr)
			result.Status, result.Message = "failed", runErr.Error()
		}
	}()
	if !card.Enabled || card.Directory == "" {
		return result, errors.New("115链接未设置生成位置或未启用")
	}
	if err := a.library115Check(ctx, card); err != nil {
		return result, err
	}
	s, err := a.store.storage(card.StorageID)
	if err != nil || s.Type != "115" || !s.Enabled {
		return result, errors.New("115解析存储不可用")
	}
	revision := storageRevision(s)
	_, code, pass, err := parseShareLink(card.Secret, "")
	if err != nil {
		return result, err
	}
	_, items, err := a.readShare(ctx, s, code, pass)
	if err != nil {
		return result, err
	}
	batches, err := a.planShare(ctx, s, code, pass, items)
	if err != nil {
		return result, err
	}
	for _, protected := range []string{a.store.dir, a.dataDir} {
		if protected != "" && (backupPathsOverlap(quarkLibraryRoot(card), protected) || backupPathsOverlap(protected, quarkLibraryRoot(card))) {
			return result, errors.New("生成目录不能覆盖配置或运行数据")
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
	check := func() error {
		if err := a.library115Check(ctx, card); err != nil {
			return err
		}
		current, e := a.store.storage(s.ID)
		if e != nil || storageRevision(current) != revision {
			return errors.New("115解析存储已改变")
		}
		return nil
	}
	st := a.store.snapshotWithLogLimit(0)
	err = a.ensureLibraryIdentity(ctx, card, root, st.Library115Runtime.Roots[card.ID], st.Library115Runtime.Files, check, func(previous, record quarkLibraryOwnership) error {
		return a.store.update(func(st *State) error {
			initLibrary115(st)
			if current, ok := library115CardAt(*st, card.ID); !ok || current != card || st.Library115Runtime.Roots[card.ID] != previous {
				return errors.New("115目录归属已改变")
			}
			st.Library115Runtime.Roots[card.ID] = record
			return nil
		})
	})
	if err != nil {
		return result, err
	}
	owner := a.store.snapshotWithLogLimit(0).Library115Runtime.Roots[card.ID]
	checkOutput := func() error {
		if err := check(); err != nil {
			return err
		}
		live, e := openStrmReplaceRoot(owner.Directory)
		if e != nil {
			return e
		}
		defer live.Close()
		if err := verifyQuarkLibraryOwner(live, owner); err != nil {
			return err
		}
		return verifyQuarkLibraryOwner(root, owner)
	}
	names := virtual115Names{Version: 1, Files: []virtual115NamedFile{}}
	for _, batch := range batches {
		for _, item := range batch.Items {
			if !taskMedia(Task{MediaExtensions: card.MediaExtensions}, item.Name) || quarkLibraryExcluded(card, batch.Directory, item.Name) {
				continue
			}
			if err := check(); err != nil {
				return result, err
			}
			live, e := openStrmReplaceRoot(owner.Directory)
			if e != nil {
				return result, e
			}
			e = verifyQuarkLibraryOwner(live, owner)
			live.Close()
			if e != nil {
				return result, e
			}
			relative := path.Join(batch.Directory, item.Name+shareSTRMSuffix)
			relative = strings.TrimPrefix(relative, card.ShareName+"/")
			key := code + "_" + pass + "_" + item.ID + strings.ToLower(path.Ext(item.Name))
			content, e := virtualShareURL(a.currentSTRMSettings().Address, code, pass, item, a.currentSTRMSettings().Content == "encoded")
			if e != nil {
				return result, e
			}
			if err := a.virtualGrant(key, true, batch.Source); err != nil {
				return result, err
			}
			names.Files = append(names.Files, virtual115NamedFile{Key: key, Parent: batch.Source, Name: item.Name, Size: item.Size, SHA1: item.SHA1})
			if info, e := root.Lstat(relative); e == nil {
				if !info.Mode().IsRegular() {
					return result, errors.New("115生成目标不是普通文件")
				}
				result.Skipped++
				continue
			} else if !os.IsNotExist(e) {
				return result, e
			}
			sum := sha256.Sum256([]byte(content + "\n"))
			grant := quarkLibraryFile{Key: key, CardID: card.ID, Relative: relative, Digest: hex.EncodeToString(sum[:]), Name: item.Name, Size: item.Size, Media: true, Published: false}
			if err := a.store.update(func(st *State) error {
				initLibrary115(st)
				if len(st.Library115Runtime.Files) >= 20000 {
					return errors.New("115链库登记已达上限")
				}
				st.Library115Runtime.Files[key] = grant
				return nil
			}); err != nil {
				return result, err
			}
			if err := check(); err != nil {
				return result, err
			}
			if err := checkOutput(); err != nil {
				return result, err
			}
			if err := writeCASOutput(root, relative, []byte(content+"\n"), true, true); err != nil {
				return result, err
			}
			grant.Published = true
			if err := a.store.update(func(st *State) error { st.Library115Runtime.Files[key] = grant; return nil }); err != nil {
				return result, err
			}
			result.Generated++
		}
	}
	if err := a.saveVirtual115Names(code, pass, names); err != nil {
		return result, err
	}
	return result, nil
}
