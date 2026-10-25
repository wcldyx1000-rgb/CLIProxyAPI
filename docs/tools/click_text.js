// 用法: node click_text.js "<文本>" [...更多文本]  经 9333 调试端口依次点击页面上文字完全匹配的最内层元素（不需要窗口焦点）
const http = require('http');
const texts = process.argv.slice(2);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
http.get('http://127.0.0.1:9333/json/list', (r) => { let d = ''; r.on('data', (c) => (d += c)); r.on('end', async () => {
  const page = JSON.parse(d).find((t) => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl); let id = 0; const pend = {};
  ws.onmessage = (m) => { const j = JSON.parse(m.data); if (pend[j.id]) pend[j.id](j); };
  const call = (method, params = {}) => new Promise((res) => { const i = ++id; pend[i] = res; ws.send(JSON.stringify({ id: i, method, params })); });
  await new Promise((r) => (ws.onopen = r));
  for (const t of texts) {
    const expr = `(()=>{const all=[...document.querySelectorAll('*')].filter(e=>e.innerText&&e.innerText.trim()===${JSON.stringify(t)});
      const leaf=all.filter(e=>!all.some(o=>o!==e&&e.contains(o)));if(!leaf.length)return 'not found';
      const e=leaf[leaf.length-1];(e.closest('button,[role=button],[role=option],[role=menuitem]')||e).click();return 'clicked '+leaf.length})()`;
    const v = (await call('Runtime.evaluate', { expression: expr, returnByValue: true })).result.result.value;
    console.log(t, '->', v);
    await sleep(800);
  }
  ws.close();
}); });
