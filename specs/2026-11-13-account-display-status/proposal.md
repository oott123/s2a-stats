# 需求：账号展示状态改为 sub2api 后台口径

`GET /v1/accounts` 现在只回传 `accounts.status` 原始值（`active` / `error` / `disabled`），界面上除 active 外一律显示成灰色的原始英文状态。sub2api 后台的账号列表则把限流、过载、临时不可调度、手工暂停等调度态一并归纳进状态标签，用户看到的是「限流中」「暂停」这类可直接排障的结论。

需求：把 s2astats 界面的账号状态改成与 sub2api 后台一致的口径，并沿用同样的展示文案与配色。

## 关键定义与约定

- 判定次序与 sub2api 后台一致。`accounts.status` 不是 `active` 时原样返回（`error`、`disabled` 等）；否则依次判定限流、过载、临时不可调度，最后落到手工暂停。
- 状态取值：`active`、`rate_limited`、`overloaded`、`temp_unschedulable`、`paused`，以及原始状态透传值。前端文案：正常、限流中、过载中、临时不可调度、暂停。
- 判定期限字段：`rate_limit_reset_at`（限流）、`overload_until`（过载）、`temp_unschedulable_until`（临时不可调度）。时刻已过视为解除，不再影响状态。
- `paused` 对应 `schedulable = false`，无解除时刻。
- 响应新增 `status_until`：状态预计解除时刻，仅限流、过载、临时不可调度有值，其余为 `null`。
- 账号集合、三个端点的窗口口径与隐私字段集合均不变。

## 范围

- `internal/store/store.go` — 窗口查询增加 `schedulable` 与三个解除时刻列。
- `internal/stats/status.go` — 新增展示状态归纳。
- `internal/stats/service.go` — `AccountDTO` 增加 `status_until`，列表填充归纳后的状态。
- `internal/web/index.html` — 状态标签按新状态渲染文案、配色与剩余时间。
- 不动 `appscript.js`：它只用 `monthly-usage` 的用户消费，不读状态字段。
