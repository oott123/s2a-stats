# 执行计划

按顺序实现。模块名 `s2astats`,go.mod 声明 `go 1.23`。数据源为 sub2api 的 PostgreSQL(只读)。

## 1. 项目骨架
- `go mod init s2astats`;`go get github.com/jackc/pgx/v5`。
- 建目录:`cmd/app`、`internal/{config,store,stats,cache,httpapi}`。
- 写 `.env.default`,包含全部 `S2A_*` 键的占位值(必填项留空占位)。

## 2. internal/config
- `Config` 结构含全部 `S2A_*` 字段 + 已解析的 `*time.Location`。
- `Load()`:读环境变量;必填项(`S2A_SUB2API_DSN`、`S2A_PUBLIC_TOKEN`)缺失返回错误;`time.LoadLocation` 解析时区;duration 字段用 `time.ParseDuration`,非法即报错。
- `main` 顶部 `import _ "time/tzdata"`。

## 3. internal/cache
- `Cache`:`map[string]entry{ value any; expiresAt time.Time }` + `sync.RWMutex`。
- `Get(key) (any, bool)`(读时判过期)、`Set(key, value, ttl)`。

## 4. internal/store(Postgres 只读访问层)
- `Store{pool *pgxpool.Pool; timeout time.Duration}`,`New(ctx, dsn, timeout)` 建 `pgxpool`。
- 类型:`AccountWindowRow{ID int64; Name, Status string; FiveUtil, SevenUtil *float64; FiveReset, SevenReset *time.Time; SampledAt *time.Time}`;`UserCost{UserID int64; Username string; StandardCost float64}`。
- 方法(每次查询包一层 `context.WithTimeout`):
  - `AnthropicAccountWindows(ctx) ([]AccountWindowRow, error)`:执行 design.md「需求 1」的 SQL(`session_window_end` 作 5h reset、`extra` JSONB 解析、`to_timestamp` 还原 7d reset);可空列扫到指针;`sampled_at` 文本按 RFC3339 解析为 `*time.Time`。
  - `UserStandardCost(ctx, accountID int64, from, to time.Time) ([]UserCost, error)`:执行 design.md「需求 2、3 共用」的 GROUP BY SQL;`username` 用 `pgtype` 处理可空(NULL/空串)。
- 所有金额、utilization 在 SQL 内 `::float8`,直接扫 `float64` / `*float64`。

## 5. internal/stats
- `windows.go`:
  - `BillingCycle(loc *time.Location, year, month int) (start, end time.Time)` → `start = time.Date(year, month, 10, 0,0,0,0, loc)`、`end = start.AddDate(0,1,0)`。
  - `WindowStart(reset time.Time, d time.Duration) time.Time` → `reset.Add(-d)`。
- `Service{store, cache, loc, cacheTTL, now func() time.Time}`(`now` 便于测试)。
- `AccountWindows(ctx)`(需求 1):缓存命中即返回;否则 `store.AnthropicAccountWindows` → 组装 DTO(utilization、resets_at、`sampled = SampledAt != nil`,某窗口缺 reset 或 util → 该窗口 null)→ 缓存。
- `WindowUsage(ctx, accountID)`(需求 2):从 `AnthropicAccountWindows` 结果(或单独查该账号行)取该账号 `FiveReset`/`SevenReset`;`now := s.now()`;对每个非空 reset:`start = WindowStart(reset, 5h/7d)`,`store.UserStandardCost(accountID, start, now)`;用户名空回退 `user-<id>`;缺 reset 的窗口 `available:false`,users 空数组。缓存。
- `MonthlyUsage(ctx, accountID, year, month)`(需求 3):`BillingCycle` 得 `[start,end)`;`store.UserStandardCost(accountID, start, end)`;用户名回退;`standard_cost = StandardCost`。缓存。
- DTO 中金额保留原始 float64,JSON 序列化即可。

## 6. internal/httpapi
- `auth` 中间件:取 `Authorization: Bearer` 或 `?token=`,与 `S2A_PUBLIC_TOKEN` 用 `subtle.ConstantTimeCompare` 比较;不符 401。
- handler:
  - `GET /v1/accounts` → `AccountWindows`。
  - `GET /v1/accounts/{id}/window-usage` → 解析 `id`(正整数,否则 400)→ `WindowUsage`。
  - `GET /v1/accounts/{id}/monthly-usage` → 解析 `id` + `month`(正则 `^\d{4}-(0[1-9]|1[0-2])$`,否则 400;拆出 year/month)→ `MonthlyUsage`。
  - `GET /healthz` → `{"status":"ok"}`,不鉴权。
- 用 `ServeMux` 注册方法+路径模式;统一 JSON 写出与错误封装(`{"error":...}`,DB 错误 502)。

## 7. cmd/app/main.go
- `import _ "time/tzdata"`;`config.Load` → `store.New` → `stats.Service` → mux → `http.Server`;监听 `SIGINT/SIGTERM` 优雅关停,关停时 `pool.Close()`。

## 8. 验证
- `go build ./...`、`go vet ./...`。
- `internal/stats/windows.go` 单元测试:账期边界(含 12 月跨年、闰年 2 月)、`WindowStart`,用固定 `loc` 与输入。
- 配置 `.env`(指向真实 sub2api 只读 DSN + 公共 token)后 `mise run server`,curl 实测:
  - `GET /v1/accounts`(带 token):窗口 utilization/reset 与后台一致;
  - `GET /v1/accounts/{id}/window-usage`:抽查某账号 5h/7d 各用户标准消费合计,与后台用量记录对照;
  - `GET /v1/accounts/{id}/monthly-usage?month=YYYY-MM`:核对账期边界 `[10 日 00:00, 次月 10 日 00:00)` 与用户名回退;
  - 无 token / 错 token → 401;非法 `month`、非法 `id` → 400。
- 用只读角色连接,确认服务无任何写操作;核对响应不含 email 等内部字段。
