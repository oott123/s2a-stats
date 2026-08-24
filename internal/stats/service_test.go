package stats

import (
	"context"
	"errors"
	"testing"
	"time"

	"s2astats/internal/cache"
	"s2astats/internal/store"
)

type costCall struct {
	accountID int64
	from, to  time.Time
	model     string
}

type fakeStore struct {
	accounts    map[int64]*store.AccountWindowRow
	costs       []store.UserCost
	costCalls   []costCall
	windowCalls int
	err         error
}

func (f *fakeStore) AnthropicAccountWindows(context.Context) ([]store.AccountWindowRow, error) {
	return nil, f.err
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

func (f *fakeStore) UserStandardCostByModel(_ context.Context, accountID int64, from, to time.Time, model string) ([]store.UserCost, error) {
	f.costCalls = append(f.costCalls, costCall{accountID: accountID, from: from, to: to, model: model})
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
	if fake.costCalls[2].model != fableModel {
		t.Errorf("fable call model = %q, want %q", fake.costCalls[2].model, fableModel)
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
	fable := fake.costCalls[2]
	if fable.model != fableModel || !fable.from.Equal(reset.Add(-sevenDayWindow)) || !fable.to.Equal(now) {
		t.Errorf("fable cost call = %+v, want model=%q from=%v to=%v", fable, fableModel, reset.Add(-sevenDayWindow), now)
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
	if call.accountID != 123 || !call.from.Equal(wantStart) || !call.to.Equal(wantEnd) || call.model != "" {
		t.Errorf("cost call = %+v, want accountID=123 from=%v to=%v model=\"\"", call, wantStart, wantEnd)
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
