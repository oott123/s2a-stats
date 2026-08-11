# API 变更

仅影响两个按账号 id 查询的端点，新增一个错误响应。请求参数、成功响应结构均不变。

## 账号范围约定

`{id}` 必须指向一个 **live 的 Anthropic OAuth 账号**，即满足
`platform = 'anthropic' AND type = 'oauth' AND deleted_at IS NULL`
——与 `GET /v1/accounts` 返回的集合完全一致。

不满足时（账号不存在、platform 非 anthropic、type 非 oauth、已软删）返回：

**404 Not Found**

```json
{ "error": "account not found" }
```

两类情形返回同一个 404，不区分「不存在」与「不符合筛选条件」。

## GET /v1/accounts/{id}/window-usage

- 新增 `404`：`{id}` 不在上述账号集合内。
- 行为变更：此前该情形返回 `200` 且三个窗口均为 `available:false`，现在返回 `404`。
- 不变：账号在集合内但某窗口缺被动采样重置时刻 → `200`，该窗口 `available:false`、`users: []`。

## GET /v1/accounts/{id}/monthly-usage

- 新增 `404`：`{id}` 不在上述账号集合内。此前该情形会返回该账号的真实按用户消费明细。
- 校验顺序：`month` 参数非法优先返回 `400 invalid month, expected YYYY-MM`，
  再校验账号范围。
- 不变：账号在集合内且账期内无消费 → `200`，`users: []`。

## 状态码汇总

| 状态码 | 含义 |
| --- | --- |
| 200 | 成功 |
| 400 | 路径参数 `id` 非正整数；`month` 不匹配 `YYYY-MM` |
| 401 | token 缺失或错误 |
| 404 | `{id}` 不是 live 的 Anthropic OAuth 账号 |
| 502 | 回源数据库失败 |
