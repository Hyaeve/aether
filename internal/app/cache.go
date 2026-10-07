package app

import (
	"container/list"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type cacheEntry struct {
	Key     string    `json:"key"`
	Files   []File    `json:"files"`
	Expires time.Time `json:"expires"`
	Bytes   int       `json:"bytes"`
}

type Cache struct {
	mu                 sync.Mutex
	items              map[string]*list.Element
	order              *list.List
	bytes              int
	hits, misses       uint64
	evictions, expired uint64
	generation         uint64
	calls              map[string]*cacheCall
}

type cacheCall struct {
	done  chan struct{}
	files []File
	err   error
}

func (c *Cache) begin(key string) (*cacheCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if call := c.calls[key]; call != nil {
		return call, false
	}
	if c.calls == nil {
		c.calls = map[string]*cacheCall{}
	}
	call := &cacheCall{done: make(chan struct{})}
	c.calls[key] = call
	return call, true
}

func (c *Cache) finish(key string, call *cacheCall, files []File, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	call.files, call.err = append([]File{}, files...), err
	delete(c.calls, key)
	close(call.done)
}

func NewCache() *Cache { return &Cache{items: map[string]*list.Element{}, order: list.New()} }

func (c *Cache) get(key string) ([]File, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(cacheEntry)
		if time.Now().Before(e.Expires) {
			c.order.MoveToFront(el)
			c.hits++
			return append([]File{}, e.Files...), true
		}
		c.remove(el)
		c.expired++
	}
	c.misses++
	return nil, false
}

func (c *Cache) remove(el *list.Element) {
	e := el.Value.(cacheEntry)
	delete(c.items, e.Key)
	c.bytes -= e.Bytes
	c.order.Remove(el)
}

func (c *Cache) put(key string, files []File, ttl int, cfg Settings) {
	c.putGeneration(key, files, ttl, cfg, nil)
}

func (c *Cache) revision() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generation
}

func (c *Cache) putGeneration(key string, files []File, ttl int, cfg Settings, generation *uint64) {
	if !cfg.CacheEnabled {
		return
	}
	data, _ := json.Marshal(files)
	e := cacheEntry{key, append([]File{}, files...), time.Now().Add(time.Duration(ttl) * time.Minute), len(data) + len(key) + 128}
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != nil && *generation != c.generation {
		return
	}
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	if e.Bytes > cfg.CacheMemoryMB*1024*1024 {
		return
	}
	c.items[key] = c.order.PushFront(e)
	c.bytes += e.Bytes
	c.trim(cfg)
}

func (c *Cache) trim(cfg Settings) {
	for c.order.Len() > 0 && (c.order.Len() > cfg.CacheMaxItems || c.bytes > cfg.CacheMemoryMB*1024*1024) {
		c.remove(c.order.Back())
		c.evictions++
	}
}

func (c *Cache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = map[string]*list.Element{}
	c.order.Init()
	c.bytes = 0
	c.generation++
}

func (c *Cache) stats() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.order.Back(); el != nil; {
		prev := el.Prev()
		if time.Now().After(el.Value.(cacheEntry).Expires) {
			c.remove(el)
			c.expired++
		}
		el = prev
	}
	return map[string]any{"entries": c.order.Len(), "bytes": c.bytes, "hits": c.hits, "misses": c.misses, "evictions": c.evictions, "expired": c.expired}
}

func (c *Cache) persist(dir string) error {
	c.mu.Lock()
	entries := []cacheEntry{}
	for el := c.order.Front(); el != nil; el = el.Next() {
		e := el.Value.(cacheEntry)
		if time.Now().Before(e.Expires) {
			entries = append(entries, e)
		}
	}
	c.mu.Unlock()
	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "cache.json"), data)
}

func (c *Cache) restore(dir string, cfg Settings) {
	if !cfg.CachePersist || !cfg.CacheEnabled {
		return
	}
	f, err := os.Open(filepath.Join(dir, "cache.json"))
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > int64(cfg.CacheMemoryMB)*1024*1024*2 {
		return
	}
	var entries []cacheEntry
	if json.NewDecoder(f).Decode(&entries) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range entries {
		if time.Now().After(e.Expires) {
			continue
		}
		if _, exists := c.items[e.Key]; exists {
			continue
		}
		b, _ := json.Marshal(e.Files)
		e.Bytes = len(b) + len(e.Key) + 128
		c.items[e.Key] = c.order.PushBack(e)
		c.bytes += e.Bytes
	}
	c.trim(cfg)
}
