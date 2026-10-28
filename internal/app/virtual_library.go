package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	driver "github.com/SheltonZhu/115driver/pkg/driver"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var virtual115Key = regexp.MustCompile(`^([a-zA-Z0-9]{1,128})_([a-zA-Z0-9]{0,32})_([0-9]{1,24})(\.[a-zA-Z0-9]{1,16})?$`)

// Generation grants only this exact share resource the right to create a temporary copy.
// Unregistered portable URLs can use the provider's read-only share download interface.
func (a *App) virtualGrant(key string, write bool, parent ...string) error {
	sum := sha256.Sum256([]byte(key))
	name := filepath.Join(a.dataDir, "virtual-library", hex.EncodeToString(sum[:])+".enc")
	a.strmIndexMu.Lock()
	defer a.strmIndexMu.Unlock()
	if !write {
		b, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		n := a.store.aead.NonceSize()
		if len(b) < n || len(b) > 4096 {
			return errors.New("链库授权记录损坏")
		}
		plain, err := a.store.aead.Open(nil, b[:n], b[n:], []byte("virtual:"+key))
		if err != nil || strings.SplitN(string(plain), "\n", 2)[0] != key {
			return errors.New("链库授权记录无效")
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	if !a.virtualGrantReady {
		entries, err := os.ReadDir(filepath.Dir(name))
		if err != nil {
			return err
		}
		a.virtualGrantCount = len(entries)
		a.virtualGrantReady = true
	}
	_, statErr := os.Stat(name)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if a.virtualGrantCount >= 10000 && os.IsNotExist(statErr) {
		return errors.New("115链库已达到10000项上限")
	}
	payload := key
	if len(parent) > 0 {
		if parent[0] != "root" && !regexp.MustCompile(`^[0-9]{1,24}$`).MatchString(parent[0]) {
			return errors.New("分享目录ID无效")
		}
		payload += "\n" + parent[0]
	}
	nonce := make([]byte, a.store.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	if err := atomicWrite(name, a.store.aead.Seal(nonce, nonce, []byte(payload), []byte("virtual:"+key))); err != nil {
		return err
	}
	if os.IsNotExist(statErr) {
		a.virtualGrantCount++
	}
	return nil
}

func (a *App) virtualShareParent(key string) (string, error) {
	sum := sha256.Sum256([]byte(key))
	b, err := os.ReadFile(filepath.Join(a.dataDir, "virtual-library", hex.EncodeToString(sum[:])+".enc"))
	if err != nil {
		return "", err
	}
	n := a.store.aead.NonceSize()
	if len(b) < n || len(b) > 4096 {
		return "", errors.New("链库授权记录损坏")
	}
	plain, err := a.store.aead.Open(nil, b[:n], b[n:], []byte("virtual:"+key))
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(string(plain), "\n", 2)
	if parts[0] != key {
		return "", errors.New("链库授权记录无效")
	}
	if len(parts) == 2 {
		return parts[1], nil
	}
	return "root", nil
}

func virtualShareURL(base, code, pass string, item shareEntry) (string, error) {
	if _, err := virtual115ResourceKey(code, pass, item); err != nil {
		return "", err
	}
	return virtual115FilenameURL(base, code, pass, item.Name), nil
}

func legacyVirtualShareURL(base, key, filename string) string {
	name := strings.NewReplacer("%", "%25", "#", "%23", "?", "%3F", "\r", "%0D", "\n", "%0A").Replace(filename)
	return strings.TrimRight(base, "/") + "/s/" + key + "?/" + name
}

func (a *App) virtualLibrary(w http.ResponseWriter, r *http.Request) {
	var in struct {
		StorageID string `json:"storageId"`
		URL       string `json:"url"`
		Directory string `json:"directory"`
		Preview   string `json:"preview"`
	}
	if !decode(w, r, &in) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	s, err := a.store.storage(in.StorageID)
	if err != nil || s.Type != "115" || !s.Enabled {
		fail(w, 400, errors.New("请选择已启用的115存储池"))
		return
	}
	if r.PathValue("action") == "preview" {
		provider, code, pass, err := parseShareLink(in.URL, "")
		if err != nil || provider != "115" {
			fail(w, 400, errors.New("请输入有效的115分享链接及访问码"))
			return
		}
		token, items, err := a.readShare(ctx, s, code, pass)
		if err != nil {
			fail(w, 400, err)
			return
		}
		batches, err := a.planShare(ctx, s, code, pass, items)
		if err != nil {
			fail(w, 400, err)
			return
		}
		files := []map[string]any{}
		for _, batch := range batches {
			for _, item := range batch.Items {
				if taskMedia(Task{}, item.Name) {
					content, e := virtualShareURL(a.currentSTRMSettings().Address, code, pass, item)
					if e != nil {
						fail(w, 400, e)
						return
					}
					files = append(files, map[string]any{"path": path.Join(batch.Directory, item.Name) + ".strm", "content": content, "size": item.Size})
				}
			}
		}
		key := id()
		a.shareMu.Lock()
		if a.sharePreviews == nil {
			a.sharePreviews = map[string]*sharePreview{}
		}
		for k, p := range a.sharePreviews {
			if time.Now().After(p.Expires) {
				delete(a.sharePreviews, k)
			}
		}
		if len(a.sharePreviews) >= 64 {
			a.shareMu.Unlock()
			fail(w, 429, errors.New("预览过多，请稍后再试"))
			return
		}
		a.sharePreviews[key] = &sharePreview{Owner: authorizationOwner(r), Config: shareConfig(s), Storage: s.ID, Code: code, Pass: pass, Token: token, Items: items, Batches: batches, Expires: time.Now().Add(10 * time.Minute)}
		a.shareMu.Unlock()
		jsonResponse(w, 200, map[string]any{"preview": key, "files": files})
		return
	}
	if r.PathValue("action") != "generate" {
		http.NotFound(w, r)
		return
	}
	if !filepath.IsAbs(in.Directory) {
		fail(w, 400, errors.New("请选择容器内绝对生成目录"))
		return
	}
	root, err := openStrmReplaceRoot(filepath.Clean(in.Directory))
	if err != nil {
		fail(w, 400, err)
		return
	}
	defer root.Close()
	a.shareMu.Lock()
	p := a.sharePreviews[in.Preview]
	if p == nil || p.Used || p.BatchBusy || p.Next != 0 || p.Owner != authorizationOwner(r) || p.Config != shareConfig(s) || p.Storage != s.ID || time.Now().After(p.Expires) {
		a.shareMu.Unlock()
		fail(w, 409, errors.New("预览已失效，请重新解析"))
		return
	}
	p.Used = true
	preview := *p
	a.shareMu.Unlock()
	generated, skipped := 0, 0
	names := virtual115Names{Version: 1, Files: []virtual115NamedFile{}}
	for _, batch := range preview.Batches {
		for _, item := range batch.Items {
			if err = ctx.Err(); err != nil {
				fail(w, 400, err)
				return
			}
			if !taskMedia(Task{}, item.Name) {
				continue
			}
			current, e := a.store.storage(s.ID)
			if e != nil || !current.Enabled || shareConfig(current) != preview.Config {
				fail(w, 409, errors.New("存储配置已改变"))
				return
			}
			content, e := virtualShareURL(a.currentSTRMSettings().Address, preview.Code, preview.Pass, item)
			if e != nil {
				fail(w, 400, e)
				return
			}
			key := preview.Code + "_" + preview.Pass + "_" + item.ID + strings.ToLower(path.Ext(item.Name))
			grantErr := a.virtualGrant(key, false)
			if grantErr != nil && !os.IsNotExist(grantErr) {
				fail(w, 400, grantErr)
				return
			}
			if e = a.virtualGrant(key, true, batch.Source); e != nil {
				fail(w, 400, e)
				return
			}
			names.Files = append(names.Files, virtual115NamedFile{Key: key, Parent: batch.Source, Name: item.Name, Size: item.Size, SHA1: item.SHA1})
			child, e := virtualOutputRoot(root, batch.Directory)
			if e != nil {
				fail(w, 400, e)
				return
			}
			f, e := child.OpenFile(item.Name+".strm", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if os.IsExist(e) {
				changed := false
				if grantErr == nil {
					legacy := legacyVirtualShareURL(a.currentSTRMSettings().Address, key, item.Name) + "\n"
					changed, e = replaceStrmFileMode(child, item.Name+".strm", `\A`+regexp.QuoteMeta(legacy)+`\z`,
						strings.ReplaceAll(content+"\n", "$", "$$"), "regex", nil)
				}
				child.Close()
				if grantErr == nil && e != nil {
					fail(w, 400, e)
					return
				}
				if changed {
					generated++
				} else {
					skipped++
				}
				continue
			}
			if e != nil {
				child.Close()
				fail(w, 400, e)
				return
			}
			_, e = f.WriteString(content + "\n")
			if e == nil {
				e = f.Sync()
			}
			closeErr := f.Close()
			if e == nil {
				e = closeErr
			}
			if e != nil {
				_ = child.Remove(item.Name + ".strm")
				child.Close()
				fail(w, 400, e)
				return
			}
			child.Close()
			generated++
		}
	}
	current, err := a.store.storage(s.ID)
	if err != nil || shareConfig(current) != preview.Config || ctx.Err() != nil {
		fail(w, 409, errors.New("存储配置已改变或执行已取消，未发布文件名索引"))
		return
	}
	if err := a.saveVirtual115Names(preview.Code, preview.Pass, names); err != nil {
		fail(w, 400, err)
		return
	}
	a.store.event("info", "virtual", fmt.Sprintf("115链库生成完成：%d 个，跳过 %d 个已有文件", generated, skipped))
	jsonResponse(w, 200, map[string]int{"generated": generated, "skipped": skipped})
}

func virtualOutputRoot(root *os.Root, directory string) (*os.Root, error) {
	if directory == "." {
		directory = ""
	}
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(directory, "/") {
		if part == "" {
			continue
		}
		if !safeName(part) {
			current.Close()
			return nil, errors.New("生成目录不安全")
		}
		if err = current.Mkdir(part, 0755); err != nil && !os.IsExist(err) {
			current.Close()
			return nil, err
		}
		child, e := strmReplaceSubroot(current, part)
		current.Close()
		if e != nil {
			return nil, e
		}
		current = child
	}
	return current, nil
}

func (a *App) virtual115Storage(now time.Time) (Storage, ED2KPoolConfig, error) {
	st := a.store.snapshotWithLogLimit(0)
	pool := requiredED2KPool(st.ED2KPool, st)
	available := []Storage{}
	for _, sid := range pool.StorageIDs {
		for _, s := range st.Storages {
			if s.ID == sid && s.Type == "115" && s.Enabled && storageAuthBlocked(s, now) == nil {
				available = append(available, s)
			}
		}
	}
	if err := validateED2KPoolShape(pool); err != nil {
		return Storage{}, pool, err
	}
	if len(available) == 0 {
		return Storage{}, pool, errors.New("请在115绑定池中启用健康存储")
	}
	return available[(now.Unix()/int64(pool.RotationMinutes*60))%int64(len(available))], pool, nil
}

func (a *App) playVirtualShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(405)
		return
	}
	match := virtual115Key.FindStringSubmatch(r.PathValue("key"))
	if match == nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	// Serialize writes and cleanup with the same pool gate. HEAD never creates files.
	if err := a.acquireCAS(ctx); err != nil {
		fail(w, 503, err)
		return
	}
	defer func() { <-a.casGate }()
	s, pool, err := a.virtual115Storage(time.Now())
	if err != nil {
		fail(w, 503, err)
		return
	}
	stPool := a.store.snapshotWithLogLimit(0)
	for _, temporary := range stPool.ED2KTemporary {
		if time.Now().After(temporary.ExpiresAt) {
			continue
		}
		for _, sid := range pool.StorageIDs {
			if temporary.StorageID != sid {
				continue
			}
			candidate, e := a.store.storage(sid)
			if e != nil || !candidate.Enabled || candidate.Type != "115" || ed2kRevision(candidate) != temporary.Revision || storageAuthBlocked(candidate, time.Now()) != nil {
				continue
			}
			sum := sha256.Sum256([]byte(ed2kRevision(candidate) + "\x00" + r.PathValue("key")))
			if temporary.Key == hex.EncodeToString(sum[:]) {
				s = candidate
			}
		}
	}
	parent, grantErr := a.virtualShareParent(r.PathValue("key"))
	identity, named := r.Context().Value(virtual115IdentityKey{}).(virtual115NamedFile)
	if grantErr != nil {
		if !os.IsNotExist(grantErr) {
			fail(w, 403, grantErr)
			return
		}
		if named {
			http.NotFound(w, r)
			return
		}
		c, e := client115(ctx, s)
		if e != nil {
			fail(w, 502, e)
			return
		}
		if e = a.waitAPI(ctx, s.ID); e != nil {
			fail(w, 502, e)
			return
		}
		info, e := c.DownloadByShareCodeWithUA(r.UserAgent(), match[1], match[2], match[3])
		if e != nil || info == nil || info.FileID != match[3] || info.URL.URL == "" {
			fail(w, 502, errors.New("115未提供分享直链；需先在115链库中生成以授权临时转存"))
			return
		}
		redirectDownload(w, r, info.URL.URL)
		return
	}
	_, items, err := a.readShareAt(ctx, s, match[1], match[2], parent)
	if err != nil {
		fail(w, 502, err)
		return
	}
	var source *shareEntry
	for _, item := range items {
		if item.ID == match[3] {
			copy := item
			source = &copy
		}
	}
	if source == nil || source.IsDir || !safeName(source.Name) || !simulcastSHA1(source.SHA1) || source.Size <= 0 || strings.ToLower(path.Ext(source.Name)) != match[4] {
		http.NotFound(w, r)
		return
	}
	if named && (identity.Key != r.PathValue("key") || identity.Parent != parent || identity.Name != source.Name ||
		identity.Size != source.Size || identity.SHA1 != "" && !strings.EqualFold(identity.SHA1, source.SHA1)) {
		http.NotFound(w, r)
		return
	}
	sum := sha256.Sum256([]byte(ed2kRevision(s) + "\x00" + r.PathValue("key")))
	key := hex.EncodeToString(sum[:])
	st := a.store.snapshotWithLogLimit(0)
	var temporary *ED2KTemporary
	for _, item := range st.ED2KTemporary {
		if item.Key == key && item.StorageID == s.ID && item.Revision == ed2kRevision(s) {
			copy := item
			temporary = &copy
		}
	}
	if r.Method == "HEAD" && (temporary == nil || named) {
		w.Header().Set("Content-Length", fmt.Sprint(source.Size))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		return
	}
	c, err := client115(ctx, s)
	if err != nil {
		fail(w, 502, err)
		return
	}
	ops := virtualPlaybackOps{
		folder: func(ctx context.Context) (string, error) {
			folder, _, e := a.uploadParent(ctx, s, "0", strings.TrimPrefix(pool.Directory, "/")+"/share-"+key+"/.placeholder")
			return folder, e
		},
		submit: func(ctx context.Context, folder string) error {
			_, e := submitShare(ctx, s, sharePreview{Code: match[1], Pass: match[2]}, folder, []shareEntry{*source})
			return e
		},
		files: func(ctx context.Context, folder string) ([]*driver.File, error) {
			return (&pan115Simulcast{app: a}).list(ctx, s, c, folder)
		},
		download: func(ctx context.Context, file *driver.File) (Download, error) {
			return a.downloadWithUA(ctx, s, file.FileID, file.PickCode, r.UserAgent())
		},
		interval: 2 * time.Second,
	}
	d, err := a.virtualShareDownload(ctx, s, pool, key, *source, ops)
	if err != nil {
		fail(w, 502, err)
		return
	}
	redirectDownload(w, r, d.URL)
}

type virtualPlaybackOps struct {
	folder   func(context.Context) (string, error)
	submit   func(context.Context, string) error
	files    func(context.Context, string) ([]*driver.File, error)
	download func(context.Context, *driver.File) (Download, error)
	interval time.Duration
}

func (a *App) virtualShareDownload(ctx context.Context, s Storage, pool ED2KPoolConfig, key string, source shareEntry, ops virtualPlaybackOps) (Download, error) {
	check := func() error {
		current, e := a.store.storage(s.ID)
		if e != nil || !current.Enabled || ed2kRevision(current) != ed2kRevision(s) || storageAuthBlocked(current, time.Now()) != nil {
			return errors.New("存储配置或健康状态已改变")
		}
		return ctx.Err()
	}
	if err := check(); err != nil {
		return Download{}, err
	}
	record, err := a.readED2KRecord(key)
	if err != nil && !os.IsNotExist(err) {
		return Download{}, err
	}
	if os.IsNotExist(err) {
		records, e := os.ReadDir(filepath.Join(a.dataDir, "ed2k-playback"))
		if e != nil && !os.IsNotExist(e) {
			return Download{}, e
		}
		if len(a.store.snapshotWithLogLimit(0).ED2KTemporary) >= 10000 || len(records) >= 10000 {
			return Download{}, errors.New("临时文件已达上限，请先清理")
		}
		folder, e := ops.folder(ctx)
		if e != nil {
			return Download{}, e
		}
		if folder == "" {
			return Download{}, errors.New("临时目录无效")
		}
		if e = check(); e != nil {
			return Download{}, e
		}
		record = ed2kRecord{Folder: folder, Attempted: true}
		if e = a.writeED2KRecord(key, record); e != nil {
			return Download{}, e
		}
		e = ops.submit(ctx, folder)
		if e != nil {
			return Download{}, errors.New("分享接收结果未确认；保留提交记录，不会自动重试")
		}
	}
	for {
		if e := check(); e != nil {
			return Download{}, e
		}
		files, e := ops.files(ctx, record.Folder)
		if e != nil {
			return Download{}, e
		}
		for _, f := range files {
			if f != nil && f.FileID != "" && (record.FileID == "" || f.FileID == record.FileID) && f.ParentID == record.Folder && !f.IsDirectory && f.Name == source.Name && f.Size == source.Size && strings.EqualFold(f.Sha1, source.SHA1) && f.PickCode != "" {
				if e = check(); e != nil {
					return Download{}, e
				}
				record.FileID = f.FileID
				if e = a.writeED2KRecord(key, record); e != nil {
					return Download{}, e
				}
				d, e := ops.download(ctx, f)
				if e != nil {
					return Download{}, e
				}
				if e = check(); e != nil {
					return Download{}, e
				}
				now := time.Now()
				temp := ED2KTemporary{Key: key, StorageID: s.ID, Revision: ed2kRevision(s), Folder: record.Folder, FileID: f.FileID, Name: f.Name, Size: f.Size, SHA1: source.SHA1, LastUsed: now, ExpiresAt: now.Add(time.Duration(pool.RetentionHours) * time.Hour)}
				if e = a.store.update(func(st *State) error {
					for j, item := range st.ED2KTemporary {
						if item.Key == key {
							st.ED2KTemporary[j] = temp
							return nil
						}
					}
					st.ED2KTemporary = append(st.ED2KTemporary, temp)
					return nil
				}); e != nil {
					return Download{}, e
				}
				return d, nil
			}
		}
		timer := time.NewTimer(ops.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Download{}, errors.New("分享接收尚未确认；不重复提交，请稍后重试")
		case <-timer.C:
		}
	}
}
