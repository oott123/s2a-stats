// s2astats → Google Sheets 月度同步脚本
// 用法：整段复制到 Sheets 的 扩展程序 → Apps Script，改好 CONFIG 后运行 syncMonthlyUsage。
//
// 行为：取上个月（按 TIME_ZONE）各账号的 /v1/accounts/{id}/monthly-usage，
// 按用户名跨账号加总 API Cost，降序写入月份 Sheet（如 202609）：
// 保留第 1 行表头与最后一行（合计行），删除并重写中间数据区。
// A=用户名，B=API Cost，C..H=公式（当前行 r，末行=end）。

// 配置在 config.gs 中（顶层 const 在 Apps Script 里是全局的）。
// 若 CONFIG 未定义（漏建 config.gs），报错并中止。

// 主入口：拉取上月各账号月度数据，按用户加总，写入月份 Sheet。
function syncMonthlyUsage() {
  if (typeof CONFIG === 'undefined') {
    throw new Error('缺少配置：请新建 config.gs 并粘贴 config.js 的内容');
  }

  const { year, month } = previousMonth_();
  const monthParam = year + '-' + pad2_(month); // API 参数，如 2026-09
  const sheetName = String(year) + pad2_(month); // Sheet 名，如 202609

  const users = fetchMonthlyUsage_(monthParam);

  // 按用户名跨账号加总
  const totals = {};
  for (const user of users) {
    if (typeof user.username !== 'string' || !user.username ||
        typeof user.standard_cost !== 'number') {
      throw new Error('响应包含非法用户行: ' + JSON.stringify(user));
    }
    totals[user.username] = (totals[user.username] || 0) + user.standard_cost;
  }

  // API Cost 高的排前面
  const rows = Object.keys(totals)
    .map(function (name) { return [name, totals[name]]; })
    .sort(function (a, b) { return b[1] - a[1]; });

  writeToSheet_(sheetName, rows);
}

// 并发请求所有账号的月度数据，任何一步失败直接抛错。
function fetchMonthlyUsage_(monthParam) {
  const base = CONFIG.BASE_URL.replace(/\/+$/, '');
  const requests = CONFIG.ACCOUNT_IDS.map(function (id) {
    return {
      url: base + '/v1/accounts/' + id + '/monthly-usage?month=' + monthParam,
      headers: { Authorization: 'Bearer ' + CONFIG.TOKEN },
      muteHttpExceptions: true,
    };
  });

  const responses = UrlFetchApp.fetchAll(requests);
  const users = [];
  for (let i = 0; i < responses.length; i++) {
    const code = responses[i].getResponseCode();
    const body = responses[i].getContentText();
    if (code !== 200) {
      throw new Error('账号 ' + CONFIG.ACCOUNT_IDS[i] + ' 请求失败: HTTP ' + code + ' ' + body);
    }
    const data = JSON.parse(body);
    if (!data || !Array.isArray(data.users)) {
      throw new Error('账号 ' + CONFIG.ACCOUNT_IDS[i] + ' 响应格式异常: ' + body);
    }
    Array.prototype.push.apply(users, data.users);
  }
  return users;
}

// 覆写月份 Sheet：保留第 1 行（表头）和最后一行（合计行），重写中间数据区。
// A=用户名，B=API Cost，C..H=公式：不带 $ 的行号替换为当前行 r（从 2 起），
// $8 替换为实际末行行号 end；$D$4 不属于这两条规则，按原样保留。
function writeToSheet_(sheetName, rows) {
  const sheet = SpreadsheetApp.getActiveSpreadsheet().getSheetByName(sheetName);
  if (!sheet) {
    throw new Error('找不到 Sheet: ' + sheetName);
  }

  const lastRow = sheet.getLastRow();
  if (lastRow >= 3) {
    // 删除第 2 行到倒数第二行（旧数据区）
    sheet.deleteRows(2, lastRow - 2);
  }
  if (rows.length === 0) {
    return;
  }

  sheet.insertRowsAfter(1, rows.length);
  const end = sheet.getLastRow(); // 最后一行（$8 类引用的目标）

  const values = [];
  for (let i = 0; i < rows.length; i++) {
    const r = i + 2; // 当前行号
    values.push([
      rows[i][0],                           // A 用户名
      rows[i][1],                           // B API Cost
      '=0',     // C
      '=B' + r + '/$B$' + end,              // D
      '=MAX(C' + r + ':D' + r + ')',        // E
      '=E' + r + '/$E$' + end,              // F
      '=$G$' + end + '*F' + r,              // G
      '=G' + r + '/B' + r,                  // H
    ]);
  }
  sheet.getRange(2, 1, values.length, 8).setValues(values);

  // 数据区 B~H：等宽字体，不加粗（A 列用户名不动）
  const dataRange = sheet.getRange(2, 2, values.length, 7);
  dataRange.setFontFamily('Roboto Mono');
  dataRange.setFontWeight('normal');

  const costRange = sheet.getRange(2, 6, values.length, 6);
  costRange.setBackground('#ffe599');
}

// 上月 {year, month}，按 CONFIG.TIME_ZONE 计算。
function previousMonth_() {
  const now = new Date();
  const y = Number(Utilities.formatDate(now, CONFIG.TIME_ZONE, 'yyyy'));
  const m = Number(Utilities.formatDate(now, CONFIG.TIME_ZONE, 'M'));
  return m === 1 ? { year: y - 1, month: 12 } : { year: y, month: m - 1 };
}

function pad2_(n) {
  return ('0' + n).slice(-2);
}
