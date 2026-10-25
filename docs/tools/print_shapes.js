// 打印形状日志里每个请求的请求行、请求头和请求体形状。用法: node print_shapes.js <日志> [请求体最大字符数]
const fs = require('fs');
const max = Number(process.argv[3] || 2000);
for (const l of fs.readFileSync(process.argv[2], 'utf8').trim().split('\n')) {
  const j = JSON.parse(l);
  if (!j.method) { console.log('--', j.event, j.host || '', j.alpn || j.error || ''); continue; }
  console.log(`== ${j.method} ${j.host || ''}${j.path} query=${JSON.stringify(j.query)} bytes=${j.bodyBytes}`);
  console.log('  headers:', JSON.stringify(j.headers));
  console.log('  body:', JSON.stringify(j.body).slice(0, max));
}
