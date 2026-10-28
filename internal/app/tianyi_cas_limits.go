package app

import (
	"context"
	"errors"
	"time"
)

const tianyiGoldCASLimit int64 = 200_000_000_000

func (a *App) checkTianyiCASSize(ctx context.Context, s Storage, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Smaller files retain the existing upstream entitlement check. Do not
	// invent an ordinary-user limit from the paid membership thresholds.
	if size <= tianyiGoldCASLimit {
		return nil
	}
	var member *storageMembership
	if usage, ok := savedStorageUsage(s); ok {
		member = usage.Membership
	}
	if member == nil {
		member = a.readStorageMembership(ctx, s)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if member != nil && member.Tier == "platinum" && freshMembership(member) != nil {
		return nil
	}
	if member != nil && member.Tier == "gold" && (member.ExpiresAt == nil || member.ExpiresAt.After(time.Now())) {
		return errors.New("天翼黄金会员单文件秒传上限200 GB，未提交秒传")
	}
	return errors.New("未确认天翼铂金会员，未对超过200 GB的文件提交秒传")
}
