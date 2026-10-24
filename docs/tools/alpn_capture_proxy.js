// 只转发 CONNECT 隧道，并记录 ClientHello 中的 SNI / ALPN / 扩展顺序，不解密内容。
const net = require('net');
const fs = require('fs');
const LOG = process.argv[2]; // 用法: node alpn_capture_proxy.js <日志文件路径>
const PORT = 18080;
const log = (o) => fs.appendFileSync(LOG, JSON.stringify({ t: new Date().toISOString(), ...o }) + '\n');

function parseHello(b) {
  try {
    if (b[0] !== 0x16 || b[5] !== 0x01) return { err: 'not-clienthello' };
    let p = 9; p += 2 + 32;
    p += 1 + b[p];
    p += 2 + b.readUInt16BE(p);
    p += 1 + b[p];
    const end = p + 2 + b.readUInt16BE(p); p += 2;
    const exts = []; let sni = null; let alpn = null;
    while (p + 4 <= end && p + 4 <= b.length) {
      const type = b.readUInt16BE(p), len = b.readUInt16BE(p + 2), d = p + 4;
      exts.push(type);
      if (type === 0 && d + 5 <= b.length) sni = b.slice(d + 5, d + 5 + b.readUInt16BE(d + 3)).toString();
      if (type === 16) {
        alpn = []; let q = d + 2; const e = d + len;
        while (q < e) { const l = b[q]; alpn.push(b.slice(q + 1, q + 1 + l).toString()); q += 1 + l; }
      }
      p = d + len;
    }
    return { sni, alpn: alpn === null ? 'NO-ALPN-EXTENSION' : alpn, exts };
  } catch (e) { return { err: String(e) }; }
}

net.createServer((c) => {
  c.once('data', (req) => {
    const m = /^CONNECT ([^ :]+):(\d+)/.exec(req.toString('latin1'));
    if (!m) { c.destroy(); return; }
    const up = net.connect(+m[2], m[1], () => {
      c.write('HTTP/1.1 200 Connection Established\r\n\r\n');
      let first = true;
      c.on('data', (d) => {
        if (first) { first = false; log({ target: `${m[1]}:${m[2]}`, ...parseHello(d) }); }
        up.write(d);
      });
      up.on('data', (d) => c.write(d));
    });
    up.on('error', () => c.destroy()); c.on('error', () => up.destroy());
    up.on('close', () => c.destroy()); c.on('close', () => up.destroy());
  });
}).listen(PORT, '127.0.0.1', () => console.log('listening', PORT));
