# Fable 进度条显示

## 需求

为 Anthropic 账号增加 **Fable 专属 7 天窗口（7d_oi）**的进度条显示。该窗口与已有的 5h/7d 窗口并列存在，数据已保存在 sub2api 数据库 `accounts.extra` JSONB 字段中（`passive_usage_7d_oi_utilization`、`passive_usage_7d_oi_reset`）。

- 账号列表卡片中增加 Fable 7 天窗口的使用率进度条（与时间线）。
- 账号详情（window-usage）中增加 "7 天 Fable 窗口" 用户消费表格，与 "5 小时窗口" / "7 天窗口" 并列。

## 范围

后端 `store` + `stats` 两层扩展以读取并暴露 Fable 数据；前端 `internal/web/index.html` 增加对应的进度条与详情表格。`httpapi` 路由无需新增。
