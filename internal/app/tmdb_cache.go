package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const tmdbCacheIdle = 7 * 24 * time.Hour
const tmdbCacheBytes = 256 << 20

func tmdbCacheScope(cfg PluginConfig) string {
	return archiveStamp(cfg.APIURL, cfg.APIKey, cfg.Language)
}

type tmdbCacheKey struct{}
type tmdbDiskCache struct {
	app        *App
	gate       chan struct{}
	now        func() time.Time
	swept      time.Time
	bytes      int64
	count      int
	maxBytes   int64
	maxEntries int
}
type tmdbDiskEntry struct {
	Scope    string          `json:"scope"`
	Endpoint string          `json:"endpoint"`
	Fetched  time.Time       `json:"fetched"`
	Accessed time.Time       `json:"accessed"`
	Data     json.RawMessage `json:"data"`
}

func (a *App) tmdbContext(ctx context.Context) context.Context {
	a.discoveryMu.Lock()
	if a.tmdbDisk == nil {
		c := &tmdbDiskCache{app: a, gate: make(chan struct{}, 1), now: time.Now, maxBytes: tmdbCacheBytes, maxEntries: 5000}
		a.tmdbDisk = c
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-a.ctx.Done():
					return
				case <-ticker.C:
					if c.lock(a.ctx) == nil {
						c.prune()
						c.unlock()
					}
				}
			}
		}()
	}
	c := a.tmdbDisk
	a.discoveryMu.Unlock()
	return context.WithValue(ctx, tmdbCacheKey{}, c)
}
func (c *tmdbDiskCache) lock(ctx context.Context) error {
	select {
	case c.gate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			c.unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *tmdbDiskCache) unlock()     { <-c.gate }
func (c *tmdbDiskCache) dir() string { return filepath.Join(c.app.dataDir, "cache", "tmdb") }
func (c *tmdbDiskCache) read(name string) (tmdbDiskEntry, error) {
	var entry tmdbDiskEntry
	f, err := os.Open(filepath.Join(c.dir(), name))
	if err != nil {
		return entry, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
	if err != nil || len(b) > 8<<20 {
		return entry, errors.New("TMDB缓存无效")
	}
	err = c.app.store.decodeJSONConfig("cache/tmdb/"+name, b, &entry)
	return entry, err
}
func (c *tmdbDiskCache) write(name string, entry tmdbDiskEntry) error {
	if err := os.MkdirAll(c.dir(), 0700); err != nil {
		return err
	}
	b, err := c.app.store.encodeJSONConfig("cache/tmdb/"+name, entry)
	if err != nil {
		return err
	}
	file := filepath.Join(c.dir(), name)
	old, statErr := os.Stat(file)
	if err := durableConfigWrite(file, b); err != nil {
		return err
	}
	if statErr == nil {
		c.bytes -= old.Size()
	} else {
		c.count++
	}
	c.bytes += int64(len(b))
	if c.bytes > c.maxBytes || c.count > c.maxEntries {
		c.prune()
	}
	return nil
}
func tmdbCacheName(name string) bool {
	if len(name) != 69 || !strings.HasSuffix(name, ".json") {
		return false
	}
	for _, r := range name[:64] {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
func (c *tmdbDiskCache) prune() {
	entries, _ := os.ReadDir(c.dir())
	type candidate struct {
		name string
		at   time.Time
		size int64
	}
	keep := []candidate{}
	var total int64
	for _, f := range entries {
		if c.app.ctx.Err() != nil {
			return
		}
		if f.IsDir() || !tmdbCacheName(f.Name()) {
			continue
		}
		v, e := c.read(f.Name())
		if e != nil || v.Accessed.IsZero() || !c.now().Before(v.Accessed.Add(tmdbCacheIdle)) {
			_ = os.Remove(filepath.Join(c.dir(), f.Name()))
			continue
		}
		info, e := f.Info()
		if e == nil {
			keep = append(keep, candidate{f.Name(), v.Accessed, info.Size()})
			total += info.Size()
		}
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].at.Before(keep[j].at) })
	for i, v := range keep {
		if total <= c.maxBytes && len(keep)-i <= c.maxEntries {
			break
		}
		if os.Remove(filepath.Join(c.dir(), v.name)) == nil {
			total -= v.size
			keep[i].size = 0
		}
	}
	c.bytes = total
	c.count = 0
	for _, v := range keep {
		if v.size > 0 {
			c.count++
		}
	}
	c.swept = c.now()
}
func (c *tmdbDiskCache) get(ctx context.Context, scope, endpoint, key string, fetch func() (json.RawMessage, error), out any) error {
	if err := c.lock(ctx); err != nil {
		return err
	}
	defer c.unlock()
	if c.swept.IsZero() || c.now().Sub(c.swept) >= time.Hour {
		c.prune()
	}
	name := key + ".json"
	if cached, err := c.read(name); err == nil && cached.Scope == scope && cached.Endpoint == endpoint && c.now().Before(cached.Accessed.Add(tmdbCacheIdle)) && c.now().Before(cached.Fetched.Add(tmdbCacheFreshness(endpoint))) && json.Unmarshal(cached.Data, out) == nil {
		cached.Accessed = c.now()
		_ = c.write(name, cached)
		return ctx.Err()
	}
	raw, err := fetch()
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return err
	}
	if len(raw) <= 4<<20 && ctx.Err() == nil {
		_ = c.write(name, tmdbDiskEntry{Scope: scope, Endpoint: endpoint, Fetched: c.now(), Accessed: c.now(), Data: raw})
	}
	return ctx.Err()
}

// Retention is based on idle time; active feeds and episode data still refresh.
func tmdbCacheFreshness(endpoint string) time.Duration {
	parts := strings.Split(endpoint, "/")
	if len(parts) >= 2 && (parts[0] == "tv" || parts[0] == "movie") {
		if id, err := strconv.Atoi(parts[1]); err == nil && id > 0 {
			if len(parts) > 2 {
				return time.Hour
			}
			return 24 * time.Hour
		}
	}
	return 15 * time.Minute
}

type cacheArchiveItem struct {
	ID       string    `json:"id"`
	Source   string    `json:"source"`
	Title    string    `json:"title"`
	Kind     string    `json:"kind"`
	TMDB     int       `json:"tmdb"`
	Poster   string    `json:"poster"`
	Release  string    `json:"release"`
	Overview string    `json:"overview"`
	Accessed time.Time `json:"accessed"`
	Expires  time.Time `json:"expires"`
	Path     string    `json:"path"`
	Files    []File    `json:"files,omitempty"`
	Count    int       `json:"count"`
}

func (a *App) cacheArchiveAPI(w http.ResponseWriter, r *http.Request) {
	c := a.tmdbContext(r.Context()).Value(tmdbCacheKey{}).(*tmdbDiskCache)
	if err := c.lock(r.Context()); err != nil {
		return
	}
	c.prune()
	st := a.store.snapshotWithLogLimit(0)
	cfg := pluginDefaults("tmdb", st.Plugins["tmdb"])
	scope := tmdbCacheScope(cfg)
	records := map[string]cacheArchiveItem{}
	files, _ := os.ReadDir(c.dir())
	for _, f := range files {
		if !tmdbCacheName(f.Name()) {
			continue
		}
		v, err := c.read(f.Name())
		if err != nil || v.Scope != scope || !cfg.Enabled {
			continue
		}
		var raw struct {
			tmdbDiscovery
			Results []tmdbDiscovery `json:"results"`
		}
		if json.Unmarshal(v.Data, &raw) != nil {
			continue
		}
		parts := strings.Split(v.Endpoint, "/")
		kind := ""
		for _, p := range parts {
			if p == "tv" || p == "movie" {
				kind = p
			}
		}
		values := raw.Results
		if len(parts) == 2 && (parts[0] == "tv" || parts[0] == "movie") && raw.ID > 0 {
			values = append(values, raw.tmdbDiscovery)
		}
		for _, m := range values {
			k := kind
			if m.MediaType != "" {
				k = m.MediaType
			}
			if (k != "movie" && k != "tv") || m.ID < 1 {
				continue
			}
			media := m.media(k)
			release := m.Date
			if k == "tv" {
				release = m.FirstDate
			}
			key := archiveStamp(k, strconv.Itoa(m.ID))
			old, exists := records[key]
			if exists && old.Accessed.After(v.Accessed) {
				continue
			}
			if media.Title == "" {
				continue
			}
			if media.Overview == "" && exists {
				media.Overview = old.Overview
			}
			if media.Poster == "" && exists {
				media.Poster = old.Poster
			}
			if release == "" && exists {
				release = old.Release
			}
			records[key] = cacheArchiveItem{ID: key, Source: "tmdb", Title: media.Title, Kind: k, TMDB: m.ID, Poster: media.Poster, Release: release, Overview: media.Overview, Accessed: v.Accessed, Expires: v.Accessed.Add(tmdbCacheIdle)}
		}
	}
	c.unlock()
	items := []cacheArchiveItem{}
	for _, v := range records {
		items = append(items, v)
	}
	a.cache.mu.Lock()
	for key, el := range a.cache.items {
		entry := el.Value.(cacheEntry)
		if !time.Now().Before(entry.Expires) {
			continue
		}
		for _, s := range st.Storages {
			prefix := s.ID + ":"
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			dir := strings.TrimPrefix(key, prefix)
			files := append([]File{}, entry.Files[:min(200, len(entry.Files))]...)
			items = append(items, cacheArchiveItem{ID: archiveStamp("directory", key), Source: "directory", Title: s.Name, Path: dir, Kind: s.Type, Count: len(entry.Files), Files: files, Expires: entry.Expires})
			break
		}
	}
	a.cache.mu.Unlock()
	search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	source := r.URL.Query().Get("source")
	filtered := []cacheArchiveItem{}
	for _, v := range items {
		if source != "" && v.Source != source {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(v.Title+" "+v.Path+" "+strconv.Itoa(v.TMDB)), search) {
			continue
		}
		filtered = append(filtered, v)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Title < filtered[j].Title })
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = max(1, page)
	pages := max(1, (len(filtered)+29)/30)
	page = min(page, pages)
	start := min(len(filtered), (page-1)*30)
	end := min(len(filtered), start+30)
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]any{"items": filtered[start:end], "total": len(filtered), "page": page, "pages": pages})
}
