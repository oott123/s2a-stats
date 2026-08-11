# 公共 API 设计

所有业务端点前缀 `/v1`，需鉴权。金额单位 USD。时间为 RFC3339（Asia/Singapore 偏移）。

## 鉴权

每个 `/v1/*` 请求需携带公共只读 token，二选一：

- 请求头：`Authorization: Bearer <token>`
- 查询参数：`?token=<token>`

缺失或错误返回 `401`：

```json
{ "error": "unauthorized" }
```

错误响应统一形如 `{ "error": "<message>" }`，HTTP 状态码：入参非法 `400`、账号不在 Anthropic OAuth / Setup Token 集合内 `404`、回源失败 `502`、未鉴权 `401`。

---

## GET /v1/accounts

需求 1。列出所有 Anthropic 账号及其被动采样用量窗口。

**响应 200**

```json
{
  "accounts": [
    {
      "id": 123,
      "name": "anthropic-main",
      "status": "active",
      "sampled": true,
      "sampled_at": "2026-06-16T18:30:00+08:00",
      "five_hour": { "utilization": 0.455, "resets_at": "2026-06-16T22:30:00+08:00" },
      "seven_day": { "utilization": 0.652, "resets_at": "2026-06-21T09:00:00+08:00" }
    },
    {
      "id": 124,
      "name": "anthropic-backup",
      "status": "active",
      "sampled": false,
      "sampled_at": null,
      "five_hour": null,
      "seven_day": null
    }
  ]
}
```

- `utilization`：0–1 小数（被动采样的使用率）。
- 某窗口缺重置时间 → 该窗口为 `null`。账号从无被动采样 → `sampled:false` 且两窗口为 `null`。

---

## GET /v1/accounts/{id}/window-usage

需求 2。指定账号在其 5h、7d 窗口内，按用户的标准消费。窗口起点对齐该账号 Anthropic 重置窗口（`resets_at - 5h/7d`），终点为当前时间。

- 路径参数 `id`：账号数字 ID，必须是 live 的 Anthropic OAuth / Setup Token 账号（与 `GET /v1/accounts` 返回的集合一致）。

**响应 200**

```json
{
  "account_id": 123,
  "five_hour": {
    "available": true,
    "window_start": "2026-06-16T17:30:00+08:00",
    "window_end": "2026-06-16T18:30:00+08:00",
    "users": [
      { "username": "alice", "standard_cost": 1.5023 },
      { "username": "user-42", "standard_cost": 0.8100 }
    ]
  },
  "seven_day": {
    "available": true,
    "window_start": "2026-06-14T09:00:00+08:00",
    "window_end": "2026-06-16T18:30:00+08:00",
    "users": [
      { "username": "alice", "standard_cost": 42.0710 },
      { "username": "bob", "standard_cost": 12.3400 }
    ]
  }
}
```

- `users` 按 `standard_cost` 降序。
- 某窗口无被动采样重置时间 → `available:false`，无 `window_start/window_end`，`users` 为空数组。
- `username` 为空的用户回退为 `user-<id>`。

`{id}` 不是 live 的 Anthropic OAuth / Setup Token 账号（不存在 / platform 非 anthropic / type 既非 oauth 也非 setup-token / 已软删）→ **404**：

```json
{ "error": "account not found" }
```

---

## GET /v1/accounts/{id}/monthly-usage

需求 3。指定账号在指定账期内，按用户的标准消费。账期 = 以 Asia/Singapore 计的 `[当月 10 日 00:00, 次月 10 日 00:00)`。

- 路径参数 `id`：账号数字 ID，必须是 live 的 Anthropic OAuth / Setup Token 账号（与 `GET /v1/accounts` 返回的集合一致）。
- 查询参数 `month`：`YYYY-MM`（必填，严格校验）。`month=2026-06` 表示 2026-06-10 00:00 起的账期。

**响应 200**

```json
{
  "account_id": 123,
  "month": "2026-06",
  "cycle_start": "2026-06-10T00:00:00+08:00",
  "cycle_end": "2026-07-10T00:00:00+08:00",
  "users": [
    { "username": "alice", "standard_cost": 128.4500 },
    { "username": "bob", "standard_cost": 76.1200 },
    { "username": "user-99", "standard_cost": 3.0000 }
  ]
}
```

- `users` 按 `standard_cost` 降序，返回该账期内有消费的全部用户（无条数上限）。
- `username` 为空的用户回退为 `user-<id>`。

**入参错误示例（400）**

```json
{ "error": "invalid month, expected YYYY-MM" }
```

`{id}` 不是 live 的 Anthropic OAuth / Setup Token 账号 → **404**：

```json
{ "error": "account not found" }
```

`month` 参数非法优先返回 `400`，再校验账号范围。

---

## GET /healthz

存活探针，无需鉴权。返回 `200`：

```json
{ "status": "ok" }
```
