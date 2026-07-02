1. `.bar` 移除 `overflow: hidden`；填充 span 的 `border-radius` 从 `999px` 改为 `3px`（让三角能从底部伸出，同时保持圆角）。
2. 新增 `--indicator` CSS 变量（浅色 `#a855f7`，深色 `#a78bfa`）。
3. 新增 `.bar-indicator` 规则：紫色竖线，`position: absolute; left: 0; top: 0; bottom: 0; width: 2px; background: var(--indicator); border-radius: 1px; z-index: 1`。
4. 新增 `.bar-indicator::after` 规则：CSS border 倒三角，`left: 50%; bottom: -6px; transform: translateX(-50%)`，8px 宽 × 5px 高，紫色。
   - 若 `!w` 或 `!w.resets_at`，跳过时间线（已有 `"无采样"` 分支）。
   - 解析 `resets_at` 为时间戳：
     ```js
     const resetMs = new Date(w.resets_at).getTime();
     ```
   - 根据 `label` 判断窗口时长：
     ```js
     const durationMs = label === "7 天" ? 7 * 86400 * 1000 : 5 * 3600 * 1000;
     ```
   - 计算时间进度：
     ```js
     const windowStartMs = resetMs - durationMs;
     const nowMs = Date.now();
     const elapsed = nowMs - windowStartMs;
     const timeProgress = Math.max(0, Math.min(1, elapsed / durationMs));
     ```
3. 创建竖线元素：
   ```js
   const indicator = el("div", { class: "bar-indicator" });
   indicator.style.left = (timeProgress * 100) + "%";
   ```
4. 在 `.bar` 容器内追加 `indicator`（在 `fill` 之后）。

## 3. 验证

- 打开浏览器，用 `?token=...` 访问页面。
- 检查卡片中的进度条是否出现蓝色竖线。
- 检查竖线位置是否与窗口剩余时间大致吻合：
  - 5h 窗口，如剩余 2h30min → 50% 位置。
  - 7d 窗口，如已过 3 天 → 约 43% 位置。
- 检查深色主题下竖线颜色是否正常。
- 检查无采样数据的账号是否不显示竖线。
- 检查窗口已重置时竖线是否位于 100%。