# 设计

## 概念

当前进度条（`meter()` 函数）展示的是 **API 使用率**（`utilization`），即已消耗的 API 配额占窗口总额度的百分比。用户要求增加一条**时间进度线**，展示的是**当前窗口时间已流逝的比例**，两者是不同维度的独立指标。

**时间进度** = now 到窗口起点的距离 / 窗口总时长 × 100%

- 窗口起点 = `resets_at - 窗口时长`
- 5h 窗口时长 = 5 小时，7d 窗口时长 = 7 天

## 变更范围

仅 `internal/web/index.html`，CSS 和 JS 两部分。

### CSS 改动

1. `.bar` 容器增加 `position: relative`，为内部绝对定位元素建立锚点。移除 `overflow: hidden`，填充条的 `border-radius` 从 `999px` 改为 `3px`，避免 overflow 裁剪底部的三角。
2. 新增 `--indicator` CSS 变量：浅色 `#a855f7`（紫），深色 `#a78bfa`。
3. 新增 `.bar-indicator` 样式：紫色竖线，`position: absolute`，`width: 2px`，`top: 0`，`bottom: 0`，`background: var(--indicator)`，`border-radius: 1px`，`z-index: 1`。
4. 新增 `.bar-indicator::after` 伪元素：用 CSS border 画一个倒三角（8px 宽 × 5px 高），`position: absolute` 定位于竖线底部居中，通过 `bottom: -6px` 延伸到进度条下方。
### JS 改动

`meter()` 函数：
1. 根据 `w.resets_at` 和窗口时长计算时间进度百分比。
   - 窗口时长通过 `label` 参数推断：`"5 小时" → 5h`，`"7 天" → 7d`
   - `windowStart = new Date(w.resets_at).getTime() - durationMs`
   - `elapsed = now - windowStart`
   - `timeProgress = elapsed / durationMs`
   - 范围限制在 `[0, 1]`（防止负值或超 100%）
2. 在 `.bar` 内追加一个 `<div class="bar-indicator">`，`left` 设置为 `timeProgress × 100%`。

### 边界情况

| 场景 | 行为 |
|------|------|
| `resets_at` 缺失（窗口无采样） | `meter()` 返回 `"无采样"`，不显示进度条，也不显示时间线 |
| 窗口尚未开始（`now < windowStart`） | 进度线位于 0% |
| 窗口已超越重置时刻（`now > resets_at`） | 进度线位于 100% |
| 窗口时长接近 0（罕见数据异常） | 进度线位于 0% |

## 不变的部分

- 后端 API 响应结构完全不修改。
- 进度条本身的 utilization 填充逻辑、颜色分级（绿/黄/红/灰）不变。
- 重置时间行、采样时间行不变。
- 深色/浅色主题：`bar-indicator` 使用 `var(--indicator)`，随主题自动切换。