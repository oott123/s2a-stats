package stats

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Singapore")
	if err != nil {
		t.Fatalf("load Asia/Singapore: %v", err)
	}
	return loc
}

func TestBillingCycle(t *testing.T) {
	loc := mustLoc(t)
	tests := []struct {
		name        string
		year, month int
		wantStart   string
		wantEnd     string
	}{
		{"普通月", 2026, 6, "2026-06-10T00:00:00+08:00", "2026-07-10T00:00:00+08:00"},
		{"12月跨年", 2026, 12, "2026-12-10T00:00:00+08:00", "2027-01-10T00:00:00+08:00"},
		{"闰年2月起点", 2024, 2, "2024-02-10T00:00:00+08:00", "2024-03-10T00:00:00+08:00"},
		{"1月", 2026, 1, "2026-01-10T00:00:00+08:00", "2026-02-10T00:00:00+08:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := BillingCycle(loc, tt.year, tt.month)
			if got := start.Format(time.RFC3339); got != tt.wantStart {
				t.Errorf("start = %s, want %s", got, tt.wantStart)
			}
			if got := end.Format(time.RFC3339); got != tt.wantEnd {
				t.Errorf("end = %s, want %s", got, tt.wantEnd)
			}
		})
	}
}

func TestWindowStart(t *testing.T) {
	reset := time.Date(2026, 6, 16, 22, 30, 0, 0, time.UTC)

	if got := WindowStart(reset, fiveHourWindow); !got.Equal(reset.Add(-5 * time.Hour)) {
		t.Errorf("5h start = %s, want %s", got, reset.Add(-5*time.Hour))
	}
	if got := WindowStart(reset, sevenDayWindow); !got.Equal(reset.AddDate(0, 0, -7)) {
		t.Errorf("7d start = %s, want %s", got, reset.AddDate(0, 0, -7))
	}
}
