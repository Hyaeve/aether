package app

import (
	"net/http"
	"sync"
	"time"
)

type trafficBucket struct {
	tick             int64
	upload, download int64
}

// A bounded two-second window records bytes while IO is in progress.
type trafficMeter struct {
	mu      sync.Mutex
	buckets [20]trafficBucket
}

func (m *trafficMeter) add(kind string, n int, now time.Time) {
	if n <= 0 || (kind != "upload" && kind != "download") {
		return
	}
	tick := now.UnixMilli() / 100
	m.mu.Lock()
	defer m.mu.Unlock()
	b := &m.buckets[tick%20]
	if b.tick != tick {
		*b = trafficBucket{tick: tick}
	}
	if kind == "upload" {
		b.upload += int64(n)
	} else {
		b.download += int64(n)
	}
}

func (m *trafficMeter) rates(now time.Time) map[string]int64 {
	tick := now.UnixMilli() / 100
	m.mu.Lock()
	defer m.mu.Unlock()
	var up, down int64
	for _, b := range m.buckets {
		if age := tick - b.tick; age >= 0 && age < 20 {
			up += b.upload
			down += b.download
		}
	}
	return map[string]int64{"uploadRate": up / 2, "downloadRate": down / 2}
}

func (a *App) trafficRates(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, a.traffic.rates(time.Now()))
}
