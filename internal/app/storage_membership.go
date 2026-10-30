package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type storageMembership struct {
	Tier      string     `json:"tier"`
	Label     string     `json:"label"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CheckedAt time.Time  `json:"checkedAt"`
}

func membership(tier, label string, expires time.Time) *storageMembership {
	if !expires.IsZero() && !expires.After(time.Now()) {
		return nil
	}
	value := &storageMembership{Tier: tier, Label: label, CheckedAt: time.Now()}
	if !expires.IsZero() {
		value.ExpiresAt = &expires
	}
	return value
}
func membershipTime(raw json.RawMessage) time.Time {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "20060102150405", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, text, time.FixedZone("CST", 8*3600)); err == nil {
			return t
		}
	}
	return time.Time{}
}
func (a *App) readStorageMembership(ctx context.Context, s Storage) *storageMembership {
	switch s.Type {
	case "quark":
		return a.readQuarkMember(ctx, s).Membership
	case "115":
		c, err := client115(ctx, s)
		if err != nil || a.waitAPI(ctx, s.ID) != nil {
			return nil
		}
		var base struct {
			State bool `json:"state"`
			Data  struct {
				UserID json.Number `json:"user_id"`
				VIP    struct {
					Name string `json:"level_name"`
				} `json:"vip_info"`
			} `json:"data"`
		}
		res, baseErr := c.NewRequest().ForceContentType("application/json").SetResult(&base).Get("https://passportapi.115.com/app/1.0/web/26.0/user/base_info")
		if baseErr == nil && res.StatusCode() == 200 && base.State {
			uid, _ := base.Data.UserID.Int64()
			if uid == c.UserID {
				name := base.Data.VIP.Name
				switch {
				case strings.Contains(name, "永久"):
					return membership("permanent", "永久VIP", time.Time{})
				case strings.Contains(name, "年费"):
					return membership("annual", "年费VIP", time.Time{})
				}
			}
		}
		if a.waitAPI(ctx, s.ID) != nil {
			return nil
		}
		user, err := c.GetUser()
		if err != nil || user == nil {
			return nil
		}
		if user.Forever > 0 {
			return membership("permanent", "永久VIP", time.Time{})
		}
		if user.Vip > 0 && int64(user.Expire) > time.Now().Unix() {
			return membership("vip", "VIP", time.Unix(int64(user.Expire), 0))
		}
	case "mobile":
		if !nativeMobile(s) {
			return nil
		}
		var data struct {
			Members []struct {
				Name     string `json:"memberLvName"`
				Policies []struct {
					Name string `json:"prodName"`
				} `json:"chargingPolicyList"`
			} `json:"userSubMemberList"`
		}
		if a.mobilePost(ctx, s, "https://yun.139.com/orchestration/group-rebuild/member/v1.0/queryUserBenefits", map[string]int{"isNeedBenefit": 0}, false, &data) != nil {
			return nil
		}
		var best *storageMembership
		rank := 0
		for _, m := range data.Members {
			names := []string{m.Name}
			for _, p := range m.Policies {
				names = append(names, p.Name)
			}
			for _, name := range names {
				for i, tier := range []struct{ name, tier string }{{"白银会员", "silver"}, {"黄金会员", "gold"}, {"钻石会员", "diamond"}} {
					if strings.Contains(name, tier.name) && i+1 > rank {
						rank = i + 1
						best = membership(tier.tier, tier.name, time.Time{})
					}
				}
			}
		}
		return best
	case "tianyi":
		var data struct {
			Data struct {
				Members []struct {
					Level int             `json:"memberLevel"`
					End   json.RawMessage `json:"endTime"`
				} `json:"vipInfoList"`
			} `json:"data"`
		}
		if a.tianyiRequest(ctx, s, "GET", tianyiAPI+"/order/queryUserLogo.action", nil, &data) != nil {
			return nil
		}
		var best *storageMembership
		rank := 0
		for _, m := range data.Data.Members {
			exp := membershipTime(m.End)
			if exp.IsZero() || !exp.After(time.Now()) || m.Level <= rank {
				continue
			}
			switch m.Level {
			case 1000:
				best = membership("gold", "黄金会员", exp)
				rank = m.Level
			case 2000:
				best = membership("platinum", "铂金会员", exp)
				rank = m.Level
			}
		}
		return best
	}
	return nil
}

func quarkMembership(memberType string) *storageMembership {
	for _, tier := range []struct{ upstream, tier, label string }{
		{"VIP", "vip", "VIP"}, {"SUPER_VIP", "svip", "SVIP"},
		{"Z_VIP", "svip-plus", "SVIP+"}, {"EXP_SVIP", "88vip", "88VIP"},
		{"MINI_VIP", "mini", "迷你VIP"},
	} {
		if strings.ToUpper(strings.TrimSpace(memberType)) == tier.upstream {
			return membership(tier.tier, tier.label, time.Time{})
		}
	}
	return nil
}

// The PC member response carries both quota and the explicit current tier.
func (a *App) readQuarkMember(ctx context.Context, s Storage) storageUsage {
	var result struct {
		Code   int `json:"code"`
		Status int `json:"status"`
		Data   struct {
			Total      int64  `json:"total_capacity"`
			Used       int64  `json:"use_capacity"`
			MemberType string `json:"member_type"`
		} `json:"data"`
	}
	if a.waitAPI(ctx, s.ID) != nil || quarkTVJSON(ctx, "GET", "https://drive-pc.quark.cn/1/clouddrive/member?pr=ucpro&fr=pc&fetch_subscribe=false&_ch=home&fetch_identity=false", cloudHeaders(s), nil, &result) != nil || result.Code != 0 || result.Status != 200 {
		return storageUsage{}
	}
	return storageUsage{Total: result.Data.Total, Used: result.Data.Used, Membership: quarkMembership(result.Data.MemberType)}
}
func freshMembership(m *storageMembership) *storageMembership {
	if m == nil || m.CheckedAt.IsZero() || time.Since(m.CheckedAt) > 5*time.Minute || (m.ExpiresAt != nil && !m.ExpiresAt.After(time.Now())) {
		return nil
	}
	return m
}
