package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type davReadClientKey struct{}

var errDAVPartialRead = errors.New("按需分段读取结束，尚未读取整个文件")

const davReadSessionIdle = 2 * time.Minute

type davReadSpan struct{ start, end int64 }
type davReadSession struct {
	mu          sync.Mutex
	linkMu      sync.Mutex
	progress    *transferProgress
	spans       []davReadSpan
	covered     int64
	total       int64
	refs        int
	last        time.Time
	timer       *time.Timer
	stop        func() bool
	failure     error
	finished    bool
	download    Download
	linkAt      time.Time
	refreshedAt time.Time
}

// A client may open many HTTP ranges for one download. Keep identity and
// coverage separate from handle lifetime; a closed range is not a full file.
func (a *App) attachDAVRead(ctx context.Context, s Storage, f File) *davReadSession {
	client, _ := ctx.Value(davReadClientKey{}).(string)
	if client == "" {
		return nil
	}
	key := fmt.Sprintf("%s:%s:%x:%s:%d:%d", client, s.ID, shareConfig(s), f.ID, f.Size, f.Modified.UnixNano())
	a.davReadMu.Lock()
	defer a.davReadMu.Unlock()
	if a.davReadClosed || a.ctx.Err() != nil {
		return nil
	}
	if a.davReads == nil {
		a.davReads = map[string]*davReadSession{}
	}
	now := time.Now()
	session := a.davReads[key]
	if session != nil {
		session.mu.Lock()
		expired := session.finished && session.refs == 0 && now.Sub(session.last) > 30*time.Second
		session.mu.Unlock()
		if expired {
			delete(a.davReads, key)
			session = nil
		}
	}
	if session == nil {
		for k, old := range a.davReads {
			old.mu.Lock()
			if old.finished && old.refs == 0 && now.Sub(old.last) > davReadSessionIdle {
				delete(a.davReads, k)
			}
			old.mu.Unlock()
		}
		if len(a.davReads) >= 256 {
			return nil
		}
		base := context.WithValue(a.ctx, transferSourceKey{}, "WebDAV")
		base = context.WithValue(base, davReadClientKey{}, client)
		session = &davReadSession{progress: a.beginTransfer(base, "download", s, f.Name, f.Size,
			transferLocations{From: a.transferFileLocation(s, f.ID), To: transferClientLocation(ctx)}), last: now, total: f.Size}
		a.davReads[key] = session
		session.mu.Lock()
		session.stop = context.AfterFunc(a.ctx, session.shutdown)
		session.mu.Unlock()
	}
	session.mu.Lock()
	session.refs++
	if !session.finished {
		session.last = now
	}
	if session.timer != nil {
		session.timer.Stop()
	}
	session.mu.Unlock()
	return session
}

func (a *App) closeDAVReads() {
	a.davReadMu.Lock()
	defer a.davReadMu.Unlock()
	a.davReadClosed = true
	for _, session := range a.davReads {
		session.shutdown()
	}
	a.davReads = nil
}

func (s *davReadSession) shutdown() {
	// Serialize with in-flight finalizers so no callback can persist after
	// shutdown returns, even when stopping an already-started callback fails.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.stop != nil {
		s.stop()
	}
	if !s.finished {
		s.finished, s.failure, s.last = true, context.Canceled, time.Now()
		s.progress.finish(context.Canceled)
	}
}

func (s *davReadSession) add(offset int64, n int) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	start, end := max(0, offset), min(s.total, offset+int64(n))
	if end <= start {
		return
	}
	spans := make([]davReadSpan, 0, len(s.spans)+1)
	for _, span := range s.spans {
		if span.end < start || span.start > end {
			spans = append(spans, span)
			continue
		}
		start, end = min(start, span.start), max(end, span.end)
	}
	if len(spans) >= 1024 {
		return
	}
	spans = append(spans, davReadSpan{start, end})
	covered := int64(0)
	for _, span := range spans {
		covered += span.end - span.start
	}
	delta := covered - s.covered
	s.spans, s.covered, s.last = spans, covered, time.Now()
	s.progress.add(int(delta))
}

func (s *davReadSession) fail(err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return
	}
	s.failure, s.finished, s.last = err, true, time.Now()
	if s.stop != nil {
		s.stop()
	}
	s.progress.finish(err)
}

func (s *davReadSession) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refs--
	if s.finished {
		return
	}
	s.last = time.Now()
	if s.refs > 0 {
		return
	}
	if s.total > 0 && s.covered >= s.total {
		s.finished = true
		if s.stop != nil {
			s.stop()
		}
		s.progress.finish(nil)
		return
	}
	s.timer = time.AfterFunc(davReadSessionIdle, s.expireIdle)
}

func (s *davReadSession) expireIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refs == 0 && !s.finished && time.Since(s.last) >= davReadSessionIdle {
		s.finished, s.last = true, time.Now()
		if s.stop != nil {
			s.stop()
		}
		s.progress.finish(errDAVPartialRead)
	}
}

func (s *davReadSession) link(ctx context.Context, a *App, pool Storage, f File, refresh bool) (Download, error) {
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	s.mu.Lock()
	failure := s.failure
	s.mu.Unlock()
	if failure != nil {
		return Download{}, failure
	}
	if s.download.URL != "" || s.download.Local != "" {
		if !refresh && time.Since(s.linkAt) < 30*time.Second || refresh && time.Since(s.refreshedAt) < 30*time.Second {
			return s.download, nil
		}
	}
	var d Download
	var err error
	if refresh {
		info, e := download115API(ctx, pool, f.PickCode, pan115ReadUA, true)
		err = e
		if err == nil {
			d = Download{URL: info.Url.Url, Headers: info.Header}
		}
		s.refreshedAt = time.Now()
	} else {
		d, err = a.download(ctx, pool, f.ID, f.PickCode)
	}
	if err == nil {
		s.download, s.linkAt = d, time.Now()
	}
	return d, err
}

// Redirected 115 tickets may use another official CDN host. Rebind only known
// login fields over HTTPS within the official allowlist, never arbitrary hosts.
func redirect115ReadCookies(next *http.Request, via []*http.Request) {
	if len(via) == 0 || !official115DownloadURL(via[0].URL) || !official115DownloadURL(next.URL) {
		return
	}
	for _, previous := range via {
		if !official115DownloadURL(previous.URL) {
			return
		}
	}
	for _, cookie := range via[0].Cookies() {
		switch cookie.Name {
		case "UID", "CID", "SEID", "KID":
			next.AddCookie(cookie)
		}
	}
}
