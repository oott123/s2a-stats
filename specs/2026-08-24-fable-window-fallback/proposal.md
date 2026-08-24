# Fable 窗口缺采样时回退为普通 7d 窗口

## 需求

`GET /v1/accounts/{id}/window-usage` 的「7 天 Fable 窗口」目前依赖被动采样的 `passive_usage_7d_oi_reset` 反推窗口起点；当该采样缺失（`FableReset == nil`）时返回 `available:false`、无用量数据。

改为：Fable 重置采样缺失时，自动回退为**当前普通 7d 窗口**（复用 `SevenReset`，窗口 `[seven_reset - 7d, now)`），照常按 `claude-fable-5` 模型过滤统计各用户标准消费，`available:true`。即 Fable 窗口与「7 天窗口」区间一致，仅模型过滤不同。

## 关键定义与约定

- 回退目标 = `SevenReset`（普通 7d 窗口的重置采样）。若 `SevenReset` 也缺失，则与 `seven_day` 一致保持 `available:false`（不查库）。
- 回退仅作用于 `seven_day_fable` 窗口；5h / 7d 窗口口径不变（缺重置仍 `available:false`）。
- 查询仍走 `UserStandardCostByModel`（`model = claude-fable-5`），不新增 SQL。
- 响应结构不变：`seven_day_fable.window_start/window_end/users`，前端无需改动。
- 缓存 key 不变（`window-usage:%d`）；回退值跟随 `SevenReset` 采样滚动，TTL 到期后刷新。

## 范围

- `internal/stats/service.go` — `WindowUsage`：`FableReset == nil` 时以 `SevenReset` 作为重置时刻传入 `windowUsage`。
- `internal/stats/service_test.go` — `TestWindowUsagePartialSampling` 覆盖「两者皆缺」分支；新增 `TestWindowUsageFableFallbackToSevenDay` 覆盖回退分支。
- 后端/前端接口契约不变；`internal/web/index.html` 不改。
