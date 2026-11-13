# 设计：账号展示状态改为 sub2api 后台口径

## 现状

`GET /v1/accounts` 直接把 `accounts.status` 透传给前端：`internal/store/store.go` 的
`accountWindowColumns` 选出 `status`，`stats.AccountWindows` 原样赋给 `AccountDTO.Status`。
界面上 `internal/web/index.html` 只在 `status === "active"` 时给徽章上绿色，其余一律灰色并显示英文原值。

sub2api 后台的账号列表则不直接展示原始状态。`frontend/src/components/account/AccountStatusIndicator.vue`
把调度态一并归纳进标签：`status === 'error'` → 错误；`rate_limit_reset_at` 未过期 → 限流中；
`overload_until` 未过期 → 过载中；`temp_unschedulable_until` 未过期 → 临时不可调度；
`status === 'active'` 但 `schedulable === false` → 暂停。文案在
`frontend/src/i18n/locales/zh/admin/accounts.ts` 的 `admin.accounts.status` 下。

`s2astats` 此前只 SELECT 了 `status`，判定期限字段与 `schedulable` 都没有读到，所以无从归纳。

## 变更

### 1. `internal/store/store.go`

`accountWindowColumns` 增加调度列：

```sql
schedulable, rate_limit_reset_at, overload_until, temp_unschedulable_until,
```

`AccountWindowRow` 增加同名 Go 字段，`scanAccountWindowRow` 同步绑定。
`anthropicAccountWindowsSQL` 与 `anthropicAccountWindowSQL` 共用该列清单，两条查询一起生效；
单账号查询多读的字段当前没有消费方，保留是为了让两条查询的列保持一致。

### 2. `internal/stats/status.go`（新增）

```go
func deriveStatus(r store.AccountWindowRow, now time.Time) (string, *time.Time)
```

判定次序与 sub2api 一致：

1. `r.Status != "active"` → 原样返回该状态，无解除时刻（对齐 `AccountStatusIndicator` 的
   `statusText`：非 active 时直接 `t(status)`）。
2. `rate_limit_reset_at` 晚于 now → `rate_limited`，解除时刻为该值。
3. `overload_until` 晚于 now → `overloaded`。
4. `temp_unschedulable_until` 晚于 now → `temp_unschedulable`。
5. `schedulable == false` → `paused`，无解除时刻（手工暂停不会自动恢复）。
6. 否则 `active`。

限流、过载、临时不可调度三个字段为 `nil` 或时刻已过都视为不生效，与
`internal/service/account.go` 的 `IsRateLimited` / `IsOverloaded` 及
`ops_account_availability.go` 的 `now.Before(*until)` 判定一致。

sub2api 在 `ops_account_availability.go` 里还做了「错误态压平限流/过载标记」的互斥处理；
这里因为第 1 步就提前返回，天然不会出现冲突标记，无需额外分支。

### 3. `internal/stats/service.go`

`AccountDTO` 增加 `StatusUntil *time.Time`（JSON `status_until`）；`AccountWindows` 对每行调用
`deriveStatus`，把归纳结果与解除时刻（经 `inLoc` 转到账期时区）填进 DTO。
`AccountDTO.Status` 的 JSON 名不变，消费方读到的值域变宽。

### 4. `internal/web/index.html`

- 新增 `STATUS_LABELS` 与 `statusLabel`：按归纳状态给出中文文案，未知原始状态原样回显。
- 新增 `statusClass`：`active` → 绿，`rate_limited` / `overloaded` / `temp_unschedulable` → 琥珀，
  `error` → 红，其余 → 灰。对应新增 `.badge.ok` / `.badge.warn` / `.badge.bad` 三个类，
  原来的 `.badge.active` 一并被 `.badge.ok` 取代。
- `status_until` 非空时在名称下方追加一行「状态，还剩 X」，徽章 `title` 给出精确解除时刻。
  剩余时间复用已有的 `relReset`。

## API 响应变化

`GET /v1/accounts` 每个账号新增 `status_until`，`status` 值域扩大：

```json
{
  "id": 123,
  "name": "anthropic-main",
  "status": "rate_limited",
  "status_until": "2026-11-13T11:22:00+08:00",
  "sampled": true,
  "...": "..."
}
```

| 场景 | `status` | `status_until` |
| --- | --- | --- |
| 原始状态非 `active` | 该原始值（`error` / `disabled` …） | `null` |
| `rate_limit_reset_at` 未过期 | `rate_limited` | `rate_limit_reset_at` |
| `overload_until` 未过期 | `overloaded` | `overload_until` |
| `temp_unschedulable_until` 未过期 | `temp_unschedulable` | `temp_unschedulable_until` |
| 状态正常但 `schedulable = false` | `paused` | `null` |
| 其余 | `active` | `null` |

限流优先于过载，过载优先于临时不可调度，三者在原始状态为 `active` 时才会出现。

## 边界情况

| 场景 | 行为 |
| --- | --- |
| 解除时刻恰好等于 now | 视为已解除，不进入该状态（用 `After` 判定） |
| 多个期限字段同时生效 | 按限流 → 过载 → 临时不可调度的次序取第一个 |
| 原始状态非 `active` 且期限字段生效 | 只回传原始状态，期限字段被忽略 |
| 账号无被动采样 | 状态照常归纳，采样字段仍为 `nil` |

## 不变的部分

- 账号集合谓词、三个端点的窗口口径、隐私字段集合均不变。
- `AnthropicAccountWindow` 多读的列不影响 `window-usage` / `monthly-usage` 的响应。
- 缓存 key 与 TTL 不变；状态在 TTL 到期重建响应时重算。
- `deriveStatus` 的 `now` 取自 `Service.now`，与窗口计算共用同一个时刻注入点，便于测试。
