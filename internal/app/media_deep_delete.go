package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"aether/internal/linkcore/upstream"
)

type mediaDeepDeleteSettings struct {
	Enabled bool     `json:"enabled"`
	LinkIDs []string `json:"linkIds"`
}
type mediaDeepDeleteRecord struct {
	ID       string    `json:"id"`
	LinkName string    `json:"linkName"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Message  string    `json:"message"`
	Count    int       `json:"count"`
	Time     time.Time `json:"time"`
}
type mediaDeepDeleteLedger struct {
	Intents map[string]bool         `json:"intents"`
	Records []mediaDeepDeleteRecord `json:"records"`
}

func (a *App) deepLedger() (mediaDeepDeleteLedger, error) {
	result := mediaDeepDeleteLedger{Intents: map[string]bool{}}
	f, err := os.Open(filepath.Join(a.dataDir, "runtime", "media-deep-delete.json"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return result, errors.New("深度删除回执无效")
	}
	raw, err := io.ReadAll(io.LimitReader(f, (16<<20)+1))
	if err != nil || len(raw) > 16<<20 {
		return result, errors.New("深度删除回执读取失败")
	}
	if err = a.store.decodeJSONConfig("runtime/media-deep-delete", raw, &result); err != nil {
		return result, err
	}
	if result.Intents == nil {
		result.Intents = map[string]bool{}
	}
	return result, nil
}
func (a *App) saveDeepLedger(v mediaDeepDeleteLedger) error {
	raw, err := a.store.encodeJSONConfig("runtime/media-deep-delete", v)
	if err != nil {
		return err
	}
	if len(raw) > 16<<20 || len(v.Intents) > 100000 {
		return errors.New("深度删除回执已满，请保留数据并检查")
	}
	filename := filepath.Join(a.dataDir, "runtime", "media-deep-delete.json")
	if err = os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	return durableConfigWrite(filename, raw)
}

func (a *App) mediaDeepDeleteAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		ledger, err := a.deepLedger()
		if err != nil {
			fail(w, 500, errors.New("深度删除记录无法读取"))
			return
		}
		for i := range ledger.Records {
			if ledger.Records[i].Status == "prepared" || ledger.Records[i].Status == "deleting" {
				ledger.Records[i].Message = "执行中或结果未确认；不会自动重试"
			}
		}
		jsonResponse(w, 200, map[string]any{"settings": a.store.snapshotWithLogLimit(0).MediaDeepDelete, "records": ledger.Records})
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(405)
		return
	}
	if !a.mediaDeepDeleteMu.TryLock() {
		fail(w, 409, errors.New("媒体深度删除正在执行，请稍后修改配置"))
		return
	}
	defer a.mediaDeepDeleteMu.Unlock()
	var in struct {
		mediaDeepDeleteSettings
		Confirmed bool `json:"confirmed"`
	}
	if !decode(w, r, &in) {
		return
	}
	st := a.store.snapshotWithLogLimit(0)
	seen := map[string]bool{}
	for _, sid := range in.LinkIDs {
		if seen[sid] || sid == "" {
			fail(w, 400, errors.New("以链选择重复或无效"))
			return
		}
		seen[sid] = true
		found := false
		for _, l := range st.Links {
			if l.ID == sid && l.Enabled && l.MediaRoot != "" {
				if root, err := filepath.Abs(l.MediaRoot); err == nil && filepath.Dir(root) != root {
					found = true
				}
			}
		}
		if !found {
			fail(w, 400, errors.New("请选择已启用且设置了Aether媒体目录的以链"))
			return
		}
	}
	if in.Enabled && len(in.LinkIDs) == 0 {
		fail(w, 400, errors.New("请选择要联动的以链"))
		return
	}
	expanded := !st.MediaDeepDelete.Enabled
	for _, sid := range in.LinkIDs {
		expanded = expanded || !slices.Contains(st.MediaDeepDelete.LinkIDs, sid)
	}
	if in.Enabled && expanded && !in.Confirmed {
		fail(w, 400, errors.New("请确认启用后会联动删除STRM对应的真实源文件"))
		return
	}
	if err := a.store.update(func(st *State) error { st.MediaDeepDelete = in.mediaDeepDeleteSettings; return nil }); err != nil {
		fail(w, 500, err)
		return
	}
	jsonResponse(w, 200, in.mediaDeepDeleteSettings)
}

func ordinaryDeepSTRM(name string) bool {
	name = strings.ToLower(name)
	if slices.Contains([]string{"cas.strm", "share.strm", "ed2k.strm"}, path.Base(name)) {
		return false
	}
	return strings.HasSuffix(name, ".strm") && !strings.HasSuffix(name, ".cas.strm") && !strings.HasSuffix(name, ".share.strm") && !strings.HasSuffix(name, ".ed2k.strm")
}

// Match explicit file deletion, never database maintenance, scan events or playback.
func deepDeleteIDs(kind string, r *http.Request) ([]string, bool, error) {
	p := strings.TrimSuffix(r.URL.Path, "/")
	if kind == "audiobookshelf" {
		if r.Method != "DELETE" || !strings.HasPrefix(p, "/api/items/") || r.URL.Query().Get("hard") != "1" {
			return nil, false, nil
		}
		id := strings.TrimPrefix(p, "/api/items/")
		if strings.Contains(id, "/") {
			return nil, false, nil
		}
		if !safeName(id) {
			return nil, true, errors.New("媒体标识无效")
		}
		return []string{id}, true, nil
	}
	if strings.HasPrefix(strings.ToLower(p), "/emby/") {
		p = p[len("/emby"):]
	}
	if r.Method == "POST" && strings.HasSuffix(strings.ToLower(p), "/delete") {
		p = p[:len(p)-len("/Delete")]
	} else if r.Method != "DELETE" {
		return nil, false, nil
	}
	if strings.ToLower(p) != "/items" && !strings.HasPrefix(strings.ToLower(p), "/items/") {
		return nil, false, nil
	}
	ids := ""
	if len(p) > len("/Items/") {
		ids = p[len("/Items/"):]
	}
	if strings.EqualFold(p, "/Items") {
		ids = r.URL.Query().Get("Ids")
		if ids == "" {
			ids = r.URL.Query().Get("ids")
		}
	}
	if strings.Contains(ids, "/") {
		return nil, false, nil
	}
	if ids == "" {
		return nil, true, errors.New("删除请求缺少媒体标识")
	}
	result := strings.Split(ids, ",")
	if len(result) > 200 {
		return nil, true, errors.New("一次最多删除200个作品")
	}
	for _, id := range result {
		if !safeName(id) {
			return nil, true, errors.New("媒体标识无效")
		}
	}
	return result, true, nil
}

type deepPointer struct {
	name   string
	info   os.FileInfo
	digest string
}
type deepSource struct {
	storage  Storage
	parent   string
	file     File
	identity os.FileInfo
}
type deepPlan struct {
	root     *os.Root
	rootInfo os.FileInfo
	pointers []deepPointer
	sources  []deepSource
}

// Reuse fresh parent listings within one plan, never across requests or stages.
type deepInventory struct {
	lists   map[string][]File
	entries int
}

func (i *deepInventory) list(ctx context.Context, a *App, s Storage, parent string) ([]File, error) {
	if i == nil {
		return a.rawList(ctx, s, parent)
	}
	key := s.ID + "\x00" + storageRevision(s) + "\x00" + parent
	if files, ok := i.lists[key]; ok {
		return files, nil
	}
	files, err := a.rawList(ctx, s, parent)
	if err != nil {
		return nil, err
	}
	if i.entries+len(files) > 100000 {
		return nil, errors.New("源目录核验超过十万项")
	}
	i.entries += len(files)
	i.lists[key] = files
	return files, nil
}
func freshDeepInventory() *deepInventory { return &deepInventory{lists: map[string][]File{}} }

func deepFileID(s Storage, id string) string {
	if backupPathBased(s) {
		return path.Clean("/" + id)
	}
	return id
}
func (a *App) deepSourceRef(content string) (Storage, string, error) {
	content = strings.TrimSpace(content)
	bad := errors.New("普通STRM目标无法唯一核验到已配置存储，未联动删除")
	if strings.ContainsAny(content, "\r\n\x00") || len(content) > 64<<10 {
		return Storage{}, "", bad
	}
	u, err := url.Parse(content)
	if err != nil {
		return Storage{}, "", bad
	}
	st := a.store.snapshotWithLogLimit(0)
	var sid, fid string
	switch {
	case strings.HasPrefix(u.Path, "/stream/"):
		token := strings.TrimPrefix(u.Path, "/stream/")
		mac := hmac.New(sha256.New, []byte(st.SignKey))
		mac.Write([]byte(token))
		sig, e := base64.RawURLEncoding.DecodeString(u.Query().Get("sign"))
		raw, e2 := base64.RawURLEncoding.DecodeString(token)
		var claim streamClaim
		if e != nil || e2 != nil || !hmac.Equal(sig, mac.Sum(nil)) || json.Unmarshal(raw, &claim) != nil || claim.CAS != nil || claim.ED2K != nil || claim.Redirect {
			return Storage{}, "", bad
		}
		sid, fid = claim.Storage, claim.File
	case strings.HasPrefix(u.Path, "/d/"):
		parts := strings.Split(strings.TrimPrefix(u.Path, "/d/"), "/")
		if len(parts) >= 2 && pickcodePath.MatchString(parts[1]) {
			ref, e := a.readSTRMReference(parts[0] + "/" + parts[1])
			s, e2 := a.store.storage(ref.Storage)
			account, e3 := account115(s)
			if e == nil && e2 == nil && e3 == nil && account == parts[0] && (len(parts) == 2 || len(parts) == 3 && parts[2] == ref.Name) {
				sid, fid = ref.Storage, ref.File
			}
		}
	case strings.HasPrefix(u.Path, "/api/strm/play/"):
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/api/strm/play/"), "/", 6)
		if len(parts) == 6 && parts[2] == "t" && parts[4] == "n" && hmac.Equal([]byte(parts[3]), []byte(a.quarkSTRMSign(parts[0], parts[1], parts[5]))) {
			raw, e := base64.RawURLEncoding.DecodeString(parts[1])
			if e == nil {
				sid, fid = parts[0], string(raw)
			}
		}
	}
	if sid != "" && fid != "" {
		s, e := a.store.storage(sid)
		return s, fid, e
	}
	// Local paths and provider HTTP paths are resolved only against configured roots.
	matches := []deepSource{}
	for _, s := range st.Storages {
		if !s.Enabled {
			continue
		}
		candidate := ""
		if s.Type == "local" && filepath.IsAbs(content) {
			rel, e := filepath.Rel(s.Config["root"], content)
			if e == nil && rel != "." && !strings.HasPrefix(rel, "..") {
				candidate = "/" + filepath.ToSlash(rel)
			}
		} else if s.Type == "openlist" || s.Type == "webdav" {
			base, e := url.Parse(s.Config["address"])
			if e != nil || u.Scheme != base.Scheme || u.Host != base.Host || u.RawQuery != "" || u.Fragment != "" {
				continue
			}
			prefix := path.Join(base.Path, s.Config["root"])
			if s.Type == "openlist" {
				prefix = path.Join(base.Path, "d", s.Config["root"])
			}
			if strings.HasPrefix(u.Path, strings.TrimRight(prefix, "/")+"/") {
				candidate = strings.TrimPrefix(u.Path, strings.TrimRight(prefix, "/"))
			}
		}
		if candidate != "" {
			matches = append(matches, deepSource{storage: s, file: File{ID: candidate}})
		}
	}
	if len(matches) != 1 {
		return Storage{}, "", bad
	}
	return matches[0].storage, matches[0].file.ID, nil
}

func (a *App) findDeepSource(ctx context.Context, s Storage, fid string) (deepSource, error) {
	return a.findDeepSourceIn(ctx, s, fid, nil)
}
func (a *App) findDeepSourceIn(ctx context.Context, s Storage, fid string, inventory *deepInventory) (deepSource, error) {
	queue := []string{rootOf(s)}
	if backupPathBased(s) {
		queue = []string{path.Dir(deepFileID(s, fid))}
	}
	seen := map[string]bool{}
	count := 0
	for i := 0; i < len(queue); i++ {
		parent := queue[i]
		if seen[parent] {
			continue
		}
		seen[parent] = true
		if len(seen) > 10000 {
			return deepSource{}, errors.New("源目录核验范围过大")
		}
		files, err := inventory.list(ctx, a, s, parent)
		if err != nil {
			return deepSource{}, errors.New("无法读取真实源目录")
		}
		count += len(files)
		if count > 100000 {
			return deepSource{}, errors.New("源目录核验超过十万项")
		}
		for _, f := range files {
			if !f.IsDir && deepFileID(s, f.ID) == deepFileID(s, fid) {
				if !safeName(f.Name) {
					return deepSource{}, errors.New("真实源文件名称无效")
				}
				if strings.EqualFold(path.Ext(f.Name), ".strm") {
					return deepSource{}, errors.New("STRM目标仍是指针文件，未联动删除")
				}
				d := deepSource{storage: s, parent: parent, file: f}
				if s.Type == "local" {
					root, e := openStrmReplaceRoot(s.Config["root"])
					if e != nil {
						return d, e
					}
					name, e := relative(f.ID)
					if e == nil {
						var parentRoot *os.Root
						parentRoot, e = quarkLibraryParent(root, name)
						if e == nil {
							d.identity, e = parentRoot.Lstat(path.Base(name))
							parentRoot.Close()
						}
					}
					root.Close()
					if e != nil || d.identity == nil || !d.identity.Mode().IsRegular() {
						return d, errors.New("源文件不是普通文件或路径已改变")
					}
				}
				return d, nil
			}
			if f.IsDir && !backupPathBased(s) {
				queue = append(queue, f.ID)
			}
		}
	}
	return deepSource{}, errors.New("真实源文件不存在或不在存储范围内")
}

func (a *App) prepareDeepPlan(ctx context.Context, link MediaLink, paths []string) (*deepPlan, error) {
	root, err := openStrmReplaceRoot(link.MediaRoot)
	if err != nil {
		return nil, errors.New("无法打开Aether媒体目录")
	}
	p := &deepPlan{root: root}
	p.rootInfo, err = root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			root.Close()
		}
	}()
	seen, targets := map[string]bool{}, map[string]bool{}
	inventory := freshDeepInventory()
	from := link.UpstreamRoot
	if from == "" {
		from = link.MediaRoot
	}
	from = strings.TrimRight(strings.ReplaceAll(from, "\\", "/"), "/")
	for _, raw := range paths {
		raw = strings.ReplaceAll(raw, "\\", "/")
		if !ordinaryDeepSTRM(raw) {
			continue
		}
		if !strings.HasPrefix(raw, from+"/") {
			return nil, errors.New("媒体路径不在以链映射范围")
		}
		name := strings.TrimPrefix(raw, from+"/")
		if !quarkLibraryRelative(name) {
			return nil, errors.New("STRM路径无效")
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		parent, e := quarkLibraryParent(root, name)
		if e != nil {
			return nil, errors.New("STRM父目录不可核验")
		}
		f, e := parent.Open(path.Base(name))
		if e != nil {
			parent.Close()
			return nil, e
		}
		info, e := parent.Lstat(path.Base(name))
		opened, e2 := f.Stat()
		body, readErr := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		f.Close()
		parent.Close()
		if e != nil || e2 != nil || !info.Mode().IsRegular() || !os.SameFile(info, opened) || readErr != nil || len(body) > 64<<10 {
			return nil, errors.New("STRM文件类型或内容无效")
		}
		s, fid, e := a.deepSourceRef(string(body))
		if e != nil {
			return nil, e
		}
		key := s.ID + "\x00" + deepFileID(s, fid)
		if !targets[key] {
			source, e := a.findDeepSourceIn(ctx, s, fid, inventory)
			if e != nil {
				return nil, e
			}
			p.sources = append(p.sources, source)
			targets[key] = true
		}
		sum := sha256.Sum256(body)
		p.pointers = append(p.pointers, deepPointer{name, info, fmt.Sprintf("%x", sum)})
	}
	failed = false
	return p, nil
}

type deepStatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *deepStatusWriter) WriteHeader(status int) {
	if w.status == 0 && status >= 200 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *deepStatusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}
func (w *deepStatusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Native user permissions remain enforced by the original upstream request. The
// cloud stage is never reached for failed requests, scans, or database-only removals.
func (a *App) withMediaDeepDelete(link MediaLink, provider upstream.Provider, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		settings := a.store.snapshotWithLogLimit(0).MediaDeepDelete
		ids, matched, err := deepDeleteIDs(link.Type, r)
		if !settings.Enabled || !slices.Contains(settings.LinkIDs, link.ID) || !matched {
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			fail(w, 400, err)
			return
		}
		if !a.mediaDeepDeleteMu.TryLock() {
			fail(w, 409, errors.New("媒体深度删除正在执行，请稍后重试"))
			return
		}
		defer a.mediaDeepDeleteMu.Unlock()
		ctx, cancel := context.WithTimeout(upstream.WithDeletionCredentials(r.Context(), r), 45*time.Second)
		defer cancel()
		paths := []string{}
		for _, id := range ids {
			values, e := upstream.DeletionPaths(ctx, provider, id)
			if e != nil {
				fail(w, 409, errors.New("无法读取完整作品删除范围，未执行删除"))
				return
			}
			if len(paths)+len(values) > 100000 {
				fail(w, 409, errors.New("一次作品删除范围超过十万项，未执行删除"))
				return
			}
			paths = append(paths, values...)
		}
		plan, err := a.prepareDeepPlan(ctx, link, paths)
		if err != nil {
			fail(w, 409, err)
			return
		}
		defer plan.root.Close()
		if len(plan.sources) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		ledger, err := a.deepLedger()
		if err != nil {
			fail(w, 409, errors.New("深度删除回执无法核验"))
			return
		}
		for _, s := range plan.sources {
			if ledger.Intents[deepIntent(s)] {
				fail(w, 409, errors.New("源文件有已提交或未确认删除回执，不会重复执行"))
				return
			}
		}
		nodes := []taskFileNode{}
		for _, pointer := range plan.pointers {
			nodes = append(nodes, taskFileNode{storage: Storage{Type: "local", Config: map[string]string{"root": link.MediaRoot}}, id: pointer.name})
		}
		for _, s := range plan.sources {
			nodes = append(nodes, taskFileNode{storage: s.storage, id: s.file.ID})
		}
		a.runMu.Lock()
		release, e := a.reserveTaskFileNodes(ctx, nodes)
		a.runMu.Unlock()
		if e != nil {
			fail(w, 409, e)
			return
		}
		defer release()
		if e := verifyDeepPointers(plan); e != nil {
			fail(w, 409, e)
			return
		}
		record := mediaDeepDeleteRecord{ID: id(), LinkName: link.Name, Name: fmt.Sprintf("%s · %d个普通STRM", plan.pointers[0].name, len(plan.pointers)), Status: "prepared", Message: "等待媒体服务确认删除", Time: time.Now()}
		ledger.Records = append([]mediaDeepDeleteRecord{record}, ledger.Records...)
		if len(ledger.Records) > 500 {
			ledger.Records = ledger.Records[:500]
		}
		if err = a.saveDeepLedger(ledger); err != nil {
			fail(w, 500, errors.New("删除意图保存失败，未执行删除"))
			return
		}
		status := &deepStatusWriter{ResponseWriter: w}
		next.ServeHTTP(status, r)
		if status.status != 200 && status.status != 204 {
			ledger.Records[0].Status = "skipped"
			ledger.Records[0].Message = "媒体服务未确认成功，未删除源文件"
			_ = a.saveDeepLedger(ledger)
			return
		}
		work, cancelWork := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancelWork()
		a.runMu.Lock()
		err = a.executeDeepPlan(work, link, settings, plan, &ledger)
		a.runMu.Unlock()
		if err != nil {
			ledger.Records[0].Status = "failed"
			ledger.Records[0].Message = err.Error()
			a.store.event("error", "media-deep-delete", link.Name+"："+err.Error())
		} else {
			ledger.Records[0].Status = "success"
			ledger.Records[0].Message = fmt.Sprintf("已删除%d个真实源文件", ledger.Records[0].Count)
			a.store.event("info", "media-deep-delete", link.Name+"："+ledger.Records[0].Message)
		}
		if a.saveDeepLedger(ledger) != nil {
			a.store.event("error", "media-deep-delete", "完成回执保存失败；保留写前意图，不自动重试")
		}
	})
}

func deepIntent(s deepSource) string {
	return archiveStamp(s.storage.ID, storageRevision(s.storage), s.file.ID)
}

func verifyDeepPointers(p *deepPlan) error {
	for _, pointer := range p.pointers {
		parent, err := quarkLibraryParent(p.root, pointer.name)
		if err != nil {
			return errors.New("STRM目录在删除前已改变")
		}
		info, err := parent.Lstat(path.Base(pointer.name))
		if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, pointer.info) {
			parent.Close()
			return errors.New("STRM在删除前已改变")
		}
		f, err := parent.Open(path.Base(pointer.name))
		if err != nil {
			parent.Close()
			return err
		}
		opened, e := f.Stat()
		if e != nil || !os.SameFile(opened, pointer.info) {
			f.Close()
			parent.Close()
			return errors.New("STRM身份在删除前已改变")
		}
		raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
		f.Close()
		parent.Close()
		sum := sha256.Sum256(raw)
		if err != nil || fmt.Sprintf("%x", sum) != pointer.digest {
			return errors.New("STRM内容在删除前已改变")
		}
	}
	return nil
}
func (a *App) executeDeepPlan(ctx context.Context, link MediaLink, settings mediaDeepDeleteSettings, p *deepPlan, ledger *mediaDeepDeleteLedger) error {
	check := func() error {
		st := a.store.snapshotWithLogLimit(0)
		b, _ := json.Marshal(st.MediaDeepDelete)
		want, _ := json.Marshal(settings)
		if !bytes.Equal(b, want) {
			return errors.New("深度删除配置已改变，停止联动")
		}
		found := false
		for _, l := range st.Links {
			b, _ := json.Marshal(l)
			want, _ := json.Marshal(link)
			if bytes.Equal(b, want) && l.Enabled {
				found = true
			}
		}
		if !found {
			return errors.New("以链配置已改变，停止联动")
		}
		live, e := openStrmReplaceRoot(link.MediaRoot)
		if e != nil {
			return errors.New("媒体目录已移动或替换")
		}
		defer live.Close()
		info, e := live.Stat(".")
		if e != nil || !os.SameFile(info, p.rootInfo) {
			return errors.New("媒体目录身份已改变")
		}
		for _, pointer := range p.pointers {
			if _, e = p.root.Lstat(filepath.FromSlash(pointer.name)); !os.IsNotExist(e) {
				return errors.New("STRM仍存在、已重建或无法确认删除，源文件保留")
			}
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return err
	}
	// Validate the entire source batch before the first write.
	inventory := freshDeepInventory()
	for _, s := range p.sources {
		if err := a.verifyDeepSourceIn(ctx, s, inventory); err != nil {
			return err
		}
	}
	groups := [][]deepSource{}
	groupIndices := map[string]int{}
	for _, source := range p.sources {
		key := source.storage.ID + "\x00" + storageRevision(source.storage) + "\x00" + source.parent
		index, ok := groupIndices[key]
		if !ok || len(groups[index]) >= 200 {
			index = len(groups)
			groups = append(groups, nil)
			groupIndices[key] = index
		}
		groups[index] = append(groups[index], source)
	}
	for _, group := range groups {
		if err := check(); err != nil {
			return err
		}
		fresh := freshDeepInventory()
		for _, s := range group {
			if err := a.verifyDeepSourceIn(ctx, s, fresh); err != nil {
				return err
			}
		}
		if group[0].storage.Type == "local" {
			for _, s := range group {
				ledger.Intents[deepIntent(s)] = true
				ledger.Records[0].Status = "deleting"
				if err := a.saveDeepLedger(*ledger); err != nil {
					return errors.New("写前回执保存失败，未继续删除")
				}
				if err := a.deleteDeepSource(ctx, s); err != nil {
					return errors.New("源文件删除结果未确认，请核对存储；不会自动重试")
				}
				ledger.Records[0].Count++
				if err := a.saveDeepLedger(*ledger); err != nil {
					return errors.New("完成回执保存失败，未继续删除")
				}
			}
		} else {
			for _, s := range group {
				ledger.Intents[deepIntent(s)] = true
			}
			ledger.Records[0].Status = "deleting"
			if err := a.saveDeepLedger(*ledger); err != nil {
				return errors.New("写前回执保存失败，未继续删除")
			}
			ids := make([]string, len(group))
			for i, s := range group {
				ids[i] = s.file.ID
			}
			s := group[0]
			if err := a.cloudFileAction(ctx, s.storage, s.storage, fileActionRequest{Action: "delete", Source: s.parent, StorageID: s.storage.ID, IDs: ids}); err != nil {
				return errors.New("源文件删除结果未确认，请核对存储；不会自动重试")
			}
			ledger.Records[0].Count += len(group)
		}
		if err := a.saveDeepLedger(*ledger); err != nil {
			return errors.New("完成回执保存失败，未继续删除")
		}
	}
	a.cache.clear()
	return nil
}
func (a *App) verifyDeepSource(ctx context.Context, s deepSource) error {
	return a.verifyDeepSourceIn(ctx, s, nil)
}
func (a *App) verifyDeepSourceIn(ctx context.Context, s deepSource, inventory *deepInventory) error {
	current, e := a.store.storage(s.storage.ID)
	if e != nil || storageRevision(current) != storageRevision(s.storage) {
		return errors.New("源存储配置已改变")
	}
	files, e := inventory.list(ctx, a, current, s.parent)
	if e != nil {
		return errors.New("源目录无法重新核验")
	}
	found := false
	for _, f := range files {
		if deepFileID(current, f.ID) == deepFileID(current, s.file.ID) {
			if f.IsDir || f.Name != s.file.Name || f.Size != s.file.Size || !f.Modified.Equal(s.file.Modified) || f.SHA256 != s.file.SHA256 || f.MD5 != s.file.MD5 || f.PickCode != s.file.PickCode {
				return errors.New("真实源文件已改变，未继续删除")
			}
			found = true
		}
	}
	if !found {
		return errors.New("真实源文件已移动或消失，未继续删除")
	}
	if s.storage.Type == "local" {
		root, e := openStrmReplaceRoot(current.Config["root"])
		if e != nil {
			return errors.New("本地源目录不可核验")
		}
		defer root.Close()
		name, e := relative(s.file.ID)
		if e != nil {
			return e
		}
		parent, e := quarkLibraryParent(root, name)
		if e != nil {
			return e
		}
		defer parent.Close()
		info, e := parent.Lstat(path.Base(name))
		if e != nil || !info.Mode().IsRegular() || !os.SameFile(info, s.identity) {
			return errors.New("本地源文件身份已改变")
		}
	}
	return ctx.Err()
}
func (a *App) deleteDeepSource(ctx context.Context, s deepSource) error {
	if s.storage.Type != "local" {
		return a.cloudFileAction(ctx, s.storage, s.storage, fileActionRequest{Action: "delete", Source: s.parent, StorageID: s.storage.ID, IDs: []string{s.file.ID}})
	}
	root, err := openStrmReplaceRoot(s.storage.Config["root"])
	if err != nil {
		return err
	}
	defer root.Close()
	name, err := relative(s.file.ID)
	if err != nil || name == "." {
		return errors.New("不能删除源根目录")
	}
	parent, err := quarkLibraryParent(root, name)
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(path.Base(name))
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(info, s.identity) || info.Size() != s.file.Size || !info.ModTime().Equal(s.file.Modified) {
		return errors.New("源文件身份改变")
	}
	if s.storage.Config["deleteMode"] == "permanent" {
		return parent.Remove(path.Base(name))
	}
	target := path.Join(".aether-trash", id(), path.Base(name))
	if err = root.MkdirAll(path.Dir(target), 0700); err != nil {
		return err
	}
	return root.Rename(name, target)
}
