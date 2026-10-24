// 汇总 timing_proxy.js 的日志：每个连接的打开、最后一个数据块、关闭时刻，关闭时已空闲多久，以及哪一方先关闭。
// 用法: node timeline_summary.js <timing_proxy 日志>
// 注意：每个连接的第一个客户端数据块（约 1.5 KB）是 TLS ClientHello，不是请求。
const fs = require('fs');
const t = (ms) => new Date(ms).toISOString().slice(11, 23);
for (const l of fs.readFileSync(process.argv[2], 'utf8').trim().split('\n')) {
  const c = JSON.parse(l);
  const last = c.opened + c.ev[c.ev.length - 1][1];
  console.log(`#${c.conn} 打开${t(c.opened)} 最后数据${t(last)} 关闭${t(c.wall)} 存活${((c.wall - c.opened) / 1000).toFixed(1)}s 关闭时空闲${((c.wall - last) / 1000).toFixed(1)}s 先关闭:${c.closer} 数据块${c.ev.length}`);
}
