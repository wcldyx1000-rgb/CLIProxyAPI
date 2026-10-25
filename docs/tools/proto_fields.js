// 在二进制里找某个 protobuf 消息名，打印它附近出现的可读字符串（描述符里的字段名、json 名）。
// 用法: node proto_fields.js <二进制> <消息名> [窗口字节数]
const fs = require('fs');
const [file, name, winArg] = process.argv.slice(2);
const win = Number(winArg || 1500);
const buf = fs.readFileSync(file);
const needle = Buffer.from(name);
let pos = 0, hits = 0;
while ((pos = buf.indexOf(needle, pos)) !== -1 && hits < 6) {
  const seg = buf.subarray(pos, Math.min(buf.length, pos + win));
  const strs = seg.toString('latin1').match(/[\x20-\x7e]{3,}/g) || [];
  console.log(`== @0x${pos.toString(16)}`);
  console.log(strs.join(' | '));
  pos += needle.length; hits++;
}
