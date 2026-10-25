// 列出二进制里匹配正则的可读字符串（去重）。用法: node enum_scan.js <二进制> <正则>
const fs = require('fs');
const [file, re] = process.argv.slice(2);
const s = fs.readFileSync(file).toString('latin1');
console.log([...new Set(s.match(new RegExp(re, 'g')) || [])].slice(0, 80).join('\n'));
