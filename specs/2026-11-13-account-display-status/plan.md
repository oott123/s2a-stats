# 执行计划

## 步骤 1 — `internal/store/store.go`：补出调度列

`AccountWindowRow` 增加 `Schedulable bool`、`RateLimitResetAt`、`OverloadUntil`、
`TempUnschedulableUntil`（`*time.Time`）。`accountWindowColumns` 在 `status` 之后插入：

```sql
schedulable, rate_limit_reset_at, overload_until, temp_unschedulable_until,
```

`scanAccountWindowRow` 的 `rows.Scan` 按同一顺序补上四个绑定。
列清单被两条查询共用，`AnthropicAccountWindow` 一并多读到这四个字段。

## 步骤 2 — `internal/stats/status.go`：归纳展示状态

新增导出给包内使用的常量与 `deriveStatus(r store.AccountWindowRow, now time.Time) (string, *time.Time)`，
判定次序为原始状态 → 限流 → 过载 → 临时不可调度 → 手工暂停，全部用 `After(now)` 判定期限字段。

## 步骤 3 — `internal/stats/service.go`：接进 DTO

`AccountDTO` 增加 `StatusUntil *time.Time`（JSON `status_until`）；
`AccountWindows` 取一次 `s.now()`，逐行调用 `deriveStatus`，解除时刻经 `s.inLoc` 转换后填入。

## 步骤 4 — `internal/web/index.html`：渲染

- 新增 `STATUS_LABELS`、`statusLabel`、`statusClass`；`.badge.active` 改为 `.badge.ok`，
  另加 `.badge.warn` / `.badge.bad`，新增 `.status-until` 样式。
- 卡片徽章改用归纳状态渲染文案与配色，`status_until` 非空时给出 `title` 与一行剩余时间。

## 步骤 5 — 测试

`internal/stats/service_test.go`：

- `fakeStore` 增加 `rows`，`AnthropicAccountWindows` 返回它。
- 新增 `TestAccountWindowsStatus`，逐条覆盖各状态、期限字段已过期、以及多个字段同时生效时的优先级。
- 同时修正既有用例中过期的 Fable 模型名断言（`claude-fable-5.1` → `claude-fable-5-1`），
  这两处断言与 `fableModels` 的实际取值不符，改动前就是失败的。

## 步骤 6 — 文档

- 新增 `specs/2026-11-13-account-display-status/` 的 `proposal.md`、`design.md`、`api.md`、`plan.md`。
- 更新 `specs/2026-06-16-sub2api-public-stats-api/api.md` 的 `GET /v1/accounts` 一节。
- 更新 `CLAUDE.md` 的领域规则与隐私条目。

## 步骤 7 — 验证

1. 对改动过的文件执行 `gofmt -w`。
2. `go build ./... && go vet ./... && go test ./...` 全绿。
3. 起一个临时 PostgreSQL，建出 `accounts` / `usage_logs` / `users` 三张表并造出各类状态的账号，
   `mise run server` 指向它：
   - `GET /v1/accounts` 中各账号状态与 `status_until` 符合预期；
   - `GET /v1/accounts/{id}/window-usage` 仍返回窗口数据，非 Anthropic 账号仍 404；
   - 打开前端首页，逐张卡片核对徽章文案、配色与剩余时间。
4. 删除临时库与临时数据。

不新增 `internal/store` 的测试：该包无 DB 测试夹具，SQL 列清单由 `go vet` 与冒烟验证覆盖。
