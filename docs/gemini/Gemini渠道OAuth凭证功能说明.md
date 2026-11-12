# Gemini 渠道 OAuth 凭证（Bearer / Broker 自动刷新）功能说明

- **上线日期**：2026-11-12
- **适用渠道**：Google Gemini（渠道类型 24，端点 `generativelanguage.googleapis.com`）
- **一句话**：Gemini 渠道除了 AI Studio API Key，现在也能用 Google OAuth access token（`ya29...`）。token 可以手动填，也可以让平台自动从 broker 拉取并在到期前刷新。

---

## 1. 背景

有些上游给的不是 AI Studio 的 `AIza...` / `AQ....` API Key，而是 Google OAuth 的 access token：

- 认证方式是 `Authorization: Bearer ya29...`，不是 `x-goog-api-key`
- 有效期大约 30 分钟，需要定期去上游的 token broker 换新

改动之前，Gemini 适配器会把渠道 key 无条件写进 `x-goog-api-key`，header_override 删不掉这个头，所以这类凭证接不进来。这次**没有新增渠道类型**，而是在 Gemini 渠道上加了一个「凭证类型」设置。协议转换、计费、日志等全部沿用 Gemini 渠道原有逻辑。

## 2. 三种凭证类型

| 凭证类型 | `settings.gemini_key_type` | 密钥字段填什么 | 发给 Google 的认证头 | token 刷新 |
|---|---|---|---|---|
| API Key（默认） | `api_key`，或不填 | `AIza...` / `AQ....`，一行一个 | `x-goog-api-key` | 不需要 |
| OAuth Token（静态） | `oauth_token` | `ya29...`，一行一个，多行就轮询 | `Authorization: Bearer` | **外部负责**更新渠道密钥 |
| OAuth Broker（自动刷新） | `oauth_broker` | broker 配置 JSON，一行一份 | `Authorization: Bearer` | **平台自动** |

- 两种 OAuth 模式下，请求里一定**不会**带 `x-goog-api-key`，保证 Google 只看到一种凭据。
- 老渠道没有这个字段时按 `api_key` 处理，行为完全不变。
- `oauth_token` 只适合联调和短期验证。生产环境请用 `oauth_broker`。

## 3. 配置方法

### 3.1 后台界面

Go 后台（classic）和 Vue 管理端的渠道编辑页，在渠道类型选 **Google Gemini** 后都会多出「凭证类型」下拉框：

1. 渠道类型：Google Gemini
2. 凭证类型：OAuth Broker（自动刷新）
3. 代理地址：保持默认，或填 Google 的反代地址（不要带 `/v1beta`）
4. 密钥：填 broker 配置 JSON（见 3.2）
5. 模型：手动添加实际模型名，例如 `gemini-4-carbon`；也可以点「获取模型列表」从上游拉取

### 3.2 broker 配置 JSON

```json
{"broker_url":"https://broker.example.com/token","method":"GET","headers":{"X-Api-Key":"xxx"},"user_project":""}
```

| 字段 | 必填 | 说明 |
|---|---|---|
| `broker_url` | 是 | 必须是 https，否则保存渠道时会报错 |
| `method` | 否 | `GET`（默认）或 `POST` |
| `headers` | 否 | 调 broker 时带的请求头，一般放 broker 的认证 secret |
| `user_project` | 否 | 填了就给 Google 加上 `x-goog-user-project` 头（OAuth 用户凭证的配额项目）。遇到配额项目相关的 403 时再填 |

> broker 配置放在密钥字段里，是为了沿用渠道列表 / 详情接口对密钥的脱敏和权限控制。
>
> 多 key 模式下每行写一份 broker 配置，每份配置各自缓存 token。

### 3.3 broker 返回格式

broker 返回 HTTP 200 和 JSON 即可。下面几种 Google 体系里常见的写法都能识别：

| 内容 | 支持的字段 |
|---|---|
| token | `access_token`（单个）、`accessToken`（单个）、`tokens`（数组，整组轮询） |
| 过期时间 | `expires_in`（秒），或 `expireTime` / `expire_time` / `expiry`（RFC3339 时间） |

示例：

```json
{"access_token":"ya29.a0Af...","expires_in":1800}
{"accessToken":"ya29.a0Af...","expireTime":"2026-11-12T10:30:00Z"}
{"tokens":["ya29.a...","ya29.b..."],"expires_in":1800}
```

没有返回任何过期字段时，按 30 分钟处理，并在系统日志里记一条。没有拿到任何 token 就当作 broker 调用失败。

### 3.4 通过管理 API 创建渠道

```bash
curl -X POST "$HOST/api/channel/" \
  -H "Authorization: Bearer $ADMIN_ACCESS_TOKEN" -H "New-Api-User: 1" \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "single",
    "channel": {
      "type": 24,
      "name": "gemini-oauth",
      "key": "{\"broker_url\":\"https://broker.example.com/token\",\"headers\":{\"X-Api-Key\":\"xxx\"}}",
      "models": "gemini-4-carbon",
      "group": "default",
      "settings": "{\"gemini_key_type\":\"oauth_broker\"}"
    }
  }'
```

## 4. 运行机制（oauth_broker）

| 机制 | 行为 |
|---|---|
| 缓存 | token 缓存在每个 pod 的进程内存里，缓存键是 broker 配置内容的哈希。改了配置就自动换新缓存，同一份配置在对话、任务轮询、视频下载等路径共用同一组 token |
| 提前刷新 | 剩余有效期不足 **150 秒**就向 broker 刷新 |
| 并发合并 | 同一份配置同时只有一个请求去调 broker，其余请求等它的结果（single-flight），避免 token 过期瞬间大量请求同时打 broker |
| 整组替换 | broker 一次返回多个 token 时组内轮询；每次刷新都整组替换，不是追加 |
| 401 重放 | 对话类请求收到 401 时，作废当前 token 组，拿新 token **重放一次**，第二次还是 401 就原样返回。所以 token 过期竞态不会触发渠道自动禁用；真正的鉴权错误（scope 不对、项目没开通）仍然会走正常的错误处理和自动禁用 |
| broker 故障 | 刷新失败但旧 token 还没真正过期时，继续用旧 token，并记系统日志；已经没有可用 token 时，请求直接报错（错误信息里会带 broker 的状态码） |
| 多 pod | 每个 pod 各自调 broker。Google 的 access token 在到期前不会因为签发新 token 而失效，所以 pod 之间不会互相影响 |

## 5. 覆盖范围

| 功能 | API Key | OAuth Token | OAuth Broker |
|---|---|---|---|
| OpenAI / Claude / Responses 格式转 Gemini、Gemini 原生透传（generateContent / streamGenerateContent / countTokens / embedding 等） | ✅ | ✅ | ✅（含 401 重放） |
| 渠道测试、获取模型列表、上游模型更新检测 | ✅ | ✅ | ✅ |
| Veo 视频、Native Interactions、Lyria 音乐：提交和轮询 | ✅ | ✅ | ✅ |
| TTS 音色列表 | ✅ | ✅ | ✅ |
| 视频代理播放、视频转存 OSS | ✅ | ⚠️ 用渠道**当前**的 key | ✅ |

任务轮询和视频下载这两条路径拿不到渠道设置，会按密钥内容判断凭证类型：`{` 开头当作 broker 配置，`ya29.` 开头当作 OAuth token，其余当作 API Key。任务提交时保存的 ya29 快照半小时就过期，所以视频下载时会改用渠道当前的 key。

## 6. 注意事项

- **Google 的 OpenAI 兼容端点（`/v1beta/openai/...`）不接受 OAuth token**（社区实测一律 401）。平台的 Gemini 适配器只走原生端点，不受影响。以后如果要给 Gemini 渠道加 OpenAI 兼容端点直通，OAuth 模式下必须禁用。
- token 的 scope 不够时，Google 返回 403 `ACCESS_TOKEN_SCOPE_INSUFFICIENT`，需要上游换 scope（官方示例用的是 `cloud-platform` 加 `generative-language.retriever`）。
- 渠道的 header_override 仍然在认证头之后执行，优先级不变；不需要再手动配置 `Authorization`。
- 建议给同一个模型保留一个 AI Studio API Key 渠道做备份，broker 或 token 池的账号出问题时还能切过去。
- 供应商自助编辑渠道（channel-lite）的界面没有提供这个选项。供应商编辑渠道时 settings 是合并更新，不会把这个字段冲掉。

## 7. 问题排查

| 现象 | 排查方向 |
|---|---|
| 保存渠道时提示 `gemini oauth broker_url must be an https URL` / `must be a JSON object` | 检查密钥字段：必须是单行 JSON，地址必须是 https |
| 请求报 `gemini oauth broker returned status xxx` | broker 不可用或认证失败，检查 broker 地址和 `headers` 里的 secret |
| 请求报 `gemini oauth broker response contains no token` | broker 返回的字段名不在 3.3 的支持范围内，需要调整解析函数 `service.parseGeminiOAuthBrokerResponse` |
| 上游返回 403 并提到配额项目或 user project | 在 broker 配置里填写 `user_project` |
| 某些 pod 周期性出现 401 | broker 可能每次签发新 token 时会吊销旧 token。这种情况需要把 token 缓存改成 Redis 共享 |

## 8. 验证记录（2026-11-12）

- **单元测试**：
  - `service/gemini_credential_test.go`：三种模式的认证头、broker 返回格式兼容、缓存与提前刷新、并发合并、组内轮询、精确作废、broker 故障时回退到旧 token、类型推断、视频下载凭证；用 `-race` 跑
  - `relay/channel/gemini/adaptor_oauth_test.go`：认证头、401 重放一次且请求体完整、连续 401 不重复重试
  - `relay/channel/task/gemini/adaptor_oauth_test.go`：任务轮询按 key 内容发 Bearer
- **本地冒烟**：真实服务 + SQLite + 本地 https 假 broker + 假 Gemini 上游
  - 保存 http 地址的 broker 被拒绝
  - 渠道测试、非流式对话、流式对话、获取模型列表都成功
  - 上游只收到 `Authorization: Bearer ya29...` 和 `x-goog-user-project`，没有 `x-goog-api-key`
  - 人为让上游返回 401 后，平台调 broker 换新 token 重放成功，渠道没有被禁用，消费日志正常计费
- **回归**：service、relay、model 各包里失败的测试和改动前的 HEAD 完全一致，没有新增失败
- **尚未完成**：用真实 broker 和真实 Google 端点的联调

## 9. 代码位置

| 位置 | 内容 |
|---|---|
| `relaykit/dto/channel_settings.go` | `GeminiKeyType` 和 `ChannelOtherSettings.GeminiKeyType` |
| `service/gemini_credential.go` | 凭证解析、broker 拉取 token 与缓存、类型推断、任务凭证解析 |
| `relay/channel/gemini/adaptor.go` | 认证头、401 重放 |
| `relay/channel/gemini/relay-gemini.go`、`controller/channel_upstream_update.go`、`controller/channel.go` | 获取模型列表、保存渠道时的校验 |
| `relay/channel/task/gemini/adaptor.go`、`relay/channel/task/lyria/adaptor.go` | Veo / Interactions / Lyria |
| `controller/voices.go`、`controller/video_proxy.go`、`controller/task_video.go` | TTS 音色、视频代理、视频转存 OSS |
| `web/classic/src/components/table/channels/modals/EditChannelModal.jsx` | classic 渠道编辑页 |
| `gravitex-api-admin/.../channel/channel-form.vue` | Vue 管理端渠道表单 |
