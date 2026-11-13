package stats

import (
	"time"

	"s2astats/internal/store"
)

// 展示状态取值，与 sub2api 后台账号列表的状态标签一致。
const (
	// displayStatusActive 账号可调度。
	displayStatusActive = "active"
	// displayStatusRateLimited 命中 429，等待 rate_limit_reset_at 恢复。
	displayStatusRateLimited = "rate_limited"
	// displayStatusOverloaded 命中 529，等待 overload_until 恢复。
	displayStatusOverloaded = "overloaded"
	// displayStatusTempUnschedulable 命中临时不可调度规则，等待 temp_unschedulable_until 恢复。
	displayStatusTempUnschedulable = "temp_unschedulable"
	// displayStatusPaused 状态正常但被手工暂停调度（schedulable = false）。
	displayStatusPaused = "paused"
)

// deriveStatus 归纳账号的展示状态与解除时刻，判定次序与 sub2api 后台一致：
// 原始状态非 active（错误、停用等）时原样返回，否则依次取限流、过载、临时不可调度、
// 手工暂停中优先级最高的一项。
func deriveStatus(r store.AccountWindowRow, now time.Time) (string, *time.Time) {
	if r.Status != displayStatusActive {
		return r.Status, nil
	}
	if until := activeUntil(r.RateLimitResetAt, now); until != nil {
		return displayStatusRateLimited, until
	}
	if until := activeUntil(r.OverloadUntil, now); until != nil {
		return displayStatusOverloaded, until
	}
	if until := activeUntil(r.TempUnschedulableUntil, now); until != nil {
		return displayStatusTempUnschedulable, until
	}
	if !r.Schedulable {
		return displayStatusPaused, nil
	}
	return displayStatusActive, nil
}

// activeUntil 返回仍然生效的解除时刻，已过期或未设置时返回 nil。
func activeUntil(until *time.Time, now time.Time) *time.Time {
	if until == nil || !until.After(now) {
		return nil
	}
	return until
}
