// 把一个 HTTP 请求转成“形状”：保留请求行、请求头顺序和 JSON 字段顺序，去掉令牌和对话内容。
// 桌面端的解密代理和本项目的假上游共用这套规则，两边的结果才能逐字段对比。

// 这些字段的取值是枚举或版本信息，记录原值；其余字符串只记长度和格式类别。
// requestId 与 labels 里的值是随机 ID、枚举或布尔字符串，不含凭据和对话内容，记原值以便对比格式。
const KEEP_VALUE_KEYS = new Set(['model', 'requestType', 'userAgent', 'role', 'type', 'mode', 'alt',
  'requestId', 'request_id', 'model_enum', 'last_step_index', 'used_claude', 'used_claude_conservative', 'used_non_gemini_model',
  'thinkingLevel', 'cascade_id', 'root_cascade_id', 'trajectory_id', 'last_execution_id', 'mode']);
// 这些请求头的取值可能含令牌或会话凭据，只记长度。
const SECRET_HEADERS = new Set(['authorization', 'cookie', 'x-goog-api-key', 'proxy-authorization']);

function strClass(s) {
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(s)) return 'uuid';
  if (/^-?\d+$/.test(s)) return 'digits';
  if (/^[0-9a-f]+$/i.test(s)) return 'hex';
  if (/^[A-Za-z0-9+/_=-]+$/.test(s)) return 'token-chars';
  return 'text';
}

function shapeValue(v, key, depth) {
  if (v === null) return null;
  if (typeof v === 'string') return KEEP_VALUE_KEYS.has(key) ? v : `<str ${v.length} ${strClass(v)}>`;
  if (typeof v !== 'object') return v; // 数字和布尔值是参数，不是内容，保留原值
  if (depth > 12) return '<deep>';
  if (Array.isArray(v)) {
    // 对话和工具列表很长，只保留第一个元素的形状和总数
    return v.length ? { '<array>': v.length, first: shapeValue(v[0], key, depth + 1) } : [];
  }
  const out = {};
  for (const [k, val] of Object.entries(v)) out[k] = shapeValue(val, k, depth + 1);
  return out;
}

function shapeHeaders(rawHeaders) {
  const out = [];
  for (let i = 0; i < rawHeaders.length; i += 2) {
    const name = rawHeaders[i];
    const value = rawHeaders[i + 1];
    out.push([name, SECRET_HEADERS.has(name.toLowerCase()) ? `<secret ${value.length}>` : value]);
  }
  return out;
}

function shapeBody(buf, contentType) {
  if (!buf.length) return null;
  if (/json/i.test(contentType || '') || buf[0] === 0x7b) {
    try { return shapeValue(JSON.parse(buf.toString('utf8')), '', 0); } catch (e) { /* 不是 JSON，按二进制处理 */ }
  }
  return `<binary ${buf.length} bytes ${contentType || ''}>`;
}

function decode(body, encoding) {
  const zlib = require('zlib');
  switch ((encoding || '').toLowerCase()) {
    case 'gzip': return zlib.gunzipSync(body);
    case 'deflate': return zlib.inflateSync(body);
    case 'br': return zlib.brotliDecompressSync(body);
    case 'zstd': return zlib.zstdDecompressSync(body);
    default: return body;
  }
}

// req 为 Node 的 IncomingMessage，body 为完整请求体（线上的原始字节）。
function shapeRequest(req, body) {
  const [path, query] = req.url.split('?');
  let decoded = body;
  try { decoded = decode(body, req.headers['content-encoding']); } catch (e) { /* 解压失败时按原样记录 */ }
  return {
    method: req.method,
    path,
    // 查询参数只记名称；alt 的取值（如 sse）决定响应格式，记录原值
    query: query ? query.split('&').map((kv) => (kv.startsWith('alt=') ? kv : kv.split('=')[0])) : [],
    httpVersion: req.httpVersion,
    headers: shapeHeaders(req.rawHeaders),
    bodyBytes: body.length,
    body: shapeBody(decoded, req.headers['content-type']),
  };
}

module.exports = { shapeRequest };
