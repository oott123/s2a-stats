# 设计：Fable 窗口缺采样时回退为普通 7d 窗口

## 现状

`stats.Service.WindowUsage` 对三个窗口统一调用 `windowUsage(ctx, accountID, reset, d, now, model)`。
`windowUsage` 在 `reset == nil` 时直接返回 `available:false`（不查 usage_logs）。
Fable 窗口的 `reset` 来自 `AccountWindowRow.FableReset`（`extra->>'passive_usage_7d_oi_reset'`），无该采样时为 `nil` → 前端「7 天 Fable 窗口」显示"无重置数据，窗口不可用"。

## 变更

### 1. `internal/stats/service.go` — `WindowUsage`

在调用 Fable 的 `windowUsage` 之前，对 `FableReset == nil` 回退为普通 7d 窗口：

```go
fableReset := acct.FableReset
if fableReset == nil {
    // Fable 窗口缺重置采样时回退为普通 7d 窗口（SevenReset）；
    // SevenReset 也缺失时与 seven_day 一致保持 available:false。
    fableReset = acct.SevenReset
}
resp.SevenDayFable, err = s.windowUsage(ctx, accountID, fableReset, sevenDayWindow, now, fableModel)
```

- `windowUsage` 本身不改：`reset` 非空时按 `WindowStart(reset, 7d) = reset - 7d` 计算起点、终点 `now`，模型过滤（`fableModel`）与查询路径（`UserStandardCostByModel`）不变。回退时 Fable 窗口区间与 `seven_day` 完全一致（`[SevenReset-7d, now)`），仅多一层模型过滤。
- 5h / 7d 窗口调用不变，缺重置仍 `available:false`。
- 回退只影响 `WindowUsage`（需求 2 端点）；账号列表 `AccountWindows` 的 `Fable` 进度条仍按采样有无显示（`window()` 返回 `nil`），不受影响。

### 2. `internal/stats/service_test.go`

- `TestWindowUsagePartialSampling`（`SevenReset == nil`、`FableReset == nil`）：
  - `seven_day` 不可用、不查库（不变）；
  - 新增断言：`seven_day_fable.Available == false`（无 FableReset 且无 SevenReset 可回退），cost 调用总数 1（仅 five_hour）。
- 新增 `TestWindowUsageFableFallbackToSevenDay`（`SevenReset != nil`、`FableReset == nil`）：
  - `seven_day_fable.Available == true`；
  - `window_start == SevenReset - 7d`（与 `seven_day` 相同）；
  - Fable 的 cost 调用 `model == fableModel`、`from == SevenReset-7d`、`to == now`；
  - cost 调用总数 3。
- `TestWindowUsageAccountFound` 不变（三窗口都有采样时行为与原来一致）。

## API 响应变化

响应结构、字段名、JSON key 均不变。仅语义变化：

| 场景 | 之前 | 之后 |
|------|------|------|
| `FableReset` 存在 | 窗口 `[FableReset - 7d, now)` | 不变 |
| `FableReset` 缺失，`SevenReset` 存在 | `available:false`，无 start/end，`users:[]` | `available:true`，窗口 `[SevenReset - 7d, now)`（与 `seven_day` 一致），`users` 为区间内 `claude-fable-5` 消费 |
| `FableReset`、`SevenReset` 均缺失 | `available:false` | 不变（`available:false`） |

## 缓存

- key 不变（`window-usage:%d`），TTL 不变。
- 回退窗口跟随 `SevenReset` 采样滚动；TTL 到期后重建响应，无需额外失效逻辑。
- 旧缓存结构本身含 `SevenDayFable` 字段，无结构不兼容问题。

## 不变的部分

- store 层 SQL 不变，不新增查询。
- 5h / 7d 窗口口径与 `available:false` 语义不变。
- 前端 `userTable` 逻辑不变（`available:true` 即渲染区间 + 表格）。
- 隐私约束不变：仍只暴露用户名与标准消费。

## 边界情况

| 场景 | 行为 |
|------|------|
| 区间内无 `claude-fable-5` 消费 | `available:true`，`users` 为空数组（前端显示"无用量记录"）。 |
| `SevenReset-7d` 早于账号首条 usage_log | 查询自然从首条记录开始，无越界风险。 |
| Fable 采样后续到达（`FableReset` 由 nil 变有值） | TTL 到期后自动切换回 `FableReset` 对齐的独立窗口。 |
| `SevenReset` 存在但 Fable 模型尚无消费记录 | 回退生效但 `users` 为空；Fable 消费出现后（TTL 到期）表格自然填充。 |
