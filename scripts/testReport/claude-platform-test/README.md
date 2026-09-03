# Claude Platform Test

基于 Anthropic-compatible `POST /v1/messages` 的 Claude 平台能力测试工具。

## 默认模型

`config.MODELS` 默认列出以下 5 个模型，并在报告中按模型系列分组；每个模型还对应一个独立的模型预期 Case（`MODEL-001`～`MODEL-005`）。只想跑其中几个模型时，直接在 `MODELS` 里注释掉不需要的即可（不限数量，非空且不重复就行）：

```text
claude-fable-5
claude-haiku-4-5-20251001
claude-opus-4-8
claude-sonnet-5
claude-opus-5
```

系列包括 `opus`、`sonnet`、`haiku`、`fable`；thinking 能力另标记为 `manual` 或 `adaptive`。

## 用例范围

用例覆盖基础对话、system、多轮上下文、停止序列、参数边界、Prompt Cache、流式 SSE、thinking + effort、工具调用、联网搜索、视觉/PDF、错误处理和会话隔离。

### 扩展思考：统一走 effort=max

adaptive 系列模型的所有思考用例都带 `output_config: {"effort": "max"}`。思考能力只需要在最高档验证"有没有返回思考内容"——官方允许模型在低档位自行决定不思考，逐档测出来的是 token 消耗差异，会把正常行为误判成缺陷。manual-only 模型（Haiku 4.5 等）官方不支持 effort，`AN-THINK-005` 自动跳过且不发请求。

### 联网搜索：只测不评分

`AN-WEB-SEARCH-001/002` 请求照常发出、结果完整记录，但**一律记为「跳过」不计入通过率**——联网搜索取决于资源来源，Anthropic 官方支持 `web_search` 服务端工具，AWS Bedrock 版 Claude 不提供该工具，测不通过不代表资源有缺陷。判定说明里保留「实测可用 / 实测不可用」供人工判断。
### 多模态资源：两种传输方式任一通过即算通过

Claude 资源的上游能力不统一：走 AWS Bedrock 中转的资源只认 base64、且单文件常限 5MB；直连官方的资源同时支持 url source、超过 5MB 也可能读得动。所以图片 / PDF 的 URL 用例会按 `config.MEDIA_SOURCE_MODES`（默认 `("base64", "url")`）依次尝试，**任一方式成功即判定通过**，不会因为资源只支持一种传输方式而误判成"不支持图片/PDF"。>5MB 的超大资源用例额外允许"所有方式都返回 4xx 明确超限"作为通过条件。

同一个 OSS 链接在多模型、多用例之间只下载一次（`media.py` 内置下载缓存）。

### Prompt Cache：按命中率判定

5 分钟 / 1 小时缓存不再只看"一次写入 + 一次读取"，改为统计命中率：

1. 种子轮：第一次请求把超长系统提示词写进缓存（预热）；
2. 读取轮：并发发 `config.CACHE_HIT_ROUNDS`（默认 20）次相同前缀的请求；
3. 命中率 = **Σcache_read ÷ (Σcache_read + Σcache_creation)**，**种子轮和读取轮全部计入**，≥ `config.CACHE_HIT_PASS_RATIO`（默认 85%）即通过。

**为什么按 token 而不是按轮次**：官方多轮缓存里同一次请求可以既读缓存又写新缓存（文档示例 `cache_read_input_tokens: 1800` 的同时 `cache_creation_input_tokens: 248`）。按"这轮 read>0 就算命中"数轮次，会把"命中了但又重写了一大半前缀"当成满分。token 口径如实反映"可缓存的输入里有多大比例真的来自缓存"。Anthropic 未定义官方公式，`cache_read / (cache_read + cache_creation)` 是通行口径。

被缓存的系统提示词前面带**本次运行标记**（时间戳 + 随机后缀）和 TTL 标记：固定文本会让 TTL 内重跑脚本时种子轮直接命中上次残留的缓存（`cache_creation=0`），测不出真实的写入→读取链路；TTL 标记则避免 5 分钟档和 1 小时档互相串用缓存。标记同时写进报告的关键指标，便于回查两次运行有没有共用缓存。

种子轮那笔写入也计入分母——1.25x 的写入费是真花掉的，算进去才是完整的 token 账。**副作用**：完美资源的理论上限因此是 `rounds/(rounds+1)`（20 轮 → 95.2%），所以 `CACHE_HIT_ROUNDS` 别调太小（5 轮上限只有 83.3%，完美资源也会低于 85% 阈值）。理论上限一并写进报告指标。读写全为 0 的轮次（资源没把请求当可缓存）会按种子轮观测到的前缀长度折算成未命中计入分母，避免它"隐身"抬高命中率。

HTTP 失败的轮次不计入分母、只在报告里单列；成功轮次不足一半时直接判不通过（样本不足，结论不可信）。

「测试汇总」的 ④ 会按模型汇总这两档的命中率、读取/写入 token 和结论，两档都通过才算综合通过。表头和「通过阈值」列都直接读 `CACHE_HIT_PASS_RATIO`，改配置报告自动跟着变。

### 并发执行

所有 `[模型 × 用例]` 组合放进一个线程池并发执行（`config.MAX_WORKERS`，默认 8），报告行序仍按 `MODELS × ALL_CASES` 的原始顺序还原。并发越高出报告越快，但越容易撞平台限流(429)导致用例误判，建议 4~10。

参数边界用例按官方模型规则判定；当前请求不启用 `thinking`：

- Fable 5、Opus 4.7/4.8/5、Sonnet 5：非默认 `temperature/top_p/top_k` 预期 HTTP 4xx；
- Sonnet 4.5、Haiku 4.5、Opus 4.5、Opus 4.6、Sonnet 4.6：上述采样参数预期 HTTP 2xx；
- 全部模型：`max_tokens=128001` 预期 HTTP 4xx。

每个模型的实际状态会单独记录；返回符合该模型官方预期的状态才记录为“通过”。

### 安全分类器拒答（stop_reason=refusal）

拒答是 **HTTP 200 + `content: []`**，`resp.ok` 仍是 `True`，但用量里照样上报 `cache_creation`。不单独识别的话会踩两个坑：把"模型拒答"误报成"缓存坏了 / 能力缺失"，以及继续把剩下的读取轮全发出去——每轮都被拒、每轮都白付一次缓存写入费。

脚本的处理：

- **任何用例**的响应带 `stop_reason=refusal`，判定说明自动追加「本次请求被安全分类器拒绝」（`cases/base.py`）
- **缓存用例种子轮被拒 → 立即收工**，跳过后续 `CACHE_HIT_ROUNDS` 轮读取。分类器对同一段输入的判定是确定的，种子轮被拒意味着每一轮都会被拒，跳过能省下整整一轮的缓存写入费
- **读取轮零星被拒 → 剔出统计**，单独计数并汇总"白写入的 cache_creation"，不算作"未命中"
- 结论记**跳过**而不是不通过——缓存能力是没测到，不是坏了

配套地，缓存用例的长前缀已从"同一句重复 120 遍"改成一份内容自然的技术文档（`fixtures.long_system_prompt`）：重复刷屏很像 prompt-injection 的模式，正是它触发了 Fable 5 的分类器。新前缀约 5000 token，对最严的缓存下限（Opus 4.6 / Haiku 4.5 的 4096）留 23% 余量，同时比旧版省 55% 的输入。

### 响应延迟分位 P50/P95/P99

报告里每个模型多一行 `AN-LATENCY-001`（P0，结论固定「跳过」，不计入通过率），并在「测试汇总」的 ⑥ 单独出一张表，含 P50/P95/P99、平均、最小、最大和失败调用数。

样本取自**提示词缓存命中率用例**的每一次调用：这批调用负载最整齐（同一段超长系统提示词、同样的 `max_tokens`，每模型固定 `(1 种子轮 + CACHE_HIT_ROUNDS 读取轮) × 2 个 TTL` 次），跨模型可以直接横向比较。其他用例负载差异太大（2M 字符上下文、6MB 图片、4 道复合推理题），混进来算分位数没有意义。只统计 HTTP 成功的调用，失败调用单列计数——失败通常是快速返回的错误响应，混进去会把分位数拉低、掩盖真实延迟。

统计在全部用例跑完之后进行，不受并发执行顺序影响；分位算法与 `latency_test.py` 一致（`statistics.quantiles`，inclusive）。

## 接口地址与鉴权：实际调用 vs 报告展示

把别人家的资源接到自己平台上测时，真实请求要走自己平台，但报告要给对方看资源方的原始地址。config 里因此分成两组：

| 配置项 | 作用 |
|---|---|
| `BASE_URL` / `AUTH_MODE` | **实际发起请求**用的地址和鉴权方式 |
| `REPORT_BASE_URL` / `REPORT_AUTH_MODE` | **只影响报告展示**：①「测试汇总」的接口地址与鉴权方式 ②「测试明细」的调用样例(curl) |

两个 `REPORT_*` 留空就跟实际值一致。填了之后控制台会同时打印实际调用地址和报告展示地址，避免排障时看错。

## 本地校验与执行

只校验配置，不发送网络请求：

```bash
.venv/bin/python run_tests.py --validate-config
```

执行完整测试：

```bash
.venv/bin/python run_tests.py
```

## 官方依据

- [Messages API](https://platform.claude.com/docs/en/api/messages)
- [Extended thinking](https://platform.claude.com/docs/en/build-with-claude/extended-thinking)
- [Claude models overview](https://platform.claude.com/docs/en/about-claude/models/overview)

详细模型、独立模型 Case 和参数矩阵见 [docs/claude-parameter-and-model-matrix.md](docs/claude-parameter-and-model-matrix.md)。Excel 报告只保留“测试汇总”和“测试明细”两张核心 sheet；模型能力预期仍保存在代码配置中，不会额外发请求。
