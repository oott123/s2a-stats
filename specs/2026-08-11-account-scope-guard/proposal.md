# 需求：按账号 id 查询 usage 的接口需校验账号范围

修改按帐号 id 查询 usage 的几个接口，确保它们只能查询 `platform = 'anthropic' AND type = 'oauth'` 的帐号（对齐列表接口）。

## 补充确认（规划期与用户确认）

- 当 `{id}` 不是「live 的 Anthropic OAuth 账号」（不存在 / platform 非 anthropic / type 非 oauth / 已软删）时，接口返回 `404`，响应体 `{ "error": "account not found" }`。
- 不区分「账号不存在」与「账号不符合筛选条件」，两者返回同一个 404，避免通过接口探测非 Anthropic 账号的存在性。
