// 逐条打印生成请求的关键外层字段。用法: node gen_summary.js <形状日志>
const fs = require('fs');
const reqs = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(JSON.parse).filter((x) => /Generate/.test(x.path || ''));
for (const x of reqs) {
  const b = x.body;
  const r = b.request || {};
  const contents = r.contents && r.contents['<array>'];
  console.log(`${x.t}ms conn${x.conn} ${b.requestType} ${b.model} ${x.bodyBytes}B contents=${contents} keys=${Object.keys(b).join(',')} request.keys=${Object.keys(r).join(',')}`);
  console.log('   requestId:', b.requestId);
  if (r.labels) console.log('   labels:', JSON.stringify(r.labels));
  console.log('   generationConfig:', JSON.stringify(r.generationConfig), r.toolConfig ? 'toolConfig:' + JSON.stringify(r.toolConfig) : '');
}
