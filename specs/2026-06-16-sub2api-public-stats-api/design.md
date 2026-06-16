# 设计

## 角色与定位

`s2astats` 是一个独立的 Go 服务。它直接以**只读**方式连接 sub2api 的 PostgreSQL,用 SQL 取数;对外提供一组**公众只读**的统计 API(用公共只读 token 鉴权)。不经过 sub2api 的 HTTP API,不持有管理员 key。

## 技术栈

- Go(`go = latest`,go.mod 声明 `go 1.23`),入口 `cmd/app/main.go`(`mise run server`)。
- HTTP 服务:标准库 `net/http` + Go 1.22+ `ServeMux` 方法+路径模式(如 `GET /v1/accounts/{id}/window-usage`),不引入 web 框架。
- 数据库:`github.com/jackc/pgx/v5` + `pgxpool`(Postgres 的现代标准驱动),连接池。
- 时区:`time.LoadLocation("Asia/Singapore")`;`main` 中 `import _ "time/tzdata"` 内嵌时区库以兼容精简容器。
- 配置:纯环境变量(mise.toml 已配置从 `.env.default` / `.env` 注入)。
- JSON 用标准库。

## 数据源:sub2api 的 PostgreSQL(已查证字段)

只读访问下列表/列(均来自 Ent 生成的 schema):

- **`accounts`**:`id`、`name`、`status`、`platform`(Anthropic 账号该列值为 `"anthropic"` —— 见 `internal/domain/constants.go` 常量 `PlatformAnthropic`;`account.go` 注释里的 "claude" 为过时注释)、`session_window_end`(timestamptz,5h 窗口重置时刻)、`extra`(jsonb)。`extra` 里的被动采样键(由 sub2api `syncActiveToPassive` 写入):
  - `session_window_utilization` —— 5h 窗口使用率(0–1 小数)
  - `passive_usage_7d_utilization` —— 7d 窗口使用率(0–1 小数)
  - `passive_usage_7d_reset` —— 7d 窗口重置时刻(unix 秒)
  - `passive_usage_sampled_at` —— 采样时刻(RFC3339,UTC)
- **`usage_logs`**(追加表,无软删除):`user_id`、`account_id`、`total_cost`(decimal(20,10))、`created_at`(timestamptz)。已有索引 `account_id`、`created_at`。**标准消费 = `SUM(total_cost)`**。
- **`users`**:`id`、`username`(可空,默认 `''`,不唯一)、`deleted_at`(软删除)。仅用于 JOIN 取 `username`,不读 `email`。

## 组件划分

```
cmd/app/main.go        装配:读配置 → 建 pgxpool/store/service → 起 HTTP → 优雅关停
internal/config        从环境变量读配置,加载时区;缺必填项即报错退出
internal/store         Postgres 只读访问层(pgxpool),三个查询方法
internal/stats         业务编排:窗口/账期时间计算 + 调 store + 组装 DTO + 缓存
internal/cache         通用 TTL 缓存(key → 值+过期时间,读时惰性失效)
internal/httpapi       路由、token 鉴权中间件、handler、JSON 响应
```

## store 查询(只读 SQL)

### 1. Anthropic 账号窗口(需求 1)—— 单查询,无 N+1

```sql
SELECT id, name, status,
       session_window_end,
       (extra->>'session_window_utilization')::float8   AS five_util,
       (extra->>'passive_usage_7d_utilization')::float8 AS seven_util,
       CASE WHEN extra->>'passive_usage_7d_reset' IS NULL THEN NULL
            ELSE to_timestamp((extra->>'passive_usage_7d_reset')::bigint) END AS seven_reset,
       extra->>'passive_usage_sampled_at'               AS sampled_at
FROM accounts
WHERE platform = 'anthropic'
ORDER BY id;
```

可空列扫到 `*float64` / `*time.Time` / `*string`。`sampled_at` 在 Go 侧按 RFC3339 解析,渲染为配置时区。

### 2. 账号内按用户标准消费(需求 2、3 共用)—— 精确时间区间 GROUP BY

```sql
SELECT ul.user_id, u.username, SUM(ul.total_cost)::float8 AS standard_cost
FROM usage_logs ul
LEFT JOIN users u ON u.id = ul.user_id
WHERE ul.account_id = $1
  AND ul.created_at >= $2
  AND ul.created_at <  $3
GROUP BY ul.user_id, u.username
ORDER BY standard_cost DESC;
```

`$2/$3` 为绝对时刻(`time.Time` 参数,Postgres 按 timestamptz 比较,与连接时区无关)。返回 `[]UserCost{UserID, Username, StandardCost}`。**无分页、无条数上限、username 随 JOIN 返回**。

## 时间计算(internal/stats)

- **5h / 7d 窗口(需求 2)**:窗口起点对齐账号 Anthropic 重置时刻,均为绝对时刻,与时区无关。
  - 先查 `accounts` 拿该账号的 `session_window_end`(5h reset)与 `passive_usage_7d_reset`(7d reset)。
  - `fiveStart = sessionWindowEnd - 5h`,`fiveEnd = now`;`sevenStart = sevenReset - 7d`,`sevenEnd = now`。
  - 对每个可用窗口各跑一次「按用户标准消费」查询(`[start, now)`)。某窗口缺重置时刻 → `available:false`,users 为空。
- **账期(需求 3)**:`BillingCycle(loc, year, month)` → `start = Date(year, month, 10, 0,0,0, loc)`、`end = start.AddDate(0,1,0)`(自动跨年),即 `[当月 10 日 00:00, 次月 10 日 00:00)` 按 Asia/Singapore。跑一次「按用户标准消费」查询。
- **用户名回退**:`username` 为空(或 JOIN 未命中)时用 `fmt.Sprintf("user-%d", userID)`;不暴露 email。

## 缓存

`internal/cache` 提供 `Get(key) (any, bool)` / `Set(key, value, ttl)`,map + RWMutex,读时判过期。三个公共端点各自以「端点名 + 规范化参数」为 key 缓存计算结果,默认 TTL 60s。直连 DB 后单次查询已很轻,缓存的作用是为公众流量挡住重复打库(尤其需求 2 的 7d 区间 GROUP BY)。

## 鉴权

`internal/httpapi` 中间件校验公共只读 token:接受 `Authorization: Bearer <token>` 或查询参数 `?token=<token>`(后者便于公开看板/浏览器直接嵌入),与配置的 `S2A_PUBLIC_TOKEN` 做 `subtle.ConstantTimeCompare`,不符返回 401。`GET /healthz` 不鉴权。

## 配置(环境变量)

| 变量 | 必填 | 默认 | 说明 |
|------|------|------|------|
| `S2A_LISTEN_ADDR` | 否 | `:8080` | 监听地址 |
| `S2A_SUB2API_DSN` | 是 | — | sub2api Postgres 只读 DSN,如 `postgres://ro_user:pass@host:5432/sub2api?sslmode=disable` |
| `S2A_PUBLIC_TOKEN` | 是 | — | 公共只读访问 token |
| `S2A_BILLING_TIMEZONE` | 否 | `Asia/Singapore` | 账期边界时区(仅需求 3 用到) |
| `S2A_CACHE_TTL` | 否 | `60s` | 公共结果缓存 TTL |
| `S2A_DB_TIMEOUT` | 否 | `15s` | 单次查询超时(context) |

缺任一必填项 → 启动即报错退出。`.env.default` 提供全部键的占位值。建议为本服务单独建一个**只读** Postgres 角色(仅 `SELECT` 权限)。

## 隐私

公共响应只暴露:账号 `id/name/status` 与窗口 utilization、用户名(`username` 或 `user-<id>`)、标准消费(USD)。**绝不**输出 email、token、并发、倍率等内部字段。

## 失败即拒(遵循 CLAUDE.md)

- `month` 严格匹配 `^\d{4}-(0[1-9]|1[0-2])$`,否则 400,不做猜测/补全。
- 账号 `id` 严格为正整数,否则 400。
- 缺 token / token 错 → 401。
- 数据库查询出错 → 502。
- 不对入参做容错性 trim / 大小写折叠 / 空串转默认。
