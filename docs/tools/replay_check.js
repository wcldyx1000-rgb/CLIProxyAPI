// 两轮 Responses 流式对话：第一轮从 output_item.done 收集输出项（与 pi 的做法相同），原样带进第二轮。
// 用法: node replay_check.js <端口> <模型>   API 密钥从环境变量 CPA_KEY 读取。
const http = require('http');
const [port, model] = process.argv.slice(2);

function stream(input) {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify({ model, stream: true, input, reasoning: { effort: 'high', summary: 'auto' },
      tools: [{ type: 'function', name: 'add', description: 'Add two integers', parameters: { type: 'object', properties: { a: { type: 'integer' }, b: { type: 'integer' } }, required: ['a', 'b'] } }] });
    const req = http.request({ host: '127.0.0.1', port, path: '/v1/responses', method: 'POST', headers: {
      'content-type': 'application/json', 'content-length': Buffer.byteLength(data), authorization: `Bearer ${process.env.CPA_KEY}` } }, (res) => {
      let buf = '', items = [], text = '', error = '';
      res.on('data', (c) => {
        buf += c.toString('utf8');
        let i;
        while ((i = buf.indexOf('\n')) >= 0) {
          const line = buf.slice(0, i).trim(); buf = buf.slice(i + 1);
          if (!line.startsWith('data:')) continue;
          try {
            const j = JSON.parse(line.slice(5));
            if (j.type === 'response.output_item.done') items.push(j.item);
            if (j.type === 'response.output_text.delta') text += j.delta;
            if (j.error) error = JSON.stringify(j.error).slice(0, 300);
          } catch (e) { /* 忽略非 JSON */ }
        }
      });
      res.on('end', () => resolve({ status: res.statusCode, items, text, error: error || (res.statusCode !== 200 ? buf.slice(0, 300) : '') }));
    });
    req.on('error', reject);
    req.end(data);
  });
}

(async () => {
  const user1 = { role: 'user', content: 'Use the add tool to compute 17 + 25.' };
  const r1 = await stream([user1]);
  const kinds = r1.items.map((it) => `${it.type}${it.type === 'reasoning' ? (it.encrypted_content ? '(有签名)' : '(无签名)') : ''}`);
  console.log(`${model} 第 1 轮 http=${r1.status} 输出项: ${kinds.join(', ')} ${r1.error}`);
  const call = r1.items.find((it) => it.type === 'function_call');
  const turn2 = [user1, ...r1.items];
  if (call) turn2.push({ type: 'function_call_output', call_id: call.call_id, output: '42' });
  else turn2.push({ role: 'user', content: 'Now say the result in one word.' });
  const r2 = await stream(turn2);
  console.log(`${model} 第 2 轮（回放上一轮的 reasoning 项）http=${r2.status} 回答: ${JSON.stringify(r2.text.slice(0, 80))} ${r2.error}`);
})();
