// 本地假上游：模拟 Antigravity 接口，生成请求按指定方式失败，并记录每个请求的到达时刻。
// 用法: node fake_upstream.js <端口> <日志> <模式> <模型ID清单文件>
// 模式: 403 | 500 | 503 | 429rate（RetryInfo 2s）| 429quota（QUOTA_EXHAUSTED，RetryInfo 3600s）| eof（发完响应头后断开）
const http = require('http');
const fs = require('fs');
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
  let body = '';
  req.on('data', (d) => { body += d; });
  req.on('end', () => {
    const path = req.url.split('?')[0];
    const generate = /:(stream)?[gG]enerateContent$/.test(path);
    log({ path, generate, bytes: body.length });
    if (path.endsWith(':fetchAvailableModels')) {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ models: Object.fromEntries(ids.map((id) => [id, {}])) }));
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
