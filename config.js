// ==== 配置（与主脚本分开存放）====
// 在 Apps Script 编辑器里单独建一个文件（如 config.gs）粘贴本段；
// 顶层 const 在 Apps Script 中是全局的，主脚本（appscript.gs）可直接读到 CONFIG。

const CONFIG = {
  // s2astats 实例地址，不带尾斜杠
  BASE_URL: 'https://stats.example.com',
  // 公共只读 token
  TOKEN: 'your-public-token',
  // 要汇总的账号 ID 列表（GET /v1/accounts 返回的 id）
  ACCOUNT_IDS: [1, 2],
  // “上月”按此时区计算（与账期时区一致）
  TIME_ZONE: 'Asia/Singapore',
};
