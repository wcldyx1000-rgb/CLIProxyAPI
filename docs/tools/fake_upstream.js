// 本地假上游：模拟 Antigravity 接口，生成请求按指定方式失败，并记录每个请求的到达时刻。
// 用法: node fake_upstream.js <端口> <日志> <模式> <模型ID清单文件>
// 模式: 403 | 500 | 503 | 429rate（RetryInfo 2s）| 429quota（QUOTA_EXHAUSTED，RetryInfo 3600s）| eof（发完响应头后断开）
const http = require('http');
const fs = require('fs');
const { shapeRequest } = require('./req_shape');
const [port, logPath, mode, idsPath] = process.argv.slice(2);
const ids = JSON.parse(fs.readFileSync(idsPath, 'utf8'));
const t0 = Date.now();
const log = (o) => fs.appendFileSync(logPath, JSON.stringify({ t: Date.now() - t0, ...o }) + '\n');

const googleError = (code, status, message, details) =>
  JSON.stringify({ error: { code, message, status, ...(details ? { details } : {}) } });

const failures = {
  403: [403, googleError(403, 'PERMISSION_DENIED', 'The caller does not have permission')],
  500: [500, googleError(500, 'INTERNAL', 'Internal error encountered.')],
  503: [503, googleError(503, 'UNAVAILABLE', 'The service is currently unavailable.')],
  '429rate': [429, googleError(429, 'RESOURCE_EXHAUSTED', 'Resource has been exhausted.', [
    { '@type': 'type.googleapis.com/google.rpc.RetryInfo', retryDelay: '2s' },
  ])],
  '429quota': [429, googleError(429, 'RESOURCE_EXHAUSTED', 'You have exhausted your capacity on this model.', [
    { '@type': 'type.googleapis.com/google.rpc.ErrorInfo', reason: 'QUOTA_EXHAUSTED', domain: 'cloudcode-pa.googleapis.com' },
    { '@type': 'type.googleapis.com/google.rpc.RetryInfo', retryDelay: '3600s' },
  ])],
};

http.createServer((req, res) => {
  const chunks = [];
  req.on('data', (d) => chunks.push(d));
  req.on('end', () => {
    const path = req.url.split('?')[0];
    const generate = /:(stream)?[gG]enerateContent$/.test(path);
    // 记录请求形状（规则见 req_shape.js），与桌面端解密代理的记录方式相同，便于逐字段对比
    // 生成请求额外记录 contents 的角色与部件类型序列（不含内容）
    let roles;
    try {
      roles = JSON.parse(Buffer.concat(chunks).toString('utf8')).request.contents
        .map((x) => `${x.role}:${(x.parts || []).map((p) => Object.keys(p).filter((k) => k !== 'thoughtSignature').join('+')).join(',')}`);
    } catch (e) { /* 不是生成请求 */ }
    log({ generate, ...(roles ? { roles } : {}), ...shapeRequest(req, Buffer.concat(chunks)) });
    if (path.endsWith(':fetchAvailableModels')) {
      res.writeHead(200, { 'content-type': 'application/json' });
      // CATALOG 指向一份真实的模型目录时原样返回，否则按 ID 清单编造只含枚举名的目录
      res.end(process.env.CATALOG ? fs.readFileSync(process.env.CATALOG)
        : JSON.stringify({ models: Object.fromEntries(ids.map((id, i) => [id, { model: `MODEL_PLACEHOLDER_M${i}` }])) }));
      return;
    }
    if (!generate) {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end('{}');
      return;
    }
    if (mode === 'eof') {
      res.writeHead(200, { 'content-type': 'text/event-stream' });
      res.flushHeaders();
      setTimeout(() => req.socket.destroy(), 50);
      return;
    }
    const [code, text] = failures[mode];
    res.writeHead(code, { 'content-type': 'application/json' });
    res.end(text);
  });
}).listen(+port, '127.0.0.1', () => console.log('fake upstream', port, mode));
