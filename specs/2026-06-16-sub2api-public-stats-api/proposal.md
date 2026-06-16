# sub2api 公共查询 API

## 背景

我有一个运行中的 sub2api 实例。我要实现一个独立的服务（本仓库 `s2astats`），直接以**只读**方式访问 sub2api 的 PostgreSQL 数据库取数，再以**公众可见**的只读 API 形式对外暴露一部分统计数据。

sub2api 的源码放在 `.references/sub2api`，可作为接口与字段的参考。

## 需要暴露的数据

1. **所有 Anthropic 账号的用量窗口**：列出后台所有 Anthropic 账号，给出每个账号的 5h / 7d 用量窗口（取**被动采样**的数据即可，即 utilization 与 reset 时间）。

2. **指定账号的窗口内按用户消费**：对指定账号，分别在其 **5h 窗口内**和 **7d 窗口内**，返回每个用户的使用数据 —— 用户名 + 该时间段内的**标准消费**。

3. **指定账号的账期内按用户消费**：以**每月 10 日 0 点**为账期分界，给定月份和账号，返回该账号在该账期内每个用户名的**标准消费**。

## 关键定义与约定（澄清后确定）

- **标准消费**：sub2api `usage_logs.total_cost` 的求和（不含任何分组/账号倍率的原始计费金额，单位 USD）。
- **维度**：需求 2、3 按 **user + account** 聚合，**不涉及** sub2api 的「分组(Group)」实体。
- **5h / 7d 窗口口径**：对齐账号的 Anthropic 重置窗口 —— 用被动采样里该账号的 `five_hour.resets_at` / `seven_day.resets_at` 反推窗口起点（起点 = `resets_at - 5h/7d`），与需求 1 暴露的窗口一致。
- **时区**：账期「10 日 0 点」边界及所有日期过滤一律按 **Asia/Singapore**（UTC+8，无 DST）。
- **数据源**：直连 sub2api 的 **PostgreSQL（只读角色）**，不经过 sub2api 的 HTTP API、不持有管理员 key。所有查询均为 `SELECT`。
- **访问控制**：公共 API 需要一个**公共只读 token**（仅用于对外鉴权，与数据库凭据分离）。
- **账号标识**：公共 API 中「指定账号」用 sub2api 的数字**账号 ID**。
- **用户名展示**：优先用 `username`；当 `username` 为空时回退为 `user-<id>`；**不暴露 email**（sub2api 中 `username` 可为空且不唯一，`email` 唯一但属 PII）。
