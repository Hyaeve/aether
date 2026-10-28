package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const mobileOrdinaryCASLimit int64 = 5_000_000_000

type mobileCASMembership struct {
	stamp   [32]byte
	tier    string
	expires time.Time
}

func mobileCASTierLimit(s Storage, tier string) int64 {
	switch tier {
	case "silver":
		return 8_000_000_000
	case "gold":
		return 20_000_000_000
	case "diamond":
		if mobilePCClient(s) {
			return 500_000_000_000
		}
		return 50_000_000_000
	}
	return mobileOrdinaryCASLimit
}

func (a *App) checkMobileCASSize(ctx context.Context, s Storage, size int64) error {
	if size <= mobileOrdinaryCASLimit {
		return nil
	}
	tier := ""
	known := false
	if usage, ok := savedStorageUsage(s); ok && usage.Membership != nil {
		tier, known = usage.Membership.Tier, true
	}
	stamp := usageStamp(s)
	a.usageMu.Lock()
	cached := a.mobileCASMembers[s.ID]
	a.usageMu.Unlock()
	if !known && cached.stamp == stamp && time.Now().Before(cached.expires) {
		tier, known = cached.tier, true
	}
	if !known {
		var result struct {
			Members *[]struct {
				Name     string `json:"memberLvName"`
				Policies []struct {
					Name string `json:"prodName"`
				} `json:"chargingPolicyList"`
			} `json:"userSubMemberList"`
		}
		if err := a.mobilePost(ctx, s, "https://yun.139.com/orchestration/group-rebuild/member/v1.0/queryUserBenefits", map[string]int{"isNeedBenefit": 0}, false, &result); err != nil || result.Members == nil {
			return errors.New("无法确认移动会员单文件额度，未对超过5 GB的文件提交秒传")
		}
		// Only explicit names establish paid entitlement. Unknown names are not
		// evidence of a higher tier; an empty, valid list means ordinary.
		rank := 0
		for _, member := range *result.Members {
			names := []string{member.Name}
			for _, policy := range member.Policies {
				names = append(names, policy.Name)
			}
			for _, name := range names {
				for i, value := range []struct{ name, tier string }{{"白银会员", "silver"}, {"黄金会员", "gold"}, {"钻石会员", "diamond"}} {
					if strings.Contains(name, value.name) && i+1 > rank {
						tier, rank = value.tier, i+1
					}
				}
			}
		}
		a.usageMu.Lock()
		if a.mobileCASMembers == nil {
			a.mobileCASMembers = map[string]mobileCASMembership{}
		}
		if len(a.mobileCASMembers) >= 64 {
			clear(a.mobileCASMembers)
		}
		a.mobileCASMembers[s.ID] = mobileCASMembership{stamp, tier, time.Now().Add(5 * time.Minute)}
		a.usageMu.Unlock()
	}
	limit := mobileCASTierLimit(s, tier)
	if size > limit {
		return fmt.Errorf("文件大小%d字节超过当前移动会员单文件额度%d GB", size, limit/1_000_000_000)
	}
	return ctx.Err()
}
