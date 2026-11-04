package app

import (
	"context"
	"net/http"
	"time"
)

type storageMembershipEntry struct {
	stamp [32]byte
	ready chan struct{}
	until time.Time
	value *storageMembership
}

// Membership is independent of quota latency, but shares in-flight lookups with it.
func (a *App) cachedStorageMembership(ctx context.Context, s Storage) (*storageMembership, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stamp := usageStamp(s)
	a.usageMu.Lock()
	if a.membershipCache == nil {
		a.membershipCache = map[string]*storageMembershipEntry{}
	}
	entry := a.membershipCache[s.ID]
	if entry != nil && entry.stamp == stamp {
		pending := false
		select {
		case <-entry.ready:
		default:
			pending = true
		}
		if pending || time.Now().Before(entry.until) {
			a.usageMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-entry.ready:
				return freshMembership(entry.value), nil
			}
		}
	}
	entry = &storageMembershipEntry{stamp: stamp, ready: make(chan struct{})}
	a.membershipCache[s.ID] = entry
	for key := range a.membershipCache {
		if _, err := a.store.storage(key); err != nil {
			delete(a.membershipCache, key)
		}
	}
	a.usageMu.Unlock()
	// The lookup belongs to this credential revision, not to the first page that
	// happened to request it. Navigation must not cancel every coalesced waiter.
	lookup, cancel := context.WithTimeout(a.ctx, 12*time.Second)
	value := a.readStorageMembership(lookup, s)
	lookupErr := lookup.Err()
	cancel()
	if lookupErr == nil {
		_ = a.store.update(func(st *State) error {
			for i := range st.Storages {
				current := &st.Storages[i]
				if current.ID != s.ID || !current.Enabled || usageStamp(*current) != stamp {
					continue
				}
				if current.Usage == nil || current.Usage.Stamp != storageRevision(*current) {
					current.Usage = &storageUsageState{Stamp: storageRevision(*current)}
				}
				if value != nil || displayMembership(current.Usage.Membership) == nil {
					current.Usage.Membership = value
				}
			}
			return nil
		})
	}
	a.usageMu.Lock()
	entry.value = value
	entry.until = time.Now().Add(time.Minute)
	if value != nil {
		entry.until = time.Now().Add(5 * time.Minute)
	}
	if lookupErr != nil {
		entry.until = time.Time{}
	}
	close(entry.ready)
	a.usageMu.Unlock()
	return value, ctx.Err()
}

func (a *App) storageMembershipAPI(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.PathValue("id"))
	if err != nil || !s.Enabled {
		http.NotFound(w, r)
		return
	}
	var value *storageMembership
	if cloudUsageType(s.Type) {
		if saved, ok := savedStorageUsage(s); ok {
			value = saved.Membership
		}
		if value == nil && storageAuthBlocked(s, time.Now()) == nil {
			value, err = a.cachedStorageMembership(r.Context(), s)
			if err != nil {
				return
			}
		}
	}
	current, err := a.store.storage(s.ID)
	if err != nil || !current.Enabled || usageStamp(current) != usageStamp(s) {
		jsonResponse(w, 409, map[string]string{"error": "存储配置已更改，请刷新"})
		return
	}
	if value == nil && current.Usage != nil && current.Usage.Stamp == storageRevision(current) {
		value = displayMembership(current.Usage.Membership)
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, struct {
		Membership *storageMembership `json:"membership"`
	}{value})
}
