// 只转发 CONNECT 隧道，记录每个到 daily-cloudcode-pa 的连接上的数据块时间线（方向、时间、字节数），不解密、不保存内容。
// 每条日志在连接关闭时写入：conn 编号、opened（打开时刻）、wall（关闭时刻）、closer（先关闭的一方）、ev（[方向, 距打开的毫秒, 字节数]）。
const net = require('net');
const fs = require('fs');
const LOG = process.argv[2]; // 用法: node timing_proxy.js <日志文件路径>，监听 127.0.0.1:18081
const PORT = 18081;
let seq = 0;
const out = (o) => fs.appendFileSync(LOG, JSON.stringify(o) + '\n');

net.createServer((c) => {
  c.once('data', (req) => {
    const m = /^CONNECT ([^ :]+):(\d+)/.exec(req.toString('latin1'));
    if (!m) { c.destroy(); return; }
    const host = m[1];
    const watch = host === 'daily-cloudcode-pa.googleapis.com';
    const id = ++seq;
    const t0 = process.hrtime.bigint();
    const opened = Date.now(); // 连接建立的墙钟时间，用来和发送时刻对齐
    const ev = [];
    const stamp = (dir, n) => { if (watch) ev.push([dir, Number((process.hrtime.bigint() - t0) / 1000000n), n]); };
    const up = net.connect(+m[2], host, () => {
      c.write('HTTP/1.1 200 Connection Established\r\n\r\n');
      c.on('data', (d) => { stamp('c', d.length); up.write(d); });
      up.on('data', (d) => { stamp('s', d.length); c.write(d); });
    });
    let closer = null; // 先发起关闭的一方：'server'（上游）或 'client'（被测程序）
    const done = () => { if (watch && ev.length) out({ conn: id, opened, wall: Date.now(), closer, ev }); ev.length = 0; };
    up.on('error', () => c.destroy()); c.on('error', () => up.destroy());
    up.on('end', () => { closer = closer || 'server'; }); c.on('end', () => { closer = closer || 'client'; });
    up.on('close', () => { closer = closer || 'server'; done(); c.destroy(); }); c.on('close', () => { closer = closer || 'client'; done(); up.destroy(); });
  });
}).listen(PORT, '127.0.0.1', () => console.log('listening', PORT));
