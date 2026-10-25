// 记录流式响应里每个片段的到达时刻，区分思考与正文。
// 用法: node stream_timing.js <端口> <接口: chat|messages|responses> <模型> [推理强度]
// API 密钥从环境变量 CPA_KEY 读取。
const http = require('http');
const [port, api, model, effort] = process.argv.slice(2);
const q = 'A train leaves at 3:40pm and travels 245 km at 70 km/h, then waits 25 minutes, then travels 120 km at 80 km/h. What time does it arrive? Think step by step.';
const bodies = {
  chat: { path: '/v1/chat/completions', body: { model, stream: true, messages: [{ role: 'user', content: q }], ...(effort ? { reasoning_effort: effort } : {}) } },
  messages: { path: '/v1/messages', body: { model, stream: true, max_tokens: 16000, messages: [{ role: 'user', content: q }], ...(effort ? { thinking: { type: 'enabled', budget_tokens: 4096 } } : {}) } },
  responses: { path: '/v1/responses', body: { model, stream: true, input: q, ...(effort ? { reasoning: { effort, summary: 'auto' } } : {}) } },
};
const { path, body } = bodies[api];
const data = JSON.stringify(body);
const t0 = Date.now();
const marks = [];
const req = http.request({ host: '127.0.0.1', port, path, method: 'POST', headers: {
  'content-type': 'application/json', 'content-length': Buffer.byteLength(data),
  authorization: `Bearer ${process.env.CPA_KEY}`, 'x-api-key': process.env.CPA_KEY, 'anthropic-version': '2023-06-01' } }, (res) => {
  let buf = '';
  res.on('data', (chunk) => {
    const t = Date.now() - t0;
    buf += chunk.toString('utf8');
    let i;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i).trim(); buf = buf.slice(i + 1);
      if (!line.startsWith('data:')) continue;
      const s = line.slice(5).trim();
      if (s === '[DONE]') continue;
      let kind = null, len = 0;
      try {
        const j = JSON.parse(s);
        const d = j.choices && j.choices[0] && j.choices[0].delta;
        if (d && d.reasoning_content) { kind = 'think'; len = d.reasoning_content.length; }
        else if (d && d.content) { kind = 'text'; len = d.content.length; }
        else if (j.type === 'content_block_delta' && j.delta.type === 'thinking_delta') { kind = 'think'; len = j.delta.thinking.length; }
        else if (j.type === 'content_block_delta' && j.delta.type === 'text_delta') { kind = 'text'; len = j.delta.text.length; }
        else if (j.type === 'response.reasoning_summary_text.delta' || j.type === 'response.reasoning_text.delta') { kind = 'think'; len = j.delta.length; }
        else if (j.type === 'response.output_text.delta') { kind = 'text'; len = j.delta.length; }
      } catch (e) { /* 非 JSON 行忽略 */ }
      if (kind) marks.push({ t, kind, len });
    }
  });
  res.on('end', () => {
    const total = Date.now() - t0;
    const sum = (k) => marks.filter((m) => m.kind === k);
    const th = sum('think'), tx = sum('text');
    const span = (a) => (a.length ? `${a[0].t}ms 到 ${a[a.length - 1].t}ms，${a.length} 片，${a.reduce((n, m) => n + m.len, 0)} 字符` : '无');
    console.log(`${api} ${model} effort=${effort || '-'} http=${res.statusCode} 总耗时 ${total}ms`);
    console.log(`  思考: ${span(th)}`);
    console.log(`  正文: ${span(tx)}`);
  });
});
req.end(data);
