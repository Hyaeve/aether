package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type storageUsage struct {
	Used     int64  `json:"used"`
	Total    int64  `json:"total"`
	Username string `json:"username,omitempty"`
}

type storageUsageState struct {
	storageUsage
	Stamp       string    `json:"stamp"`
	UpdatedAt   time.Time `json:"updatedAt"`
	AttemptedAt time.Time `json:"attemptedAt"`
}

func savedStorageUsage(s Storage) (storageUsage, bool) {
	if s.Usage != nil && s.Usage.Stamp == storageRevision(s) {
		return s.Usage.storageUsage, true
	}
	return storageUsage{}, false
}

type storageUsageEntry struct {
	stamp   [32]byte
	ready   chan struct{}
	expires time.Time
	value   storageUsage
}

func usageStamp(s Storage) [32]byte {
	raw, _ := json.Marshal(struct {
		Type   string
		Config map[string]string
	}{s.Type, s.Config})
	return sha256.Sum256(raw)
}

func cloudUsageType(kind string) bool {
	return kind == "115" || kind == "quark" || kind == "mobile" || kind == "tianyi"
}

func usableStorageQuota(value storageUsage) bool {
	return value.Total > 0 && value.Used >= 0 && value.Used <= value.Total
}

func (a *App) storageUsageAPI(w http.ResponseWriter, r *http.Request) {
	s, err := a.store.storage(r.PathValue("id"))
	if err != nil || !s.Enabled {
		http.NotFound(w, r)
		return
	}
	if !cloudUsageType(s.Type) {
		jsonResponse(w, 200, storageUsage{})
		return
	}
	value, saved := savedStorageUsage(s)
	if !saved && storageAuthBlocked(s, time.Now()) == nil {
		value, err = a.cachedStorageUsage(r.Context(), s)
	}
	if err != nil {
		return
	}
	current, err := a.store.storage(s.ID)
	if err != nil || !current.Enabled || usageStamp(current) != usageStamp(s) {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "存储配置已更改，请刷新"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, value)
}

func (a *App) cachedStorageUsage(ctx context.Context, s Storage) (storageUsage, error) {
	if err := ctx.Err(); err != nil {
		return storageUsage{}, err
	}
	stamp := usageStamp(s)
	a.usageMu.Lock()
	if a.usageCache == nil {
		a.usageCache = make(map[string]*storageUsageEntry)
	}
	entry := a.usageCache[s.ID]
	if entry != nil && entry.stamp == stamp {
		inFlight := false
		select {
		case <-entry.ready:
		default:
			inFlight = true
		}
		if inFlight || time.Now().Before(entry.expires) {
			a.usageMu.Unlock()
			select {
			case <-ctx.Done():
				return storageUsage{}, ctx.Err()
			case <-entry.ready:
				return entry.value, nil
			}
		}
	}
	// A single in-flight lookup per credential revision; no network work in /state.
	entry = &storageUsageEntry{stamp: stamp, ready: make(chan struct{}), expires: time.Now().Add(20 * time.Second)}
	a.usageCache[s.ID] = entry
	for key := range a.usageCache {
		if _, err := a.store.storage(key); err != nil {
			delete(a.usageCache, key)
		}
	}
	a.usageMu.Unlock()
	lookup, cancel := context.WithTimeout(ctx, 12*time.Second)
	value := a.readStorageUsage(lookup, s)
	cancel()
	if !usableStorageQuota(value) {
		value.Used, value.Total = 0, 0
	}
	value.Username = strings.TrimSpace(value.Username)
	previous, hasPrevious := savedStorageUsage(s)
	validQuota := usableStorageQuota(value)
	if hasPrevious && !validQuota {
		value = previous
	}
	if ctx.Err() == nil && (usableStorageQuota(value) || value.Username != "") {
		_ = a.store.update(func(st *State) error {
			for i := range st.Storages {
				current := &st.Storages[i]
				if current.ID == s.ID && current.Enabled && usageStamp(*current) == stamp {
					updated := time.Now()
					if hasPrevious && !validQuota {
						updated = s.Usage.UpdatedAt
					}
					current.Usage = &storageUsageState{storageUsage: value, Stamp: storageRevision(s), UpdatedAt: updated, AttemptedAt: time.Now()}
				}
			}
			return nil
		})
	}
	a.usageMu.Lock()
	entry.value = value
	ttl := time.Minute
	if value.Total > 0 {
		ttl = 5 * time.Minute
	}
	entry.expires = time.Now().Add(ttl)
	if ctx.Err() != nil {
		entry.expires = time.Time{}
	}
	close(entry.ready)
	a.usageMu.Unlock()
	return value, ctx.Err()
}

func (a *App) readStorageUsage(ctx context.Context, s Storage) storageUsage {
	value := storageUsage{}
	switch s.Type {
	case "115":
		c, err := client115(ctx, s)
		if err != nil || a.waitAPI(ctx, s.ID) != nil {
			return value
		}
		if info, err := c.GetInfo(); err == nil {
			value.Total, value.Used = info.SpaceInfo.AllTotal.Size, info.SpaceInfo.AllUse.Size
		}
		if !usableStorageQuota(value) && ctx.Err() == nil && a.waitAPI(ctx, s.ID) == nil {
			if user, err := c.GetUser(); err == nil && user != nil {
				value.Username = user.UserName
			}
		}
	case "quark":
		var member struct {
			Code   int `json:"code"`
			Status int `json:"status"`
			Data   struct {
				Total int64 `json:"total_capacity"`
				Used  int64 `json:"use_capacity"`
			} `json:"data"`
		}
		if a.waitAPI(ctx, s.ID) == nil && quarkTVJSON(ctx, "GET", "https://drive.quark.cn/1/clouddrive/member?pr=ucpro&fr=pc&fetch_subscribe=false&_ch=home&fetch_identity=false", cloudHeaders(s), nil, &member) == nil && member.Code == 0 && member.Status == 200 {
			value.Total, value.Used = member.Data.Total, member.Data.Used
		}
		if !usableStorageQuota(value) && ctx.Err() == nil && a.waitAPI(ctx, s.ID) == nil {
			var user struct {
				Success bool `json:"success"`
				Data    struct {
					Nickname string `json:"nickname"`
				} `json:"data"`
			}
			if quarkTVJSON(ctx, "GET", "https://pan.quark.cn/account/info?platform=pc&fr=pc", cloudHeaders(s), nil, &user) == nil && user.Success {
				value.Username = user.Data.Nickname
			}
		}
	case "tianyi":
		value.Username = s.Config["username"]
		var capacity struct {
			Cloud struct {
				Total int64 `json:"totalSize"`
				Used  int64 `json:"usedSize"`
			} `json:"cloudCapacityInfo"`
		}
		if a.tianyiRequest(ctx, s, "GET", tianyiAPI+"/portal/getUserSizeInfo.action", nil, &capacity) == nil {
			value.Total, value.Used = capacity.Cloud.Total, capacity.Cloud.Used
		}
		if !usableStorageQuota(value) && value.Username == "" && ctx.Err() == nil {
			var user struct {
				Nickname  string `json:"nickName"`
				LoginName string `json:"loginName"`
			}
			if a.tianyiRequest(ctx, s, "GET", tianyiAPI+"/getUserInfo.action", nil, &user) == nil {
				value.Username = user.Nickname
				if value.Username == "" {
					value.Username = user.LoginName
				}
			}
		}
	case "mobile":
		value.Username, _, _ = mobileAccount(s)
		if nativeMobile(s) && s.Config["userDomainId"] != "" {
			var quota struct {
				Total json.Number `json:"diskSize"`
				Free  json.Number `json:"freeDiskSize"`
			}
			if a.mobilePost(ctx, s, "https://user-njs.yun.139.com/user/disk/quota/detail", map[string]string{"userDomainId": s.Config["userDomainId"]}, false, &quota) == nil {
				total, e1 := quota.Total.Int64()
				free, e2 := quota.Free.Int64()
				if e1 == nil && e2 == nil && total > 0 && free >= 0 && free <= total && total <= int64(^uint64(0)>>1)/(1<<20) {
					value.Total, value.Used = total*(1<<20), (total-free)*(1<<20)
				}
			}
		}
	}
	return value
}
