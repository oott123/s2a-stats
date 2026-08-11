# 执行计划

## 步骤 1 — `internal/store/store.go`：放宽账号谓词

把第 59 行的常量改为：

```go
const anthropicAccountPredicate = `platform = 'anthropic' AND type IN ('oauth', 'setup-token') AND deleted_at IS NULL`
```

字面量必须是 `setup-token`（连字符），对应 sub2api `domain.AccountTypeSetupToken`。

`anthropicAccountWindowsSQL` 与 `anthropicAccountWindowSQL` 均由该常量拼接，无需改动。
`AnthropicAccountWindows`、`AnthropicAccountWindow`、`UserStandardCost*` 全部保持原样。

本步骤为本次改动的全部代码变更。`internal/stats`、`internal/httpapi`、`internal/cache`
不需要任何修改。

## 步骤 2 — 文档

1. `CLAUDE.md` 第 33 行「Account selection」条目：
   - 谓词改为 `platform = 'anthropic' AND type IN ('oauth','setup-token') AND deleted_at IS NULL`；
   - 「Only Anthropic OAuth accounts — api_key/setup-token/cookie types are excluded」
     改为「Only Anthropic OAuth / Setup Token accounts — apikey/upstream/bedrock/service_account 等类型被排除」；
   - 404 说明中的「非 Anthropic 账号」表述保持不变（语义仍成立）。
2. `specs/2026-06-16-sub2api-public-stats-api/api.md`：
   - 第 18 行「账号不在 Anthropic OAuth 集合内 `404`」→「账号不在 Anthropic OAuth / Setup Token 集合内 `404`」；
   - 第 62、106 行「必须是 live 的 Anthropic OAuth 账号」→「必须是 live 的 Anthropic OAuth / Setup Token 账号」；
   - 第 94 行括号内的条件「type 非 oauth」→「type 既非 oauth 也非 setup-token」；
   - 第 134 行同步措辞。
3. `specs/2026-08-11-account-scope-guard/` 下的文档不改动——它记录的是当时那次变更，
   账号集合的最新定义以本 spec 的 `api.md` 为准。

## 步骤 3 — 验证

1. 对改动过的 `.go` 文件执行 `gofmt -w`。
2. `go build ./... && go vet ./... && go test ./...` 全绿。

不新增测试：账号集合由一条 SQL 谓词表达，`internal/store` 无 DB 测试夹具，
上层 `internal/stats` 的测试使用 fake `dataStore`、不经过 SQL，
构造一个断言谓词字符串内容的测试只是复述实现，没有回归价值。

3. 人工验证（需连到真实只读库）：`mise run server` 后
   - `GET /v1/accounts` 中出现此前缺失的 setup-token 账号；
   - 用其中一个 setup-token 账号 id 请求 `/window-usage` 与 `/monthly-usage`，
     均返回 `200` 而非 `404`；
   - 用一个 `apikey` 类型账号 id 请求上述两个端点，仍返回 `404 account not found`。
