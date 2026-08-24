# 设计

## 概念

Anthropic 为 Fable 模型家族（`claude-fable-5` 及其变体）提供了一个**独立的 7 天配额窗口**，与常规的账号级 7 天窗口（`7d`）并行计算。该窗口在响应头中以 `7d_oi`（`overage_included`）标识，字段名为 `seven_day_overage_included`。

在 s2astats 中，sub2api 的被动采样已将 Fable 7d 数据写入 `accounts.extra`：
- `passive_usage_7d_oi_utilization` —— 浮点数，0–1
- `passive_usage_7d_oi_reset` —— Unix 秒级时间戳

本次改动将这两个字段接入只读统计链路，使其与现有的 5h/7d 窗口在 UI 中并列展示。

## 变更范围

### 后端

1. **`internal/store/store.go`**
   - `AccountWindowRow` 新增 `FableUtil *float64` 和 `FableReset *time.Time`。
   - `anthropicAccountWindowsSQL` 新增两列：`extra->>'passive_usage_7d_oi_utilization'` 和 `extra->>'passive_usage_7d_oi_reset'`。
   - 扫描逻辑新增两个字段绑定。

2. **`internal/stats/service.go`**
   - `AccountDTO` 新增 `Fable *WindowDTO`（账号列表卡片使用）。
   - `WindowUsageResponse` 新增 `SevenDayFable WindowUsageDTO`（详情页使用）。
   - `AccountWindows` 方法中，对每一行调用 `window(r.FableUtil, r.FableReset)` 生成 `Fable` 字段。
   - `WindowUsage` 方法中，新增逻辑：读取 `FableReset`，调用 `windowUsage` 计算 7d Fable 区间内的按用户消费，并填充到 `resp.SevenDayFable`。
   - 缓存 key 保持不变（`window-usage:%d` 已按账号聚合三个窗口，无需拆分）。

3. **`internal/httpapi/httpapi.go`**
   - 无需新增路由，现有 `/v1/accounts/{id}/window-usage` 的响应体自然扩展。

### 前端

1. **`internal/web/index.html`**
   - **账号列表卡片**：在 meter 区域增加第 3 个进度条 —— `7d F`（Fable 7 天窗口），颜色使用琥珀色（`#f59e0b` / `#fbbf24`）。时间线计算与现有 7d 逻辑相同：窗口时长 = 7 天。
   - **账号详情（window-usage）**：在 `.windows` 容器中追加第 3 个 `userTable("7 天 Fable 窗口", data.seven_day_fable)`，使用 CSS grid 的 `1fr 1fr 1fr` 布局（桌面端三列，移动端单列）。

### API 响应变化

`GET /v1/accounts` 中每个 `AccountDTO` 新增：
```json
{
  "fable": {
    "utilization": 0.42,
    "resets_at": "2026-07-18T12:00:00+08:00"
  }
}
```

`GET /v1/accounts/{id}/window-usage` 响应体新增：
```json
{
  "seven_day_fable": {
    "available": true,
    "window_start": "2026-07-04T12:00:00+08:00",
    "window_end":   "2026-07-11T12:00:00+08:00",
    "users": [...]
  }
}
```

## 边界情况

| 场景 | 行为 |
|------|------|
| `passive_usage_7d_oi_reset` 缺失 | `AccountDTO.Fable` 为 `nil`（同 5h/7d 的无采样处理）；`WindowUsageDTO` 回退为普通 7d 窗口（`SevenReset`，`available:true`）；`SevenReset` 也缺失时保持 `available:false`（见 2026-08-24-fable-window-fallback）。 |
| 窗口已重置（`now > resets_at`） | 时间线位于 100%。
| 首次接入后旧缓存命中 | 旧缓存数据结构不含 `SevenDayFable`；缓存 TTL 到期后自然刷新为新结构。 |

## 不变的部分

- PostgreSQL 表结构不改，只增加 SELECT 列。
- `UserStandardCost` 查询复用，不新增 SQL。
- 主题切换、进度条颜色分级逻辑（绿/黄/红/灰）不变。
- 限流/调度逻辑：s2astats 是只读服务，完全不涉及。
