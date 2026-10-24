# Antigravity 对齐清单

本文记录 CLIProxyAPI 的 Antigravity 部分与官方程序逐项对比的结果。**对齐的主要对象是官方桌面客户端**（Electron 应用，后端为 `resources/bin/language_server.exe`）；官方 CLI（`agy`）只作交叉参照。第 2 节里标“`agy`”的数据是在 `agy` 上测得的，标“客户端”的数据是在桌面客户端上测得的，两者不能互相替代。每一项只写实际验证过的内容，并给出证据和复现方法。

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

桌面客户端的做法：带 `HTTPS_PROXY` 启动应用，并加 `--remote-debugging-port=<端口>` 打开 Chrome DevTools 协议，用 `Input.insertText` 加回车在后台向聊天框发消息，不需要窗口在前台。测试时保持日常网络环境（VPN 不关），抓取代理只监听 127.0.0.1。若要看每个连接上的请求时间线，用能记录数据块时间和大小的代理（同样不解密）。

### 1.2 对官方程序的静态分析

对 `language_server.exe`（Go 程序）用 Ghidra 做反编译，用 Go 的 pclntab 恢复函数名。静态分析只能说明程序里有什么代码，不能说明运行时一定走哪条路径，所以凡是靠它得出的结论，都要再用抓取验证。

## 2. 已验证的结论

| 项目 | 官方 | 本项目 | 状态 |
| --- | --- | --- | --- |
| 模型接口 `daily-cloudcode-pa.googleapis.com` 的 ALPN | 10 次连接全部没有 ALPN 扩展，因此用 HTTP/1.1 | 修复后全部没有 ALPN | 已验证 |
| 模型接口握手的扩展顺序 | `[0,11,65281,23,18,5,10,13,50,43,51]` | 同一顺序 | 已验证 |
| 模型目录探测（`fetchAvailableModels`）的 ALPN | 无 ALPN | 修复前发 `h2, http/1.1`，扩展里多出 16 | 已修复 |
| 连接中断后的重试节奏 | 指数退避（静态分析：上限 5 秒，带抖动） | 修复前约 60 毫秒连发 4 次；修复后间隔 0.8、2.0、3.8 秒（假上游实测） | 已修复（客户端基数未知，见 3.4 节） |
| 连续多轮对话的连接复用 | 同一个 `agy` 会话连续 5 轮提问，约 9 个模型请求（含工具调用）全部落在同一个连接上，持续约 50 秒，请求间隔 2 到 14 秒 | 修复 2.2 节的问题后，同一提示词的连续请求除第一次外全部复用 | 已验证 |
| 其他主机（`antigravity-unleash.goog`、`play.googleapis.com`、`lh3.googleusercontent.com`、自动更新服务） | `h2, http/1.1` | 版本拉取等辅助请求同为 `h2, http/1.1` | 已验证 |
| 客户端：模型接口握手（一次启动加 6 轮对话） | `daily-cloudcode-pa.googleapis.com` 无 ALPN，扩展顺序 `[0,11,65281,23,18,5,10,13,50,43,51]`，与 `agy` 和本项目相同 | 同左 | 已验证（仅限这一次运行） |
| 客户端：多轮对话的连接复用 | 新对话连发 6 轮（约每 14 秒一轮），全部大体积请求落在同一个连接上，没有每轮新建；另有几个连接只完成了 TLS 握手（约 1.5 KB 的块是 ClientHello），没有承载请求 | 启用连接池时同样复用；默认配置不复用，见第 3.3 节 | 有差异（取决于配置，见第 3.3 节） |
| 客户端：`oauth2.googleapis.com` | `h2, http/1.1` | 刷新令牌使用标准 HTTP 客户端，未单独测 | 待验证 |
| 客户端：`www.googleapis.com` | 同一次运行里既出现过 `h2, http/1.1`，也出现过无 ALPN | 仅登录取用户信息时访问 | 待验证（原因不明） |
| 客户端：`aicode.googleapis.com` | 无 ALPN，`agy` 抓取中没有出现过 | 本项目代码里没有访问这个主机 | 有差异，是否影响对话待验证 |

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
| 官方模型请求使用的 transport 参数 | 见第 3.2 节。`newFastpushTransport` 的参数已读出，但抓取显示模型接口没有协商 h2，它不像是模型请求的 transport；模型请求实际用哪个 transport 还没找到 | 在官方进程上做运行时观测，确认模型请求实际使用的 transport |
| 空闲连接的保活 | 见第 3.3 节：客户端每 60 秒在模型连接上发一个约 630 字节的请求，连接因此一直不空闲；这个请求的内容未知（不解密） | 在可解密的环境下确认这个周期请求是什么 |
| 一次请求的并发连接数 | 见第 3.3 节：两边在“没有可用空闲连接”时都会多出一个只握手不承载请求的连接；本项目冷启动时还会和生成请求同时发一个 `credits_query` | 已基本澄清，`credits_query` 是否对应客户端的某个调用待验证 |
| 请求头、请求体字段、顺序 | 未对比 | 在可解密的环境下对比两边的请求 |
| 超时与重试策略 | 见第 3.4 节：本项目一侧已用假上游实测，传输层错误的立即重试已修复；客户端一侧只有静态分析，退避基数未知 | 在客户端上触发错误做运行时观测 |
| 单次调用耗时：约 4.5 秒的固定耗时与偶发 25 到 31 秒的长尾 | 见下文第 3.1 节 | 用相同的输入内容和相同的思考量重测 |

### 3.1 耗时对比（初步，官方一侧来自 `agy`，尚未在桌面客户端上重测）

同一句提示词“用三句话解释什么是 TCP 三次握手”，模型为 `gemini-3.8-flash` 的 medium 档，`agy` 与本项目交替各跑 10 次。`agy` 的耗时来自代理记录的线上时间线（请求体发出到首个响应字节、到最后一个响应字节）；本项目的耗时来自请求摘要日志。

| | 首包耗时中位数 | 总耗时中位数 | 范围（总耗时） |
| --- | --- | --- | --- |
| `agy` | 7.1 秒 | 8.7 秒 | 5.1 到 14.0 秒 |
| 本项目 | 6.5 秒 | 9.9 秒 | 8.4 到 10.8 秒 |

结论仅限于：在这组样本里没有看到本项目比官方更慢，两边的耗时都主要由模型思考时间决定（本项目的思考量为 744 到 1170 个 token）。限制：两边的请求内容并不相同。`agy` 带自己的系统提示和工具，请求体约 5.9 到 20 KB；本项目这次只发了 572 字节的裸提示。`agy` 的思考量未知，样本只有 10 次，没有出现 25 秒以上的长尾。所以这不能证明速度完全一致。

### 3.2 桌面客户端的静态分析（`language_server.exe`）

对象：`resources/bin/language_server.exe`，177,882,624 字节，SHA-256 `4575b7fa20280b87b527e5d6075648126b8a11e39d78fe70c4d29df2c1466ca5`。函数名来自 Go 的 pclntab（`docs/tools` 之外的临时脚本导出），反编译用 Ghidra 12.1.4（首次打开要先做整体分析，耗时较长）。

- **版本差异**：`newFastpushTransport` 在这个版本里位于 0x141fddfe0；文档此前记录的 0x141e92840 与 `agy.exe` 里的 0x141ec9240 来自其他版本的二进制。三个地址不同，说明不能把一个版本的静态结论直接套到另一个版本。
- **`newFastpushTransport` 的参数（已读出）**：反编译显示写入了 `MaxIdleConns=100`、`MaxIdleConnsPerHost=20`、`IdleConnTimeout=90000000000` 纳秒（90 秒），并置位一个布尔字段（按 `net/http.Transport` 的内存布局推断为 `ForceAttemptHTTP2=true`，字段对应关系是推断）。
- **它不像模型请求的 transport**：反编译找不到它的直接调用方；`remoteAgentFastpushClient` 的调用方是 `postHTTPRequest`、`getCodeAssistBackendRequest`、`StreamRemoteAgentStateUpdates`、`StreamRemoteAgentTerminalOutput`。再加上抓取里模型接口没有 ALPN（`ForceAttemptHTTP2` 会带出 `h2`），两条线索一致，但都不是直接证据。
- **重试库**：包含 `hashicorp/retryablehttp`，但它的 `NewClient` 只被 `scm_utils`（代码仓库工具）和 `ls_unleash.GetEnterpriseExperiments`（实验开关）调用，不在模型请求路径上。模型请求的重试见第 3.4 节。
- **`aicode` 相关代码**：存在 `GenerateContentFromAICode`、`StreamGenerateContentFromAICode`、`getAICodeURL`、`runAICodeShadowEval`、`CompareAGYExperiments`，对应抓取里的 `aicode.googleapis.com`。名字提示可能与对照评测有关，但这只是从函数名的猜测，没有验证它是否承载真实对话内容。
- **局限**：`agy.exe`（204 MB）上 Ghidra 超时，原因未确认；静态分析只说明代码存在，不说明运行时走哪条路径。

### 3.3 连接生命周期（客户端与本项目实测）

方法：时间线代理记录每个到 `daily-cloudcode-pa.googleapis.com` 的连接的打开时刻、每个数据块的时刻和大小，以及哪一方先关闭（代理的上游一侧先关闭记为“服务端”）。客户端用 CDP 在后台发消息；本项目用 `curl` 发同一句提示词，模型 `gemini-3.8-flash` medium。VPN 保持开启。

| 场景 | 客户端 | 本项目，默认配置（`connection-pool.enabled: false`） | 本项目，启用连接池（`idle-conn-timeout: 120s`） |
| --- | --- | --- | --- |
| 第 1 条消息 | 复用启动时已建的连接 | 新建连接 | 冷启动，新建连接 |
| 空闲约 40 秒后第 2 条 | 复用 | 新建连接 | 复用（日志 `reused=true idle_time=40s`） |
| 空闲更久后再发 | 复用（空闲 110 秒后），但连接期间一直有每 60 秒一次的请求，实际没有空闲超过约 51 秒 | 新建连接；一次样本里新连接握手卡住 10 秒后重连，整次请求 14.9 秒 | 空闲 150 秒后新建连接，原连接已被关闭 |

关键观察：

- **空闲连接由上游一侧关闭**：本项目两个空闲连接分别在空闲 56.5 秒、58.5 秒时被上游一侧先关闭（2 个样本）。上游一侧包括 VPN，所以不能断定是 Google 服务端关的。代码注释里写的“GFE 240 秒空闲断开”与这个观测不一致，这里只记录观测，不改注释的依据。
- **客户端用周期请求保活**：客户端每隔整 60 秒在模型连接上发一个约 630 字节的请求，响应几 KB，连接因此持续可用。请求内容未知。本项目没有这种周期请求。
- **默认短连接的来历**：上游在 #5494 中发现，大凭据池轮换时每个凭据各自保留 10 分钟的空闲连接，一小时内积压数千条，因此在 d5397905 中把默认改成短连接。这是面向大凭据池的取舍；对单用户、单凭据的使用方式，启用连接池更接近客户端。

结论：在启用连接池、空闲时间短于上游关闭时间的情况下，本项目的连接行为与客户端一致；默认配置下每个请求都新建连接，与客户端不同。`idle-conn-timeout` 设得比上游关闭时间（本次约 56 秒）更长没有收益，因为连接会先被上游关掉。

### 3.4 模型请求的重试（客户端静态分析与本项目代码对比）

客户端一侧来自反编译，没有在运行时触发过错误来验证；字段含义是按代码结构推断的，下表标注了把握程度。

**哪些错误会重试**

客户端的判定函数是 `cortex/utils.IsRetryableAPIError`（0x1417a3ba0），由 `modelapiccpa.(*Client).ClassifyError` 和 `codeassistclient.classifyCCPAMidStreamRetryError` 调用。本项目的判定是 `sdk/cliproxy/auth` 里的 `isRequestRetryRoundError`（状态码列表加传输层瞬时错误）。

| 错误 | 客户端 | 本项目 |
| --- | --- | --- |
| 403 | 不在重试列表里 | 重试（换凭据或等冷却） |
| 408、502、503、504 | 重试 | 重试 |
| 429 | 重试；但原因为 `QUOTA_EXHAUSTED`，或服务端要求等待超过 30 秒时不重试 | 重试，等待时间取冷却时间或服务端给的延迟，超过 `max-retry-interval` 则不重试 |
| 500 | 按反编译结构看不重试（把握一般） | 重试 |
| 402 | 不重试 | 不在重试列表里 |
| gRPC `DEADLINE_EXCEEDED`、`UNAVAILABLE`，以及错误信息含 `DECODE_PREEMPTED` 等字符串 | 重试 | 不适用（本项目走 HTTP） |
| 连接断开、超时等传输层错误 | 未分析 | 重试 |

**重试前等多久**

- 客户端（`continuation.SleepForMidStreamRetry`，0x141c80360，流中途重试）：等待时间 = 基数 × 2^min(次数−1, 6)，上限 5 秒；再取一半固定、一半随机；若服务端给了重试延迟，至少等那么久。基数由调用方传入，没有读出。
- 本项目：有冷却时间或服务端延迟时按它等待（另加最多 2 秒抖动）；否则**立即重试**，没有指数退避。

客户端另有 `ModelAPIRetryConfig`（字段：最大重试次数、初始等待毫秒、指数倍数、抖动范围、最大等待毫秒），二进制里只有字段定义、没有默认值，推测由运行时配置下发，所以退避基数读不出来。

**本项目的实测（本地假上游）**

方法：用一份假凭据把 `base_url` 指向本地假上游，假上游对生成请求按设定方式失败并记录每个请求的到达时刻；`request-retry: 3`，`max-retry-interval: 30`。脚本和步骤见第 4 节，结果如下（“次数”是假上游收到的生成请求数）。

| 失败方式 | 1 份凭据 | 2 份凭据 |
| --- | --- | --- |
| 403、500、503 | 1 次，不重试 | 2 次：换另一份凭据再试，相隔约 5 毫秒 |
| 429，`RetryInfo` 2 秒 | 4 次，间隔约 10 到 11 秒 | 8 次，两两成对，对与对之间约 11 到 12 秒 |
| 429，`QUOTA_EXHAUSTED`，`RetryInfo` 3600 秒 | 1 次，不重试 | 2 次，各凭据 1 次 |
| 连接中断（响应头后断开） | 修复前：4 次，间隔约 60 毫秒，0.3 秒内结束；修复后：4 次，间隔 0.8、2.0、3.8 秒 | 修复前：8 次，间隔约 60 毫秒；修复后：每轮内立即换凭据，轮与轮之间 0.8、1.9、2.7 秒 |

实测纠正了静态读代码时的判断：本项目对 403、500 的“重试”实际是换凭据，单凭据时不会在同一凭据上重复请求；额度耗尽也不重试，与客户端一致。

**已修复的差异：传输层错误立即重试**

连接中断、超时等没有 HTTP 状态码的错误不会产生冷却，调度层因此立即开始下一轮，一次断流在 0.3 秒内打出 4 个请求。客户端在这种情况下按指数退避等待。

修复（`sdk/cliproxy/auth/antigravity_transient_retry.go`）：只对 Antigravity、只对传输层瞬时错误，下一轮前等待“窗口的一半加上一半以内的随机值”，窗口依次为 1、2、4 秒，之后保持 4 秒；调度层原有的等待抖动最多再加 25%，所以最长等待是 5 秒，与客户端上限相同。基数 1 秒是本项目的选择，因为客户端的基数读不出来。`max-retry-interval` 设为 0 时仍然立即重试。其他提供方的行为不变。回归测试：`TestShouldRetryAfterErrorBacksOffTransientErrorsOnlyForAntigravity`。

**仍有差异、未改动的**：客户端会对 503 等错误在同一凭据上退避重试，本项目单凭据时不重试；本项目对 429（短延迟）的等待约 10 秒，比客户端更保守。这两处本项目更少发请求，暂不改动。

## 4. 复现与回归

- 单元测试：`go test ./internal/runtime/executor/ ./sdk/cliproxy/`，其中 `TestNewAntigravityCatalogHTTPClientOmitsALPN` 保证非生成请求不发 ALPN。
- 抓取回归：每次改动 Antigravity 的网络层后，重新跑第 1.1 节的抓取，确认 `daily-cloudcode-pa.googleapis.com` 的所有连接都是 `NO-ALPN-EXTENSION`。
- 连接时间线（第 3.3 节）：`node docs/tools/timing_proxy.js timeline.log` 启动代理（端口 18081），被测程序带 `HTTPS_PROXY=http://127.0.0.1:18081` 运行，结束后用 `node docs/tools/timeline_summary.js timeline.log` 汇总。
- 重试实测（第 3.4 节），不访问真实服务：
  1. `node docs/tools/make_fake.js <一份真实凭据> <配置文件> <输出目录> 18090 8319 [凭据份数]`：生成指向本地假上游的假凭据和测试配置。只从真实凭据里复制模型 ID 列表，不复制令牌。
  2. `node docs/tools/fake_upstream.js 18090 up.log <模式> <输出目录>/ids.json`：模式为 `403`、`500`、`503`、`429rate`、`429quota`、`eof`。
  3. 用生成的配置启动本项目（不要设置 `HTTPS_PROXY`），向 8319 端口发一个请求，再看 `up.log` 里生成请求的次数和间隔。
  4. Windows 上 `node` 可能经 Volta 启动器包了一层，结束进程时要按命令行找到真正的 `node` 进程，否则端口会被残留进程占住，后面的结果全部作废。

## 5. 范围说明

本文只比较网络层行为。账号风险、服务条款与合规性不在本文的验证范围内，不能由这些对比结果推出。
