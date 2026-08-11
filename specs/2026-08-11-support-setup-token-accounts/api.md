# API 变更

请求参数、响应结构、状态码集合均不变。唯一变化是三个端点共用的**账号集合定义**。

## 账号范围约定（更新）

原：

```
platform = 'anthropic' AND type = 'oauth' AND deleted_at IS NULL
```

现：

```
platform = 'anthropic' AND type IN ('oauth', 'setup-token') AND deleted_at IS NULL
```

即「live 的 Anthropic OAuth 账号」改为「live 的 Anthropic OAuth / Setup Token 账号」。
`apikey`、`upstream`、`bedrock`、`service_account` 类型以及非 anthropic 平台仍被排除。

## GET /v1/accounts

- 响应结构不变。
- 返回集合扩大：新增 live 的 `type = 'setup-token'` 账号，与 oauth 账号同样按 `id` 升序排列，
  窗口字段语义完全一致。
- 响应中不含账号类型字段，消费方无法（也无需）区分两类账号。

## GET /v1/accounts/{id}/window-usage

- 响应结构与状态码不变。
- 行为变更：`{id}` 为 live setup-token 账号时，此前返回 `404 account not found`，
  现在返回 `200` 及其窗口用量。

## GET /v1/accounts/{id}/monthly-usage

- 响应结构与状态码不变。
- 行为变更：`{id}` 为 live setup-token 账号时，此前返回 `404 account not found`，
  现在返回 `200` 及其账期内按用户消费明细。

## 状态码汇总（不变）

| 状态码 | 含义 |
| --- | --- |
| 200 | 成功 |
| 400 | 路径参数 `id` 非正整数；`month` 不匹配 `YYYY-MM` |
| 401 | token 缺失或错误 |
| 404 | `{id}` 不是 live 的 Anthropic OAuth / Setup Token 账号 |
| 502 | 回源数据库失败 |
