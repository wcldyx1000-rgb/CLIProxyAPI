// 用法: node send.js "<消息>" [new]   经本机 9333 调试端口向 Antigravity 页面发一条消息（不需要窗口焦点）
const http = require('http'); const fs = require('fs');
const [msg, mode] = [process.argv[2], process.argv[3]];
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
http.get('http://127.0.0.1:9333/json/list', (r) => { let d = ''; r.on('data', (c) => (d += c)); r.on('end', async () => {
  const page = JSON.parse(d).find((t) => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl); let id = 0; const pend = {};
  ws.onmessage = (m) => { const j = JSON.parse(m.data); if (pend[j.id]) pend[j.id](j); };
  const call = (method, params = {}) => new Promise((res) => { const i = ++id; pend[i] = res; ws.send(JSON.stringify({ id: i, method, params })); });
  const ev = async (e) => (await call('Runtime.evaluate', { expression: e, returnByValue: true, awaitPromise: true })).result.result.value;
  const shot = async (n) => fs.writeFileSync(n, Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).result.data, 'base64'));
  await new Promise((r) => (ws.onopen = r));
  if (mode === 'new') { console.log('new:', await ev(`(()=>{const e=[...document.querySelectorAll('button,div,span')].find(x=>x.children.length<3&&x.innerText&&x.innerText.trim()==='New Conversation');if(!e)return 'not found';e.click();return 'clicked'})()`)); await sleep(1500); }
  console.log('focus:', await ev(`(()=>{const e=document.querySelector('[role=combobox][contenteditable],div[role=combobox]');if(!e)return 'no editor';e.focus();return 'ok'})()`));
  await call('Input.insertText', { text: msg }); await sleep(500);
  await shot('before.png');
  for (const t of ['keyDown', 'keyUp']) await call('Input.dispatchKeyEvent', { type: t, key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, text: t === 'keyDown' ? '\r' : undefined });
  console.log('sent at', new Date().toISOString()); await sleep(8000); await shot('after.png'); ws.close();
}); });
