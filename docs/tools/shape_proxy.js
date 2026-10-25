// 本地 CONNECT 代理：对指定主机解密但不转发，只记录请求形状（见 req_shape.js）并在本地返回响应；
// 其他主机原样转发隧道、不解密。解密的请求不会发往真实服务。
// 用法: node shape_proxy.js <日志> <证书.pem> <私钥.pem> [状态码，默认 503]
//   状态码为 200 时，生成请求得到一条本地编造的成功回复（固定文本 "ok"），可以继续下一轮对话；
//   其他状态码时生成请求一律返回该错误，用来观察客户端的重试。
//   环境变量 DEFAULT_MODEL 设置编造的模型目录里的默认模型（桌面端会优先沿用上次选过的模型）。
const net = require('net');
const tls = require('tls');
const http = require('http');
const fs = require('fs');
const { shapeRequest } = require('./req_shape');

const [logPath, certPath, keyPath, statusArg] = process.argv.slice(2);
const STATUS = Number(statusArg || 503);
const PORT = 18082;
const INTERCEPT = new Set(['daily-cloudcode-pa.googleapis.com', 'aicode.googleapis.com']);
const secureContext = tls.createSecureContext({ cert: fs.readFileSync(certPath), key: fs.readFileSync(keyPath) });
// 本地编造的最小响应（测试数据，不是真实账号信息），按接口方法名匹配
const MODEL_IDS = ['gemini-3.8-flash-medium', 'gemini-3.8-pro-high', 'claude-sonnet-4-6'];
const STUBS = {
  loadCodeAssist: { currentTier: { id: 'standard-tier' }, paidTier: { id: 'standard-tier', availableCredits: [] }, cloudaicompanionProject: 'capture-test-project' },
  listExperiments: {},
  // 字段名取自 language_server.exe 内的 FetchAvailableModelsResponse / ModelDetails 描述符
  fetchAvailableModels: {
    models: Object.fromEntries(MODEL_IDS.map((id, i) => [id, {
      displayName: id, model: `MODEL_PLACEHOLDER_M${20 + i}`, recommended: true, supportsImages: true, supportsThinking: true,
      maxTokens: 1048576, maxOutputTokens: 65536, quotaInfo: { remainingFraction: 1 },
      apiProvider: id.startsWith('claude') ? 'API_PROVIDER_ANTHROPIC_VERTEX' : 'API_PROVIDER_GOOGLE_GEMINI',
      modelProvider: id.startsWith('claude') ? 'MODEL_PROVIDER_ANTHROPIC' : 'MODEL_PROVIDER_GOOGLE',
    }])),
    defaultAgentModelId: process.env.DEFAULT_MODEL || MODEL_IDS[0],
    agentModelSorts: [{ displayName: 'Recommended', groups: [{ displayName: 'Models', modelIds: MODEL_IDS }] }],
  },
  retrieveUserQuotaSummary: {},
  fetchUserInfo: {},
  recordCodeAssistMetrics: {},
  recordClientEvent: {},
};
const t0 = Date.now();
let seq = 0;
const log = (o) => fs.appendFileSync(logPath, JSON.stringify({ t: Date.now() - t0, wall: new Date().toISOString(), ...o }) + '\n');

// 只用来解析解密后的 HTTP/1.1 请求，不监听端口
const inner = http.createServer((req, res) => {
  const chunks = [];
  req.on('data', (d) => chunks.push(d));
  req.on('end', () => {
    log({ conn: req.socket.connId, host: req.socket.servername, ...shapeRequest(req, Buffer.concat(chunks)) });
    // 启动阶段的查询接口返回本地编造的最小响应，让桌面端走到对话请求；对话请求一律返回错误。
    const path = req.url.split('?')[0];
    if (STATUS === 200 && /generateContent$/i.test(path)) {
      // 本地编造的一条成功回复（固定文本），让桌面端能进入下一轮对话
      const chunk = { response: { candidates: [{ content: { role: 'model', parts: [{ text: 'ok' }] }, finishReason: 'STOP' }],
        usageMetadata: { promptTokenCount: 10, candidatesTokenCount: 1, totalTokenCount: 11 }, modelVersion: 'local-capture' }, traceId: 'local' };
      res.writeHead(200, { 'content-type': 'text/event-stream' });
      res.end(`data: ${JSON.stringify(chunk)}\r\n\r\n`);
      return;
    }
    const stub = STUBS[path.slice(path.lastIndexOf(':') + 1)];
    const [code, body] = stub && !/generateContent$/i.test(path)
      ? [200, JSON.stringify(stub)]
      : [STATUS, JSON.stringify({ error: { code: STATUS, message: 'local capture proxy: request not forwarded', status: 'UNAVAILABLE' } })];
    res.writeHead(code, { 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) });
    res.end(body);
  });
});

net.createServer((client) => {
  client.once('data', (head) => {
    const m = /^CONNECT ([^ :]+):(\d+)/.exec(head.toString('latin1'));
    if (!m) { client.destroy(); return; }
    const [host, port] = [m[1], Number(m[2])];
    if (!INTERCEPT.has(host)) {
      const up = net.connect(port, host, () => { client.write('HTTP/1.1 200 Connection Established\r\n\r\n'); client.pipe(up); up.pipe(client); });
      up.on('error', () => client.destroy()); client.on('error', () => up.destroy());
      return;
    }
    client.write('HTTP/1.1 200 Connection Established\r\n\r\n');
    const secure = new tls.TLSSocket(client, { isServer: true, secureContext, ALPNProtocols: ['http/1.1'] });
    secure.connId = ++seq;
    secure.servername = host;
    secure.on('secure', () => log({ conn: secure.connId, host, event: 'tls-ok', alpn: secure.alpnProtocol || null }));
    secure.on('error', (e) => log({ conn: secure.connId, host, event: 'tls-error', error: e.message }));
    inner.emit('connection', secure);
  });
  client.on('error', () => {});
}).listen(PORT, '127.0.0.1', () => console.log('shape proxy', PORT, 'status', STATUS));
