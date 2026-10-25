// 列出页面上文字含指定关键词的可点击元素文字。用法: node list_buttons.js <正则>
const http = require('http');
const re = process.argv[2];
http.get('http://127.0.0.1:9333/json/list', (r) => { let d = ''; r.on('data', (c) => (d += c)); r.on('end', async () => {
  const page = JSON.parse(d).find((t) => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((res) => (ws.onopen = res));
  ws.onmessage = (m) => { const j = JSON.parse(m.data); console.log(j.result.result.value); ws.close(); };
  ws.send(JSON.stringify({ id: 1, method: 'Runtime.evaluate', params: { returnByValue: true, expression:
    `[...new Set([...document.querySelectorAll('button,[role=button],[role=option],div,span')].filter(e=>e.children.length<3&&e.innerText&&new RegExp(${JSON.stringify(re)}).test(e.innerText)).map(e=>e.innerText.trim().slice(0,60)))].join(' | ')` } }));
}); });
