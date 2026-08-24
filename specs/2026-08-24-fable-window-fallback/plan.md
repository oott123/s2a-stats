# 实施计划：Fable 窗口缺采样时回退为普通 7d 窗口

## Phase 1 — 后端回退逻辑

1. `internal/stats/service.go` 的 `WindowUsage`：
   - 在 `resp.SevenDayFable` 赋值前，提取 `fableReset := acct.FableReset`；
   - `fableReset == nil` 时回退为 `acct.SevenReset`（加注释说明；`SevenReset` 也缺失时由 `windowUsage` 走 `available:false` 分支，不查库）。
2. `windowUsage`、store、httpapi 均不改。

## Phase 2 — 测试

1. `internal/stats/service_test.go` 的 `TestWindowUsagePartialSampling`（`SevenReset == nil`、`FableReset == nil`）：
   - 保留既有断言（`seven_day` 不可用、five_hour 可用）；
   - 改为断言 `seven_day_fable.Available == false`、cost 调用总数 1（Fable 无回退目标也不查库）。
2. 新增 `TestWindowUsageFableFallbackToSevenDay`（`SevenReset != nil`、`FableReset == nil`）：
   - 断言 `seven_day_fable.Available == true`、`WindowStart == SevenReset - 7d`；
   - 断言 Fable 的 cost 调用：`model == fableModel`、`from == SevenReset-7d`、`to == now`；
   - 断言 cost 调用总数 3。
3. `TestWindowUsageAccountFound` 不动（有采样路径回归保护）。

## Phase 3 — 文档同步

1. 新建 `specs/2026-08-24-fable-window-fallback/`（proposal.md、design.md、plan.md）。
2. `specs/2026-07-11-fable-progress-bar/design.md` 边界表第一行（`passive_usage_7d_oi_reset` 缺失 → `available:false`）更新为新行为并交叉引用本 spec。

## 验证

1. `gofmt -l internal/` 无输出。
2. `go build ./... && go vet ./...` 通过。
3. `go test ./internal/stats -run 'TestWindowUsage' -v` 全绿；`go test ./...` 无回归。

## 验证（手动）

1. `mise run server` 起服务。
2. 选一个有 `passive_usage_7d_reset` 但 `passive_usage_7d_oi_reset` 缺失的账号（缓存 TTL 过期后），`GET /v1/accounts/{id}/window-usage`：
   - `seven_day_fable.available == true`，`window_start/window_end` 与 `seven_day` 完全一致；
   - `seven_day_fable.users` 为同一区间内 `claude-fable-5` 模型消费；
   - 前端详情页「7 天 Fable 窗口」列渲染与「7 天窗口」相同的区间与表格。
3. `FableReset` 存在的账号行为不变：窗口起点仍对齐 `FableReset - 7d`。
