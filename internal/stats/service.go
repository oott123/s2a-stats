// Package stats 编排窗口/账期时间计算、调用 store 取数、组装公共 DTO 并缓存。
package stats

import (
	"context"
	"fmt"
	"time"

	"s2astats/internal/cache"
	"s2astats/internal/store"
)

const (
	fiveHourWindow = 5 * time.Hour
	sevenDayWindow = 7 * 24 * time.Hour
)

// dataStore 是 stats 依赖的只读取数接口（便于测试替换）。
type dataStore interface {
	AnthropicAccountWindows(ctx context.Context) ([]store.AccountWindowRow, error)
	UserStandardCost(ctx context.Context, accountID int64, from, to time.Time) ([]store.UserCost, error)
}

// Service 提供三个公共统计端点的业务逻辑。
type Service struct {
	store    dataStore
	cache    *cache.Cache
	loc      *time.Location
	cacheTTL time.Duration
	now      func() time.Time
}

// New 创建 Service。loc 用于渲染时间与账期边界。
func New(st dataStore, c *cache.Cache, loc *time.Location, cacheTTL time.Duration) *Service {
	return &Service{store: st, cache: c, loc: loc, cacheTTL: cacheTTL, now: time.Now}
}

// ---- DTO ----

// AccountsResponse 是 GET /v1/accounts 的响应体。
type AccountsResponse struct {
	Accounts []AccountDTO `json:"accounts"`
}

// AccountDTO 是单个 Anthropic 账号的窗口采样。
type AccountDTO struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	Sampled   bool       `json:"sampled"`
	SampledAt *time.Time `json:"sampled_at"`
	FiveHour  *WindowDTO `json:"five_hour"`
	SevenDay  *WindowDTO `json:"seven_day"`
}

// WindowDTO 是 GET /v1/accounts 中单个窗口的采样使用率与重置时刻。
type WindowDTO struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// WindowUsageResponse 是 GET /v1/accounts/{id}/window-usage 的响应体。
type WindowUsageResponse struct {
	AccountID int64          `json:"account_id"`
	FiveHour  WindowUsageDTO `json:"five_hour"`
	SevenDay  WindowUsageDTO `json:"seven_day"`
}

// WindowUsageDTO 是某窗口内按用户的标准消费。
type WindowUsageDTO struct {
	Available   bool       `json:"available"`
	WindowStart *time.Time `json:"window_start,omitempty"`
	WindowEnd   *time.Time `json:"window_end,omitempty"`
	Users       []UserDTO  `json:"users"`
}

// MonthlyUsageResponse 是 GET /v1/accounts/{id}/monthly-usage 的响应体。
type MonthlyUsageResponse struct {
	AccountID  int64     `json:"account_id"`
	Month      string    `json:"month"`
	CycleStart time.Time `json:"cycle_start"`
	CycleEnd   time.Time `json:"cycle_end"`
	Users      []UserDTO `json:"users"`
}

// UserDTO 是单个用户的标准消费。
type UserDTO struct {
	Username     string  `json:"username"`
	StandardCost float64 `json:"standard_cost"`
}

// ---- 端点逻辑 ----

// AccountWindows 实现需求 1：列出所有 Anthropic 账号及其被动采样窗口。
func (s *Service) AccountWindows(ctx context.Context) (*AccountsResponse, error) {
	const key = "accounts"
	if v, ok := s.cache.Get(key); ok {
		return v.(*AccountsResponse), nil
	}

	rows, err := s.store.AnthropicAccountWindows(ctx)
	if err != nil {
		return nil, err
	}

	resp := &AccountsResponse{Accounts: make([]AccountDTO, 0, len(rows))}
	for _, r := range rows {
		resp.Accounts = append(resp.Accounts, AccountDTO{
			ID:        r.ID,
			Name:      r.Name,
			Status:    r.Status,
			Sampled:   r.SampledAt != nil,
			SampledAt: s.inLoc(r.SampledAt),
			FiveHour:  s.window(r.FiveUtil, r.FiveReset),
			SevenDay:  s.window(r.SevenUtil, r.SevenReset),
		})
	}

	s.cache.Set(key, resp, s.cacheTTL)
	return resp, nil
}

// window 仅在 util 与 reset 均存在时返回窗口，否则返回 nil。
func (s *Service) window(util *float64, reset *time.Time) *WindowDTO {
	if util == nil || reset == nil {
		return nil
	}
	return &WindowDTO{Utilization: *util, ResetsAt: reset.In(s.loc)}
}

// WindowUsage 实现需求 2：指定账号在其 5h、7d 窗口内按用户的标准消费。
func (s *Service) WindowUsage(ctx context.Context, accountID int64) (*WindowUsageResponse, error) {
	key := fmt.Sprintf("window-usage:%d", accountID)
	if v, ok := s.cache.Get(key); ok {
		return v.(*WindowUsageResponse), nil
	}

	rows, err := s.store.AnthropicAccountWindows(ctx)
	if err != nil {
		return nil, err
	}
	var acct *store.AccountWindowRow
	for i := range rows {
		if rows[i].ID == accountID {
			acct = &rows[i]
			break
		}
	}

	now := s.now()
	resp := &WindowUsageResponse{AccountID: accountID}

	var fiveReset, sevenReset *time.Time
	if acct != nil {
		fiveReset = acct.FiveReset
		sevenReset = acct.SevenReset
	}

	resp.FiveHour, err = s.windowUsage(ctx, accountID, fiveReset, fiveHourWindow, now)
	if err != nil {
		return nil, err
	}
	resp.SevenDay, err = s.windowUsage(ctx, accountID, sevenReset, sevenDayWindow, now)
	if err != nil {
		return nil, err
	}

	s.cache.Set(key, resp, s.cacheTTL)
	return resp, nil
}

// windowUsage 计算单个窗口的按用户消费；缺重置时刻 → available:false。
func (s *Service) windowUsage(ctx context.Context, accountID int64, reset *time.Time, d time.Duration, now time.Time) (WindowUsageDTO, error) {
	if reset == nil {
		return WindowUsageDTO{Available: false, Users: []UserDTO{}}, nil
	}
	start := WindowStart(*reset, d)
	costs, err := s.store.UserStandardCost(ctx, accountID, start, now)
	if err != nil {
		return WindowUsageDTO{}, err
	}
	startLoc := start.In(s.loc)
	endLoc := now.In(s.loc)
	return WindowUsageDTO{
		Available:   true,
		WindowStart: &startLoc,
		WindowEnd:   &endLoc,
		Users:       toUserDTOs(costs),
	}, nil
}

// MonthlyUsage 实现需求 3：指定账号在指定账期内按用户的标准消费。
func (s *Service) MonthlyUsage(ctx context.Context, accountID int64, year, month int) (*MonthlyUsageResponse, error) {
	key := fmt.Sprintf("monthly-usage:%d:%04d-%02d", accountID, year, month)
	if v, ok := s.cache.Get(key); ok {
		return v.(*MonthlyUsageResponse), nil
	}

	start, end := BillingCycle(s.loc, year, month)
	costs, err := s.store.UserStandardCost(ctx, accountID, start, end)
	if err != nil {
		return nil, err
	}

	resp := &MonthlyUsageResponse{
		AccountID:  accountID,
		Month:      fmt.Sprintf("%04d-%02d", year, month),
		CycleStart: start,
		CycleEnd:   end,
		Users:      toUserDTOs(costs),
	}

	s.cache.Set(key, resp, s.cacheTTL)
	return resp, nil
}

// toUserDTOs 转换 store 结果，空 username 回退为 user-<id>。
func toUserDTOs(costs []store.UserCost) []UserDTO {
	out := make([]UserDTO, 0, len(costs))
	for _, c := range costs {
		name := c.Username
		if name == "" {
			name = fmt.Sprintf("user-%d", c.UserID)
		}
		out = append(out, UserDTO{Username: name, StandardCost: c.StandardCost})
	}
	return out
}

// inLoc 把可空时刻转到配置时区。
func (s *Service) inLoc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.In(s.loc)
	return &v
}
