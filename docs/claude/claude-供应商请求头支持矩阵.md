# Claude 供应商请求头支持矩阵（AWS Bedrock / Vertex AI）

> 用途：网关转发 Anthropic 兼容请求到 Bedrock / Vertex AI 时，按本文白名单对 `anthropic-beta` 与 HTTP header 做过滤，避免 `ValidationException: invalid beta flag` 类报错。
>
> 数据来源：
> - Anthropic SDK 官方枚举（`anthropic-sdk-python/src/anthropic/types/anthropic_beta_param.py`）
> - AWS Bedrock 官方文档（`model-parameters-anthropic-claude-messages-request-response`）
> - Google Vertex AI Claude 官方文档（`platform.claude.com/docs/en/build-with-claude/claude-on-vertex-ai`）
> - LiteLLM 生产环境过滤配置（`BerriAI/litellm/litellm/anthropic_beta_headers_config.json`）

---

## 0. 基础概念

**`anthropic-beta` 不是一组请求头，而是一个 header 里逗号分隔的多个 flag 值。**

| 传递形式 | Anthropic 直连 | AWS Bedrock | Vertex AI |
|---------|---------------|-------------|-----------|
| 载体 | HTTP Header | **请求 body 字段** | HTTP Header |
| 字段名 | `anthropic-beta` | `anthropic_beta`（数组） | `anthropic-beta` |
| 多值分隔 | 逗号 | JSON 数组 | 逗号 |
| version 字段 | `anthropic-version: 2023-06-01` | `"anthropic_version": "bedrock-2023-05-31"` | `"anthropic_version": "vertex-2023-10-16"` |

> ⚠️ **Bedrock 不接 HTTP header 形式的 anthropic-beta**，必须搬到 body 的 `anthropic_beta` 数组里。

### 0.1 为什么需要"改写"和"迁移"？

Anthropic 直连、Bedrock、Vertex 这三家虽然都跑 Claude 模型，但**协议外壳完全不一样**——Bedrock 套了一层 AWS SDK / SigV4 签名，Vertex 套了一层 GCP OAuth + URL 路由。同一份请求要适配到不同后端，就需要把客户端发的"Anthropic 原生 header"按目标改写。

**举两个最常踩坑的字段：**

#### ① `Anthropic-Version` header → body 字段（Bedrock & Vertex 都要改）

- **原始作用**：告诉 Anthropic API 服务端"我希望按哪个 API 版本解析这个请求"（类似 RESTful API 的 `Accept-Version`）。
- **直连 Anthropic 时**：作为 HTTP header 发送，固定值 `2023-06-01`。
- **Bedrock 不认这个 header**：Bedrock 自己定义了一个特殊字段 `anthropic_version`，必须**放在请求 body 的 JSON 里**，固定值 `bedrock-2023-05-31`。
- **Vertex 也不认 header 版本**：Vertex 也要求放在 body 里，固定值 `vertex-2023-10-16`。

  **改写示意：**

  ```diff
  - HTTP Header:  Anthropic-Version: 2023-06-01
  - HTTP Header:  Anthropic-Version: 2023-06-01
  + Bedrock body: { "anthropic_version": "bedrock-2023-05-31", ... }
  + Vertex body:  { "anthropic_version": "vertex-2023-10-16",  ... }
  ```

#### ② `Anthropic-Beta` header → body 数组（仅 Bedrock 要迁移）

- **原始作用**：开启 Anthropic 的 beta 特性（如 1M 上下文、Computer Use 等）。
- **直连 Anthropic / Vertex 时**：作为 HTTP header 发送，多个 flag 用逗号分隔。
- **Bedrock 不认这个 header**：Bedrock 把 beta 开关也搬到了 body 里，字段名是 `anthropic_beta`，**值是 JSON 数组**（不是字符串）。

  **迁移示意：**

  ```diff
  // 客户端原始请求
  - HTTP Header: Anthropic-Beta: context-1m-2025-08-07,effort-2025-11-24

  // 转发到 Bedrock 前
  - HTTP Header: Anthropic-Beta: ...   // 删掉
  + Bedrock body: {
  +   "anthropic_version": "bedrock-2023-05-31",
  +   "anthropic_beta": ["context-1m-2025-08-07", "effort-2025-11-24"],
  +   ...
  + }
  ```

  **关键点**：
  - 字符串变数组，逗号分隔 → JSON `[]`
  - header 里别再留这个字段，否则 Bedrock 不会读，最多被忽略
  - 数组里每个值还要先过本文档第 1.2/1.3 节的白名单，**不在白名单的元素必须剔除**，否则 Bedrock 返回 `ValidationException: invalid beta flag`

---

---

## 1. AWS Bedrock 支持矩阵

### 1.1 HTTP Header 处理

| Header | 行为 | 字段作用 / 处理理由 |
|--------|------|-------------------|
| `Authorization` | ❌ 必须替换为 AWS SigV4 签名 | 客户端发的是 `Bearer sk-xxx`（Anthropic API Key），Bedrock 走 AWS 身份认证体系，必须用 AccessKey + SecretKey 做 SigV4 签名（一般通过 AWS SDK 自动完成） |
| `Anthropic-Version` | ❌ 改写为 body `"anthropic_version": "bedrock-2023-05-31"` | 指定 Anthropic API 协议版本号。Bedrock 不读 header 里的版本，只看 body 字段，且只接受 `bedrock-2023-05-31` 这个 Bedrock 专属版本号 |
| `Anthropic-Beta` | ❌ 从 header 移除，迁移到 body `"anthropic_beta": [...]` | 开启 beta 特性（如 1M 上下文）。Bedrock 协议要求把它放在 body 里且是 JSON 数组形式，header 里发不会生效 |
| `Anthropic-Dangerous-Direct-Browser-Access` | ❌ 直接丢弃 | Anthropic 直连场景下，告知服务端"我允许浏览器直接调 API"（关闭 CORS 保护）。Bedrock 没这个概念，留着会引发未知字段拒绝 |
| `Content-Type` | ✅ 保留 `application/json` | 标准 HTTP header，告知 body 是 JSON 格式，所有 REST API 都需要 |
| `Accept` | ✅ 保留 `application/json` | 告知客户端期望的响应格式 |
| `Accept-Encoding` | ✅ 保留（如 `gzip`） | 启用压缩传输，减少响应体积 |
| `User-Agent` | ✅ 保留（可附加自己的标识） | 客户端版本标识（如 `claude-cli/2.1.187`）。AWS 不会因 UA 拒绝请求，但保留方便追踪客户端类型 |
| `X-Stainless-*` | ✅ 透传（不影响） | Anthropic 官方 SDK（Stainless 生成）的元信息（语言/版本/平台等）。Bedrock 会忽略未识别的自定义 header，留着不影响功能 |
| `X-Claude-Code-Session-Id` | ✅ 透传（不影响） | Claude Code CLI 的会话追踪 ID，对 Bedrock 透明 |
| `X-App` / `X-Cli-Port` | ✅ 透传 | 客户端来源标识，Bedrock 忽略 |
| `X-Forwarded-For` / `X-Real-Ip` | ⚠️ 视情况保留 | 反向代理链路上的真实 IP 追踪。如果要让 AWS 看到原始客户端 IP，可保留；否则可删 |
| `X-Bd-*` / `X-Toutiao-*` / `X-Ksap-*` | ❌ 建议丢弃 | 中间反代/CDN（火山引擎/字节系）注入的自定义 header，对 Bedrock 无意义 |
| `Content-Length` | ⚠️ AWS SDK 自动重算 | body 改写后长度会变，让 AWS SDK 重新计算，**不要手动透传客户端原始值**，否则会因签名不匹配被拒 |
| `Host` | ❌ 必须替换 | 客户端写的是 Anthropic 域名（如 `api.anthropic.com`），转发时必须替换为 Bedrock 端点（如 `bedrock-runtime.us-east-1.amazonaws.com`） |

### 1.2 `anthropic_beta` 白名单（InvokeModel / InvokeModelWithResponseStream）

| Beta Flag | 用途 | 兼容模型 |
|-----------|------|---------|
| `computer-use-2025-01-24` | Computer Use（旧版） | Claude 3.7 Sonnet |
| `computer-use-2025-11-24` | Computer Use（新版） | Claude 4+ |
| `token-efficient-tools-2025-02-19` | Tool 调用 token 优化 | Claude 3.7 / 4+ |
| `output-128k-2025-02-19` | 输出最大 128K tokens | Claude 3.7 Sonnet |
| `dev-full-thinking-2025-05-14` | 原始 thinking（需 AWS 开通） | Claude 4+ |
| `context-1m-2025-08-07` | 1M 上下文窗口 | Sonnet 4 / 4.6 / Opus 4.6 |
| `effort-2025-11-24` | Effort 参数 | Opus 4.5 |
| `tool-search-tool-2025-10-19` | 工具搜索 | Opus 4.5 |
| `tool-examples-2025-10-29` | 工具调用示例 | Opus 4.5 |
| `fine-grained-tool-streaming-2025-05-14` | 细粒度工具流 | Claude 4+ |
| `compact-2026-01-12` | 上下文压缩 | — |

### 1.3 `anthropic_beta` 白名单（Converse / ConverseStream 额外支持）

> Converse API 是 Bedrock 的高层 API，比 InvokeModel 多支持一些 beta：

| Beta Flag | 备注 |
|-----------|------|
| `interleaved-thinking-2025-05-14` | 交错思考，**InvokeModel 不支持** |
| `context-management-2025-06-27` | 上下文自动管理，**InvokeModel 不支持** |
| `structured-outputs-2025-11-13` | 结构化输出 |

### 1.4 Header 值映射（客户端→Bedrock 名字不同）

| 客户端发的值 | 转发给 Bedrock 时改写为 |
|------------|---------------------|
| `advanced-tool-use-2025-11-20` | `tool-search-tool-2025-10-19` |

### 1.5 Bedrock 明确**不支持**的（高频踩坑）

```
claude-code-20250219          # Claude Code 内部 flag，仅 Anthropic 直连接受
redact-thinking-2026-02-12    # 新 flag，Bedrock 未实现
thinking-token-count-2026-05-13
prompt-caching-2024-07-31     # Bedrock 用自己的 cachePoint 字段
prompt-caching-scope-2026-01-05
extended-cache-ttl-2025-04-11
advisor-tool-2026-03-01
fast-mode-2026-02-01
skills-2025-10-02
files-api-2025-04-14
web-search-2025-03-05         # Bedrock 走 Bedrock Agents
web-fetch-2025-09-10
code-execution-2025-05-22
code-execution-2025-08-25
mcp-client-2025-04-04
mcp-client-2025-11-20
mcp-servers-2025-12-04
message-batches-2024-09-24
pdfs-2024-09-25
token-counting-2024-11-01
model-context-window-exceeded-2025-08-26
output-300k-2026-03-24
user-profiles-2026-03-24
managed-agents-2026-04-01
cache-diagnosis-2026-04-07
oauth-2025-04-20
server-side-fallback-2026-06-01
fallback-credit-2026-06-01
bash_20241022 / bash_20250124               # 用 tool type 而非 beta
text_editor_20241022 / text_editor_20250124 # 同上
```

---

## 2. Google Vertex AI 支持矩阵

### 2.1 HTTP Header 处理

| Header | 行为 | 字段作用 / 处理理由 |
|--------|------|-------------------|
| `Authorization` | ❌ 必须替换为 GCP `Bearer <access_token>` | 客户端发的是 Anthropic 的 `sk-xxx` API Key，Vertex 走 GCP 身份认证。需要先用 Service Account 或 `gcloud auth application-default login` 拿到 OAuth2 access_token，再以 `Bearer <token>` 形式放进 Authorization |
| `Anthropic-Version` | ❌ 改写为 body `"anthropic_version": "vertex-2023-10-16"` | 同 Bedrock：Vertex 也要求把版本号搬到 body，且只接受 `vertex-2023-10-16` 这个 Vertex 专属版本号 |
| `Anthropic-Beta` | ✅ 保留为 HTTP header（与 Anthropic 直连一致） | Vertex 完整复用了 Anthropic 的 beta 协议外壳，header 形式即可。但仍需按本文 2.2 节做白名单过滤，不支持的 flag 要剔除 |
| `Anthropic-Dangerous-Direct-Browser-Access` | ❌ 直接丢弃 | Vertex 不支持这个浏览器直连开关，留着可能被拒 |
| `Content-Type` | ✅ 保留 `application/json` | 标准 REST API 必备 |
| `Accept` | ✅ 保留 `application/json` | 同上 |
| `Accept-Encoding` | ✅ 保留（如 `gzip`） | Vertex 支持响应压缩 |
| `User-Agent` | ✅ 保留 | 客户端标识，Vertex 不依赖此字段做鉴权 |
| `X-Stainless-*` | ✅ 透传（不影响） | SDK 元信息，Vertex 忽略未识别 header |
| `X-Claude-Code-Session-Id` | ✅ 透传 | 同上 |
| `X-Forwarded-For` / `X-Real-Ip` | ⚠️ 视情况保留 | GCP 通常会重写这些 header，按需保留 |
| `X-Bd-*` / `X-Toutiao-*` / `X-Ksap-*` | ❌ 建议丢弃 | 内部反代/CDN 注入，对 Vertex 无意义 |
| `Content-Length` | ⚠️ HTTP 客户端自动重算 | body 改写后让底层 HTTP 库重新计算，**不要手动透传** |
| `Host` | ❌ 必须替换 | 替换为 Vertex 区域端点（如 `<region>-aiplatform.googleapis.com` 或 `aiplatform.googleapis.com`） |

> 💡 **关键差异**：Vertex 仍把 `Anthropic-Beta` 当 HTTP header 用，**不像 Bedrock 要搬到 body**。但 Vertex 自己的白名单更窄（见 2.2），还是要过滤。

### 2.2 `anthropic-beta` 白名单

| Beta Flag | 用途 |
|-----------|------|
| `computer-use-2025-01-24` | Computer Use（旧版） |
| `computer-use-2025-11-24` | Computer Use（新版） |
| `context-1m-2025-08-07` | 1M 上下文窗口 |
| `context-management-2025-06-27` | 上下文自动管理 |
| `compact-2026-01-12` | 上下文压缩 |
| `interleaved-thinking-2025-05-14` | 交错思考 |
| `tool-search-tool-2025-10-19` | 工具搜索 |
| `web-search-2025-03-05` | Web 搜索工具 |

### 2.3 Header 值映射（客户端→Vertex 名字不同）

| 客户端发的值 | 转发给 Vertex 时改写为 |
|------------|---------------------|
| `advanced-tool-use-2025-11-20` | `tool-search-tool-2025-10-19` |

### 2.4 Vertex AI 明确**不支持**的

```
claude-code-20250219
redact-thinking-2026-02-12
thinking-token-count-2026-05-13
prompt-caching-2024-07-31         # Vertex 用自己的缓存机制
prompt-caching-scope-2026-01-05
advisor-tool-2026-03-01
effort-2025-11-24
fast-mode-2026-02-01
files-api-2025-04-14
fine-grained-tool-streaming-2025-05-14
output-128k-2025-02-19
skills-2025-10-02
structured-outputs-2025-11-13
token-efficient-tools-2025-02-19
web-fetch-2025-09-10
code-execution-2025-05-22 / 2025-08-25
mcp-client-2025-04-04 / 2025-11-20
mcp-servers-2025-12-04
bash_20241022 / bash_20250124
text_editor_20241022 / text_editor_20250124
```

---

## 3. 三供应商 Beta Flag 对比速查表（全量）

> 按"支持广度"从高到低分组，方便主人快速判断哪些 flag 在所有供应商上都能用。

### 3.1 三家全支持 ✅✅✅

| Beta Flag | 用途 | Anthropic | Bedrock | Vertex |
|-----------|------|:-:|:-:|:-:|
| `computer-use-2025-01-24` | Computer Use（旧版） | ✅ | ✅ | ✅ |
| `computer-use-2025-11-24` | Computer Use（新版） | ✅ | ✅ | ✅ |
| `context-1m-2025-08-07` | 1M 上下文窗口 | ✅ | ✅ | ✅ |
| `compact-2026-01-12` | 上下文压缩 | ✅ | ✅ | ✅ |
| `tool-search-tool-2025-10-19` | 工具搜索 | ✅ | ✅ | ✅ |

### 3.2 仅 Bedrock 不支持（Vertex 支持）

| Beta Flag | 用途 | Anthropic | Bedrock | Vertex |
|-----------|------|:-:|:-:|:-:|
| `web-search-2025-03-05` | Web 搜索工具 | ✅ | ❌（走 Bedrock Agents） | ✅ |

### 3.3 仅 Vertex 不支持（Bedrock 支持）

| Beta Flag | 用途 | Anthropic | Bedrock | Vertex |
|-----------|------|:-:|:-:|:-:|
| `dev-full-thinking-2025-05-14` | 原始 thinking（需 AWS 开通） | ✅ | ✅ | ❌ |
| `effort-2025-11-24` | Effort 参数（Opus 4.5） | ✅ | ✅ | ❌ |
| `output-128k-2025-02-19` | 输出最大 128K tokens | ✅ | ✅ | ❌ |
| `token-efficient-tools-2025-02-19` | Tool 调用 token 优化 | ✅ | ✅ | ❌ |
| `tool-examples-2025-10-29` | 工具调用示例 | ✅ | ✅ | ❌ |
| `fine-grained-tool-streaming-2025-05-14` | 细粒度工具流 | ✅ | ✅ | ❌ |

### 3.4 Bedrock 条件支持（仅 Converse API）

| Beta Flag | 用途 | Anthropic | Bedrock | Vertex |
|-----------|------|:-:|:-:|:-:|
| `context-management-2025-06-27` | 上下文自动管理 | ✅ | ⚠️ 仅 Converse | ✅ |
| `interleaved-thinking-2025-05-14` | 交错思考 | ✅ | ⚠️ 仅 Converse | ✅ |
| `structured-outputs-2025-11-13` | 结构化输出 | ✅ | ⚠️ 仅 Converse | ❌ |

### 3.5 重命名 / 转译规则

| 客户端发的值 | Bedrock 改写为 | Vertex 改写为 |
|------------|---------------|--------------|
| `advanced-tool-use-2025-11-20` | `tool-search-tool-2025-10-19` | `tool-search-tool-2025-10-19` |

### 3.6 仅 Anthropic 直连支持（Bedrock & Vertex 都拒绝）

| Beta Flag | 用途 | Anthropic | Bedrock | Vertex |
|-----------|------|:-:|:-:|:-:|
| `claude-code-20250219` | Claude Code 内部 flag | ✅ | ❌ | ❌ |
| `prompt-caching-2024-07-31` | Prompt 缓存（v1） | ✅ | ❌（用 cachePoint） | ❌ |
| `prompt-caching-scope-2026-01-05` | Prompt 缓存作用域 | ✅ | ❌ | ❌ |
| `extended-cache-ttl-2025-04-11` | 扩展缓存 TTL | ✅ | ❌ | ❌ |
| `files-api-2025-04-14` | Files API | ✅ | ❌ | ❌ |
| `skills-2025-10-02` | Skills | ✅ | ❌ | ❌ |
| `web-fetch-2025-09-10` | Web Fetch 工具 | ✅ | ❌ | ❌ |
| `code-execution-2025-05-22` | Code Execution（旧） | ✅ | ❌ | ❌ |
| `code-execution-2025-08-25` | Code Execution（新） | ✅ | ❌ | ❌ |
| `mcp-client-2025-04-04` | MCP Client（旧） | ✅ | ❌ | ❌ |
| `mcp-client-2025-11-20` | MCP Client（新） | ✅ | ❌ | ❌ |
| `message-batches-2024-09-24` | 批量消息 API | ✅ | ❌ | ❌ |
| `pdfs-2024-09-25` | PDF 输入 | ✅ | ❌ | ❌ |
| `token-counting-2024-11-01` | Token 计数 API | ✅ | ❌ | ❌ |
| `model-context-window-exceeded-2025-08-26` | 超长上下文错误处理 | ✅ | ❌ | ❌ |
| `redact-thinking-2026-02-12` | Thinking 内容脱敏 | ✅ | ❌ | ❌ |
| `thinking-token-count-2026-05-13` | Thinking token 计数 | ✅ | ❌ | ❌ |
| `advisor-tool-2026-03-01` | Advisor 工具 | ✅ | ❌ | ❌ |
| `fast-mode-2026-02-01` | Fast 模式 | ✅ | ❌ | ❌ |
| `output-300k-2026-03-24` | 输出 300K tokens | ✅ | ❌ | ❌ |
| `user-profiles-2026-03-24` | 用户画像 | ✅ | ❌ | ❌ |
| `managed-agents-2026-04-01` | Managed Agents | ✅ | ❌ | ❌ |
| `cache-diagnosis-2026-04-07` | 缓存诊断 | ✅ | ❌ | ❌ |
| `server-side-fallback-2026-06-01` | 服务端 fallback | ✅ | ❌ | ❌ |
| `fallback-credit-2026-06-01` | Fallback 额度 | ✅ | ❌ | ❌ |
| `oauth-2025-04-20` | OAuth 认证 | ✅ | ❌ | ❌ |

### 3.7 已废弃 / 非 Beta（仅作参考）

| 标识 | 类型 | 说明 |
|------|------|------|
| `computer-use-2024-10-22` | 老版 beta | 已被 `computer-use-2025-01-24` 取代 |
| `mcp-servers-2025-12-04` | 实验性 | Anthropic 官方也已 null，未真正发布 |
| `structured-output-2024-03-01` | 已废弃 | 注意是单数 output，已被 `structured-outputs-2025-11-13` 取代 |
| `bash_20241022` / `bash_20250124` | 工具类型 | 不是 beta flag，而是 `tools[].type` 值 |
| `text_editor_20241022` / `text_editor_20250124` | 工具类型 | 同上 |

> 图例：✅ 支持　❌ 不支持　⚠️ 条件支持

### 3.8 速记口诀（小c 帮主人记住核心规律）

```
 1M / Computer-Use / tool-search / compact  —— 三家通吃
 effort / output-128k / 细粒度工具流         —— Bedrock 独占
 web-search                                  —— Vertex 独占（Bedrock 没）
 interleaved-thinking / context-management   —— Bedrock 仅 Converse 接
 prompt-caching / files-api / skills / mcp   —— 只能直连 Anthropic
 claude-code-* / redact-thinking-*           —— Claude Code 内部专用，转发必拒
```

---

## 4. Go 网关实现（gravitex-api 实际改动）

> **重要前提**：项目里已经有完整的请求头处理基础设施（见下表）。本次改造**只补"白名单过滤 + 重命名"这一层**，不重写已有逻辑。

### 4.0 现状全图（改造前）

| 渠道 | 现有处理位置 | 已有能力 | 缺失能力 |
|------|------------|---------|---------|
| Claude 直连 | `claude/adaptor.go` `CommonClaudeHeadersOperation()` | 透传 `anthropic-beta` header；透传 `anthropic-version`（默认 `2023-06-01`） | 无 |
| AWS Bedrock | `aws/dto.go` `formatRequest()` | header `anthropic-beta` → body `anthropic_beta` 数组；写入 `anthropic_version: bedrock-2023-05-31` | 没过滤、没重命名 |
| Vertex AI | `vertex/adaptor.go:SetupRequestHeader()` 复用 `claude.CommonClaudeHeadersOperation()`；`vertex/adaptor.go:53` 写入 `anthropic_version: vertex-2023-10-16` | 透传 `anthropic-beta` header | 没过滤、没重命名 |

### 4.1 三种渠道的差别化策略

| 渠道类型 | 策略 | 调试日志 |
|---------|------|---------|
| **Anthropic 直连** | 完全透传，不改 header / body | 仅 debug 模式下识别"未知新 flag"并告警 |
| **AWS Bedrock** | 过滤+重命名 beta、删除多余 header、`Anthropic-Beta`/`Anthropic-Version` 搬到 body | ✅ 上线初期记录所有丢弃/转换，稳定后关闭 |
| **Vertex AI** | 过滤+重命名 beta、删除多余 header、仅 `Anthropic-Version` 搬到 body | ✅ 同上 |

### 4.2 新增文件 `relay/channel/claude/beta_filter.go`

放在 `claude` 包下，因为是 Anthropic 协议兼容能力。AWS / Vertex 渠道通过 import claude 包复用，避免循环依赖。

#### 对外 API（约 200 行实现，详见源文件）

```go
package claude

// BetaFilterDebugLog 控制是否输出过滤/重命名的详细日志。
// 上线初期保持 true；稳定后改为 false，或挂到环境变量 / 配置中心。
var BetaFilterDebugLog = true

// FilterTarget 标识目标上游
type FilterTarget int
const (
    TargetAnthropicDirect FilterTarget = iota // 直连：透传 + 未知 flag 告警
    TargetBedrock                             // AWS Bedrock InvokeModel(Stream)
    TargetBedrockConverse                     // AWS Bedrock Converse(Stream)
    TargetVertex                              // Google Vertex AI
)

// TargetFromChannelType 根据 RelayInfo.ChannelType 推导目标渠道
//   constant.ChannelTypeAws       → TargetBedrock
//   constant.ChannelTypeVertexAi  → TargetVertex
//   其他                          → TargetAnthropicDirect
func TargetFromChannelType(channelType int) FilterTarget { ... }

// FilterBetaFlags 对 anthropic-beta 字符串做白名单过滤 + 重命名
//   raw       客户端发来的逗号分隔字符串
//   target    目标上游
//   requestID 仅用于调试日志关联
// 返回经重命名后的最终 flag 列表，调用方自行序列化（拼字符串 / JSON 数组）
func FilterBetaFlags(raw string, target FilterTarget, requestID string) []string { ... }
```

#### 内部数据（4 张表）

- `bedrockInvokeBetaWhitelist` — 11 项（Bedrock InvokeModel 白名单）
- `bedrockConverseBetaExtra` — 3 项（Converse 额外支持）
- `vertexBetaWhitelist` — 8 项（Vertex 白名单）
- `bedrockBetaRename` / `vertexBetaRename` — `advanced-tool-use-2025-11-20 → tool-search-tool-2025-10-19`
- `anthropicKnownBetas` — 42 项（直连场景识别未知 flag）

> 完整内容见仓库内的 `relay/channel/claude/beta_filter.go`，与本文 1.2、1.3、2.2、3.x 节的数据完全对齐，单一数据源。

### 4.3 三处插入式修改（基于现有代码的最小 diff）

#### ① `relay/channel/claude/adaptor.go` — `CommonClaudeHeadersOperation` 加过滤

```diff
 import (
     "errors"
     "fmt"
     "io"
     "net/http"
     "net/url"
+    "strings"

     "github.com/QuantumNous/new-api/dto"
     ...
 )

 func CommonClaudeHeadersOperation(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) {
+    // 透传 anthropic-beta header，但按目标渠道做白名单过滤 + 重命名，
+    // 避免把 Bedrock/Vertex 不支持的 flag 发上去触发 "invalid beta flag"。
     anthropicBeta := c.Request.Header.Get("anthropic-beta")
     if anthropicBeta != "" {
-        req.Set("anthropic-beta", anthropicBeta)
+        filtered := FilterBetaFlags(anthropicBeta, TargetFromChannelType(info.ChannelType), info.RequestId)
+        if len(filtered) > 0 {
+            req.Set("anthropic-beta", strings.Join(filtered, ","))
+        }
     }
     model_setting.GetClaudeSettings().WriteHeaders(info.OriginModelName, req)
 }
```

→ 一次改动同时覆盖 **Claude 直连** 和 **Vertex AI**（Vertex 通过 `vertex/adaptor.go:234` 调用同一个函数）。`TargetFromChannelType()` 根据 `info.ChannelType` 自动区分目标。

#### ② `relay/channel/aws/dto.go` — `formatRequest` 加过滤 + 重命名

```diff
 import (
     "context"
     "encoding/json"
     "io"
     "net/http"
-    "strings"

     "github.com/QuantumNous/new-api/common"
     "github.com/QuantumNous/new-api/dto"
     "github.com/QuantumNous/new-api/logger"
+    "github.com/QuantumNous/new-api/relay/channel/claude"
 )

-func formatRequest(requestBody io.Reader, requestHeader http.Header) (*AwsClaudeRequest, error) {
+func formatRequest(requestBody io.Reader, requestHeader http.Header, requestID string) (*AwsClaudeRequest, error) {
     var awsClaudeRequest AwsClaudeRequest
     err := common.DecodeJson(requestBody, &awsClaudeRequest)
     if err != nil {
         return nil, err
     }
     awsClaudeRequest.AnthropicVersion = "bedrock-2023-05-31"

-    // check header anthropic-beta
+    // 把 anthropic-beta header 从客户端请求迁移到 Bedrock 的 body 数组，
+    // 过程中按 Bedrock 白名单过滤 + 必要时重命名（详见 claude.FilterBetaFlags）。
     anthropicBetaValues := requestHeader.Get("anthropic-beta")
     if len(anthropicBetaValues) > 0 {
-        var tempArray []string
-        tempArray = strings.Split(anthropicBetaValues, ",")
-        if len(tempArray) > 0 {
-            betaJson, err := json.Marshal(tempArray)
+        filtered := claude.FilterBetaFlags(anthropicBetaValues, claude.TargetBedrock, requestID)
+        if len(filtered) > 0 {
+            betaJson, err := json.Marshal(filtered)
             if err != nil {
                 return nil, err
             }
             awsClaudeRequest.AnthropicBeta = betaJson
         }
     }
     logger.LogJson(context.Background(), "json", awsClaudeRequest)
     return &awsClaudeRequest, nil
 }
```

#### ③ `relay/channel/aws/relay-aws.go` — 调用点传 `requestID`

```diff
-        awsClaudeReq, err := formatRequest(requestBody, requestHeader)
+        awsClaudeReq, err := formatRequest(requestBody, requestHeader, info.RequestId)
```

#### 不需要改动的文件

- `relay/channel/vertex/*.go` — 复用 `claude.CommonClaudeHeadersOperation`，自动获得过滤能力
- `relay/channel/aws/dto.go` 里的结构体定义 — `AnthropicBeta json.RawMessage` 已经存在
- `vertex/dto.go` / `vertex/adaptor.go` 的 `anthropic_version` 字段写入逻辑 — 已经存在

### 4.4 日志开关：上线初期 vs 稳定后

```go
// 方案 A：编译期常量（最简单）
var BetaFilterDebugLog = true   // 上线初期
// 稳定后改为 false 重新发布

// 方案 B：环境变量（推荐，无需改代码）
var BetaFilterDebugLog = os.Getenv("BETA_FILTER_DEBUG") == "true"

// 方案 C：挂配置中心 / 数据库 setting 表（最灵活）
// 在 setting/system_setting.go 加 BetaFilterDebugLog 字段，
// 通过管理后台 toggle，运行时生效
```

### 4.5 日志样例（与代码实际输出对齐）

开启 `BetaFilterDebugLog = true` 时，对客户案例那个 9 个 flag 的请求，`common.SysLog` 实际输出（单行）：

**Bedrock 渠道：**
```
[anthropic_compat] channel=bedrock req=cbc3f1d6-a551 kept=[effort-2025-11-24 context-1m-2025-08-07] dropped=[claude-code-20250219 interleaved-thinking-2025-05-14 redact-thinking-2026-02-12 thinking-token-count-2026-05-13 context-management-2025-06-27 prompt-caching-scope-2026-01-05 advisor-tool-2026-03-01] renamed=map[]
```

**Vertex 渠道：**
```
[anthropic_compat] channel=vertex req=cbc3f1d6-a551 kept=[interleaved-thinking-2025-05-14 context-management-2025-06-27 context-1m-2025-08-07] dropped=[claude-code-20250219 redact-thinking-2026-02-12 thinking-token-count-2026-05-13 prompt-caching-scope-2026-01-05 advisor-tool-2026-03-01 effort-2025-11-24] renamed=map[]
```

**Anthropic 直连（客户端突发新 flag 时才打）：**
```
[anthropic_compat] channel=anthropic req=cbc3f1d6-a551 unknown_betas=[some-new-flag-2026-08-01] (consider updating whitelist)
```

**重命名场景（客户端发了 `advanced-tool-use-2025-11-20` 转给 Bedrock）：**
```
[anthropic_compat] channel=bedrock req=xxx kept=[tool-search-tool-2025-10-19] dropped=[] renamed=map[advanced-tool-use-2025-11-20:tool-search-tool-2025-10-19]
```

→ 主人靠这些日志就能 **持续维护**：哪些 flag 该加进 Bedrock/Vertex 白名单、Anthropic 又出了什么新东西。

### 4.6 实际改动统计

```
新增  relay/channel/claude/beta_filter.go   约 210 行（白名单 + 过滤函数 + 日志）
修改  relay/channel/aws/dto.go              ±7 行（替换 split 为 FilterBetaFlags 调用）
修改  relay/channel/aws/relay-aws.go        1 行（传入 info.RequestId）
修改  relay/channel/claude/adaptor.go       6 行（在 CommonClaudeHeadersOperation 加过滤）
不动  relay/channel/vertex/*.go             0 行（自动复用 CommonClaudeHeadersOperation）
```

**编译 / 测试结果**：
- ✅ `go build ./relay/channel/claude/... ./relay/channel/aws/... ./relay/channel/vertex/...` 通过
- ✅ `TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta` 测试通过（验证现有 header→body 迁移契约）

---

## 5. 真实案例（Claude Code CLI v2.1.187）

客户原始请求头：

```
Anthropic-Beta: claude-code-20250219,interleaved-thinking-2025-05-14,
                redact-thinking-2026-02-12,thinking-token-count-2026-05-13,
                context-management-2025-06-27,prompt-caching-scope-2026-01-05,
                advisor-tool-2026-03-01,effort-2025-11-24,context-1m-2025-08-07
```

| 转发到 Bedrock InvokeModel | 转发到 Bedrock Converse | 转发到 Vertex AI |
|---------------------------|------------------------|----------------|
| `effort-2025-11-24` | `interleaved-thinking-2025-05-14` | `interleaved-thinking-2025-05-14` |
| `context-1m-2025-08-07` | `context-management-2025-06-27` | `context-management-2025-06-27` |
| | `effort-2025-11-24` | `context-1m-2025-08-07` |
| | `context-1m-2025-08-07` | |
| **= 2 个** | **= 4 个** | **= 3 个** |

剩下 5+ 个客户端发的 flag 都需被静默过滤掉，否则就会触发 `ValidationException: invalid beta flag`。

---

## 6. 排查 Checklist

遇到 `invalid beta flag` 时按顺序排查：

1. **确认目的端**：是 Bedrock InvokeModel？Converse？Vertex AI？规则不同
2. **抓取入站请求**：`grep -i "anthropic-beta" <网关日志>` 看客户端发了哪些 flag
3. **对照本文白名单**：找出不在列表的 flag
4. **检查 body 字段位置**：Bedrock 必须用 body `anthropic_beta` 数组，不是 HTTP header
5. **检查 anthropic_version**：Bedrock 必须 `bedrock-2023-05-31`，Vertex 必须 `vertex-2023-10-16`
6. **重命名映射**：`advanced-tool-use-2025-11-20` → `tool-search-tool-2025-10-19`

---

## 7. 维护说明

- Anthropic 持续发新 beta，本表需定期对照 LiteLLM 的 [`anthropic_beta_headers_config.json`](https://raw.githubusercontent.com/BerriAI/litellm/main/litellm/anthropic_beta_headers_config.json) 同步
- AWS Bedrock 新增支持的 flag 见 [官方文档](https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages-request-response.html)
- Vertex AI 新增支持的 flag 见 [Anthropic 官方 Vertex 集成文档](https://platform.claude.com/docs/en/build-with-claude/claude-on-vertex-ai)
