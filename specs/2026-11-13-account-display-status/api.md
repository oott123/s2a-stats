# API 变更

仅影响 `GET /v1/accounts`。请求参数不变，路径、状态码集合、其余两个端点均不变。

## GET /v1/accounts

- 每个账号新增 `status_until` 字段：状态预计解除的时刻（RFC3339，Asia/Singapore 偏移），
  仅限流、过载、临时不可调度有值，其余为 `null`。
- `status` 字段名不变，值域扩大：此前只回传 `accounts.status` 原始值，现在回传归纳后的展示状态。

## `status` 取值

| `status` | 含义 | 判定条件 |
| --- | --- | --- |
| `active` | 正常 | 原始状态为 `active`，无生效的期限字段，且 `schedulable = true` |
| `rate_limited` | 限流中 | 原始状态为 `active` 且 `rate_limit_reset_at` 晚于当前时刻 |
| `overloaded` | 过载中 | 原始状态为 `active` 且前述不成立，`overload_until` 晚于当前时刻 |
| `temp_unschedulable` | 临时不可调度 | 原始状态为 `active` 且前述均不成立，`temp_unschedulable_until` 晚于当前时刻 |
| `paused` | 暂停 | 原始状态为 `active` 且前述均不成立，`schedulable = false` |
| 原始状态透传 | 错误、停用等 | `accounts.status` 不是 `active` 时原样返回 |

判定次序为原始状态 → 限流 → 过载 → 临时不可调度 → 手工暂停。期限字段恰好等于当前时刻视为已解除。

## 响应示例

```json
{
  "accounts": [
    {
      "id": 123,
      "name": "anthropic-main",
      "status": "active",
      "status_until": null,
      "sampled": true,
      "sampled_at": "2026-06-16T18:30:00+08:00",
      "five_hour": { "utilization": 0.455, "resets_at": "2026-06-16T22:30:00+08:00" },
      "seven_day": { "utilization": 0.652, "resets_at": "2026-06-21T09:00:00+08:00" }
    },
    {
      "id": 124,
      "name": "anthropic-backup",
      "status": "rate_limited",
      "status_until": "2026-06-16T19:05:00+08:00",
      "sampled": false,
      "sampled_at": null,
      "five_hour": null,
      "seven_day": null
    }
  ]
}
```

## 不变的部分

- 账号集合谓词与 `GET /v1/accounts` 返回的账号数量不变。
- `sampled` / `sampled_at` / `five_hour` / `seven_day` / `fable` 语义与字段名不变。
- `GET /v1/accounts/{id}/window-usage`、`GET /v1/accounts/{id}/monthly-usage` 的请求与响应不变。
- 状态码集合不变：200 成功、400 入参非法、401 未鉴权、404 账号不在集合内、502 回源失败。
