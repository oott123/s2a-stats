# 需求：账号集合需要支持 Anthropic setup-token 类型

现在会过滤 `anthropic` & `oauth` 的帐号，但 anthropic 还有一种 `setup-token` 的帐号，也需要支持起来。

## 补充确认（规划期与用户确认）

- `GET /v1/accounts` 响应**不新增** `type` 字段，保持现有 `AccountDTO` 结构不变。
  两类账号的被动采样字段完全一致，消费方无需区分。
