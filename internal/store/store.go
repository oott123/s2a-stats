// Package store 是 sub2api PostgreSQL 的只读访问层。所有查询均为 SELECT。
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 持有 pgx 连接池与单次查询超时。
type Store struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// AccountWindowRow 是一个 Anthropic 账号的被动采样窗口数据。
// 可空字段用指针表示「无该项采样」。
type AccountWindowRow struct {
	ID         int64
	Name       string
	Status     string
	FiveUtil   *float64   // 5h 窗口使用率（0–1）
	SevenUtil  *float64   // 7d 窗口使用率（0–1）
	FiveReset  *time.Time // 5h 窗口重置时刻（= session_window_end）
	SevenReset *time.Time // 7d 窗口重置时刻
	SampledAt  *time.Time // 被动采样时刻
}

// UserCost 是某账号、某时间区间内单个用户的标准消费聚合。
type UserCost struct {
	UserID       int64
	Username     string // NULL/空串由上层回退为 user-<id>
	StandardCost float64
}

// New 建立 pgxpool 连接池并校验连通性。
func New(ctx context.Context, dsn string, timeout time.Duration) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool, timeout: timeout}, nil
}

// Close 关闭连接池。
func (s *Store) Close() {
	s.pool.Close()
}

const anthropicAccountWindowsSQL = `
SELECT id, name, status,
       session_window_end,
       (extra->>'session_window_utilization')::float8   AS five_util,
       (extra->>'passive_usage_7d_utilization')::float8 AS seven_util,
       CASE WHEN extra->>'passive_usage_7d_reset' IS NULL THEN NULL
            ELSE to_timestamp((extra->>'passive_usage_7d_reset')::bigint) END AS seven_reset,
       extra->>'passive_usage_sampled_at'               AS sampled_at
FROM accounts
WHERE platform = 'anthropic'
  AND type = 'oauth'
  AND deleted_at IS NULL
ORDER BY id`

// AnthropicAccountWindows 返回所有 Anthropic 账号的被动采样窗口数据（需求 1）。
func (s *Store) AnthropicAccountWindows(ctx context.Context) ([]AccountWindowRow, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, anthropicAccountWindowsSQL)
	if err != nil {
		return nil, fmt.Errorf("query anthropic account windows: %w", err)
	}
	defer rows.Close()

	var out []AccountWindowRow
	for rows.Next() {
		var r AccountWindowRow
		var sampledAt *string
		if err := rows.Scan(
			&r.ID, &r.Name, &r.Status,
			&r.FiveReset,
			&r.FiveUtil, &r.SevenUtil,
			&r.SevenReset,
			&sampledAt,
		); err != nil {
			return nil, fmt.Errorf("scan account window row: %w", err)
		}
		if sampledAt != nil {
			t, err := time.Parse(time.RFC3339, *sampledAt)
			if err != nil {
				return nil, fmt.Errorf("parse passive_usage_sampled_at %q: %w", *sampledAt, err)
			}
			r.SampledAt = &t
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account window rows: %w", err)
	}
	return out, nil
}

const userStandardCostSQL = `
SELECT ul.user_id, u.username, SUM(ul.total_cost)::float8 AS standard_cost
FROM usage_logs ul
LEFT JOIN users u ON u.id = ul.user_id
WHERE ul.account_id = $1
  AND ul.created_at >= $2
  AND ul.created_at <  $3
GROUP BY ul.user_id, u.username
ORDER BY standard_cost DESC`

// UserStandardCost 返回指定账号在 [from, to) 内按用户聚合的标准消费（需求 2、3 共用）。
func (s *Store) UserStandardCost(ctx context.Context, accountID int64, from, to time.Time) ([]UserCost, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, userStandardCostSQL, accountID, from, to)
	if err != nil {
		return nil, fmt.Errorf("query user standard cost: %w", err)
	}
	defer rows.Close()

	var out []UserCost
	for rows.Next() {
		var c UserCost
		var username *string
		if err := rows.Scan(&c.UserID, &username, &c.StandardCost); err != nil {
			return nil, fmt.Errorf("scan user cost row: %w", err)
		}
		if username != nil {
			c.Username = *username
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user cost rows: %w", err)
	}
	return out, nil
}
