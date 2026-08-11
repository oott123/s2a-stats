# 设计：按账号 id 查询接口的账号范围校验

## 现状与问题

列表接口 `GET /v1/accounts` 通过 `store.AnthropicAccountWindows` 的 SQL 谓词
`platform = 'anthropic' AND type = 'oauth' AND deleted_at IS NULL` 限定了账号范围，
但两个按 id 查询的接口没有对齐这个范围：

- `GET /v1/accounts/{id}/window-usage`（`stats.Service.WindowUsage`）
  拉取全量 Anthropic 账号列表后在内存里查找 id。找不到时不报错，而是把三个窗口的 reset
  置为 nil，返回 `200` + 三个 `available:false`。行为上不泄露数据，但语义错误：
  非法 id 与「账号存在但无被动采样」不可区分。

- `GET /v1/accounts/{id}/monthly-usage`（`stats.Service.MonthlyUsage`）
  **完全不校验账号**，直接把 `accountID` 传给 `store.UserStandardCost`。
  `usage_logs.account_id` 上没有任何 platform/type/软删过滤，因此任意账号 id
  （api_key / setup-token / cookie 类型、其他 platform、已软删账号）的按用户消费明细
  都会被原样返回。这是本次修改要堵的越权读取。

## 目标

两个按 id 查询的接口在取数前先确认 `{id}` 落在与列表接口完全相同的账号集合内；
不在集合内则返回 `404 account not found`，不执行任何 `usage_logs` 查询。

## 设计

### 1. store：新增单账号查询，谓词单点定义

把账号筛选谓词与列结果集抽成共享的 SQL 片段常量，由列表查询和单账号查询共同拼接，
保证两者永远不会漂移：

```go
const anthropicAccountPredicate = `platform = 'anthropic' AND type = 'oauth' AND deleted_at IS NULL`

const accountWindowColumns = `id, name, status, session_window_end, ...`

var anthropicAccountWindowsSQL = `SELECT ` + accountWindowColumns +
    ` FROM accounts WHERE ` + anthropicAccountPredicate + ` ORDER BY id`

var anthropicAccountWindowSQL = `SELECT ` + accountWindowColumns +
    ` FROM accounts WHERE id = $1 AND ` + anthropicAccountPredicate
```

新增方法：

```go
// AnthropicAccountWindow 返回单个 live Anthropic OAuth 账号的窗口采样；
// 账号不存在或不符合筛选条件时返回 (nil, nil)。
func (s *Store) AnthropicAccountWindow(ctx context.Context, accountID int64) (*AccountWindowRow, error)
```

行扫描逻辑（含 `passive_usage_sampled_at` 的 RFC3339 解析）从
`AnthropicAccountWindows` 中提取为共享函数 `scanAccountWindowRow(rows pgx.Rows) (AccountWindowRow, error)`，
两个查询共用。

`AnthropicAccountWindows` 保留不变（列表接口仍需要全量）。

`UserStandardCost` / `UserStandardCostByModel` 的 SQL **不变**：
账号合法性由上层的存在性校验负责，不把 accounts 谓词塞进 usage_logs 查询里
——否则「不合规账号」与「零消费」在结果上无法区分，也就无法返回 404。

### 2. stats：哨兵错误 + 前置校验

```go
// ErrAccountNotFound 表示目标账号不在「live Anthropic OAuth 账号」集合内。
var ErrAccountNotFound = errors.New("account not found")
```

`dataStore` 接口增加 `AnthropicAccountWindow(ctx, accountID)`。

- `WindowUsage`：缓存未命中后调用 `AnthropicAccountWindow`。返回 nil → 直接
  `return nil, ErrAccountNotFound`。命中则用该行的三个 reset 计算窗口。
  这同时替换掉原先「拉全表 + 内存线性查找」的实现，单次 PK 查询更省。
- `MonthlyUsage`：缓存未命中后先调用 `AnthropicAccountWindow` 做存在性校验，
  nil → `ErrAccountNotFound`；通过后才执行 `UserStandardCost`。
  该接口不使用返回行的窗口字段，只取存在性。

**缓存策略**：只缓存成功响应，不缓存 404。`AnthropicAccountWindow` 是主键索引上的
单行查询，重复非法请求的成本可忽略，不值得引入负缓存及其失效复杂度。
校验放在缓存未命中之后：缓存命中意味着该 id 之前已通过校验。

### 3. httpapi：错误映射

`handleWindowUsage` 与 `handleMonthlyUsage` 在现有 502 分支前增加：

```go
if errors.Is(err, stats.ErrAccountNotFound) {
    writeError(w, http.StatusNotFound, "account not found")
    return
}
```

`monthly-usage` 的 `month` 参数校验保持在账号校验之前（handler 层入参校验先行），
即非法 month + 非法 id 返回 `400`。

## 不做的事

- 不引入兼容开关或「宽松模式」保留旧的 200-空响应行为。
- 不改动 `usage_logs` 查询的 `LEFT JOIN users`（历史消费仍归属已软删用户）。
- 前端 `internal/web/index.html` 无需改动：它只用列表接口返回的 id 调
  `window-usage`，正常路径不会触发 404，且已有 `!r.ok` 的通用错误处理。
- 不新增第三方依赖。
