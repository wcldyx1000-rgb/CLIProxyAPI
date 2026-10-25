# Antigravity 对齐清单

本文记录 CLIProxyAPI 的 Antigravity 部分与官方程序逐项对比的结果。**对齐的主要对象是官方桌面客户端**（Electron 应用，后端为 `resources/bin/language_server.exe`）；官方 CLI（`agy`）只作交叉参照。第 2 节里标“`agy`”的数据是在 `agy` 上测得的，标“客户端”的数据是在桌面客户端上测得的，两者不能互相替代。每一项只写实际验证过的内容，并给出证据和复现方法。

状态含义：

- **已验证**：官方与本项目都测过，结果一致。
- **已修复**：测出差异，已修改并复测一致。
- **有差异**：测出差异，没有修改（原因写在对应位置）。
- **待验证**：还没有对比，或只有间接线索。

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

### 2.3 请求外层（桌面客户端解密抓取，2026-10-25）

**对比范围。** 对话内容、系统提示和工具定义由使用本项目的客户端决定，本来就不会、也不应该与桌面端相同，不在对比范围内。对比的是请求的“外层”：请求路径和查询参数、请求头（名称、取值格式、顺序）、请求体的包装字段及 JSON 字段顺序。

**取证方式。** 桌面端（Windows 版 2.26.0）经本地代理 `docs/tools/shape_proxy.js` 运行。代理用临时根证书只解密 `daily-cloudcode-pa.googleapis.com` 和 `aicode.googleapis.com`，记录请求后在本地返回响应，**请求不转发给 Google**；其他主机原样转发隧道、不解密。启动阶段的查询接口（`loadCodeAssist`、`fetchAvailableModels` 等）由代理返回本地编造的最小响应，字段名取自 `language_server.exe` 里的 protobuf 描述符（`docs/tools/proto_fields.js`）；生成请求返回错误（观察重试）或一条编造的成功回复（继续下一轮）。本项目一侧用第 4 节的假上游记录。两边用同一套规则（`docs/tools/req_shape.js`）把请求转成“形状”：令牌只记长度，对话内容只记长度和字符类别。临时根证书测完已从用户证书库删除，私钥已删除。

**抓到的样本**：Gemini 模型新对话 1 轮（503）、新对话 3 轮（编造的成功回复）、Claude 模型新对话 1 轮，以及对 503 的完整重试过程。

| 项目 | 客户端 2.26.0 | 本项目修复前 | 本项目修复后 | 状态 |
| --- | --- | --- | --- | --- |
| 生成请求的路径和查询 | `POST /v1internal:streamGenerateContent?alt=sse` | 相同 | 相同 | 已验证 |
| 生成请求的请求头 | `Host`、`User-Agent`、`Transfer-Encoding: chunked`、`Authorization`、`Content-Type`、`Accept-Encoding: gzip`，依此顺序 | 用 `Content-Length` 代替 `Transfer-Encoding`，其余相同 | 与客户端相同 | 已修复 |
| `loadCodeAssist` 的请求头和请求体 | 用 `Content-Length`；请求体 `{"metadata":{"ideType":…}}` | 相同 | 相同 | 已验证 |
| `User-Agent` 的 `cl` | `antigravity/hub/2.26.0 (aidev_client; os_type=windows; arch=amd64; cl=1005016104)` | 2.26.0 不带 `cl`；启动后第一个 `fetchAvailableModels` 用的是后备版本 2.24.0 | 2.26.0 带 `cl=1005016104`；模型目录探测先等首次版本拉取（最多 10 秒） | 已修复 |
| 顶层字段顺序 | `project`、`requestId`、`request`、`model`、`userAgent`、`requestType` | `requestId` 在最后 | 与客户端相同 | 已修复 |
| `requestId` | `agent/<cascade_id>/<毫秒时间戳>/<trajectory_id>/<step>` | `agent-<uuid>` | 与客户端格式相同 | 已修复（`step` 的取法见下） |
| `request` 内字段顺序 | `contents`、`systemInstruction`、`tools`、`labels`、`generationConfig`、`sessionId` | 没有 `labels`，其余顺序取决于下游请求 | 与客户端相同 | 已修复（`toolConfig` 的位置没有观测到，暂放在 `tools` 之后） |
| `request.labels` | 见下文，键按名称排序 | 没有 | 与客户端相同 | 已修复（部分取值是推断，见下） |
| Gemini 的 `generationConfig`（真实目录） | `{"maxOutputTokens":65536,"thinkingConfig":{"includeThoughts":true,"thinkingBudget":-1}}`，两个数值都取自目录里该模型的条目 | 不发 `maxOutputTokens`；3.8 系列发 `thinkingLevel` | 发目录里的 `maxOutputTokens`；思考档位与模型后缀一致时换成目录预算（medium → 4000），字段顺序同客户端 | 已修复（规则见下） |
| Claude 的 `generationConfig`（真实目录） | `maxOutputTokens` 64000、`thinkingBudget` 1024，都取自目录 | 按下游请求的 `max_tokens` 和推理强度 | 未改 | 有差异：下游明确要求的输出上限和思考量应当尊重 |
| Claude 的 `toolConfig`（真实目录） | 不带 | 强制设 `functionCallingConfig.mode=VALIDATED`，连下游的 `tool_choice: none` 也会被覆盖 | 保留下游给的设置；没有时不带 | 已修复 |
| 工具结果（`functionResponse`）的角色 | Gemini 模型时为 `model`，Claude 模型时为 `user` | 相同：chat、responses、messages 三个接口实测都是 Gemini 为 `model`、Claude 为 `user`（由 `normalizeAntigravityGeminiFunctionResponseRoles` 处理） | 无需修改 | 已验证（此前误记为有差异，见下） |
| 工具参数的 `type` 写法 | 大写，如 `OBJECT`、`STRING` | 原样转发下游的小写写法 | 未改 | 有差异：取值来自下游的工具定义，属于内容层 |

关于 `User-Agent`：本项目固定报 `os_type=darwin; arch=arm64`，即模拟 mac 版客户端，这是原有的选择，本次不变。为确认 mac 版也用同一个 `cl`，下载了官方 2.26.0 darwin-arm 安装包（SHA-512 与更新清单一致），其中的 `language_server` 和 `webm_encoder` 都含 `1005016104`。后备版本同时改为 2.26.0。

**`requestId` 与 `labels` 的规律**（纯对话 3 轮，以及 3 轮“每轮一次 `view_file` 工具调用”的对话实测）：

- `step` 等于 `request.contents` 的条数，工具调用后的续写请求也一样（1、3、5、7、9、11）；`labels.last_step_index` 等于 `step − 1`。
- `cascade_id`、`trajectory_id` 是两个不同的随机 UUID（版本 4），同一对话内不变；`root_cascade_id` 等于 `cascade_id`。
- `labels.request_id` 为 `<trajectory_id>-<n>`，`n` 是请求里**模型已经产出的次数**（模型回复和工具调用都算，工具结果不算），每发一次生成请求加 1：0、1、2、3、4、5。纯对话时它恰好等于轮次，所以最初误以为是轮次。
- 用户第二次发言起出现 `last_execution_id`（UUID），之后的工具结果续写请求里也有；第一轮的工具结果续写请求里没有。它和其他 ID 看不出关系。
- `model_enum` 为模型目录里该模型的 `model` 字段（真实目录里 `gemini-3.8-flash-high` 是 `MODEL_PLACEHOLDER_M318`）。
- `used_claude`、`used_claude_conservative`、`used_non_gemini_model`：Gemini 模型三个都是 `"false"`，Claude 模型三个都是 `"true"`。

本项目的做法（`internal/runtime/executor/antigravity_request_envelope.go`）：本项目没有客户端的会话状态，所以 `cascade_id`、`trajectory_id`、`last_execution_id` 由 `project` 和 `sessionId` 经 SHA-256 推导，并按版本 4 UUID 的格式输出，同一对话的多轮和重试保持不变，不需要保存状态。计数时把只含 `functionResponse` 的消息当作工具结果，不论角色是 `model` 还是 `user`。下面几点是推断，没有实测依据：

- 其他模型（既非 Gemini 也非 Claude）的 `used_non_gemini_model` 取 `"true"`、两个 `used_claude*` 取 `"false"`。
- 模型目录尚未报告 `model_enum` 时省略这个键。
- 本项目的 `sessionId` 在下游没有提供时由第一条用户消息的哈希生成，所以两段开头相同的对话会得到相同的 ID；客户端每段对话都是新的随机 ID。

**真实服务端验证**：用修复后的构建另起一个实例，向真实服务各发一条请求：Gemini 多轮对话（带系统提示）返回 200，3.96 秒；Claude 带工具调用返回 200，2.28 秒，正确产生工具调用。

**用真实模型目录重抓（第二次抓取）**：第一次抓取时模型目录是编造的，`generationConfig` 和 Claude 的 `toolConfig` 只能标为待验证。第二次先用本项目自己的目录探测客户端（`NewAntigravityCatalogHTTPClient`，无 ALPN，与平时探测相同）拉取一份真实的 `fetchAvailableModels` 响应，再由不转发代理把它交给桌面端（`shape_proxy.js` 的 `CATALOG` 变量）。目录里 3.8 各档位都只有 `thinkingBudget`（low 1000、medium 4000、high -1），没有 `thinkingLevel`；只有 `gemini-3.5-flash-lite` 用 level。桌面端界面里 3.8 只提供 High 档（来自目录的 `agentModelSorts`），所以它的对话请求只会用 `gemini-3.8-flash-high`。另外，每段对话开头的 checkpoint 请求用 `gemini-3.5-flash-lite`，配 `thinkingLevel: "LOW"`。

本项目按目录填写 Gemini 的 `generationConfig` 时遵守两条限制：

- 只有思考档位与模型后缀一致时才换成目录预算（例如 `effort=medium` 选中 `gemini-3.8-flash-medium`，`medium` 换成 4000），两者表示的是同一个档位；其他档位原样保留。
- 请求里没有任何思考量时不补预算。走到这一步时，客户端明确关闭思考的请求（思考配置已被删除）和什么都没给的请求看起来一样，补上预算会把关掉的思考重新打开。

目录要等本项目的模型探测完成后才有；在那之前的请求按原来的方式发送，不会出错。第二次抓取后的真实服务端验证：`gemini-3.8-flash` 的 low、medium、high、none 四档都返回 200；Gemini 和 Claude 的两轮工具调用回放都返回 200，Claude 不再强制 `VALIDATED` 后照常产生工具调用。

**更正：工具结果的角色。** 第二次抓取后，本文一度把“工具结果的角色”记为有差异，写的是本项目“按下游格式转换，一般为 `user`”。这是根据下游请求的原始格式推测的，没有实测本项目实际发出的内容。准备做对比测试时，先用假上游记录了本项目的实际请求：chat、responses、messages 三个接口都是 Gemini 模型为 `model`、Claude 模型为 `user`，与桌面端完全相同。原因是执行器里早就有 `normalizeAntigravityGeminiFunctionResponseRoles`，会把 Gemini 请求里的工具结果改成 `model`。因此这一项改为已验证，没有改代码，也没有为此发真实请求。教训：本项目一侧的结论同样要看实际发出的请求，不能看转换前的格式。

**客户端的其他请求**（本项目都没有，未改）：

- 启动时还会调用 `listExperiments`、`fetchUserInfo`、`retrieveUserQuotaSummary`、`/v1internal/cascadeNuxes`、`/v1internal/agentPlugins`、`/v1internal/buildWithGooglePlugins`，对话开始时调用 `writeTrajectoryAcls`。
- 第 3.3 节里每 60 秒一次、约 630 字节的请求是 **`listExperiments`**（实验开关轮询；形状日志算出的线上大小约 614 字节，间隔正好 60.0 秒）。此外 `cascadeNuxes` 每 120 秒一次，`loadCodeAssist` 和 `fetchAvailableModels` 约每 6 分钟一次。
- 每段新对话开始时，客户端另发 `requestType: checkpoint` 的生成请求（`requestId` 为 `checkpoint/<uuid>`，没有 `labels`），这是客户端自身的功能。

这些请求承载的是客户端自己的功能。本项目照着发，等于凭空制造请求，而且在大凭据池下会成倍增加请求量（参见第 3.3 节 #5494 的教训），所以不跟。

### 2.4 Responses 接口上思考内容被压到最后（体验，2026-10-25）

现象：pi 通过 `/v1/responses` 使用本项目时，思考内容总是和答案同时出现，看起来像“结束时才显示”。

测量：`docs/tools/stream_timing.js` 记录每个流式片段的到达时刻，同一道题分别走三个接口（真实上游）：

| 接口 | Claude Sonnet 4.6 的思考 | Gemini 3.8 Flash 的思考 |
| --- | --- | --- |
| `/v1/chat/completions` | 约 1 到 6 秒之间陆续到达 | 2 段摘要，第一段比正文早 1.5 到 2 秒 |
| `/v1/responses`（修复前） | 39 到 48 片在同一毫秒到达，紧接着就是正文（3/3 次） | 2 段摘要与正文在同一毫秒到达（3/3 次） |
| `/v1/responses`（修复后） | 约 1.3 到 5.1 秒之间陆续到达 | 摘要先于正文到达 |

原因分两层：

1. **上游**：Gemini 只在思考快结束时给出思考摘要，前几秒没有任何思考内容。chat 接口不缓冲（同一路径上 Claude 的思考是陆续到达的），在 chat 上看到的 Gemini 时序也是这样，所以这一层来自上游，本项目改不了。
2. **本项目**：`internal/translator/gemini/openai/responses/gemini_openai-responses_response.go` 在拿到思考签名之前，把思考片段都存在 `ReasoningPendingDeltas` 里，签名一到才打开 reasoning 项并一次性发出。Claude 的签名在整段思考结束时才给，Gemini 的签名跟着正文来，所以思考全部被压到正文开始的那一刻。这段逻辑来自上游 #4525，目的是让 reasoning 项在 `output_item.added` 和 `output_item.done` 里带同一个签名。

修复（`354c6571`）：思考文字一到就打开 reasoning 项。签名晚到时，`output_item.added` 里的 `encrypted_content` 为空，签名放在 `output_item.done` 里。这样做依据的是两个客户端的源码：pi（`pi-ai` 的 `openai-responses-shared.js`）和 OpenAI 官方 SDK 的流式累加器，都用 done 事件里的完整项保存 reasoning 项，不用 added 里的。三个原本要求“added 与 done 签名一致”的测试改为新的约定：思考片段在第一个数据块里就发出；签名晚到时 added 为空；done 带正确的签名，项 ID 前后不变。

验证：用 `docs/tools/replay_check.js` 做两轮对话，把第一轮 done 事件里的输出项（含 reasoning 项和工具调用）原样带进第二轮。Gemini 和 Claude 都返回 200，答案正确，说明签名回放没有受影响。

还没有验证的：除 pi 和 OpenAI SDK 以外，其他 Responses 客户端是否从 added 事件读取签名。如果某个客户端这样做，签名晚到时它拿到的是空值。

### 2.5 思考内容的可见性（体验，2026-10-25）

两处让思考内容“明明产生了却看不到”的问题：

1. **`/v1/messages` 上 `gemini-3.8-flash` 不返回思考**（`40780b98`）。Claude 格式的请求开了思考、没有 `thinking.display` 时，发给上游的只有 `thinkingBudget`，没有 `includeThoughts`。实测同样的请求：`gemini-3-flash` 返回思考，`gemini-3.8-flash` 一个思考片段都没有（2/2）。本项目的思考可见性框架在没有 `display` 时刻意不做决定，因为 Anthropic 不同模型的默认值不同；但 Gemini 模型没有这套默认值。修复只针对 Gemini 目标模型：思考开启且没有 `display` 时补上 `includeThoughts: true`。Claude 目标模型不变，明确的 `display` 仍以客户端为准。修复后同一请求返回思考摘要（2/2）。
2. **客户端不传推理参数时不返回思考**（配置，不在仓库里）。不传 `reasoning_effort` 时请求里没有思考配置，Gemini 3 照样思考（实测用掉 378 个思考 token），但不返回摘要。在本机配置的 `requests.payload.default` 里为 `gemini-*` 加了 `"generationConfig.thinkingConfig.includeThoughts": true`。默认规则只补缺失的字段，而明确关闭思考时请求里写的是 `includeThoughts: false`（用假上游确认过），所以不会推翻客户端的选择，也不增加思考量。线上实测不传参数的 chat 请求 5 次里 4 次返回了思考，没返回的那次本项目确实发了 `includeThoughts: true`，是上游没给摘要。

## 3. 尚未完成的部分

**结论先行：连接层、生成请求的外层和 Gemini 的生成参数已对齐，有实测依据，修复后的格式也经过真实服务验证。但仍有下表中的差异或未验证项，不能说本项目与官方客户端完全一致。**

按对“像不像官方客户端”的影响排序：

| 优先级 | 项目 | 目前状况 | 怎么完成 |
| --- | --- | --- | --- |
| 1 | Claude 的输出上限和思考预算 | 桌面端用目录值（64000、1024），本项目按下游请求 | 属于下游意图，暂不改 |
| 2 | 客户端对 503 在同一凭据上退避重试，本项目单凭据时不重试 | 已实测客户端的节奏（第 3.4 节） | 是否要跟需要权衡，暂不改 |
| 3 | `aicode.googleapis.com` | 两次解密抓取期间都没有出现对它的请求，用途未知 | 待观察 |
| 4 | `credits_query` 对应的客户端调用 | 抓取中客户端调用的是 `retrieveUserQuotaSummary`，与本项目的积分查询是否对应还没有比较 | 对比两边的请求体 |
| 5 | 耗时对比 | 只有 `agy` 与本项目的数据（第 3.1 节）。两边请求体本来就不同，耗时主要由模型思考决定，对判断对齐程度帮助有限 | 暂不做 |
| 6 | 模型请求实际使用的传输参数 | 静态分析没有找到调用方（第 3.2 节） | 暂不做 |

已完成、有实测依据的项目见第 2 节。

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
- **客户端的周期请求顺带保活**：客户端每隔整 60 秒在模型连接上发一个约 630 字节的请求，响应几 KB，连接因此持续可用。解密抓取（第 2.3 节）确认这是实验开关轮询 `listExperiments`，不是专门的保活请求。本项目没有这种周期请求。
- **默认短连接的来历**：上游在 #5494 中发现，大凭据池轮换时每个凭据各自保留 10 分钟的空闲连接，一小时内积压数千条，因此在 d5397905 中把默认改成短连接。这是面向大凭据池的取舍；对单用户、单凭据的使用方式，启用连接池更接近客户端。

结论：在启用连接池、空闲时间短于上游关闭时间的情况下，本项目的连接行为与客户端一致；默认配置下每个请求都新建连接，与客户端不同。`idle-conn-timeout` 设得比上游关闭时间（本次约 56 秒）更长没有收益，因为连接会先被上游关掉。

### 3.4 模型请求的重试（客户端静态分析与实测，本项目代码与实测）

下面“哪些错误会重试”一表的客户端一侧来自反编译，字段含义是按代码结构推断的，表中标注了把握程度。客户端对 503 的重试节奏已经实测，见本节末尾。

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

**客户端对 503 的实测重试**（第 2.3 节的不转发代理对每个生成请求都返回 503）：

| 请求 | 次数 | 相邻两次的间隔（秒） |
| --- | --- | --- |
| 对话请求（`requestType: agent`），第 1 次运行 | 5 次后代理被停止 | 4.0、7.1、15.2、20.2 |
| 对话请求，第 2 次运行 | 7 次后代理被停止 | 4.0、6.0、15.0、30.6、60.7、119.0 |
| `checkpoint` 请求，两次运行 | 3 次后停止 | 1.0、1.6 到 1.7 |

对话请求大致每次加倍，从约 4 秒开始，测到 119 秒时还在重试，上限没有测到；每次重试的 `requestId` 末段 `step` 和 `labels.last_step_index` 都加 1。这与静态分析的 `SleepForMidStreamRetry`（上限 5 秒）不同，那个函数处理的是流中途的重试，不是开始前收到 503 的情况。

**仍有差异、未改动的**：客户端对 503 在同一凭据上长时间退避重试，本项目单凭据时不重试，把错误交还给下游客户端；本项目对 429（短延迟）的等待约 10 秒，比客户端更保守。这两处本项目发的请求更少，暂不改动。

## 4. 复现与回归

- 单元测试：`go test ./internal/runtime/executor/ ./sdk/cliproxy/ ./internal/misc/`，其中 `TestNewAntigravityCatalogHTTPClientOmitsALPN` 保证非生成请求不发 ALPN，`antigravity_request_envelope_test.go` 固定第 2.3 节的请求外层，`antigravity_version_wait_test.go` 覆盖启动时的版本等待。在 Windows 上跑全量测试时，`internal/store` 有 3 个测试失败（git 凭据存储的损坏恢复：删除仍被打开的 packfile 在 Windows 上照样可读，且 Windows 的“找不到文件”报错没有被判定为仓库损坏）。它们在本轮改动之前就失败，与 Antigravity 无关；要修就得放宽凭据仓库的损坏判定，判错的代价比测试失败大，所以没有改。
- 抓取回归：每次改动 Antigravity 的网络层后，重新跑第 1.1 节的抓取，确认 `daily-cloudcode-pa.googleapis.com` 的所有连接都是 `NO-ALPN-EXTENSION`。
- 连接时间线（第 3.3 节）：`node docs/tools/timing_proxy.js timeline.log` 启动代理（端口 18081），被测程序带 `HTTPS_PROXY=http://127.0.0.1:18081` 运行，结束后用 `node docs/tools/timeline_summary.js timeline.log` 汇总。
- 重试实测（第 3.4 节），不访问真实服务：
  1. `node docs/tools/make_fake.js <一份真实凭据> <配置文件> <输出目录> 18090 8319 [凭据份数]`：生成指向本地假上游的假凭据和测试配置。只从真实凭据里复制模型 ID 列表，不复制令牌。
  2. `node docs/tools/fake_upstream.js 18090 up.log <模式> <输出目录>/ids.json`：模式为 `403`、`500`、`503`、`429rate`、`429quota`、`eof`。
  3. 用生成的配置启动本项目（不要设置 `HTTPS_PROXY`），向 8319 端口发一个请求，再看 `up.log` 里生成请求的次数和间隔。
  4. Windows 上 `node` 可能经 Volta 启动器包了一层，结束进程时要按命令行找到真正的 `node` 进程，否则端口会被残留进程占住，后面的结果全部作废。被测的本项目进程同样可能残留：发请求前先确认测试端口上监听的正是本次启动的进程（`Get-NetTCPConnection -State Listen -LocalPort 8319`）。本次就有一组结果来自残留的旧构建，发现后已重测。
  5. 本项目的模型探测要先等首次版本拉取（最多 10 秒），依赖模型目录的结果（`model_enum`、按目录填写的 `generationConfig`）需要启动后等二三十秒再发请求。
  6. 在 Git Bash 里调用 PowerShell 脚本时，以 `/` 开头的参数（如 `/v1/messages`）会被自动改写成 Windows 路径，要加 `MSYS_NO_PATHCONV=1`。
- 流式思考时序（第 2.4 节）：`CPA_KEY=<本项目的 API 密钥> node docs/tools/stream_timing.js 8317 responses gemini-3.8-flash high`，接口可换成 `chat` 或 `messages`；推理回放用 `node docs/tools/replay_check.js 8317 <模型>`。两者都会向真实上游发请求。
- 请求外层对比（第 2.3 节）：
  1. 生成有效期 2 天的临时根证书和服务器证书（扩展配置见 `docs/tools/shape_leaf.ext`）：
     ```bash
     openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.pem -days 2 -subj "/CN=CPA Alignment Test CA (temporary, delete after test)"
     openssl req -newkey rsa:2048 -nodes -keyout leaf.key -out leaf.csr -subj "/CN=daily-cloudcode-pa.googleapis.com"
     openssl x509 -req -in leaf.csr -CA ca.pem -CAkey ca.key -CAcreateserial -out leaf.pem -days 2 -extfile docs/tools/shape_leaf.ext
     openssl x509 -in ca.pem -outform der -out ca.cer
     ```
  2. 由本人把根证书加入当前用户的信任列表：`certutil -user -addstore Root ca.cer`（Windows 会弹出确认框）。这一步削弱本机的 TLS 校验，测完必须删除。
  3. `node docs/tools/shape_proxy.js client.log leaf.pem leaf.key 503`（或 `200`），监听 127.0.0.1:18082。要用真实模型目录时设 `CATALOG=<目录响应文件>`，要抓工具调用轮次时设 `FAKE_TOOL=<一个可读的文件>`（详见脚本开头的说明）。真实目录用本项目的 `NewAntigravityCatalogHTTPClient` 拉取，不要用 `cmd/fetch_antigravity_models`：后者用的是 Go 默认客户端，会宣告 `h2`（见第 2.1 节）。
  4. 带 `HTTPS_PROXY=http://127.0.0.1:18082`、`NO_PROXY=127.0.0.1,localhost` 和 `--remote-debugging-port=9333 --remote-allow-origins=*` 启动桌面端；`node docs/tools/send.js "<消息>" new` 在后台发消息，`node docs/tools/click_text.js "<文字>"...` 点击界面（例如切换模型）。
  5. 汇总：`node docs/tools/print_shapes.js client.log`（全部请求）、`node docs/tools/gen_summary.js client.log`（生成请求的关键字段）、`node docs/tools/retry_gaps.js client.log`（重试间隔）。本项目一侧用上面的假上游，日志格式相同。
  6. 收尾：`certutil -user -delstore Root <根证书 SHA-1 指纹>`，删除 `ca.key`、`leaf.key`，再用 `certutil -user -store Root <指纹>` 确认已找不到。
  7. 编造响应所需的字段名可从客户端二进制里读：`node docs/tools/proto_fields.js language_server.exe FetchAvailableModelsResponse`；枚举名用 `node docs/tools/enum_scan.js language_server.exe 'MODEL_PLACEHOLDER_M[0-9]+'`。

## 5. 范围说明

本文比较网络层行为、请求外层格式和流式输出的时序。账号风险、服务条款与合规性不在本文的验证范围内，不能由这些对比结果推出。
