# Antigravity 对齐清单

本文记录 CLIProxyAPI 的 Antigravity 部分与官方 Antigravity CLI（`agy`）、官方客户端（`language_server.exe`）逐项对比的结果。每一项只写实际验证过的内容，并给出证据和复现方法。

状态含义：

- **已验证**：官方与本项目都测过，结果一致。
- **已修复**：测出差异，已修改并复测一致。
- **未验证**：还没有对比，或只有间接线索。

## 1. 对比方法

### 1.1 握手抓取（本文第 2 节的依据）

用 `docs/tools/alpn_capture_proxy.js`：一个只转发 CONNECT 隧道的本地代理，读取 TLS ClientHello 的 SNI、ALPN 和扩展顺序并写入日志，不解密、不保存请求内容。

```bash
node docs/tools/alpn_capture_proxy.js alpn.log          # 监听 127.0.0.1:18080

# 官方 CLI
HTTPS_PROXY=http://127.0.0.1:18080 NO_PROXY=127.0.0.1,localhost agy -p "只回复一个字：好"

# 本项目（另开一个端口的实例）
HTTPS_PROXY=http://127.0.0.1:18080 NO_PROXY=127.0.0.1,localhost ./cpa -config config.yaml
```

只要程序读取 `HTTPS_PROXY` 环境变量，就能用这个方法。

### 1.2 对官方程序的静态分析

对 `language_server.exe`（Go 程序）用 Ghidra 做反编译，用 Go 的 pclntab 恢复函数名。静态分析只能说明程序里有什么代码，不能说明运行时一定走哪条路径，所以凡是靠它得出的结论，都要再用抓取验证。

## 2. 已验证的结论

| 项目 | 官方 | 本项目 | 状态 |
| --- | --- | --- | --- |
| 模型接口 `daily-cloudcode-pa.googleapis.com` 的 ALPN | 10 次连接全部没有 ALPN 扩展，因此用 HTTP/1.1 | 修复后全部没有 ALPN | 已验证 |
| 模型接口握手的扩展顺序 | `[0,11,65281,23,18,5,10,13,50,43,51]` | 同一顺序 | 已验证 |
| 模型目录探测（`fetchAvailableModels`）的 ALPN | 无 ALPN | 修复前发 `h2, http/1.1`，扩展里多出 16 | 已修复 |
| 连续多轮对话的连接复用 | 同一个 `agy` 会话连续 5 轮提问，约 9 个模型请求（含工具调用）全部落在同一个连接上，持续约 50 秒，请求间隔 2 到 14 秒 | 修复 2.2 节的问题后，同一提示词的连续请求除第一次外全部复用 | 已验证 |
| 其他主机（`antigravity-unleash.goog`、`play.googleapis.com`、`lh3.googleusercontent.com`、自动更新服务） | `h2, http/1.1` | 版本拉取等辅助请求同为 `h2, http/1.1` | 已验证 |

### 2.1 一处差异的来龙去脉

启动本项目时，抓取发现对 `daily-cloudcode-pa.googleapis.com` 有两次连接带了 `h2` 的 ALPN，而官方对这个主机从不发 ALPN。原因是模型目录探测（`sdk/cliproxy/antigravity_models.go`）使用了默认的 `http.Client`，退回到 `http.DefaultTransport`，Go 会自动宣告 `h2`。

修复方式：新增 `executor.NewAntigravityCatalogHTTPClient`，复用执行器的 `cloneTransportWithHTTP11`（关闭 HTTP/2、清空 ALPN），探测改用它。对应测试在 `antigravity_executor_catalog_client_test.go`。

教学要点：Go 的 `http.Transport` 默认会按 ALPN 升级到 HTTP/2。只改一处 client 而漏掉另一处，对外表现就不一致。所以对齐要靠抓取逐个出口验证，不能只读代码。

### 2.2 流式请求丢连接的问题（性能）

现象：连接池已启用，请求间隔只有几秒，日志里的 `antigravity upstream connection acquired` 却大多是 `reused=false`。每丢一次连接，下一个请求要重新做 TCP 和 TLS 握手，实测 TLS 握手约 0.47 秒，偶尔超过 1.5 秒。

定位过程：

1. 经代理记录每个连接是谁先关闭的，发现连接是客户端（本项目）在响应结束时关闭的，服务端没有先断开。
2. 临时加诊断日志，确认流读到了结尾（正常 EOF），同时此刻请求上下文已被取消。
3. 原因：API 处理函数拿到最后一个数据块就返回，并取消请求上下文；上游响应体大约几毫秒后才到 EOF。`net/http` 的读循环在“响应体已读完，放回连接池”和“请求已取消，关闭连接”之间随机选一个，所以大约一半以上的连接被丢掉。

修复：流式请求使用独立的上游上下文（`antigravity_executor_stream_context.go`）。最终数据块发出之前，下游取消仍会立刻中止上游；最终数据块发出之后，给上游最多 2 秒读到 EOF，连接才能放回池里。

回归测试：`TestAntigravityExecuteStreamReusesConnectionWhenConsumerCancelsAtTerminal` 模拟“消费者在终止块处取消、响应体稍后才结束”，修复前 4 次请求建了 4 个连接，修复后 1 个。

实测（同一提示词，连续 6 次 `/v1/responses` 流式请求，间隔 5 秒）：修复前约 1 到 2 次复用，修复后除第一次外全部复用（5/5）。

## 3. 未验证的项目

| 项目 | 目前掌握的线索 | 怎么验证 |
| --- | --- | --- |
| 官方模型请求使用的 transport 参数 | 静态分析看到 `newFastpushTransport`（0x141e92840）设置了 `ForceAttemptHTTP2=true`、`MaxIdleConns=100`、`MaxIdleConnsPerHost=20`、`IdleConnTimeout=90s`。抓取显示模型接口并未协商 h2，所以这个 transport 更可能服务于其他主机。静态引用没有找到它的调用方 | 在官方进程上做运行时观测，确认模型请求实际使用的 transport |
| 空闲连接数与保活时长 | 本项目默认每主机 2 个空闲连接；连接池配置可调 | 对比官方在连续请求下的连接数变化 |
| 一次请求的并发连接数 | 本项目一次请求会同时建 2 个连接；官方一次简单请求期间对该主机共 10 次连接，含启动阶段的其他调用 | 分别只抓“发送请求”这一段，对比连接数 |
| 请求头、请求体字段、顺序 | 未对比 | 在可解密的环境下对比两边的请求 |
| 超时与重试策略 | 官方有流式空闲超时代码（`idleTimeoutReadCloser`），未找到专用重试库 | 同上，并结合运行时观测 |
| 单次调用耗时：约 4.5 秒的固定耗时与偶发 25 到 31 秒的长尾 | 见下文第 3.1 节 | 用相同的输入内容和相同的思考量重测 |

### 3.1 耗时对比（初步）

同一句提示词“用三句话解释什么是 TCP 三次握手”，模型为 `gemini-3.8-flash` 的 medium 档，`agy` 与本项目交替各跑 10 次。`agy` 的耗时来自代理记录的线上时间线（请求体发出到首个响应字节、到最后一个响应字节）；本项目的耗时来自请求摘要日志。

| | 首包耗时中位数 | 总耗时中位数 | 范围（总耗时） |
| --- | --- | --- | --- |
| `agy` | 7.1 秒 | 8.7 秒 | 5.1 到 14.0 秒 |
| 本项目 | 6.5 秒 | 9.9 秒 | 8.4 到 10.8 秒 |

结论仅限于：在这组样本里没有看到本项目比官方更慢，两边的耗时都主要由模型思考时间决定（本项目的思考量为 744 到 1170 个 token）。限制：两边的请求内容并不相同。`agy` 带自己的系统提示和工具，请求体约 5.9 到 20 KB；本项目这次只发了 572 字节的裸提示。`agy` 的思考量未知，样本只有 10 次，没有出现 25 秒以上的长尾。所以这不能证明速度完全一致。

## 4. 复现与回归

- 单元测试：`go test ./internal/runtime/executor/ ./sdk/cliproxy/`，其中 `TestNewAntigravityCatalogHTTPClientOmitsALPN` 保证非生成请求不发 ALPN。
- 抓取回归：每次改动 Antigravity 的网络层后，重新跑第 1.1 节的抓取，确认 `daily-cloudcode-pa.googleapis.com` 的所有连接都是 `NO-ALPN-EXTENSION`。

## 5. 范围说明

本文只比较网络层行为。账号风险、服务条款与合规性不在本文的验证范围内，不能由这些对比结果推出。
