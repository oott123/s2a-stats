# 执行计划

## Phase 1 — 后端：Store 层扩展

**目标文件：`internal/store/store.go`**

1. 在 `AccountWindowRow` 结构体末尾新增两个字段：
   - `FableUtil  *float64   ` —— 7d Fable 窗口使用率（0–1）
   - `FableReset *time.Time ` —— 7d Fable 窗口重置时刻
2. 在 `anthropicAccountWindowsSQL` 的 SELECT 子句中，在现有 `seven_reset` 之后追加：
   - `(extra->>'passive_usage_7d_oi_utilization')::float8 AS fable_util`
   - `CASE WHEN extra->>'passive_usage_7d_oi_reset' IS NULL THEN NULL ELSE to_timestamp((extra->>'passive_usage_7d_oi_reset')::bigint) END AS fable_reset`
3. 在 `rows.Scan(...)` 调用中增加 `&r.FableUtil, &r.FableReset` 两个扫描目标。
4. 在 `internal/store/store.go` 同级运行 `go test ./internal/store`（如存在）确认编译通过。若无可执行测试则执行 `go build ./internal/store`。

## Phase 2 — 后端：Stats 层扩展

**目标文件：`internal/stats/service.go`**

1. 在 `AccountDTO` 结构体中新增 `Fable *WindowDTO \`json:"fable,omitempty"\`` 字段。
2. 在 `WindowUsageResponse` 结构体中新增 `SevenDayFable WindowUsageDTO \`json:"seven_day_fable"\`` 字段。
3. 在 `AccountWindows` 方法内，于 `FiveHour`/`SevenDay` 填充逻辑相同位置，为每个账号追加：
   ```go
   Fable: s.window(r.FableUtil, r.FableReset),
   ```
4. 在 `WindowUsage` 方法中，于 `resp.SevenDay` 赋值之后，新增：
   ```go
   resp.SevenDayFable, err = s.windowUsage(ctx, accountID, fableReset, sevenDayWindow, now)
   ```
   其中 `fableReset` 需从 `store` 查询结果中提取（同 `sevenReset` 的获取方式）。
5. 执行 `go test ./internal/stats -v` 确保现有窗口测试全部通过。若测试硬编码了 `WindowUsageResponse` 字段数或 JSON key，需同步更新断言。

## Phase 3 — 前端：进度条与详情表格

**目标文件：`internal/web/index.html`**

1. **CSS 变量**：在 `:root` 和 `[data-theme="dark"]` 中新增 `--amber: #f59e0b` / `--amber: #fbbf24`，用于 Fable 进度条。
2. **账号列表卡片** —— `meter()` 函数：
   - 接收额外参数或在每个窗口对象中增加 `fable` 字段。
   - 为 `7d F` 生成独立进度条：背景色使用 `var(--amber)`，宽度为 `utilization * 100%`。
   - 在每个 `.bar` 内追加时间线指示器（复用 `bar-indicator` CSS 类），窗口时长固定为 7 天。
   - 若 `resets_at` 缺失，显示 "无采样"。
3. **账号详情** —— `showDetail()` 中 api 调用后的渲染逻辑：
   - 将 `.windows` 的 `grid-template-columns: 1fr 1fr` 改为 `1fr 1fr 1fr`。
   - 在 `userTable("5 小时窗口", data.five_hour)` 和 `userTable("7 天窗口", data.seven_day)` 之后追加：
     ```js
     userTable("7 天 Fable 窗口", data.seven_day_fable)
     ```
4. 复制 `internal/web/index.html` 到本地备份，确保回滚路径。

## Phase 4 — 验证

1. **编译**：执行 `go build ./...` 与 `go vet ./...`，零错误。
2. **单元测试**：执行 `go test ./...`，全部通过。
3. **本地启动**：设置有效 `S2A_SUB2API_DSN` 与 `S2A_PUBLIC_TOKEN`，执行 `mise run server`。
4. **浏览器验证**：
   - 打开 `http://localhost:8080/stats?token=<token>`（根据 `basePath` 调整路径）。
   - 选择任一 Anthropic 账号，观察卡片是否出现琥珀色 `7d F` 进度条。
   - 点击账号进入详情页，确认三列并排：`5 小时窗口`、`7 天窗口`、`7 天 Fable 窗口`。
   - 检查无 Fable 数据的账号：`7d F` 进度条显示 "无采样"，详情页第三列显示 "窗口不可用"。
   - 切换深色/浅色主题，确认琥珀色进度条与时间线颜色正常。

## 回滚策略

- 任何阶段若测试失败或浏览器验证异常，优先回滚 `internal/web/index.html`（纯前端不影响数据），再回滚 `internal/stats/service.go` 与 `internal/store/store.go`。
- 回滚顺序：前端 → stats service → store。
