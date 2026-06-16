package stats

import "time"

// BillingCycle 返回以 loc 计的账期区间 [当月 10 日 00:00, 次月 10 日 00:00)。
// AddDate 自动处理跨年（如 12 月 → 次年 1 月）。
func BillingCycle(loc *time.Location, year, month int) (start, end time.Time) {
	start = time.Date(year, time.Month(month), 10, 0, 0, 0, 0, loc)
	end = start.AddDate(0, 1, 0)
	return start, end
}

// WindowStart 由窗口重置时刻反推窗口起点：reset - d。
func WindowStart(reset time.Time, d time.Duration) time.Time {
	return reset.Add(-d)
}
