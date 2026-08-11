# 设计：账号集合纳入 Anthropic setup-token 账号

## 现状与问题

`internal/store/store.go` 中的单点谓词限定了整个服务的账号集合：

```go
const anthropicAccountPredicate = `platform = 'anthropic' AND type = 'oauth' AND deleted_at IS NULL`
```

三个端点（`GET /v1/accounts`、`{id}/window-usage`、`{id}/monthly-usage`）共用它。
`type = 'oauth'` 把 sub2api 中另一类同样受 Anthropic 5h/7d 窗口限额约束的账号
——`type = 'setup-token'`——挡在了外面：这类账号既不出现在列表里，按 id 查询也一律 404。

## setup-token 与 oauth 的等价性（依据 `.references/sub2api`）

- `backend/internal/domain/constants.go:31` 定义 `AccountTypeSetupToken = "setup-token"`
  （注意是带连字符的字面量，不是下划线）。
- `backend/internal/service/account.go:1777` 的 `IsAnthropicOAuthOrSetupToken()`
  把 `platform == anthropic && (type == oauth || type == setup-token)` 视为同一类，
  注释明确「仅这两类账号支持 5h 窗口额度控制和会话数量控制」。
- `backend/internal/service/account_usage_service.go` 的 `GetPassiveUsage`
  以 `IsAnthropicOAuthOrSetupToken()` 为准入条件，两类账号共用
  `estimateSetupTokenUsage` 构建 5h 窗口，并共用 `passive_usage_7d_*` /
  `passive_usage_7d_oi_*` / `passive_usage_sampled_at` 这几个 `extra` 键。

结论：本服务读取的所有列（`session_window_end`、`extra` 中的 utilization/reset/sampled_at）
对 setup-token 账号的语义与 oauth 完全一致。差异仅存在于 sub2api 的**主动**查询路径
（setup-token 无 profile scope，无法调用上游 usage API），而本服务只读被动采样结果，
不受影响。

## 设计

把谓词中的等值判断改为集合判断，其余一律不动：

```go
const anthropicAccountPredicate = `platform = 'anthropic' AND type IN ('oauth', 'setup-token') AND deleted_at IS NULL`
```

该常量已被 `anthropicAccountWindowsSQL` 与 `anthropicAccountWindowSQL` 共同拼接，
所以列表接口与两个按 id 查询接口的账号集合自动保持一致，不存在漂移。

不做的事：

- 不新增 DTO 字段。`AccountDTO` 保持 `id/name/status/sampled/sampled_at/five_hour/seven_day/fable`。
- 不新增配置开关。账号集合是固定的领域规则，不是可配置项。
- 不为 setup-token 做任何采样字段回退或特判——它与 oauth 走完全相同的代码路径。

## 影响面

- `GET /v1/accounts`：返回条目增多，包含 live 的 setup-token 账号。
- `GET /v1/accounts/{id}/window-usage`、`{id}/monthly-usage`：setup-token 账号的 id
  从 `404 account not found` 变为正常返回数据。
- 缓存：`internal/cache` 按 endpoint+参数键缓存，谓词变更不影响键结构；
  进程重启后自然生效，无需清理逻辑。
- 隐私：暴露字段集合不变，仅账号集合扩大。
