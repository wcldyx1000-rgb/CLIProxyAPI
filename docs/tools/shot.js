// 截取 Antigravity 页面截图。用法: node shot.js <文件名>
const http = require('http'); const fs = require('fs');
http.get('http://127.0.0.1:9333/json/list', (r) => { let d = ''; r.on('data', (c) => (d += c)); r.on('end', async () => {
  const page = JSON.parse(d).find((t) => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res) => (ws.onopen = res));
  ws.onmessage = (m) => { const j = JSON.parse(m.data); fs.writeFileSync(process.argv[2], Buffer.from(j.result.data, 'base64')); ws.close(); };
  ws.send(JSON.stringify({ id: 1, method: 'Page.captureScreenshot', params: { format: 'png' } }));
}); });
