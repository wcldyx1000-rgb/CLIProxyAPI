// 生成假凭据（指向本地假上游）、模型 ID 清单和测试配置。只复制模型 ID，不复制任何令牌。
// 用法: node make_fake.js <真实凭据> <源配置> <输出目录> <假上游端口> <CPA端口> [凭据份数]
const fs = require('fs');
const path = require('path');
const [realCred, srcCfg, outDir, upPort, cpaPort, countArg] = process.argv.slice(2);
const count = Number(countArg || 1);
const real = JSON.parse(fs.readFileSync(realCred, 'utf8'));
const ids = real.antigravity_capability_hints.model_ids;
const base = `http://127.0.0.1:${upPort}`;
const authDir = path.join(outDir, 'auths');
fs.rmSync(authDir, { recursive: true, force: true });
fs.mkdirSync(authDir, { recursive: true });
fs.writeFileSync(path.join(outDir, 'ids.json'), JSON.stringify(ids));
for (let i = 1; i <= count; i++) {
  const cred = {
    type: 'antigravity',
    access_token: `fake-access-token-${i}`,
    refresh_token: `fake-refresh-token-${i}`,
    email: `fake${i}@example.invalid`,
    project_id: `fake-project-${i}`,
    expired: '2030-01-01T00:00:00+08:00',
    expires_in: 3599,
    timestamp: Date.now(),
    disabled: false,
    base_url: base,
    antigravity_capability_hints: { ...real.antigravity_capability_hints, endpoint: base },
  };
  fs.writeFileSync(path.join(authDir, `antigravity-fake${i}.json`), JSON.stringify(cred, null, 2));
}
const cfg = fs.readFileSync(srcCfg, 'utf8')
  .replace(/^(\s*port:\s*)\d+/m, `$1${cpaPort}`)
  .replace(/^(\s*auth-dir:\s*).*$/m, `$1"${authDir.replace(/\\/g, '/')}"`);
fs.writeFileSync(path.join(outDir, 'config.yaml'), cfg);
console.log('auths', count, 'ids', ids.length, 'base', base);
