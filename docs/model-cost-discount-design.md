# 渠道成本折扣按模型覆盖：最小侵入开发文档

## 1. 结论

本方案将渠道 81 与 219 合并为一个渠道，在渠道上增加一个与“模型重定向”同类的 JSON 配置，用于保存渠道成本折扣的模型级覆盖值：

```json
{
  "seedance-2-0-fast": 0.75
}
```

计算优先级为：

1. 请求模型命中渠道的模型专有成本折扣；
2. 未命中时使用渠道通用 `cost_discount`；
3. 通用折扣为空时按 `1.0` 处理。

本方案的边界：

- 不修改 Java；
- 不新增数据库表；
- 不改变用户扣费、模型售价、分组倍率、渠道权重和上游请求；
- 只在 Go 侧解析专有折扣，并继续写入 Java 已经使用的 `other.admin_info.cost_discount`；
- 不新增数据库字段；优先复用现有 `channels.settings` JSON 字段。

经过代码确认，`settings` 本身就是渠道的扩展配置容器，适合承载这类不参与渠道选择的渠道级参数，因此比新增列更符合本需求的“最小侵入性”。

### 历史兼容边界（必须遵守）

历史提交 `664b9c118 feat chz 渠道成本折扣` 已经让异步视频任务在 `tasks.data` 中保存旧的：

```json
"billing_cost_discount": 0.952
```

这不是本功能新增的字段，旧字段、旧任务和旧结算逻辑不得删除、改名或改变含义。本功能的实现规则是：

- 不新增任务表字段；
- 不新增第二个模型专属折扣字段到 `tasks.data`；
- 新任务命中模型专属折扣时，将有效值复用写入已有的 `billing_cost_discount`；
- 未配置模型专属折扣时，保持旧逻辑完全不变；
- 已存在任务的 `billing_cost_discount` 优先级不变；
- 历史任务缺少该字段时，继续使用旧的通用渠道折扣兜底，不用当前新配置反算历史成本。

由于实际轮询链路可能用上游最终响应整体替换 `tasks.data`，仅依赖该字段不足以保证异步结算可读。修复方案不新增表或列，而是在已有 `tasks.private_data.billing_context` JSON 中增加可选的 `cost_discount` 双快照：

```json
{
  "billing_context": {
    "cost_discount": 0.75
  }
}
```

新任务同时写入旧的 `tasks.data.billing_cost_discount` 和新的可选双快照；结算仍以旧字段优先。未配置折扣时不写入 `cost_discount`，旧任务反序列化后该指针为 `nil`，因此不会报错或改变历史行为。

### 与旧的复制渠道行为等价

改造后的最终效果必须与“为专有模型复制一个渠道”保持一致，只是把折扣差异从渠道记录移动到渠道内的模型覆盖配置：

| 项目 | 旧方案：复制渠道 | 新方案：模型覆盖 |
| --- | --- | --- |
| 请求 `seedance-2-0-fast` | 命中渠道 219，使用 `0.75` | 命中合并后的渠道，模型覆盖为 `0.75` |
| 写入日志的 `cost_discount` | `0.75` | `0.75` |
| Java 读取的 `other.admin_info.cost_discount` | `0.75` | `0.75` |
| Java 渠道成本计算 | 按 `0.75` 计算 | 按 `0.75` 计算 |
| 用户扣费 | 不受影响 | 不受影响 |

因此新方案不是新增一套账单算法，而是在 Go 侧先解析出“有效渠道成本折扣”，然后沿用原来写入和结算的字段、任务快照及 Java 账单链路。

## 2. 要解决的问题

当前 81 和 219 使用同一个上游 Key，但为了给 `seedance-2-0-fast` 使用 `0.75` 成本折扣而拆成两个渠道。渠道本来还承担路由、素材归属、优先级和权重，导致成本配置反向影响路由。

本功能不是为 81 或 Seedance 写死的特殊逻辑，而是通用的“渠道 + 请求模型”渠道成本折扣覆盖规则：任何渠道都可以配置，任何文本、图片、音频或视频模型都可以配置；同一个模型在不同渠道上也可以使用不同折扣。

例如：

```text
渠道 A + model-x -> 0.75
渠道 B + model-x -> 0.90
渠道 A + model-y -> 使用渠道 A 的通用 cost_discount
```

没有配置 `model_cost_discount` 的渠道，行为与当前版本完全一致。

合并后建议保留一个渠道，例如渠道 81：

```text
channels.models:
seedance-2-0,seedance-2-0-pro,seedance-2-0-NSFW,seedance-2-0-mini,
seedance-2-5,dreamina-seedance-2-5-260628,seedance-2-0-mini-NSFW,
seedance-2-0-fast-NSFW,seedance-2-0-fast
```

```text
channels.cost_discount = 1.0
channels.settings.model_cost_discount = {"seedance-2-0-fast": 0.75}
```

## 3. 配置模型

### 3.1 存储位置

不增加 `channels` 表字段，直接把配置放入现有 `channels.settings` JSON 中。Go 结构体对应 `Channel.OtherSettings`，JSON 对外字段名是 `settings`。

例如现有 `settings`：

```json
{
  "vertex_key_type": "json",
  "azure_responses_version": "2024-10-21"
}
```

增加模型专有成本折扣后：

```json
{
  "vertex_key_type": "json",
  "azure_responses_version": "2024-10-21",
  "model_cost_discount": {
    "seedance-2-0-fast": 0.75,
    "some-image-model": 0.80
  }
}
```

`model_cost_discount` 的值是 JSON 对象，键是客户端请求模型名，值是成本折扣。空值、空对象和 `null` 均表示没有专有配置。

### 3.2 `setting` 与 `settings` 的语义

当前代码中的两个字段不是同一个用途：

| 字段 | Go 字段 | 主要语义 | 是否适合放模型专有成本折扣 |
| --- | --- | --- | --- |
| `setting` | `Channel.Setting` / `dto.ChannelSettings` | 渠道请求行为和连接参数，例如代理、强制格式、系统提示词、HTTP 协议和连接分片 | 不建议 |
| `settings` | `Channel.OtherSettings` / `dto.ChannelOtherSettings` | 渠道类型相关及其他扩展配置，例如 Azure 版本、Vertex/AWS Key 类型、透传开关、BytePlus 素材配置 | 适合 |

`setting` 会经过 `ValidateSettings()` 的 `ChannelSettings` 校验，继续放入成本配置会把计费业务配置和网络/请求行为混在一起。`settings` 已经是扩展容器，新增 `model_cost_discount` 更自然。

### 3.3 DTO 兼容策略

在 `dto.ChannelOtherSettings` 增加一个字段：

```go
ModelCostDiscount map[string]float64 `json:"model_cost_discount,omitempty"`
```

读取和保存应继续通过现有 `GetOtherSettings()` / `SetOtherSettings()`，使用项目的 `common.UnmarshalJsonStr` 和 `common.Marshal`。未知字段目前会被 Go 忽略，但管理端保存时会先解析原 `settings` 并保留已有键，因此不能用空对象覆盖整个配置。

如果担心把 `map[string]float64` 直接暴露给所有渠道，也可以让 DTO 字段保持 `json.RawMessage` 或增加专用解析函数；但本需求的 JSON 结构简单，使用 map 更直接。后端仍必须做有限数字和 `0 < value <= 1` 校验。

### 3.2 校验

Go 后端必须在保存和读取时都校验：

- 顶层必须是 JSON object；
- 模型名不能为空；
- 折扣必须是有限数字；
- 折扣必须大于 `0` 且小于等于 `1`；
- 不允许数组、字符串、负数、NaN、Infinity 和隐式类型转换；
- 重复模型键按 JSON 标准解析结果处理，前端保存前应阻止重复键；
- 不强制要求模型必须已经存在于 `channels.models`，以兼容模型别名和模型重定向配置；
- 后台可以提示未出现在渠道模型列表中的键，但不建议因此拒绝保存。

成本折扣不是用户扣费倍率，因此不能复用模型售价或分组倍率的配置字段。

## 4. 折扣解析和优先级

新增一个小的 Go 解析函数，输入为渠道、请求模型名和通用 `cost_discount`，输出有效折扣及来源：

```text
ResolveModelCostDiscount(channel, originModelName)
  -> effectiveDiscount, source
```

伪代码：

```text
specialized = parse(channel.model_cost_discount)
if specialized contains originModelName:
    return specialized[originModelName], "model"
if channel.cost_discount is valid:
    return channel.cost_discount, "channel"
return 1.0, "default"
```

匹配必须使用请求中的原始模型名 `originModelName`，不要默认使用重定向后的上游模型名。这样配置：

```json
{"seedance-2-0-fast": 0.75}
```

就能覆盖请求 `seedance-2-0-fast`，即使它随后被重定向为 Dreamina 的其他模型名。

不做前缀匹配、模糊匹配或大小写自动折算，避免一个专有折扣意外影响多个模型。若未来确实需要别名，应显式增加每个别名键。

## 5. Go 请求链路改造

### 5.1 渠道选择后解析

在任意已选渠道建立上下文、且已经得到请求原始模型名之后解析专有折扣。推荐顺序：

```text
选择渠道
  -> 保存 origin model
  -> 执行现有 model_mapping
  -> 解析 model_cost_discount（仍使用 origin model）
  -> 设置 ContextKeyChannelCostDiscount
  -> 进入计费、日志和任务提交
```

现有 `ContextKeyChannelCostDiscount` 继续作为下游读取入口，只是写入值从“渠道通用折扣”变成“模型专有折扣优先、通用折扣兜底”的有效值。这样可避免改动所有协议适配器。

### 5.2 用户扣费保持不变

模型专有成本折扣不能进入以下计算，无论它配置在哪个渠道或对应哪个模型：

- 模型售价；
- 用户分组倍率；
- `PriceData` 的模型倍率；
- 预扣费、结算差额和退款金额；
- `pkg/billingexpr` 表达式。

用户扣费仍按照原有模型价格、请求用量和分组倍率计算。成本折扣只代表平台向上游支付成本的内部口径。

### 5.3 同步请求

文本、Responses、Claude、Gemini、图片、音频和同步视频请求均使用同一套上下文值。请求完成写日志时，将有效值继续写入：

```json
{
  "other": {
    "admin_info": {
      "cost_discount": 0.75
    }
  }
}
```

可选地增加审计字段：

```json
{
  "cost_discount_source": "model",
  "cost_discount_model": "seedance-2-0-fast"
}
```

审计字段不是 Java 账单计算的必要条件；`cost_discount` 必须保持原有字段和语义。

### 5.4 异步视频和任务

异步视频必须复用历史任务快照机制，但不能新建模型专属任务字段：

```text
创建任务：有效折扣（模型专属优先）
          -> tasks.data.billing_cost_discount（兼容旧逻辑）
          -> tasks.private_data.billing_context.cost_discount（耐覆盖双快照）
任务结算：tasks.data 快照 -> billing_context 双快照 -> 旧的渠道通用 cost_discount 兜底
历史任务两个快照都缺少：继续走旧的通用 cost_discount 兜底
```

任务创建时使用请求原始模型名解析有效值。模型被重定向到上游名称后，仍使用原始模型名匹配：

```text
请求模型：seedance-2-0-fast
上游模型：dreamina-seedance-2-0-fast-260128
匹配配置：seedance-2-0-fast
```

管理员在任务创建后修改配置，不得改变任务创建时已写入的有效折扣。轮询、重试、分片、最终帧和异步回调应尽量保留旧的 `billing_cost_discount`；即使某条历史合并链路整体替换了 `tasks.data`，结算仍从 `billing_context.cost_discount` 恢复相同快照。

重要兼容规则：不能把“任务缺少 `billing_cost_discount`”统一改成使用新的模型专属配置，否则会让历史任务按照当前配置重新解释，改变历史成本口径。模型专属折扣只对创建时成功保存快照的新任务生效。

现网证据表明，任务提交阶段已经解析出 `0.75`，但最终 `tasks.data` 只保留上游响应，结算因而回退到渠道通用 `0.952`。根因是把可变的上游结果字段当作唯一不可变快照。本次使用既有 `private_data.billing_context` 作为第二保存位置，同时保留旧字段和旧优先级。

### 5.5 请求类型和计费链路矩阵

有效折扣的解析位置统一在“渠道选定后、上游模型重定向前后均可获得原始模型名”的上下文层；不同请求类型只复用结果，不各自实现匹配逻辑。

| 请求类型 | 是否流式 | 有效折扣保存位置 | 最终写入位置 | 用户扣费是否改变 |
| --- | --- | --- | --- | --- |
| Chat Completions / OpenAI | 否 | Gin context | 普通消费日志 `other.admin_info.cost_discount` | 否 |
| Chat Completions / OpenAI | 是 | Gin context，最终日志使用同一请求上下文 | 流结束后的消费日志 | 否 |
| Responses / Claude / Gemini | 否 | Gin context | 对应普通日志生成函数 | 否 |
| Responses / Claude / Gemini | 是 | Gin context，不能只依赖某个 SSE chunk | 流结束后的最终日志 | 否 |
| 图片生成、编辑、变体 | 否 | Gin context | 图片消费日志 | 否 |
| 音频转写、TTS、语音 | 否 | Gin context | 音频消费日志 | 否 |
| 同步视频 | 否 | Gin context | 同步视频日志 | 否 |
| 异步视频按秒计费 | 否（提交/轮询） | `tasks.data` + `private_data.billing_context` 双快照 | 轮询成功日志 | 否 |
| 异步视频按 Token 比例计费 | 否（提交/轮询） | `tasks.data` + `private_data.billing_context` 双快照 | 轮询成功日志 | 否 |
| 重试请求 | 流式或非流式 | 每次切换渠道重新解析；成功任务保存最终渠道值 | 当前尝试的日志 | 否 |

流式和非流式的关键区别只在日志生成时机，不在折扣解析规则。不能把折扣放入 SSE usage，也不能只在非流式分支写入。

### 5.6 渠道重试、模型重定向和缓存

- 每次选择或切换渠道都必须重新设置有效折扣；切换到没有折扣的渠道时清除上一次请求上下文中的旧值。
- 错误日志使用发生错误的当前渠道值，不使用下一次重试渠道的值。
- 成功的异步任务使用最终成功渠道在创建时保存的快照。
- 模型重定向只影响上游请求模型，不影响专属折扣的匹配键。
- 多 Key 渠道共享同一渠道配置，Key 轮询不改变折扣。
- 正常渠道保存接口刷新渠道缓存；直接修改 `channels.settings` 后必须重启服务或显式刷新渠道缓存，否则运行中的缓存对象可能仍没有新配置。

## 6. 不同模型类型的计费影响

| 类型 | 用户扣费 | 成本折扣来源 | Java 是否修改 |
| --- | --- | --- | --- |
| 文本 / Chat / Responses | 保持现状 | 请求模型专有折扣，否则渠道通用折扣 | 否 |
| 图片 | 按原有图片数量、尺寸和质量计费 | 同上，写入日志 | 否 |
| 音频 / 转写 / TTS | 按原有时长或 token 计费 | 同上，写入日志 | 否 |
| 同步视频 | 按原有时长、分辨率等计费 | 同上，写入日志 | 否 |
| 异步视频 | 按原有预扣费和结算规则 | 创建任务时保存快照 | 否 |
| 工具调用 | 按原有输入输出用量计费 | 同请求模型 | 否 |
| 阶梯 / 表达式计费 | 不注入成本折扣 | 仅作为日志成本字段 | 否 |

成本折扣不参与用量换算，也不参与 quota 饱和、预扣费或退款逻辑。所有用户可控数量仍需沿用现有上限校验和 quota 安全函数。

## 7. Java 兼容性

Java 当前账单和日志成本逻辑读取 Go 日志中的 `other.admin_info.cost_discount`，并使用其计算渠道成本。Go 只要将“有效模型专有折扣”写入原字段，Java 会自动按新的折扣计算，不需要修改 Java。

关键兼容规则：

- `cost_discount` 字段继续为数字，不改成对象；
- 已完成历史日志不回算、不改写；
- Java 读取不到新配置时也不受影响；
- 新配置只影响配置生效后的新请求和新任务；
- 异步任务结算使用创建时保存的折扣快照。

## 8. 管理端配置

在现有“模型重定向”编辑器附近增加“模型专有成本折扣”编辑器，复用相同的键值对交互和 JSON 序列化方式：

```text
模型名                         成本折扣
seedance-2-0-fast              0.75
```

建议交互：

- 键输入模型名；
- 值输入数字，范围 `0 < value <= 1`；
- 支持可视化编辑和手动 JSON 编辑；
- 保存前展示“未配置的模型将使用渠道通用成本折扣”；
- 编辑渠道通用 `cost_discount` 时展示其作为兜底值；
- 查询、详情、编辑接口直接返回 `model_cost_discount`；
- 不需要新增独立菜单、权限或 Java 管理接口。

前端应复用现有模型重定向编辑器的校验和布局，不复制一套渠道选择逻辑。若本期只做后端，可先支持 API/JSON 配置，后续再补同位置 UI。

## 9. 81/219 合并迁移

建议顺序：

1. 确认 81 和 219 的上游 Key、Base URL、租户/项目、区域和请求头完全一致；
2. 将 `seedance-2-0-fast` 加入保留渠道 81 的 `channels.models`；
3. 在渠道 81 写入：

   ```json
   {"seedance-2-0-fast": 0.75}
   ```

4. 预览并迁移仍在使用 219 的素材或素材组绑定到 81；只有确认上游素材命名空间一致时才迁移；
5. 历史日志、账单和已完成任务保留原渠道 ID，不做历史回写；
6. 刷新渠道和能力缓存；
7. 进行文本、图片、视频、素材引用和失败重试验证；
8. 停用 219，观察一段时间后再按运维流程处理旧渠道记录。

不要直接把所有历史 `channel_id=219` 改成 81。历史账单需要保留当时真实的渠道和折扣口径。

## 10. 目标代码改动范围

预计只涉及 Go 和管理端：

| 模块 | 改动 |
| --- | --- |
| `relaykit/dto/channel_settings.go` | 在 `ChannelOtherSettings` 增加 `ModelCostDiscount` |
| `model/channel.go` | 通过 `GetOtherSettings` / `SetOtherSettings` 读取和保存配置 |
| `controller/channel.go` | 校验并保存现有 `settings` JSON |
| `middleware/distributor.go` | 在选定渠道后设置有效成本折扣上下文 |
| `relay/helper/` | 新增专有折扣解析逻辑，或放入现有渠道计费辅助逻辑 |
| `relay/relay_task.go` | 异步任务保存有效折扣快照 |
| 现有日志生成逻辑 | 继续写 `admin_info.cost_discount`，必要时增加审计字段 |
| `web/src/features/channels/` | 复用模型重定向编辑器增加配置项 |
| Java | 不修改 |
| 数据库 | 不新增表、不新增字段；复用 `channels.settings` |

不建议把解析逻辑散落到每个 OpenAI、视频、图片和音频适配器中；统一在渠道选定后的上下文层处理，才能保证所有协议和任务类型一致。

## 11. 测试验收

### 11.1 解析优先级

| 通用折扣 | 专有配置 | 请求模型 | 期望有效折扣 |
| ---: | --- | --- | ---: |
| 1.00 | `seedance-2-0-fast: 0.75` | `seedance-2-0-fast` | 0.75 |
| 1.00 | `seedance-2-0-fast: 0.75` | `seedance-2-0` | 1.00 |
| 0.80 | 空 | 任意模型 | 0.80 |
| 空 | 空 | 任意模型 | 1.00 |

### 11.2 必测场景

- 专有模型命中和未命中；
- 重定向前模型名命中，重定向后模型名不误命中；
- 非法 JSON、非法折扣、空值和未知模型键；
- 文本、图片、音频、同步视频和异步视频；
- 异步任务创建后修改配置，结算仍使用创建时的折扣快照；
- 新异步任务命中专属折扣时，`tasks.data.billing_cost_discount` 和 `private_data.billing_context.cost_discount` 均保存同一有效值；
- 新异步任务未命中专属折扣时，与旧版本写入完全一致；
- 旧任务无快照时按原有通用折扣回退规则处理，不使用新模型配置重算；
- 异步任务轮询合并上游响应后，优先保留 `billing_cost_discount`；即使旧链路覆盖了 `tasks.data`，仍能从 `billing_context` 正确结算；
- 流式和非流式文本、Responses、Claude、Gemini、图片、音频日志都写入有效折扣；
- 流式结束、客户端中断、上游错误、重试失败日志不会读取错误渠道的折扣；
- 模型重定向前后名称、大小写不同名称、未配置模型分别验证；
- 单 Key、多 Key、渠道缓存刷新和直接改库后的缓存行为；
- `other.admin_info.cost_discount` 仍是数字；
- 用户扣费和 Java 账单中的渠道成本分别符合预期；
- 81 合并后素材请求不再被 219 强制承接；
- SQLite、MySQL 和 PostgreSQL 的字段迁移与读写兼容。

## 12. 发布和回滚

发布前先增加 DTO 字段和解析能力，默认无配置时完全走原有 `cost_discount`。确认日志和账单正确后，再合并 81/219。

回滚分两层：

1. 删除或清空 `model_cost_discount`，系统立即回到渠道通用折扣；
2. 如需恢复双渠道，重新启用 219，并恢复其模型列表和素材绑定。历史日志不变。

由于有效值最终仍写入旧的日志 `other.admin_info.cost_discount` 字段，`billing_context.cost_discount` 只在 Go 内部作为任务快照读取，Java 无需同步发布，降低了跨服务发布风险。

## 13. 第一版遗漏清单和实现前检查项

实现或合并前必须逐项确认：

1. `SetupContextForSelectedChannel` 使用请求原始模型名，而不是模型重定向后的名称。
2. 同步普通日志、流式最终日志、错误日志和重试日志都从同一个 context key 读取有效值。
3. 所有新异步任务在创建时把有效值同时写入旧的 `tasks.data.billing_cost_discount` 和已有 `private_data.billing_context` 内的可选 `cost_discount`。
4. `controller/task_video.go` 的三类任务数据合并路径都保留 `billing_cost_discount`：轮询响应合并、终态查询合并和普通上游响应合并。
5. 异步结算严格按 `tasks.data.billing_cost_discount`、`private_data.billing_context.cost_discount`、历史渠道通用折扣的顺序读取；不能对旧任务启用当前模型专属配置回算。
6. 新配置为空或未命中时，字节级行为和旧版本一致，不能把空值写成 `0` 或把通用折扣清掉。
7. 折扣只进入日志/平台成本口径，不进入模型价格、分组倍率、预扣费、结算、退款或表达式计费。
8. 新任务验证必须同时检查数据库中的 `tasks.data`、`tasks.private_data` 和最终 `logs.other`；分别验证兼容快照、耐覆盖快照和日志读取。

9. 在任务插入前增加最后一道双快照保护：仅对本次新建的异步任务，在请求上下文存在有效折扣时写入 `billing_context.cost_discount`，并仅在旧 `billing_cost_discount` 缺失时补写旧字段；已有旧字段绝不能覆盖，普通非任务请求绝不能触碰 `tasks` 数据。

10. 任务提交必须优先从 distributor 保存的 `original_model` 恢复客户端模型名，再执行渠道模型重定向；`RelayInfo.OriginModelName` 用于计费、折扣匹配、任务 `Properties.OriginModelName` 和日志，`UpstreamModelName` 只用于上游请求。不能因为上游响应或映射结果而改写日志中的客户端模型名。
