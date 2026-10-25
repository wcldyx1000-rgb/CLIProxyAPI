// 按对话（trajectory_id）或 checkpoint 请求分组，打印桌面端生成请求的重试次数和间隔。
// 用法: node retry_gaps.js <形状日志>...
const fs = require('fs');
for (const f of process.argv.slice(2)) {
  const reqs = fs.readFileSync(f, 'utf8').trim().split('\n').map(JSON.parse).filter((x) => /Generate/.test(x.path || ''));
  const groups = {};
  for (const x of reqs) {
    const labels = x.body.request && x.body.request.labels;
    const key = `${x.body.requestType} ${labels && labels.trajectory_id ? labels.trajectory_id.slice(0, 8) : ''}`;
    (groups[key] = groups[key] || []).push(x);
  }
  console.log(f);
  for (const [k, v] of Object.entries(groups)) {
    const gaps = v.slice(1).map((x, i) => ((x.t - v[i].t) / 1000).toFixed(1));
    console.log(' ', k, `${v.length} 次`, '间隔(s):', gaps.join(', '), '连接:', [...new Set(v.map((x) => x.conn))].join(','));
  }
}
