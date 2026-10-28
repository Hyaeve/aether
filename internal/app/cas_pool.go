package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type CASPoolGroup struct {
	StorageIDs     []string `json:"storageIds"`
	RetentionHours int      `json:"retentionHours"`
}

type CASPoolConfig struct {
	Mobile          CASPoolGroup `json:"mobile"`
	Tianyi          CASPoolGroup `json:"tianyi"`
	Preferred       string       `json:"preferred"`
	RotationMinutes int          `json:"rotationMinutes"`
	Configured      *bool        `json:"configured,omitempty"`
}

func normalizedCASPool(p CASPoolConfig) CASPoolConfig {
	// Legacy pools have no marker. Explicit fields (including empty lists) mean
	// they were already configured; only a pristine config gets UI defaults.
	if p.Configured == nil {
		configured := p.Preferred != "" || p.RotationMinutes != 0 || p.Mobile.RetentionHours != 0 || p.Tianyi.RetentionHours != 0 || p.Mobile.StorageIDs != nil || p.Tianyi.StorageIDs != nil
		p.Configured = &configured
	}
	if p.Preferred == "" {
		p.Preferred = "mobile"
	}
	if p.RotationMinutes == 0 {
		p.RotationMinutes = 720
	}
	if p.Mobile.RetentionHours == 0 {
		p.Mobile.RetentionHours = 12
	}
	if p.Tianyi.RetentionHours == 0 {
		p.Tianyi.RetentionHours = 12
	}
	if p.Mobile.StorageIDs == nil {
		p.Mobile.StorageIDs = []string{}
	}
	if p.Tianyi.StorageIDs == nil {
		p.Tianyi.StorageIDs = []string{}
	}
	return p
}

func requiredCASPool(p CASPoolConfig, st State) CASPoolConfig {
	p = normalizedCASPool(p)
	for kind, group := range map[string]*CASPoolGroup{"mobile": &p.Mobile, "tianyi": &p.Tianyi} {
		ids := append([]string(nil), group.StorageIDs...)
		for _, s := range st.Storages {
			if s.Enabled && s.Type == kind && casStorage(s) {
				group.StorageIDs = []string{s.ID}
				for _, sid := range ids {
					if sid != s.ID {
						group.StorageIDs = append(group.StorageIDs, sid)
					}
				}
				break
			}
		}
	}
	return p
}

func casFeatureKey(info CASInfo) string {
	raw := fmt.Sprintf("%s\x00%d\x00%s\x00%s", info.Name, info.Size, strings.ToLower(info.SHA256), strings.ToLower(info.MD5))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func validateCASPool(p CASPoolConfig, st State) error {
	if p.Preferred != "mobile" && p.Preferred != "tianyi" || p.RotationMinutes < 1 || p.RotationMinutes > 1440 {
		return errors.New("CAS 优先存储或轮换间隔无效")
	}
	for kind, group := range map[string]CASPoolGroup{"mobile": p.Mobile, "tianyi": p.Tianyi} {
		if len(group.StorageIDs) > 32 || group.RetentionHours < 1 || group.RetentionHours > 8760 {
			return errors.New("CAS 绑定池最多32个存储，保留期须为1至8760小时")
		}
		seen := map[string]bool{}
		for _, sid := range group.StorageIDs {
			if seen[sid] {
				return errors.New("CAS 绑定存储不能重复")
			}
			seen[sid] = true
			found := false
			for _, s := range st.Storages {
				if s.ID == sid && s.Enabled && s.Type == kind && casStorage(s) {
					found = true
					break
				}
			}
			if !found {
				return errors.New("CAS 绑定池须选择已启用的原生移动或天翼存储")
			}
		}
	}
	return nil
}

func (a *App) casPoolSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		st := a.store.snapshotWithLogLimit(0)
		jsonResponse(w, 200, requiredCASPool(st.CASPool, st))
		return
	}
	if r.Method != "PUT" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var pool CASPoolConfig
	if !decode(w, r, &pool) {
		return
	}
	pool = normalizedCASPool(pool)
	configured := true
	pool.Configured = &configured
	if err := a.acquireCAS(r.Context()); err != nil {
		fail(w, http.StatusRequestTimeout, err)
		return
	}
	defer func() { <-a.casGate }()
	if err := a.store.update(func(st *State) error {
		pool = requiredCASPool(pool, *st)
		if err := validateCASPool(pool, *st); err != nil {
			return err
		}
		st.CASPool, st.CASPoolVersion = pool, 1
		return nil
	}); err != nil {
		fail(w, 400, err)
		return
	}
	jsonResponse(w, 200, pool)
}

// Prefer a still-live restore before time-window rotation; playback never writes
// to an account outside the explicitly configured pool.
func (a *App) selectCASStorage(info CASInfo, now time.Time) (Storage, int, error) {
	return a.selectCASStorageExcept(info, now, "")
}

func (a *App) selectCASStorageExcept(info CASInfo, now time.Time, excludedKind string) (Storage, int, error) {
	candidates, err := a.casStorageCandidates(info, now, excludedKind)
	if err != nil {
		return Storage{}, 0, err
	}
	return candidates[0].storage, candidates[0].hours, nil
}

type casStorageCandidate struct {
	storage Storage
	hours   int
}

func (a *App) casStorageCandidates(info CASInfo, now time.Time, excludedKind string) ([]casStorageCandidate, error) {
	info = normalizeCASPlayback(info)
	if err := validateCAS(info); err != nil {
		return nil, err
	}
	st := a.store.snapshotWithLogLimit(0)
	p := requiredCASPool(st.CASPool, st)
	if p.RotationMinutes < 1 || p.RotationMinutes > 1440 || (p.Preferred != "mobile" && p.Preferred != "tianyi") {
		return nil, errors.New("CAS 存储绑定池设置无效，请重新保存")
	}
	kinds := []string{p.Preferred, "tianyi"}
	if p.Preferred == "tianyi" {
		kinds[1] = "mobile"
	}
	feature := casFeatureKey(info)
	groups := map[string]CASPoolGroup{"mobile": p.Mobile, "tianyi": p.Tianyi}
	available := map[string][]Storage{}
	for _, kind := range kinds {
		if kind == excludedKind {
			continue
		}
		group := groups[kind]
		eligible := []Storage{}
		for _, sid := range group.StorageIDs {
			for _, s := range st.Storages {
				if s.ID == sid && s.Enabled && s.Type == kind && casStorage(s) && validateCASFor(s, info) == nil && storageAuthBlocked(s, now) == nil {
					eligible = append(eligible, s)
					break
				}
			}
		}
		available[kind] = eligible
	}
	candidates := []casStorageCandidate{}
	seen := map[string]bool{}
	appendStorage := func(s Storage, hours int) {
		key := s.ID
		if s.Type == "mobile" {
			if account, _, err := mobileAccount(s); err == nil {
				key = "mobile:" + account
			}
		}
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, casStorageCandidate{s, hours})
		}
	}
	// Reuse a confirmed live fallback before creating another cloud copy.
	// Provider priority applies when no reusable temporary file exists.
	for _, kind := range kinds {
		group, eligible := groups[kind], available[kind]
		for _, entry := range st.CASTemporary {
			if entry.Feature != feature || !entry.Ready || !now.Before(entry.expiresAt()) {
				continue
			}
			for _, s := range eligible {
				if s.ID == entry.StorageID {
					appendStorage(s, group.RetentionHours)
				}
			}
		}
	}
	for _, kind := range kinds {
		group, eligible := groups[kind], available[kind]
		if len(eligible) > 0 {
			index := (now.Unix() / int64(p.RotationMinutes*60)) % int64(len(eligible))
			for i := range len(eligible) {
				appendStorage(eligible[(int(index)+i)%len(eligible)], group.RetentionHours)
				if kind != "mobile" {
					break
				}
			}
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("没有可用的CAS绑定存储：移动需SHA256，天翼需MD5、分片MD5和分片大小，请检查特征与绑定池")
	}
	return candidates, nil
}
