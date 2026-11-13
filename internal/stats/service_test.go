package stats

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"s2astats/internal/cache"
	"s2astats/internal/store"
)

type costCall struct {
	accountID int64
	from, to  time.Time
	models    []string
}

type fakeStore struct {
	rows        []store.AccountWindowRow
	accounts    map[int64]*store.AccountWindowRow
	costs       []store.UserCost
	costCalls   []costCall
	windowCalls int
	err         error
}

func (f *fakeStore) AnthropicAccountWindows(context.Context) ([]store.AccountWindowRow, error) {
	return f.rows, f.err
}

func (f *fakeStore) AnthropicAccountWindow(_ context.Context, accountID int64) (*store.AccountWindowRow, error) {
	f.windowCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.accounts[accountID], nil
}

func (f *fakeStore) UserStandardCost(_ context.Context, accountID int64, from, to time.Time) ([]store.UserCost, error) {
	f.costCalls = append(f.costCalls, costCall{accountID: accountID, from: from, to: to})
	return f.costs, f.err
}

func (f *fakeStore) UserStandardCostByModels(_ context.Context, accountID int64, from, to time.Time, models []string) ([]store.UserCost, error) {
	f.costCalls = append(f.costCalls, costCall{accountID: accountID, from: from, to: to, models: models})
	return f.costs, f.err
}

func newTestService(t *testing.T, f *fakeStore) *Service {
	t.Helper()
	return New(f, cache.New(), mustLoc(t), 10*time.Minute)
}

func TestWindowUsageAccountNotFound(t *testing.T) {
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{}}
	svc := newTestService(t, fake)

	resp, err := svc.WindowUsage(context.Background(), 123)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound", err)
	}
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
	if len(fake.costCalls) != 0 {
		t.Errorf("cost calls = %d, want 0 (usage_logs must not be queried)", len(fake.costCalls))
	}
}

func TestMonthlyUsageAccountNotFound(t *testing.T) {
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{}}
	svc := newTestService(t, fake)

	resp, err := svc.MonthlyUsage(context.Background(), 123, 2026, 6)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound", err)
	}
	if resp != nil {
		t.Fatalf("resp = %v, want nil", resp)
	}
	if len(fake.costCalls) != 0 {
		t.Errorf("cost calls = %d, want 0 (usage_logs must not be queried)", len(fake.costCalls))
	}
}

func TestWindowUsageAccountFound(t *testing.T) {
	reset := time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
	acct := &store.AccountWindowRow{
		ID:         123,
		FiveReset:  &reset,
		SevenReset: &reset,
		FableReset: &reset,
	}
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{123: acct}}
	svc := newTestService(t, fake)
	svc.now = func() time.Time { return time.Date(2026, 6, 16, 18, 30, 0, 0, time.UTC) }

	resp, err := svc.WindowUsage(context.Background(), 123)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !resp.FiveHour.Available || !resp.SevenDay.Available || !resp.SevenDayFable.Available {
		t.Errorf("windows available = (%v, %v, %v), want all true",
			resp.FiveHour.Available, resp.SevenDay.Available, resp.SevenDayFable.Available)
	}
	if got := resp.FiveHour.WindowStart; got == nil || !got.Equal(reset.Add(-fiveHourWindow)) {
		t.Errorf("five_hour window_start = %v, want %v", got, reset.Add(-fiveHourWindow))
	}
	if got := resp.SevenDay.WindowStart; got == nil || !got.Equal(reset.Add(-sevenDayWindow)) {
		t.Errorf("seven_day window_start = %v, want %v", got, reset.Add(-sevenDayWindow))
	}
	if got := resp.SevenDayFable.WindowStart; got == nil || !got.Equal(reset.Add(-sevenDayWindow)) {
		t.Errorf("seven_day_fable window_start = %v, want %v", got, reset.Add(-sevenDayWindow))
	}
	if len(fake.costCalls) != 3 {
		t.Fatalf("cost calls = %d, want 3", len(fake.costCalls))
	}
	if got := fake.costCalls[2].models; !slices.Equal(got, []string{"claude-fable-5", "claude-fable-5-1"}) {
		t.Errorf("fable call models = %q, want [claude-fable-5 claude-fable-5-1]", got)
	}
}

func TestWindowUsagePartialSampling(t *testing.T) {
	reset := time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
	acct := &store.AccountWindowRow{
		ID:         123,
		FiveReset:  &reset,
		SevenReset: nil,
		FableReset: nil,
	}
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{123: acct}}
	svc := newTestService(t, fake)
	now := time.Date(2026, 6, 16, 18, 30, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	resp, err := svc.WindowUsage(context.Background(), 123)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if resp.SevenDay.Available {
		t.Errorf("seven_day available = true, want false")
	}
	if len(resp.SevenDay.Users) != 0 {
		t.Errorf("seven_day users = %v, want empty", resp.SevenDay.Users)
	}
	if !resp.FiveHour.Available {
		t.Errorf("five_hour available = false, want true")
	}

	// Fable 窗口缺重置采样，且 SevenReset 也缺失 → 无可回退窗口，保持不可用且不查库。
	if resp.SevenDayFable.Available {
		t.Errorf("seven_day_fable available = true, want false (no FableReset, no SevenReset to fall back to)")
	}
	if len(fake.costCalls) != 1 {
		t.Errorf("cost calls = %d, want 1 (seven_day/seven_day_fable must not query)", len(fake.costCalls))
	}
}

func TestWindowUsageFableFallbackToSevenDay(t *testing.T) {
	reset := time.Date(2026, 6, 16, 12, 30, 0, 0, time.UTC)
	acct := &store.AccountWindowRow{
		ID:         123,
		FiveReset:  &reset,
		SevenReset: &reset,
		FableReset: nil,
	}
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{123: acct}}
	svc := newTestService(t, fake)
	now := time.Date(2026, 6, 16, 18, 30, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	resp, err := svc.WindowUsage(context.Background(), 123)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !resp.SevenDay.Available {
		t.Fatalf("seven_day available = false, want true")
	}
	if !resp.SevenDayFable.Available {
		t.Fatalf("seven_day_fable available = false, want true (fallback to seven_day window)")
	}
	if got := resp.SevenDayFable.WindowStart; got == nil || !got.Equal(reset.Add(-sevenDayWindow)) {
		t.Errorf("seven_day_fable window_start = %v, want %v", got, reset.Add(-sevenDayWindow))
	}
	if len(fake.costCalls) != 3 {
		t.Fatalf("cost calls = %d, want 3", len(fake.costCalls))
	}
	if fable := fake.costCalls[2]; !slices.Equal(fable.models, []string{"claude-fable-5", "claude-fable-5-1"}) ||
		!fable.from.Equal(reset.Add(-sevenDayWindow)) || !fable.to.Equal(now) {
		t.Errorf("fable cost call = %+v, want models=[claude-fable-5 claude-fable-5-1] from=%v to=%v", fable, reset.Add(-sevenDayWindow), now)
	}
}

func TestMonthlyUsageAccountFound(t *testing.T) {
	loc := mustLoc(t)
	acct := &store.AccountWindowRow{ID: 123}
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{123: acct}}
	svc := newTestService(t, fake)

	resp, err := svc.MonthlyUsage(context.Background(), 123, 2026, 6)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	wantStart, wantEnd := BillingCycle(loc, 2026, 6)
	if resp.Month != "2026-06" {
		t.Errorf("month = %q, want %q", resp.Month, "2026-06")
	}
	if !resp.CycleStart.Equal(wantStart) || !resp.CycleEnd.Equal(wantEnd) {
		t.Errorf("cycle = [%v, %v), want [%v, %v)", resp.CycleStart, resp.CycleEnd, wantStart, wantEnd)
	}
	if len(fake.costCalls) != 1 {
		t.Fatalf("cost calls = %d, want 1", len(fake.costCalls))
	}
	call := fake.costCalls[0]
	if call.accountID != 123 || !call.from.Equal(wantStart) || !call.to.Equal(wantEnd) || len(call.models) != 0 {
		t.Errorf("cost call = %+v, want accountID=123 from=%v to=%v no model filter", call, wantStart, wantEnd)
	}
}

func TestNotFoundNotCached(t *testing.T) {
	fake := &fakeStore{accounts: map[int64]*store.AccountWindowRow{}}
	svc := newTestService(t, fake)

	for i := range 2 {
		resp, err := svc.WindowUsage(context.Background(), 999)
		if !errors.Is(err, ErrAccountNotFound) {
			t.Fatalf("call %d: err = %v, want ErrAccountNotFound", i+1, err)
		}
		if resp != nil {
			t.Fatalf("call %d: resp = %v, want nil", i+1, resp)
		}
	}
	if fake.windowCalls != 2 {
		t.Errorf("AnthropicAccountWindow calls = %d, want 2 (negative results must not be cached)", fake.windowCalls)
	}
}

func TestAccountWindowsStatus(t *testing.T) {
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	future := now.Add(30 * time.Minute)
	past := now.Add(-30 * time.Minute)

	tests := []struct {
		name      string
		row       store.AccountWindowRow
		want      string
		wantUntil *time.Time
	}{
		{
			name: "active",
			row:  store.AccountWindowRow{Status: "active", Schedulable: true},
			want: "active",
		},
		{
			name: "rate limited",
			row:  store.AccountWindowRow{Status: "active", Schedulable: true, RateLimitResetAt: &future},
			want: "rate_limited",
			// DTO 中的解除时刻按配置时区渲染，比较用 Equal 而非指针相等。
			wantUntil: &future,
		},
		{
			name:      "overloaded",
			row:       store.AccountWindowRow{Status: "active", Schedulable: true, OverloadUntil: &future},
			want:      "overloaded",
			wantUntil: &future,
		},
		{
			name:      "temp unschedulable",
			row:       store.AccountWindowRow{Status: "active", Schedulable: true, TempUnschedulableUntil: &future},
			want:      "temp_unschedulable",
			wantUntil: &future,
		},
		{
			name: "paused",
			row:  store.AccountWindowRow{Status: "active", Schedulable: false},
			want: "paused",
		},
		{
			name: "rate limit takes precedence over overload and pause",
			row: store.AccountWindowRow{
				Status: "active", Schedulable: false,
				RateLimitResetAt: &future, OverloadUntil: &future, TempUnschedulableUntil: &future,
			},
			want:      "rate_limited",
			wantUntil: &future,
		},
		{
			name: "expired rate limit falls through to pause",
			row:  store.AccountWindowRow{Status: "active", Schedulable: false, RateLimitResetAt: &past},
			want: "paused",
		},
		{
			name: "non-active status is passed through",
			row:  store.AccountWindowRow{Status: "error", Schedulable: false, RateLimitResetAt: &future},
			want: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := tt.row
			row.ID, row.Name = 1, "acc"
			fake := &fakeStore{rows: []store.AccountWindowRow{row}}
			svc := newTestService(t, fake)
			svc.now = func() time.Time { return now }

			resp, err := svc.AccountWindows(context.Background())
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if len(resp.Accounts) != 1 {
				t.Fatalf("accounts = %d, want 1", len(resp.Accounts))
			}
			got := resp.Accounts[0]
			if got.Status != tt.want {
				t.Errorf("status = %q, want %q", got.Status, tt.want)
			}
			switch {
			case tt.wantUntil == nil && got.StatusUntil != nil:
				t.Errorf("status_until = %v, want nil", got.StatusUntil)
			case tt.wantUntil != nil && got.StatusUntil == nil:
				t.Errorf("status_until = nil, want %v", tt.wantUntil)
			case tt.wantUntil != nil && !got.StatusUntil.Equal(*tt.wantUntil):
				t.Errorf("status_until = %v, want %v", got.StatusUntil, tt.wantUntil)
			}
		})
	}
}
