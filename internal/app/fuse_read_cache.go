package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const fuseBlockSize = 4 << 20

type fuseCacheEntry struct {
	size int64
	used time.Time
}

// Cache files are disposable: discard previous-process blocks to avoid serving
// stale cloud data after mutations made while Aether was stopped.
type fuseReadCache struct {
	mu      sync.Mutex
	once    sync.Once
	dir     string
	limit   int64
	ttl     time.Duration
	entries map[string]fuseCacheEntry
	bytes   int64
	ready   bool
	stripes [16]chan struct{}
}

func newFuseReadCache(dir string) *fuseReadCache {
	c := &fuseReadCache{dir: dir, limit: 10 << 30, ttl: 7 * 24 * time.Hour, entries: map[string]fuseCacheEntry{}}
	for i := range c.stripes {
		c.stripes[i] = make(chan struct{}, 1)
	}
	return c
}

func (c *fuseReadCache) initialize() {
	if os.MkdirAll(c.dir, 0700) != nil {
		return
	}
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		return
	}
	defer f.Close()
	for {
		entries, err := f.ReadDir(256)
		for _, entry := range entries {
			name := entry.Name()
			if len(name) == 68 && strings.HasSuffix(name, ".blk") {
				if _, e := hex.DecodeString(name[:64]); e == nil {
					if root.Remove(name) != nil {
						return
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
	}
	c.ready = true
}

func (c *fuseReadCache) block(ctx context.Context, key string, offset int64, size int, fetch func([]byte, int64) (int, error)) ([]byte, error) {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", key, offset)))
	name := hex.EncodeToString(sum[:]) + ".blk"
	gate := c.stripes[int(sum[0])%len(c.stripes)]
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.once.Do(c.initialize)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.ready {
		c.mu.Lock()
		if entry, ok := c.entries[name]; ok && time.Since(entry.used) < c.ttl {
			root, err := os.OpenRoot(c.dir)
			if err == nil {
				f, e := root.Open(name)
				if e == nil {
					b, e := io.ReadAll(io.LimitReader(f, int64(size)+33))
					f.Close()
					if e == nil && len(b) == size+32 {
						digest := sha256.Sum256(b[32:])
						if string(digest[:]) == string(b[:32]) {
							entry.used = time.Now()
							c.entries[name] = entry
							root.Close()
							c.mu.Unlock()
							return b[32:], nil
						}
					}
				}
				root.Close()
			}
		}
		c.mu.Unlock()
	}
	b := make([]byte, size)
	n, err := fetch(b, offset)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	if n != size {
		return b[:n], io.ErrUnexpectedEOF
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if c.ready {
		c.save(name, b)
	}
	return b, nil
}

func (c *fuseReadCache) save(name string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return
	}
	defer root.Close()
	remove := func(key string) bool {
		if err := root.Remove(key); err != nil && !os.IsNotExist(err) {
			return false
		}
		c.bytes -= c.entries[key].size
		delete(c.entries, key)
		return true
	}
	if _, ok := c.entries[name]; ok && !remove(name) {
		return
	}
	for key, entry := range c.entries {
		if time.Since(entry.used) >= c.ttl && !remove(key) {
			return
		}
	}
	need := int64(len(b) + 32)
	for c.bytes+need > c.limit || len(c.entries) >= 65536 {
		oldest := ""
		var used time.Time
		for key, entry := range c.entries {
			if oldest == "" || entry.used.Before(used) {
				oldest, used = key, entry.used
			}
		}
		if oldest == "" || !remove(oldest) {
			return
		}
	}
	// Exclusive create refuses pre-existing files and symlinks. Readers are
	// coordinated by stripe; publish the index only after the complete write.
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	digest := sha256.Sum256(b)
	_, err = f.Write(digest[:])
	if err == nil {
		_, err = f.Write(b)
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		root.Remove(name)
		return
	}
	c.entries[name] = fuseCacheEntry{size: need, used: time.Now()}
	c.bytes += need
}

func (c *fuseReadCache) readAt(ctx context.Context, key string, size int64, dest []byte, off int64, fetch func([]byte, int64) (int, error)) (int, error) {
	if off < 0 {
		return 0, os.ErrInvalid
	}
	if len(dest) == 0 {
		return 0, nil
	}
	if off >= size {
		return 0, io.EOF
	}
	written := 0
	for written < len(dest) && off+int64(written) < size {
		position := off + int64(written)
		start := position / fuseBlockSize * fuseBlockSize
		b, err := c.block(ctx, key, start, int(min(int64(fuseBlockSize), size-start)), fetch)
		if err != nil {
			return written, err
		}
		written += copy(dest[written:], b[position-start:])
	}
	if written < len(dest) {
		return written, io.EOF
	}
	return written, nil
}

func (a *App) fuseCache() *fuseReadCache {
	a.fuseReadOnce.Do(func() { a.fuseReads = newFuseReadCache(filepath.Join(a.dataDir, "fuse_read_cache")) })
	return a.fuseReads
}
