# GPT 加密推理项解密失败兜底（按用户开关）开发文档

> 编写日期：2026-11-09
> 状态：待开发
> 关联调研：[azure-reasoning-encrypted-content-cross-region.md](./azure-reasoning-encrypted-content-cross-region.md)、[GPT-6 Astra 完整使用文档 §14](../openapi/gpt6/GPT-6-Astra-完整使用文档.md)

## 一、背景

GPT 推理模型走 `/v1/responses` 并使用 `store=false` 时，客户端会把上一轮的 reasoning 项（`rs_...`，带 `encrypted_content`）原样回传。这段密文绑定了**生成它的组织 / Azure 资源 / 模型**，路由到别的渠道、别的 key 或换了模型时，上游会直接返回 400，整个会话就卡住了。

目前平台上 GPT 的报错只有两类：

| 类别 | 报错原文（`code: invalid_encrypted_content`） | 加密对象 | 本期处理 |
|---|---|---|---|
| **A. 推理项** | `The encrypted content for item rs_xxx could not be verified. Reason: <reason>`<br>常见的 reason：`Encrypted content organization_id did not match the target organization.`、`Encrypted content could not be decrypted or parsed.`、`Encrypted content item id did not match the target item id.` | `type=reasoning` 的项 | ✅ 兜底：删掉 reasoning 项后重试 |
| **B. 工具结果** | `Encrypted function output content could not be decrypted or decoded.` | `function_call_output` 的密文（主要来自 Codex multi_agent_v2 的子代理） | ❌ 不处理，原样返回报错 |

B 类不处理的原因：子代理的结果**只有密文、没有明文**。删掉或替换成占位文本，等于把子任务的结果整个丢掉，模型会在信息缺失的情况下继续执行，比直接报错更危险。

## 二、目标与范围

- **只对开了开关的用户生效**，开关存在 `users.setting` JSON 里，用法和 `allow_negative_balance` 一样，默认关闭。
- 本期只覆盖原生 `/v1/responses`，**不做** Chat→Responses 桥接（`chat_via_responses.go`），**不做** Redis 预删除。
- 优先**按报错定位精确删除**，定位不到或多轮之后仍然失败，再整体删除。
- 不改变粘滞路由。粘滞规则的缺口另外治理，见第十节。

## 三、删除推理项的后果（需要告知开通的用户）

1. **模型能力下降**：拿不到前几轮的推理过程，工具调用多的 agent 场景影响最大。GPT-5.6 及之后的模型默认 `reasoning.context=all_turns`，比老模型更依赖这段上下文。
2. **缓存从删除的位置开始失效**：这一轮输入成本和首字延迟会上涨。
3. **之后每一轮都会再失败一次**：客户端手里的历史没有变，下一轮会把坏的推理项再发过来，于是每轮都要「上游 400 → 删除 → 重试」。本期不做 Redis 预删除，这个开销接受。
4. **compaction 项不删**：压缩项代表了之前的全部历史，删掉等于整段对话失忆。坏的如果是 compaction 项，就放弃兜底，原样返回报错。
5. **可能引出配对报错**：`function_call` 带了 `fc_` id、而它前面配对的 reasoning 被删掉时，可能报 `... was provided without its required 'reasoning' item`。处理方式见 4.3。

## 四、方案设计

### 4.1 报错识别与定位

上游的 `param` 字段始终是 `null`，**只能从 message 文本里定位**。识别条件：

1. 请求格式是 `RelayFormatOpenAIResponses`，路径是 `/v1/responses`（不包括 `/v1/responses/compact`）。
2. 错误文本中包含 `invalid_encrypted_content`，或者包含 `encrypted content` + `could not be verified`。
3. 错误文本中**不包含** `function output`。包含的话就是 B 类，直接放弃兜底。

有两个入口，错误文本都在 `NewAPIError` 里：

| 入口 | 状态码 | 说明 |
|---|---|---|
| 非流式 / 流式在建立前就返回 HTTP 400 | 400 | 上游错误体直接透出 |
| 流式 HTTP 200，SSE 第一帧就是 `error` | 502 | `relay_responses.go:128` 在出第一个内容前会把它拦下，包装成 `upstream error before any output: <原始 data>`（`ErrorCodeBadResponse`）。此时**客户端还没收到任何字节**，可以安全重试 |

定位规则（按优先级）：

| 报错形态 | 能定位到什么 | 提取方式 |
|---|---|---|
| `for item rs_xxx could not be verified` | 具体是哪个 item id | 正则 `for item (rs_[A-Za-z0-9]+)` |
| `The encrypted content gAAA...XYZ= could not be verified`（没有 id，带一段密文片段，见 codex#17541） | 密文的前缀和后缀 | 正则 `encrypted content (\S+) could not be verified`，再按 `...` 拆成前缀和后缀，去 input 里匹配唯一一个 `encrypted_content`；匹配到 0 个或多个都算定位失败 |
| 其他 | 定位不到 | 直接整体删除 |

### 4.2 删除策略：先精确删，最多 3 轮，最后整体删

```
第 1 次失败：能定位 → 只删定位到的那个 reasoning 项 → 重试
第 2 次失败：仍然是 A 类，并且能定位到另一个项 → 再删这一个 → 重试
第 3 次失败：仍然是 A 类 → 删掉 input 里剩下所有带 encrypted_content 的 reasoning 项 → 重试
第 4 次失败：不再兜底，原样返回
任何一次定位不到 → 直接跳到「整体删除」
定位到的项是 compaction 项 / 不是 reasoning 项 / 在 input 里找不到 → 放弃兜底，原样返回
```

为什么要先精确删：跨渠道的会话里，往往只有一部分推理项来自其他组织，当前渠道自己生成的推理项是好的，能留就留。为什么最多 3 轮：每轮都要多一次往返，上限是为了不让延迟失控。

> ⚠️ 待验证：上游是不是按 input 顺序校验、遇到第一个坏项就返回？如果是，那么定位到的项之前的推理项都已经通过了校验。这一点只影响精确删除的轮数预期，不影响正确性。

### 4.3 删除时的连带处理

- 删除粒度是**整个 reasoning 项**。只删 `encrypted_content`、保留 `id` 不可行：`store=false` 时上游没存这个 id，会变成找不到 item 的报错。
- 删掉一个 reasoning 项后，把**紧跟在它后面、属于同一轮输出**的 `function_call` 项和 assistant `message` 项上的 `id` 字段去掉（`call_id` 保留，`function_call_output` 靠它配对）。「同一轮」的边界是：遇到下一个 reasoning 项或下一条 `role=user` 的消息就停止。
- 如果重试后报 `was provided without its required 'reasoning' item`，说明这项连带处理没覆盖到位，属于实现问题。**不要**针对这个报错再加一层兜底，应该修正上面的连带规则。

### 4.4 改写请求体（两处都要改）

请求在重试循环之前就已经解析成 `info.Request`（`*dto.OpenAIResponsesRequest`，`Input` 是 `json.RawMessage`）；透传模式则每轮都从 BodyStorage 读原始请求体。所以两处都要改：

1. 用 gjson 遍历 `input` 数组，用 sjson 删除对应下标（**从后往前删**，避免下标错位）。
2. 把改写后的结果写回 `responsesReq.Input`。
3. 用改写后的完整请求体重建 BodyStorage：`common.CreateBodyStorage(newBody)`，再 `c.Set(common.KeyBodyStorage, storage)`，并关闭旧的 storage。建议在 `common/gin.go` 里新增 `ReplaceBodyStorage(c, data)` 来封装这一步。
4. JSON 操作一律用 `common.*` 或 gjson/sjson，不要直接调用 `encoding/json`。

`input` 是字符串（不是数组）时，说明请求里没有任何项，直接放弃兜底。

### 4.5 接入重试循环（`controller/relay.go:201`）

在 `newAPIError != nil` 之后、`shouldRetry` 之前插入：

```go
if relayFormat == types.RelayFormatOpenAIResponses &&
    service.IsEncryptedReasoningFallbackAllowed(c) &&
    service.TryStripInvalidEncryptedReasoning(c, relayInfo, newAPIError) {
    // 改写成功：同渠道重试，不占用常规重试次数
    continue  // 实现时注意：不能让 IncreaseRetry 消耗常规重试预算
}
```

要求：

- **不受** `AutomaticRetryStatusCodeRanges`（不包含 400）限制，也**不受**粘滞规则 `SkipRetryOnFailure` 限制。所以兜底判断必须放在 `shouldRetry` 之前。
- **不清除**粘滞绑定，不调用 `processChannelError` 去处理禁用渠道或计入渠道错误。这不是渠道故障。
- 兜底轮数单独计数（存在 gin context 里，上限 3），**不占用** `common.RetryTimes`。实现时可以回退 `retryParam` 的计数，或者把兜底做成一个内层循环。
- 预扣费只发生一次（在循环外），失败的那几次上游没有产生用量，不影响计费。

### 4.6 留痕

- 在消费日志的 `other.admin_info.encrypted_reasoning_fallback` 里记录：

```json
{ "rounds": 2, "removed": 3, "mode": "precise|all", "kinds": ["organization_id did not match", "..."] }
```

- 放在 `admin_info` 下，只有管理员能看到。**不写进 `Content`**：Content 面向用户，不暴露内部概念。
- 每一轮用 `logger.LogWarn` 打一行英文日志，带上 request id、渠道 id、删除数量、定位方式。不要打印完整的 `encrypted_content`，只打印前 16 个字符。
- 兜底全部失败、原样返回时，在日志里记录 `fallback exhausted`。

## 五、用户开关（参照 `allow_negative_balance`）

字段名：`allow_encrypted_reasoning_fallback`（bool，默认 false，`omitempty`）。

| # | 端 | 文件 | 改动 |
|---|---|---|---|
| 1 | Go DTO | `relaykit/dto/user_settings.go` | `UserSetting` 加字段 `AllowEncryptedReasoningFallback bool \`json:"allow_encrypted_reasoning_fallback,omitempty"\``，注释写明「管理员授权」 |
| 2 | Go 判断函数 | 新建 `service/encrypted_reasoning_fallback.go` | `IsEncryptedReasoningFallbackAllowed(c)`：读取 `constant.ContextKeyUserSetting`，写法和 `service/negative_balance.go` 一致，取不到时默认 false |
| 3 | Go 管理员修改 | `controller/user.go` `UpdateUser`（约 855 行、953 行） | `updateUserSettingPatch` 加 `*bool` 字段，合并逻辑里加一个分支 |
| 4 | Go 用户自改设置 | `controller/user.go` `UpdateUserSetting`（约 1832 行） | **必须显式保留**这个字段（和 `allowNegativeBalance` 一样），否则用户保存一次通知设置，管理员开的开关就被清掉了 |
| 5 | Go 管理后台 | `web/classic/src/components/table/users/modals/EditUserModal.jsx` | 照 `allow_negative_balance` 的写法加开关：解析 setting、组装 payload、表单项 |
| 6 | Java 后端 | `SysUserBo` / `SysUserVo` / `SysUserServiceImpl` | 照 `allowNegativeBalance` 的写法：VO 里 `extract...`，修改时 `merge...` 写回 `users.setting` JSON。建议把 `mergeAllowNegativeBalance` 改成一个通用的、能合并多个 key 的方法 |
| 7 | Vue 管理端 | `gravitex-api-admin/.../system/user/data.tsx`、`api/system/user/model.d.ts` | 加表单字段，开关下方的说明文字写明第三节的后果 |

注意事项：

- Java 改完 `users.setting` 后，确认 Go 侧的用户缓存会失效。沿用 `allow_negative_balance` 现有的处理方式，开发时实测一次「Java 打开开关后，Go 立刻生效」。
- 已修复（2026-11-09）：`UpdateUserSetting` 现在以现有设置为底、只覆盖通知表单负责的字段，新加的开关会自动保留，不需要再单独处理（上面第 4 行可以跳过）。回归测试在 `controller/user_setting_preserve_test.go`。

## 六、新增与修改的文件清单

| 文件 | 类型 |
|---|---|
| `relaykit/dto/user_settings.go` | 改：新增字段 |
| `service/encrypted_reasoning_fallback.go` | 新建：开关判断、报错识别与定位、input 改写、留痕 |
| `service/encrypted_reasoning_fallback_test.go` | 新建：表驱动测试 |
| `common/gin.go` | 改：新增 `ReplaceBodyStorage` |
| `controller/relay.go` | 改：在重试循环里接入 |
| `controller/user.go` | 改：管理员修改 + 用户自改时保留字段 |
| `web/classic/.../EditUserModal.jsx` | 改：开关 |
| Java：`SysUserBo` / `SysUserVo` / `SysUserServiceImpl` | 改 |
| Vue：`system/user/data.tsx`、`model.d.ts` | 改 |

`relaykit/` 下有改动，按 AGENTS.md 要求要额外执行一次 `cd relaykit && GOWORK=off go build ./...`。

## 七、测试用例（testify require/assert，表驱动）

**报错识别与定位**
| 输入 | 期望 |
|---|---|
| `for item rs_abc could not be verified. Reason: ...organization_id did not match...` | A 类，定位到 `rs_abc` |
| `...Reason: Encrypted content item id did not match the target item id.` | A 类，定位到 id |
| `The encrypted content gAAAxx...yyS6Q= could not be verified` | A 类，按密文片段定位 |
| 包在 502 `upstream error before any output: {...}` 里的 SSE 版本 | 和上面一样能识别 |
| `Encrypted function output content could not be decrypted or decoded.` | B 类，不兜底 |
| 其他 400（比如参数错误） | 不兜底 |

**input 改写**
| 场景 | 期望 |
|---|---|
| 精确删一个 reasoning 项 | 只删这一项，其余项和顺序不变 |
| 被删项后面有 `function_call`（带 `fc_` id）和 assistant message | 两者的 `id` 被去掉，`call_id` 保留，下一个 reasoning 项之后的项不受影响 |
| 整体删除 | 所有带 `encrypted_content` 的 reasoning 项都被删掉，compaction 项保留 |
| 定位到的项是 compaction 项 | 放弃兜底 |
| `input` 是字符串 | 放弃兜底 |
| 透传和非透传两条路 | `info.Request.Input` 和 BodyStorage 的内容一致 |

**重试流程**
| 场景 | 期望 |
|---|---|
| 开关关闭 | 行为和现在完全一样 |
| 开关打开，第 1 次 400、改写后成功 | 只计费一次，`admin_info` 有记录，常规重试次数没有被消耗 |
| 连续 4 次 A 类报错 | 第 4 次原样返回，日志里有 `fallback exhausted` |
| 命中了 `codex cli trace` 粘滞规则 | 兜底照常生效，粘滞绑定没有被清除 |

## 八、上线前要实测的点

1. 用两个不同 Azure 资源或两个不同 OpenAI 组织的 key 构造跨组织回放，验证精确删除后能成功。
2. 验证 4.3 的连带去 id 规则：带工具调用的多轮会话，删除推理项后不出现 `required 'reasoning' item` 报错。
3. 流式请求：确认重试成功后，客户端收到的是一条完整、正常的流，开头没有残留的 error 帧。
4. 4.2 里提到的「上游是不是按顺序校验」。

## 九、不做的事（本期）

- Chat→Responses 桥接里的 `reasoning_items`
- 用 Redis 记住坏掉的 rs_ id，下次预先删除
- B 类工具结果的任何改写
- xAI / Grok 的 `Could not decrypt the provided encrypted_content`（报错格式不同，以后单独处理）

## 十、治本：粘滞规则缺口（单独排期）

兜底只能保证不报错，推理过程还是丢了。真正要解决的是路由：

- 默认的 `codex cli trace` 规则只按 `prompt_cache_key` 做粘滞，**不带这个字段的客户端完全没有粘滞**。
- TTL 默认 3600 秒滑动过期，会话闲置超过 1 小时后可能换到别的渠道。
- `KeyAffinityEnabled` 默认关闭，多 key 渠道里的 key 如果属于不同组织或不同 Azure 资源，渠道没换、key 换了也会失败。
- 可以参考 LiteLLM 的 `encrypted_content_affinity` 方案：把渠道指纹编码进 item id 和 `encrypted_content`，下一轮解码后路由回原来的渠道（见调研文档）。
