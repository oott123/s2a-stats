# 执行计划

## 步骤 1 — `internal/store/store.go`：抽取共享 SQL 片段与行扫描

1. 新增常量 `anthropicAccountPredicate`，值为
   `platform = 'anthropic' AND type = 'oauth' AND deleted_at IS NULL`。
2. 把 `anthropicAccountWindowsSQL` 的 SELECT 列清单抽为常量 `accountWindowColumns`
   （即现有的 9 个表达式：`id, name, status, session_window_end, five_util, seven_util,
   seven_reset, fable_util, fable_reset, sampled_at`，保持原顺序与原表达式不变）。
3. 把 `anthropicAccountWindowsSQL` 改为 `var`，由
   `` `SELECT ` + accountWindowColumns + ` FROM accounts WHERE ` + anthropicAccountPredicate + ` ORDER BY id` `` 拼接。
4. 新增 `var anthropicAccountWindowSQL = `SELECT ` + accountWindowColumns + ` FROM accounts WHERE id = $1 AND ` + anthropicAccountPredicate`。
5. 把 `AnthropicAccountWindows` 循环体内的 `rows.Scan` + `sampled_at` 解析提取为
   `func scanAccountWindowRow(rows pgx.Rows) (AccountWindowRow, error)`，
   需 import `github.com/jackc/pgx/v5`。改写 `AnthropicAccountWindows` 调用它。
6. 新增方法：

   ```go
   // AnthropicAccountWindow 返回单个 live Anthropic OAuth 账号的窗口采样数据。
   // 账号不存在或不满足 platform/type/软删筛选时返回 (nil, nil)。
   func (s *Store) AnthropicAccountWindow(ctx context.Context, accountID int64) (*AccountWindowRow, error)
   ```

   实现：`context.WithTimeout(ctx, s.timeout)` → `s.pool.Query(ctx, anthropicAccountWindowSQL, accountID)`
   → `defer rows.Close()` → `if !rows.Next() { return nil, rows.Err() }`
   → `scanAccountWindowRow` → 返回 `&row`。错误用 `fmt.Errorf("query anthropic account window: %w", err)` 包装。

## 步骤 2 — `internal/stats/service.go`：哨兵错误与前置校验

1. import `errors`。新增导出哨兵：

   ```go
   // ErrAccountNotFound 表示目标账号不在 live Anthropic OAuth 账号集合内。
   var ErrAccountNotFound = errors.New("account not found")
   ```

2. `dataStore` 接口增加
   `AnthropicAccountWindow(ctx context.Context, accountID int64) (*store.AccountWindowRow, error)`。
3. 改写 `WindowUsage`：缓存未命中后改调 `s.store.AnthropicAccountWindow(ctx, accountID)`；
   `err != nil` 原样返回；`acct == nil` → `return nil, ErrAccountNotFound`。
   删除原先的全量列表拉取与内存线性查找，以及 `acct != nil` 的条件分支
   （此后 `acct` 必非 nil，直接用 `acct.FiveReset` / `acct.SevenReset` / `acct.FableReset`）。
4. 改写 `MonthlyUsage`：缓存未命中后、`BillingCycle` 计算之前，插入
   `acct, err := s.store.AnthropicAccountWindow(ctx, accountID)`；`err != nil` 返回；
   `acct == nil` → `return nil, ErrAccountNotFound`。该行的窗口字段不使用，用 `_` 承接返回值
   （`if acct, err := ...; err != nil { return nil, err } else if acct == nil { ... }` 或分两句写，
   以不触发 unused 变量为准）。
5. 缓存写入位置不动：仅成功路径 `s.cache.Set`。

## 步骤 3 — `internal/httpapi/httpapi.go`：404 映射

1. `handleWindowUsage`：在 `err != nil` 判断前插入
   `if errors.Is(err, stats.ErrAccountNotFound) { writeError(w, http.StatusNotFound, "account not found"); return }`。
2. `handleMonthlyUsage`：同上，插在现有 502 分支之前；`month` 校验位置不变（仍在调用 svc 之前）。
3. `errors` 已在 import 中，无需新增。

## 步骤 4 — 测试

### `internal/stats/service_test.go`（新建）

实现 `dataStore` 的 fake，字段：`accounts map[int64]*store.AccountWindowRow`、
`costCalls []struct{accountID int64; from, to time.Time; model string}`、可选注入 error。

用例：

1. `TestWindowUsageAccountNotFound` — fake 对该 id 返回 `(nil, nil)`；
   断言 `errors.Is(err, stats.ErrAccountNotFound)`、`resp == nil`、
   **且 `UserStandardCost` / `UserStandardCostByModel` 均未被调用**（`len(costCalls) == 0`）。
2. `TestMonthlyUsageAccountNotFound` — 同上断言，重点验证没有触达 `usage_logs` 查询。
3. `TestWindowUsageAccountFound` — fake 返回带三个 reset 的行；断言三个窗口
   `Available == true`、`WindowStart` 等于 `reset - 5h/7d/7d`、Fable 窗口的调用带上
   `model == "claude-fable-5"`。
4. `TestWindowUsagePartialSampling` — 账号存在但 `SevenReset == nil`；
   断言 `SevenDay.Available == false` 且 200 语义（无 error），与「账号不存在」区分开。
5. `TestMonthlyUsageAccountFound` — 断言 `UserStandardCost` 收到的 `from/to` 等于
   `BillingCycle(loc, year, month)`，响应 `Month`/`CycleStart`/`CycleEnd` 正确。
6. `TestNotFoundNotCached` — 对同一个不存在 id 连续请求两次，断言
   `AnthropicAccountWindow` 被调用两次（负结果不入缓存），且两次都返回 `ErrAccountNotFound`。

Service 构造：`stats.New(fake, cache.New(), loc, ttl)`，用 `Asia/Singapore`；
如需固定时间，参照包内 `now func() time.Time` 字段（同包测试可直接赋值）。

### `internal/httpapi/httpapi_test.go`（修改）

1. `fakeSvc` 增加字段控制返回 `stats.ErrAccountNotFound`（如 `notFound bool`）。
2. 新增 `TestAccountNotFound`：`notFound = true` 时
   `GET /v1/accounts/123/window-usage` 与 `GET /v1/accounts/123/monthly-usage?month=2026-06`
   均返回 `404`，响应体 `{"error":"account not found"}`。
3. 新增断言：`notFound = true` 且 `month` 非法（如 `2026-13`）时返回 `400`
   （入参校验优先于账号校验）。
4. 现有 `TestAccountID`、`TestMonthValidation` 不变（fake 默认不返回 not-found）。

## 步骤 5 — 文档与验证

1. 更新 `specs/2026-06-16-sub2api-public-stats-api/api.md`：
   在 `window-usage`、`monthly-usage` 两节补充 404 说明，并在顶部「错误响应统一形如」
   一行的状态码列表中加入 `账号不在 Anthropic OAuth 集合内 404`。
2. 检查 `CLAUDE.md` 的「Domain rules」：`Account selection (req 1)` 一条现在适用于全部三个端点，
   把「(req 1)」表述改为覆盖三端点的统一账号筛选规则。
3. 运行 `gofmt -w` 于所有改动文件。
4. `go build ./... && go vet ./... && go test ./...` 全绿。
