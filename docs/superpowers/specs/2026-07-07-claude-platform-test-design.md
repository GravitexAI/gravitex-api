# Claude 资源平台测试脚本 + Excel 分析报告 设计文档

日期：2026-07-07

## 背景

`gravitex-api/logs/kuaishou-test-feedback/` 下已有一套针对 `claude-opus-4-7` 在 gravitex 网关（走 `/v1/messages` 与 `/v1/chat/completions` 两种协议）的 QA 测试记录：`opus4-7/scripts/`（每个用例一个 Python 脚本 + `common.py` 公共工具）、`opus4-7/anthropic/*.md`（问题反馈文档）、`kuaishouquestions.xlsx`（客户测试反馈 + 复测结果汇总表）。

现在公司要接入新的第三方 Claude 资源平台，需要一套可复用、可配置的测试脚本，只针对 **Anthropic 原生协议 `/v1/messages`**，跑完后自动生成一份规范的 Excel 分析报告，用于评估新平台的协议兼容性和功能完整性。

## 目标

1. 编写一套可配置的 Python 测试脚本，覆盖 `/v1/messages` 的核心能力点（在原 8 类问题基础上，结合联网调研的 Anthropic 现有特性做了扩充）。
2. 认证方式做成互斥单选配置（`x-api-key + anthropic-version` 或 `Authorization: Bearer`），不需要同时发送两种。
3. 跑完自动生成 Excel 报告（测试汇总 + 测试明细两个 sheet），报告的关键字段位置沿用原 excel 习惯（模型/复现命令curl/预期响应/实际响应），并新增自动判定的"测试结论"字段。
4. 所有需要按平台改动的配置项（baseurl、鉴权方式、模型名、报告文件名、图片/PDF 资源地址）集中放在文件最前面，方便切换到新平台时只改几行。
5. 图片/PDF 类的 URL 资源允许留空，脚本自动判定为 SKIP，不阻塞整体运行；base64 类用例使用脚本内置生成的最小合法样本，不依赖外部资源。

## 不做的事

- 不测试 `/v1/chat/completions`（OpenAI 兼容协议）——已与用户确认本次只做原生协议。
- 不做真实的高并发/压测（不是这次的目的）。
- 不做 Computer Use / MCP Connector / Files API / Message Batches 等边缘 beta 能力的测试（属于小众场景，先聚焦核心协议兼容性，后续按需再加用例）。
- 不在 Excel 报告里保留"影响范围""修复建议"两列（已与用户确认去掉）。

## 目录结构

新建独立目录，与 kuaishou 历史目录并列，避免和历史客户反馈资料混在一起，同时脚本本身与"某个特定客户"解耦，可以复用于任意新平台：

```
gravitex-api/logs/claude-platform-test/
├── config.py              # 全部可配置项（见下）
├── client.py              # HTTP 请求 + 双鉴权模式头拼装 + SSE 解析 + curl 复现命令生成 + 内容截断工具
├── fixtures.py            # 内置生成的最小合法 base64 图片 / PDF 样本，及 4 道扩展思考推理题文本
├── cases/
│   ├── __init__.py         # 汇总 ALL_CASES 列表，供 run_tests.py 遍历
│   ├── basic.py            # 基础对话
│   ├── vision.py           # 视觉图片
│   ├── documents.py        # PDF/文档
│   ├── tools.py            # 工具调用
│   ├── thinking.py         # 扩展思考
│   ├── caching.py          # 提示缓存
│   ├── context.py          # 上下文边界
│   ├── token_counting.py   # count_tokens 接口
│   ├── errors.py           # 错误处理与协议规范
│   └── streaming.py        # SSE 流式协议完整性
├── report.py               # 生成 Excel 报告
├── run_tests.py            # 主入口
└── requirements.txt        # requests, openpyxl
```

### 模块职责（各自独立、边界清晰）

- **`config.py`**：只放常量，不含逻辑。任何切换平台的操作只需要改这一个文件。
- **`client.py`**：不知道任何具体用例的业务含义，只提供通用能力：
  - `build_auth_headers()`：按 `AUTH_MODE` 生成鉴权头
  - `post_messages(payload, stream)`：发起请求，非流式返回 `(status, json_body)`，流式返回 `(status, parsed_result, raw_sse_events)`
  - `build_curl(payload)`：生成脱敏后的 curl 复现命令字符串
  - `truncate(text, max_len)`：超长文本掐头去尾，保留 `<内容省略，原始长度:N字符>` 提示（沿用原 excel 习惯）
- **`fixtures.py`**：纯数据，无网络请求。内置：
  - 一张极小 PNG 的 base64（程序内手写字节，非外部文件）
  - 一份最小合法 PDF 的 base64
  - 一份用于超 5MB 测试的超大 base64（用重复数据拼出 ~6MB）
  - 4 道推理题原文（用户提供的 a³+b³+c³=3abc / 真假话 / 天平称重 / 过河问题）
  - 一份用于触发 prompt caching 的 >1024 token 长系统提示词
  - 一份接近上下文上限的超长文本（沿用原 `common.py` 的 `large_context_text` 思路）
- **`cases/*.py`**：每个分类一个文件，文件内是若干 `TestCase` 定义（见下方数据结构），只依赖 `client.py` 和 `fixtures.py`，不相互依赖。
- **`report.py`**：只知道"给我一批 `TestResult`，我输出一个 xlsx"，不关心这些结果是怎么跑出来的。
- **`run_tests.py`**：编排者，串联 `cases` → 执行 → `report`，并打印控制台摘要。

这样任何一层出问题（比如换平台后鉴权失败、或者只想加一个新用例分类），改动都局限在单一文件里。

## `config.py` 配置项清单

```python
# ====== 平台信息（每次换平台只改这里）======
PLATFORM_NAME = "示例平台"                # 体现在报告文件名和汇总sheet里
BASE_URL = "https://api.example-platform.com"
MODEL = "claude-opus-4-7"                 # 该平台上的模型标识

# ====== 鉴权方式（二选一，互斥）======
AUTH_MODE = "anthropic"                   # "anthropic" | "bearer"
API_KEY = "sk-xxxxxxxx"
ANTHROPIC_VERSION = "2023-06-01"          # 仅 AUTH_MODE=anthropic 时发送 x-api-key + anthropic-version
ANTHROPIC_BETA = ""                       # 逗号分隔，留空则不发送该 header

# ====== 请求行为 ======
TIMEOUT_SECONDS = 300
REQUEST_INTERVAL_SECONDS = 1              # 用例之间的间隔，避免触发限流
PRINT_HTTP = True                         # 控制台是否打印完整请求/响应

# ====== 报告输出 ======
REPORT_FILENAME = "claude-platform-test-report.xlsx"

# ====== 图片/文件测试资源（留空则自动 SKIP 对应用例，可后续回填 OSS 链接）======
IMAGE_URL_SINGLE = ""       # 单图 URL 测试，如 "https://xxx.oss.../test.jpg"
IMAGE_URLS_MULTI = []       # 多图 URL 测试，如 ["https://.../a.jpg", "https://.../b.png"]
PDF_URL = ""                # PDF URL 测试
PDF_FILE_PATH = ""          # 或本地 PDF 文件路径（与 PDF_URL 二选一都可留空）
```

## `TestCase` 数据结构

```python
@dataclass
class TestCase:
    case_id: str          # 如 "AN-BASIC-001"
    category: str         # 如 "基础对话"
    name: str             # 用例名称
    severity: str         # 失败时的严重程度: "🔴阻塞性问题" / "🟡功能缺陷" / "🟢体验建议"
    build: Callable[[], dict]              # 返回请求 payload
    validate: Callable[[...], TestOutcome] # 返回判定结果
    requires: Callable[[], bool] | None    # 返回 False 则整体判定为 SKIP（如资源留空）
```

```python
@dataclass
class TestOutcome:
    verdict: str           # PASS / FAIL / SKIP / ERROR
    expected: str          # 预期响应描述
    actual_summary: str    # 实际响应摘要（会被截断写入excel）
    fail_reason: str = ""  # 仅 FAIL 时填写
    key_metrics: dict = field(default_factory=dict)  # 耗时/stop_reason/output_tokens/是否含thinking等
```

## 测试用例目录（共 36 项）

### 1. 基础对话 basic.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-BASIC-001 | 非流式基础问答 | HTTP 200，content 非空，stop_reason=end_turn |
| AN-BASIC-002 | 流式基础问答 | SSE 完整走完 message_start→message_stop，拼接内容非空 |
| AN-BASIC-003 | 多轮对话上下文保持 | 第二轮回答中包含第一轮提到的关键词 |
| AN-BASIC-004 | system prompt 生效 | 回复以约定前缀开头 |
| AN-BASIC-005 | stop_sequences 生效 | stop_reason=stop_sequence，输出不包含终止符之后的内容 |

### 2. 视觉图片 vision.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-VISION-001 | base64 图片输入（内置样本） | 200，非 refusal，content 非空 |
| AN-VISION-002 | 图片 URL 输入（`IMAGE_URL_SINGLE`） | 同上；资源留空则 SKIP |
| AN-VISION-003 | 多图输入（内置样本或 `IMAGE_URLS_MULTI`） | 同上 |
| AN-VISION-004 | 超 5MB 图片（内置生成~6MB base64） | 期望明确 400 错误或平台自动处理，不应 5xx / 无响应 |
| AN-VISION-005 | 不支持的图片 media_type | 期望 400 + 明确错误信息 |

### 3. PDF/文档 documents.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-DOC-001 | base64 PDF 输入（内置最小样本） | 200，非 refusal |
| AN-DOC-002 | PDF URL 输入（`PDF_URL`） | 资源留空则 SKIP |
| AN-DOC-003 | document + citations | 响应 content 中出现 citations 类型 block（若平台不支持则记录 FAIL 并注明"未见citations块"）|

### 4. 工具调用 tools.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-TOOL-001 | tool_choice=auto | 触发 tool_use 或合理拒绝 |
| AN-TOOL-002 | tool_choice=any | 强制返回 tool_use |
| AN-TOOL-003 | tool_choice=none | 不返回 tool_use |
| AN-TOOL-004 | tool_choice={type:tool,name} | 返回指定工具的 tool_use |
| AN-TOOL-005 | 多工具并行调用 | 一次返回 ≥2 个 tool_use block |
| AN-TOOL-006 | 流式 tool_use | SSE 中出现 content_block_start(type=tool_use) + input_json_delta，可拼出合法 JSON |
| AN-TOOL-007 | 多轮工具结果回传 | 提交 tool_result 后二轮响应 200 且内容合理承接 |

### 5. 扩展思考 thinking.py（复杂题目用你提供的 4 道）
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-THINK-001 | 非流式 thinking，题目1（a³+b³+c³=3abc） | 存在非空 thinking content block |
| AN-THINK-002 | 流式 thinking，题目2（真假话三人） | SSE 中出现 thinking_delta 事件 |
| AN-THINK-003 | interleaved thinking + tools，题目3（天平称重） | 同时出现 thinking 与 tool_use（若平台支持该 beta） |
| AN-THINK-004 | 大 budget_tokens，题目4（过河问题） | thinking 内容长度超过阈值且无截断错误 |

### 6. 提示缓存 caching.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-CACHE-001 | 5 分钟 TTL 缓存写入+命中 | 第一次 cache_creation_input_tokens>0；第二次相同请求 cache_read_input_tokens>0 |
| AN-CACHE-002 | 1 小时 TTL 缓存写入+命中 | 同上，ttl="1h"；系统提示词固定 >1024 token 保证触发缓存 |

### 7. 上下文边界 context.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-CTX-001 | 接近最大上下文（~199万字符） | 不应为 refusal / 空输出，应正常响应或明确的容量错误 |
| AN-CTX-002 | max_tokens 极小值(=1) | stop_reason=max_tokens，输出长度受限 |
| AN-CTX-003 | 超长输出请求(max_tokens=64000+) | 记录平台是否支持扩展输出（成功或明确拒绝均可，只是记录能力边界，不视为必然失败）|

### 8. Token 计数 token_counting.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-COUNT-001 | POST /v1/messages/count_tokens | 200，返回 input_tokens 字段且为正整数 |

### 9. 错误处理与协议规范 errors.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-ERR-001 | 无效 API Key | 401，error.type=authentication_error |
| AN-ERR-002 | 无效 model 名称 | 400/404，error 字段存在 |
| AN-ERR-003 | 缺失必填参数(max_tokens) | 400，error.type=invalid_request_error |
| AN-ERR-004 | messages 为空数组 | 400 |
| AN-ERR-005 | 错误响应体结构规范性 | 顶层 `{"type":"error","error":{"type":...,"message":...}}` |

### 10. 流式协议完整性 streaming.py
| ID | 用例 | 判定要点 |
|---|---|---|
| AN-STREAM-001 | SSE 事件类型完整性 | 依次出现 message_start/content_block_start/content_block_delta/content_block_stop/message_delta/message_stop |

## 判定结果与严重程度

- **PASS**：断言全部满足。
- **FAIL**：断言不满足，`fail_reason` 写明具体差异；`severity` 取用例预设值。
- **SKIP**：因资源留空（图片/PDF URL）主动跳过，不计入通过率分母，也不算失败。
- **ERROR**：请求本身抛异常（网络错误/超时/DNS），单独统计，不与 FAIL 混淆（沿用原 excel 对 DNS 类问题单独标注的做法）。

## Excel 报告结构

### Sheet 1：测试汇总
- 平台名称 / BASE_URL / 模型 / 鉴权方式 / 测试时间
- 总用例数 / PASS / FAIL / SKIP / ERROR / 通过率
- 按分类的通过率一览
- 按严重程度统计 FAIL 数量

### Sheet 2：测试明细（一行一个用例）
列：用例编号 / 测试分类 / 用例名称 / 严重程度 / 复现命令(curl) / HTTP状态码 / 关键指标 / 预期响应 / 实际响应 / 测试结论 / 失败原因 / 备注

- "复现命令(curl)"、"实际响应"超长时按 `<内容省略，原始长度:N字符>` 规则截断（保留头尾各约 500 字符）。
- "测试结论"列按 PASS/FAIL/SKIP/ERROR 用 openpyxl 填充背景色（绿/红/灰/橙）。
- "备注"列留空，供人工在对接平台时填写。

## 验证方式

由于目前没有真实的新平台 baseurl/key，先用现有 gravitex 网关配置（`opus4-7/scripts/common.py` 中的默认 baseurl + Bearer 鉴权）作为"某平台"的替身，跑通全部用例，确认：

1. 脚本能顺利运行完不崩溃，`ERROR` 只出现在预期的网络类问题上。
2. Excel 报告能正常生成，两个 sheet 内容、格式、截断逻辑符合设计。
3. 各分类至少能产出合理的 PASS/FAIL/SKIP 判定（允许因为该网关本身已知问题——如 thinking 未透传——出现 FAIL，这属于正常发现问题，不代表脚本有 bug）。

之后切换到真实新平台时，只需要修改 `config.py` 顶部字段。
