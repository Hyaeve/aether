package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"aether/internal/linkcore/proxy"
	driver "github.com/SheltonZhu/115driver/pkg/driver"
)

// SimulcastConfig is keyed by storage ID in State and the encrypted tool snapshot.
type SimulcastConfig struct {
	Enabled        bool   `json:"enabled"`
	Directory      string `json:"directory"`
	DirectoryLabel string `json:"directoryLabel,omitempty"`
}

const (
	simulcastWindow    = 10 * time.Minute
	simulcastFiles     = 512
	simulcastClients   = 32
	simulcastTotal     = 4096
	simulcastListLimit = 10000
)

type simulcastClient struct {
	seen time.Time
	file File
	err  error
}

type pan115Simulcast struct {
	app          *App
	trustedCIDRs []string
	gate         chan struct{}
	sessions     map[[32]byte]map[[32]byte]*simulcastClient
	now          func() time.Time
}

// Keep exactly one instance per App. The cancellable gate also serializes copies
// into a shared target directory, including different source files and storages.
func newPan115Simulcast(a *App, trustedCIDRs []string) *pan115Simulcast {
	return &pan115Simulcast{app: a, trustedCIDRs: append([]string(nil), trustedCIDRs...), gate: make(chan struct{}, 1), sessions: make(map[[32]byte]map[[32]byte]*simulcastClient), now: time.Now}
}

func (a *App) pan115SimulcastSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.authenticated(r) {
		fail(w, 401, errors.New("请先登录"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		configs := a.store.snapshotWithLogLimit(0).Simulcast
		if configs == nil {
			configs = map[string]SimulcastConfig{}
		}
		jsonResponse(w, 200, configs)
	case http.MethodPut:
		var configs map[string]SimulcastConfig
		if !decode(w, r, &configs) {
			return
		}
		if configs == nil || len(configs) > 128 {
			fail(w, 400, errors.New("同播配置必须为对象，最多 128 项"))
			return
		}
		err := a.store.update(func(st *State) error {
			for storageID, cfg := range configs {
				var storage *Storage
				for i := range st.Storages {
					if st.Storages[i].ID == storageID {
						storage = &st.Storages[i]
						break
					}
				}
				if storage == nil || storage.Type != "115" || (cfg.Enabled && !storage.Enabled) {
					return errors.New("请选择可用的 115 存储")
				}
				if cfg.Directory == "/" {
					cfg.Directory = rootOf(*storage)
				}
				if !simulcastID(cfg.Directory) || len(cfg.DirectoryLabel) > 1024 {
					return errors.New("复制目录无效")
				}
				configs[storageID] = cfg
			}
			st.Simulcast = configs
			return nil
		})
		if err != nil {
			fail(w, 400, err)
			return
		}
		jsonResponse(w, 200, configs)
	default:
		w.Header().Set("Allow", "GET, PUT")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func simulcastID(value string) bool {
	if value == "" || len(value) > 20 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func simulcastHash(value any) [32]byte { b, _ := json.Marshal(value); return sha256.Sum256(b) }

// PlayFile must only run AFTER playback authorization, never from downloadWithUA,
// browser file downloads, WebDAV or FUSE. It returns a File, not a cached URL;
// callers use the unchanged downloadWithUA with that File and the request UA.
func (p *pan115Simulcast) PlayFile(r *http.Request, s Storage, source File) (File, error) {
	if s.Type != "115" || r.URL.Query().Get("download") == "1" {
		return source, nil
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return File{}, errors.New("无效的播放请求")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	select {
	case p.gate <- struct{}{}:
		defer func() { <-p.gate }()
	case <-ctx.Done():
		return File{}, ctx.Err()
	}
	st := p.app.store.snapshotWithLogLimit(0)
	cfg, ok := st.Simulcast[s.ID]
	if !ok || !cfg.Enabled {
		return source, nil
	}
	if !p.current(s, cfg) {
		return File{}, errors.New("同播存储配置已变化")
	}
	if !simulcastID(source.ID) || source.IsDir || !simulcastID(cfg.Directory) {
		return File{}, errors.New("同播文件或目录无效")
	}
	ip := proxy.ClientIP(r, p.trustedCIDRs...)
	if ip == "" || len(r.UserAgent()) > 4096 {
		return File{}, errors.New("无法识别播放客户端")
	}
	clientKey := simulcastHash([]string{ip, r.UserAgent()})
	key := simulcastHash([]any{s.ID, s.Config, cfg, source.ID})
	now, total := p.now(), 0
	for k, clients := range p.sessions {
		for c, entry := range clients {
			if now.Sub(entry.seen) >= simulcastWindow {
				delete(clients, c)
			}
		}
		if len(clients) == 0 {
			delete(p.sessions, k)
		} else {
			total += len(clients)
		}
	}
	clients := p.sessions[key]
	if entry := clients[clientKey]; entry != nil {
		entry.seen = now
		return entry.file, entry.err
	}
	if total >= simulcastTotal || len(clients) >= simulcastClients || (clients == nil && len(p.sessions) >= simulcastFiles) {
		return File{}, errors.New("同播会话已达上限，请稍后重试")
	}
	if clients == nil {
		clients = make(map[[32]byte]*simulcastClient)
		p.sessions[key] = clients
	}
	entry := &simulcastClient{seen: now, file: source}
	if len(clients) > 0 {
		// Cache failures as well: a timed-out upstream mutation must not be replayed.
		entry.file, entry.err = p.copy(ctx, s, cfg, source)
		entry.seen = p.now()
	}
	clients[clientKey] = entry
	return entry.file, entry.err
}

func (p *pan115Simulcast) current(s Storage, cfg SimulcastConfig) bool {
	st := p.app.store.snapshotWithLogLimit(0)
	if current, ok := st.Simulcast[s.ID]; !ok || current != cfg {
		return false
	}
	for _, current := range st.Storages {
		if current.ID == s.ID {
			return current.Enabled && current.Type == "115" && reflect.DeepEqual(current.Config, s.Config)
		}
	}
	return false
}

// Bounded, uncached listing keeps SHA1 and parent IDs that app.File omits.
func (p *pan115Simulcast) list(ctx context.Context, s Storage, c *driver.Pan115Client, dir string) ([]*driver.File, error) {
	var out []*driver.File
	seen := map[string]bool{}
	for offset := 0; offset < simulcastListLimit; offset += 200 {
		if err := p.app.waitAPI(ctx, s.ID); err != nil {
			return nil, err
		}
		page, err := driver.GetFiles(c.NewRequest().ForceContentType("application/json"), dir, driver.WithOffset(int64(offset)), driver.WithLimit(200))
		if err != nil {
			return nil, err
		}
		if string(page.CategoryID) != dir || page.Count < 0 || page.Count > simulcastListLimit || page.Offset != offset || len(page.Files) > 200 {
			return nil, errors.New("同播复制目录分页不一致或超过一万项")
		}
		for _, raw := range page.Files {
			f := (&driver.File{}).From(&raw)
			if f.FileID == "" || seen[f.FileID] || f.ParentID != dir {
				return nil, errors.New("同播目录结果不一致")
			}
			seen[f.FileID] = true
			out = append(out, f)
		}
		if len(out) == page.Count {
			return out, nil
		}
		if len(page.Files) != 200 || len(out) > page.Count {
			return nil, errors.New("同播目录分页中断")
		}
	}
	return nil, errors.New("同播复制目录过大")
}

func simulcastSame(source, copy *driver.File) bool {
	return !copy.IsDirectory && source.Size == copy.Size && simulcastSHA1(source.Sha1) && strings.EqualFold(source.Sha1, copy.Sha1)
}

func simulcastSHA1(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 40 && err == nil
}

func (p *pan115Simulcast) copy(ctx context.Context, s Storage, cfg SimulcastConfig, source File) (File, error) {
	c, err := client115(ctx, s)
	if err != nil {
		return File{}, err
	}
	if err = p.app.waitAPI(ctx, s.ID); err != nil {
		return File{}, err
	}
	original, err := c.GetFile(source.ID)
	if err != nil {
		return File{}, err
	}
	if original.FileID != source.ID || original.IsDirectory || original.PickCode == "" || !safeName(original.Name) || !simulcastSHA1(original.Sha1) || (source.PickCode != "" && source.PickCode != original.PickCode) {
		return File{}, errors.New("无法确认同播源文件")
	}
	before, err := p.list(ctx, s, c, cfg.Directory)
	if err != nil {
		return File{}, err
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, f := range before {
		ids[f.FileID], names[strings.ToLower(f.Name)] = true, true
	}
	// Copy cannot atomically assign a new name. Never overwrite/adopt an existing
	// same-name object, including the source itself when target equals its parent.
	if names[strings.ToLower(original.Name)] {
		return File{}, errors.New("复制目录已有同名文件，请选择独立目录或手工处理")
	}
	ext := path.Ext(original.Name)
	base := strings.TrimSuffix(original.Name, ext)
	name := ""
	for i := 1; i <= simulcastListLimit+1; i++ {
		candidate := base + " (" + strconv.Itoa(i) + ")" + ext
		if !names[strings.ToLower(candidate)] {
			name = candidate
			break
		}
	}
	if name == "" || !safeName(name) {
		return File{}, errors.New("无法分配同播副本名称")
	}
	if !p.current(s, cfg) {
		return File{}, errors.New("同播配置已变化")
	}
	if err = p.app.waitAPI(ctx, s.ID); err != nil {
		return File{}, err
	}
	defer p.app.cache.clear()
	if err = c.Copy(cfg.Directory, source.ID); err != nil {
		return File{}, fmt.Errorf("同播复制未确认，不自动重试: %w", err)
	}
	after, err := p.list(ctx, s, c, cfg.Directory)
	if err != nil {
		return File{}, err
	}
	var copied *driver.File
	newCount := 0
	for _, f := range after {
		if !ids[f.FileID] {
			newCount++
			if f.FileID != source.ID && f.Name == original.Name && f.PickCode != "" && f.PickCode != original.PickCode && simulcastSame(original, f) {
				copied = f
			}
		}
		if strings.EqualFold(f.Name, name) {
			return File{}, errors.New("副本名称已被占用，已保留云端复制结果")
		}
	}
	if newCount != 1 || copied == nil {
		return File{}, errors.New("无法唯一确认复制结果，已保留云端文件，请手工检查")
	}
	if err = p.app.waitAPI(ctx, s.ID); err != nil {
		return File{}, err
	}
	confirmed, err := c.GetFile(copied.FileID)
	if err != nil {
		return File{}, err
	}
	if confirmed.FileID != copied.FileID || confirmed.ParentID != cfg.Directory || confirmed.Name != original.Name || confirmed.PickCode != copied.PickCode || !simulcastSame(original, confirmed) {
		return File{}, errors.New("副本校验失败，未重命名")
	}
	if !p.current(s, cfg) {
		return File{}, errors.New("同播配置已变化，已保留副本")
	}
	if err = p.app.waitAPI(ctx, s.ID); err != nil {
		return File{}, err
	}
	if err = c.Rename(copied.FileID, name); err != nil {
		return File{}, err
	}
	if err = p.app.waitAPI(ctx, s.ID); err != nil {
		return File{}, err
	}
	confirmed, err = c.GetFile(copied.FileID)
	if err != nil {
		return File{}, err
	}
	if confirmed.FileID != copied.FileID || confirmed.ParentID != cfg.Directory || confirmed.Name != name || confirmed.PickCode == "" || confirmed.PickCode == original.PickCode || !simulcastSame(original, confirmed) || !p.current(s, cfg) {
		return File{}, errors.New("同播副本最终校验失败，已保留云端文件")
	}
	return File{ID: confirmed.FileID, Name: confirmed.Name, PickCode: confirmed.PickCode, Size: confirmed.Size, SizeKnown: true}, nil
}
