# main-alpha 独有功能清单（防丢清单）

> **⚠️ 每次合并 origin/main 前必读、合完必核对。**
>
> 这里记录 main-alpha 相对官方 main 独有的功能和改动。合并 main 时，官方的 merge 冲突/silent overwrite 都可能把这些东西改没，导致生产环境功能回归。
>
> 每次发现新的 main-alpha 独有点，都补到这里。

---

## 一、渠道相关

### 1.1 豆包视频渠道 BytePlus 素材库 AK/SK 配置
- **位置**：web/classic 渠道编辑页面 + web/default 渠道抽屉
- **字段**：`ChannelOtherSettings.ByteplusAssetAK/SK/Region/ProjectName`（`dto/channel_settings.go`）
- **相关 API 路由**：`/api/asset-admin/*`（BytePlus 素材库管理，见 `router/api-router.go` 的 `assetAdminRoute`）
- **用途**：豆包视频生成前需要把参考图/参考视频上传到 BytePlus 素材库，用 AK/SK 鉴权而不是 Bearer API Key
- **另有**：`EnableModerationQuery` 字段 — 是否启用 BytePlus GetModerationResult API 白名单查询

### 1.2 Channel.cost_discount 字段（成本折扣）
- **位置**：`model/channel.go` 的 `CostDiscount *float64`
- **用途**：单渠道成本折扣覆盖全局倍率
- **注意点**：`controller/channel.go UpdateChannel` 中有一段"GORM Updates() 跳过 nil 指针字段，因此当用户清除 cost_discount 时需要显式置 NULL"的特殊处理，合并时不能删
- **另有**：`controller/channel_authz.go` 的 `channelNonSensitiveFields` 必须包含 `"cost_discount"`，否则 `TestChannelFieldsAreClassified` 会失败

### 1.3 Claude 三渠道 anthropic-beta header 白名单
- **位置**：`relay/channel/claude/beta_filter.go` + `relay-claude.go` 的 `CommonClaudeHeadersOperation` / `applyHeaderOverrideToRequest`
- **原因**：Vertex/Bedrock/Anthropic 三个渠道对 anthropic-beta header 的支持有差异，Vertex 会因 `advisor-tool-*`、`prompt-caching-scope-*` 等报错 400
- **合并时注意**：`applyHeaderOverrideToRequest` 必须硬性跳过 `anthropic-beta`，由 `CommonClaudeHeadersOperation` 独家管控。任何 override/passthrough/wildcard 路径都不能绕过白名单

### 1.4 AWS Claude 增强字段
- **位置**：`relay/channel/aws/dto.go` 的 `AwsClaudeRequest`
- **字段**：`Thinking`、`OutputConfig`、`context_management`
- **合并时**：如果 body 字段过滤只清 anthropic-beta 不清这些绑定字段，Bedrock 会报错

### 1.4b 🔴 渠道类型号段分叉（与官方永久不一致，2026-08-05 定）

main-alpha 比官方早占用了 58/59/60 三个号，导致 `ChannelTypeAdvancedCustom` 两边号不同。**这些号存在生产 `channels.type` 列里，不可变更**（改号会让已有渠道全部失效）。

| 号 | main-alpha | 官方 |
|---|---|---|
| 58 | `ChannelTypeAzureVideo` | `ChannelTypeAdvancedCustom` |
| 59 | `ChannelTypeUptoken` | `ChannelTypeSub2API` |
| 60 | `ChannelTypeAdvancedCustom` | `ChannelTypeNewAPI` |
| 61 | `ChannelTypeTencentTokenHub` | — |
| 62 | `ChannelTypeSeedanceGateway` | — |
| 63 | `ChannelTypeSub2API`（官方的 59 顺延） | — |
| 64 | `ChannelTypeNewAPI`（官方的 60 顺延） | — |

**永久约定**：
- main-alpha 的 58~62 **永不变更**
- 吸收官方新渠道类型时，**一律从 65 往后顺延**，不跟官方号段
- `constant/channel.go` 里已加注释说明这个约定
- 三处要同步改：常量定义、`ChannelBaseURLs` 数组（按 index 对齐）、`ChannelTypeNames` map
- 还要检查 `constant/api_type.go`（`APIType*`）、`common/api_type.go`（channelType → apiType 映射）、`relay/relay_adaptor.go`（apiType → Adaptor）、`relay/common/relay_info.go`（渠道白名单）

**合并时验证**：`grep -E "ChannelTypeAzureVideo|ChannelTypeUptoken|ChannelTypeAdvancedCustom|ChannelTypeTencentTokenHub|ChannelTypeSeedanceGateway" constant/channel.go | grep "="` 应输出 58/59/60/61/62。

### 1.5 允许透传的 Claude 请求字段（channel-level 开关）
- **位置**：`dto/channel_settings.go`
- **字段**：`AllowInferenceGeo`（数据驻留）、`AllowSpeed`（推理速度模式）、`AllowSafetyIdentifier`、`DisableStore`、`AllowIncludeObfuscation`

---

## 二、用户 ID / Snowflake 精度处理

### 2.1 User.Id、Log.Id、Log.UserId JSON 字符串化
- **背景**：Go 后端的 User.Id 来自 Java 后端（RuoYi 系统），是 Snowflake 雪花 ID（19 位），超过 JS `Number.MAX_SAFE_INTEGER` (2^53 - 1)
- **实现**：
  - `model/user.go` — `func (u User) MarshalJSON()` 把 `Id` 输出为字符串
  - `model/log.go` — `func (l Log) MarshalJSON()` 把 `Id` 和 `UserId` 都输出为字符串（ByteHouse 里 logs.id 也是 Snowflake）
- **相关测试**：`model/log_marshal_test.go` — 回归保护
- **合并时注意**：官方合并只有 `Id int`，MarshalJSON 别被删

### 2.2 前端 user_id 类型兼容
- **位置**：
  - `web/default/src/features/profile/types.ts` — `UserProfile.id: number | string`
  - `web/default/src/features/subscriptions/types.ts` / `usage-logs/types.ts` / `users/types.ts` / `dashboard/types.ts`
  - `web/classic/src/components/table/users/modals/EditUserModal.jsx` — 不能对 userId 做 `parseInt`，直接透传字符串
- **合并时注意**：官方前端会用 `number`，合并时保持 `number | string`

### 2.3 common.Int64Flexible 类型
- **位置**：`common/utils.go`
- **用途**：接受 JSON 字符串或数字的 int64 字段（比如 Channel.Id 可能被前端以字符串形式发过来）
- **⚠️ 注意**：`controller/channel.go UpdateChannel` 中我们已改回直接用 `PatchChannel + common.Unmarshal`，不再需要 Int64Flexible 转换。但 common.Int64Flexible 类型本身保留，其它地方可能还在用

### 2.4 SumUsedQuota Row().Scan() 位置赋值
- **位置**：`model/log.go` `SumUsedQuota` 函数
- **背景**：GORM 的 `Scan(&struct)` 在 ByteHouse 下 column name 映射行为不一致，导致 stat.Quota 被 rpm/tpm 的二次 Scan 覆盖为 0
- **实现**：改用 `tx.Row().Scan(&quotaSum)` 按 SQL 列位置直接赋值
- **相关测试**：`model/sum_used_quota_test.go` — 回归保护

---

## 三、计费系统（按量 / 按秒 / 阶梯）

### 3.1 gpt-image 系列图片计费
- **支持路由**：`/v1/images/edits`、`/v1/images/generations`
- **计费维度**：图片输入 tokens + 图片输出 tokens（`CompletionTokenDetails.ImageTokens`）
- **位置**：`relay/channel/openai/adaptor.go` 及相关 dto
- **关键**：gpt-image 系列强制走 multipart 路径（Azure 的 JSON 接口不接受 images 字符串数组），multipart 请求要写 `prompt/size/quality/n/input_fidelity` 5 个字段
- **合并时注意**：`OpenAIResponse.output_tokens` 要填入 `CompletionTokenDetails.ImageTokens`，否则会按文本补全倍率计费

### 3.2 视频按秒计费 / 按量计费
- **位置**：`controller/task_video.go` 的 `handleVideoPerSecondBilling` / `handleVideoTokenRatioBilling`
- **注入点**：`main.go` 里 `service.UpdateVideoTasksFn = controller.UpdateVideoTaskAll`（已由 main 的 SystemTaskRunner 接管，见 `system_task_handlers.go` 的 asyncTaskPollHandler，调用 `service.RunTaskPollingOnce`）
- **视频回调 hook**：`relay.CompleteVideoTaskOnUpstreamSuccessFn` / `relay.MergeVideoTaskDataWithUpstreamResponseFn` — GET `/v1/videos` 收到上游终态时落库并计费，避免 Vertex 轮询仅返回 `{"name":"..."}` 时任务永不完成

### 3.3 阶梯计费 / 表达式计费
- **位置**：`pkg/billingexpr/` + `docs/功能更新/阶梯计费表达式使用指南.md`
- **触发条件**：模型定价页面选择"阶梯计费"模式，填 `计费表达式`
- **合并时注意**：整个 `pkg/billingexpr/expr.md` 是设计文档，改动要严格遵守

### 3.4 音频计费 / seed 系列音频
- **位置**：wpr 的 commit `f32225a4 seed-2-0-lite-260428 音频计费问题梳理`
- **对应字段**：`Usage.AudioTokens` / `CompletionTokenDetails.AudioTokens`

### 3.5 Claude 流式工具调用计费
- **位置**：`relay/channel/claude/relay-claude.go HandleStreamResponseData` + `stream_server_tool_use_test.go`
- **背景**：Claude 流式响应下 `server_tool_use.web_search_requests` 可能挂在 `message_start.message.usage` 或 `message_delta.usage`，两种事件都要捕获，写入 `c.Set("claude_web_search_requests", ...)`
- **合并时注意**：这是 bingyu commit `14e1aa54` 的修复

---

## 四、classic 前端定价设置界面

### 4.1 /console/setting?tab=ratio 模型定价手动编辑字段

**位置**：`web/classic/src/pages/Setting/Ratio/`（`ModelRatioSettings.jsx` 等）

**必须保留的字段清单**（官方合并时经常被删）：

| 字段 | 说明 |
|---|---|
| 模型固定价格 | 每次请求固定收费 |
| 模型倍率 | 全局输入倍率 |
| 提示缓存倍率 | prompt caching read 倍率 |
| 缓存创建倍率 | prompt caching create 倍率 |
| 模型补全倍率 | 仅对自定义模型有效 |
| 图片输入倍率 | 仅部分模型支持 |
| 音频倍率 | 仅部分模型支持 |
| 音频补全倍率 | 仅部分模型支持 |
| 图片补全倍率 | 仅部分模型支持 |
| 视频倍率 | 仅部分模型支持 |
| 视频补全倍率 | 仅部分模型支持 |
| 按张计费模型每张价格 | 单位美元 |
| 按秒计费模型每秒价格 | 单位美元 |
| 计费模式 | 阶梯计费开关 |
| 计费表达式 | 阶梯计费公式 |

**（清单待补充：主人如发现漏项请追加到这里。）**

### 4.2 classic /console/setting?tab=ratio GroupRatioSettings 字段
- 之前合并 main 时丢过 7 个字段，见 commit `19337350 恢复classic前端ratio设置丢失的视频按秒计费、阶梯计费等7个字段`
- **合并后检查方式**：本地跑 `bun run dev`，进 classic 前端 → 系统设置 → 倍率设置，逐项对比字段是否齐全

### 4.3 classic 用户/日志/订阅界面精度兼容
- `EditUserModal.jsx` 不做 `parseInt(userId)`
- Log 表的 render 用字符串 id 作为 React Table rowKey

---

## 四点五、web/ 双前端 workspace 结构（🔴 拒绝官方摊平，主人 2026-08-05 定）

### 4.5.1 main-alpha 的结构 vs 官方

官方在 `31d70fca3` 里**删掉了 `web/classic/`，并把 `web/default/*` 摊平到 `web/` 根**。main-alpha **两套前端都要保留**，所以结构上与官方长期分叉。

```
main-alpha（保持不变）                     官方 31d70fca3 之后
web/                                      web/
├── package.json   ← workspace 根          ├── package.json   ← app 根（不是 workspace）
│   workspaces: [default, classic]        ├── index.html
│   + catalog（统一 React/rsbuild 等版本） ├── src/
├── bun.lock                              ├── public/
├── default/       ← 跟随官方演进          ├── rsbuild.config.ts
│   ├── package.json                      ├── .oxlintrc.json
│   ├── index.html                        ├── knip.config.ts
│   ├── src/                              └── （无 classic）
│   └── rsbuild.config.ts
└── classic/       ← main-alpha 独家维护
    ├── package.json
    └── src/
```

### 4.5.2 为什么不接受官方摊平（评估结论）

如果照官方那样把 `web/default/*` 挪到 `web/`，会踩 4 个坑：

| # | 坑 | 后果 |
|---|---|---|
| 1 | `web/package.json` 被官方的 app package.json 覆盖，`workspaces` + `catalog` 消失 | `web/classic` 不再是 workspace 成员，`bun install --filter ./classic` 失败（Dockerfile line 18） |
| 2 | 官方工具链配置在 `web/` 根（`.oxlintrc.json` / `knip.config.ts` / `tsconfig`），默认 glob 会扫 `web/classic/**` | classic 是 React 18 + Semi Design + 不同规范，报一堆 lint/type 错 |
| 3 | `web/node_modules` 装 React 19（官方 default），classic 需要 React 18 | 共用一棵 node_modules 树，classic 可能拿到 19 直接崩 |
| 4 | `main.go` 的 4 处 `//go:embed`、Dockerfile 两段构建、`.gitignore` 都要改 | 构建链路全动，镜像打包风险 |

**决策**：保持 main-alpha 的 workspace 结构，用**路径映射**方式吸收官方前端改动（见 [官方合并待办追踪.md](官方合并待办追踪.md) 第三节的合并 SOP）。

### 4.5.3 必须保留的构建链路（合并时逐项核对）

| 位置 | 内容 | 官方会怎么改 |
|---|---|---|
| `web/package.json` | `"workspaces": ["default", "classic"]` + `catalog` 统一版本 | 官方会整体替换成 app package.json → **必须取 ours** |
| `main.go` | 4 处 embed：`web/default/dist`、`web/default/dist/index.html`、`web/classic/dist`、`web/classic/dist/index.html` | 官方改成 `web/dist` 且删 classic 两行 → **必须取 ours** |
| `router/web-router.go` | `ThemeAssets` 结构 + `SetWebRouter(router, assets)` + `common.NewThemeAwareFS(defaultFS, classicFS)` + `common.GetTheme() == "classic"` 分支 | 官方删掉 classic 分支和整个主题切换 → **必须取 ours** |
| `common` 的 `NewThemeAwareFS` / `GetTheme` | 双前端切换的核心 | 官方 `setting/system_setting/theme.go` 被删（-32 行）→ **必须取 ours** |
| `Dockerfile` | 两个 builder stage：`builder`（default，`bun install --frozen-lockfile` + `cd default && bun run build`）、`builder-classic`（`bun install --filter ./classic` + `cd classic && bun run build`）；最后 `COPY --from=builder /build/web/default/dist ./web/default/dist` + `COPY --from=builder-classic /build/web/classic/dist ./web/classic/dist` | 官方简化成单 stage 构建 `web/` → **必须取 ours** |
| `.gitignore` | `web/default/dist`、`web/classic/dist` | 官方改成 `web/dist` → 取 ours 或做并集 |

### 4.5.4 前端切换功能本身

- **设置项**：系统设置里的主题/前端选择（`classic` / 默认）
- **后端**：`router/web-router.go` 的 `common.GetTheme() == "classic"` 分支决定服务哪套 dist
- **打包**：镜像里同时包含 `web/default/dist` 和 `web/classic/dist`，运行时按设置切换
- ⚠️ 官方已彻底移除主题切换（`setting/system_setting/theme.go` 删除、`router/retired_frontend_routes_test.go` 新增测试断言旧前端路由已下线）。这些**都不接受**

### 4.5.4bis 🔴🔴 拒绝"官方大删除"最隐蔽的陷阱：git 会静默丢掉你没改过的文件（2026-08-06 踩坑实录）

**这是本仓库出过的最严重一次静默丢功能，务必理解原理。**

官方 `31d70fca3` 干了两件事：① 删掉整个 `web/classic/`（约 432 文件）② 把 `web/default/*` 摊平到 `web/`（等于删掉 `web/default/` 全部 960 文件、在 `web/` 根新建）。我们的决策是**两套嵌套目录都保留、整体拒绝这次删除**。

但**「拒绝目录删除」不是一个 git 能自动执行的动作**。三路合并时，对 `web/classic/` 和 `web/default/` 里的每个文件，git 独立判断：

- 文件**我方本地改过** → 产生 modify/delete 冲突 → 停下来等人解决 → 我们手动 `git checkout --ours` 保留 ✅
- 文件**我方从未改过**（跟某个官方历史点一致） → git 判定「官方删除 + 我方无修改」→ **静默采纳删除，不报冲突** ❌

结果：`web/classic` 只有我们改过的 35 个文件触发冲突被保住，**其余 397 个被静默删除**；`web/default` 更惨，只有 1 个 `auth-store.ts` 改过被保住，**其余 959 个全没了**（连 `index.html` / `src/index.jsx` / `src/main.tsx` 入口都没了）。当时 `go build` / `go vet` / 后端测试全过，**完全没报错**，直到 2026-08-06 主人跑 `bun run build` 才炸出 `Failed to resolve HTML template`。

**根因一句话**：越是"整体拒绝官方大删除"的目录，越危险——因为你手动改过的文件只是少数，绝大多数未改文件会被 git 静默删掉，而后端编译完全无感。

**发现方式（每次合并官方后必做）**：

```bash
# 双前端文件数必须和权威 origin/main-alpha 对齐，不能少
echo "classic HEAD/main-alpha: $(git ls-files web/classic | wc -l) / $(git ls-tree -r --name-only origin/main-alpha -- web/classic | wc -l)"
echo "default HEAD/main-alpha: $(git ls-files web/default | wc -l) / $(git ls-tree -r --name-only origin/main-alpha -- web/default | wc -l)"
# 入口文件必须在
ls web/classic/index.html web/classic/src/index.jsx web/default/index.html web/default/src/main.tsx
```

**恢复方式**（已用于 `f068ca978`）：以 `origin/main-alpha` 为权威整体恢复。恢复前先确认 HEAD 没有 main-alpha 所缺的独有文件（`comm -13` 检查），确认后：

```bash
git checkout origin/main-alpha -- web/classic web/default
cd web/classic && bun run build   # 两套都要能打包
cd ../default && bun run build
```

### 4.5.4ter 🔴🔴 `web/src/`（官方摊平版）和 `web/default/src/`（我们 embed 的）已长期分叉——default 前端落后官方几个月

**这是上面那次恢复后才暴露的第二个更深的问题，2026-08-06 记录，尚未解决。**

因为我们 HEAD 已经吸收了官方全部 commit（`git rev-list --count HEAD..origin/main` = 0），官方摊平后在 `web/` 根**持续更新**的那套 default 前端（`web/index.html` + `web/src/`，约 1070 文件）**也进到了我们树里**。于是现在 HEAD 同时存在两份 default 前端：

| 路径 | 来源 | 是否被使用 |
|---|---|---|
| `web/src/`（1042 文件） | 官方摊平后**最新**的 default 前端，跟随官方每次更新 | ❌ **没有被 embed，也没进 Dockerfile 构建**，等于死代码 |
| `web/default/src/`（960 文件） | main-alpha 嵌套保留的 default，停在旧版 | ✅ `main.go` embed `web/default/dist`、Dockerfile 构建的就是它 |

**分叉规模**（`web/src` vs `web/default/src`）：官方新增 **121** 个文件、**756** 个共同文件内容不同、我们 default 独有 **12** 个文件。也就是说线上 default 前端**落后官方几个月的更新**（auto-group、log stream status、OIDC 自定义登录名、playground 参数面板等前端改动都在 `web/src` 里，没进 `web/default`）。

**我们 `web/default/src` 独有、`web/src` 没有的 12 个文件**（迁移时不能被官方版覆盖丢掉，需逐个核对是不是 main-alpha 定制）：

```
features/channels/constants.ts
features/playground/components/message-action-button.tsx
features/playground/components/message-actions.tsx
features/playground/components/message-error.tsx
features/playground/components/playground-chat.tsx
features/playground/components/playground-input.tsx
features/playground/lib/message-styles.ts
features/playground/lib/message-utils.ts
features/playground/lib/payload-builder.ts
features/playground/lib/storage.ts
routes/console/log.tsx
routes/console/topup.tsx
```

**待办（未定方案，需主人决策）**：把官方 `web/src/` 的最新前端改动同步进 `web/default/src/`，同时保留上述 12 个 main-alpha 定制 + 756 个差异文件里的 main-alpha 改动。这是一次大工程（756 文件三路对比），必须逐文件辨别"官方更新" vs "main-alpha 定制"，不能无脑覆盖。详见 [官方合并待办追踪.md](官方合并待办追踪.md)。

> ⚠️ 顺带隐患：`web/src/`（官方摊平版死代码）如果一直留着，未来 `git checkout origin/main-alpha -- web/default` 之类的恢复操作不会动它，它会持续误导"以为 default 已是最新"。迁移完成后应决定是否删掉 `web/src/` + `web/index.html` + 根 `rsbuild.config.ts` 等官方摊平残留（注意别删到 workspace 根的 `web/package.json`）。

### 4.5.5 未来可选：改造成官方摊平结构（暂不做）

如果哪天想彻底对齐官方以减少长期合并成本，需要先解决 4.5.2 的 4 个坑：
- [ ] `web/package.json` 保留 `workspaces` + `catalog`，但同时承载官方 app 的字段（或把官方 app 配置挪到 `web/app-package.json` 之类）
- [ ] 给 `web/.oxlintrc.json` / `knip.config.ts` / `tsconfig.json` 加 `web/classic/**` 的 ignore
- [ ] 给 classic 配独立 node_modules（`bun install --cwd web/classic` 或 nohoist）
- [ ] 改 `main.go` embed + Dockerfile + `.gitignore`
- [ ] 主人构建镜像验证两套前端都能正常切换

**做这个改造前必须先在测试环境完整验证镜像构建 + 前端切换。**

---

## 五、中间件 / 环境变量 / 路由

### 5.1 RestrictAPIDomains 中间件
- **位置**：`middleware/restrict_api_domain.go` + `main.go` 里 `server.Use(...)` 注入
- **默认值**：`api.gravitex.cn,api.gravitex.ai,api.tennda.ai`（拿不到 `API_ONLY_DOMAINS` 环境变量时兜底）
- **行为**：这些域名只放行 `/api/*`、`/v1/*` 等 API 路径，其它路径返回空 HTML（关闭前端界面）
- **配套**：`docs/nginx/nginx65.conf` 已拆分 api 域名 server 块
- **合并时注意**：合并 main 前后要检查 `main.go` 中间件顺序，`RestrictAPIDomains` 一般在 CORS 之后

### 5.2 BytePlus asset admin 路由
- **位置**：`router/api-router.go` 的 `assetAdminRoute`
- **路径**：`/api/asset-admin/groups`、`/api/asset-admin/assets`、`/api/asset-admin/byteplus/*`
- **合并时注意**：官方合并把 channel 路由抽成了 `registerChannelRoutes(apiRouter)`；`assetAdminRoute` 是 main-alpha 独有，必须**独立保留**，紧跟 `registerChannelRoutes/registerAuthzRoutes` 之后

### 5.3 RuoYi JWT 认证（🔴 最高优先级 —— 生产 SSO 关键链路，任何合并都不得破坏）

**用途**：接受 Java 管理后端（Gravitex-API-End，RuoYi-Plus + sa-token）签发的 JWT，实现 **Java ↔ Go 双向 SSO**。管理员从 Java 管理端点进 Go 界面时，靠这条链路免登录。**删掉/绕过就是全线 401**（历史事故：会话开头的"go 登录之后直接报错 401"）。

#### 文件与位置

| 位置 | 内容 |
|---|---|
| `middleware/ruoyi_auth.go` | **整个文件都是 main-alpha 独有**，核心函数 `tryRuoYiJWTAuth(c) (*model.User, error)` + 哨兵错误 `errNoRuoYiJWT` |
| `middleware/auth.go` | **2 处**调用 `tryRuoYiJWTAuth`（约 line 128 用户鉴权链 authHelper、line 303 管理员鉴权链 authTokenHelper）+ `fromRuoYi` 标志位。⚠️ 2026-08-07 起 access_token JWT 层排在 RuoYi 之前，见 [5.7](#57-a2-双前端无状态-token-鉴权体系%EF%BC%88access_token--flow_token--login_session2026-08-07-补做) |
| `common/init.go` | `RuoYiAuthEnabled = GetEnvOrDefaultBool("RUOYI_AUTH_ENABLED", false)`、`RuoYiJWTSecret = GetEnvOrDefaultString("RUOYI_JWT_SECRET", "")` |
| `common/constants.go` | 两个变量的声明 |

#### 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `RUOYI_AUTH_ENABLED` | `false` | 总开关。生产必须为 `true` |
| `RUOYI_JWT_SECRET` | `""` | 与 Java 端 sa-token 的 JWT 密钥**必须一致**。为空时会打 `SysError` 并拒绝该路径 |

#### 鉴权流程（合并时必须完整保留的行为）

1. **触发时机**：`middleware/auth.go` 里 session 取不到 `username`（且未被 default 前端 access_token JWT 命中）时，尝试 RuoYi JWT。⚠️ 顺序：**access_token JWT（本体系）→ RuoYi JWT → char32 系统令牌**。access_token JWT 必须排在 RuoYi 之前（见 [5.7](#57-a2-双前端无状态-token-鉴权体系%EF%BC%88access_token--flow_token--login_session2026-08-07-补做)），否则 RuoYi 层会把本体系 JWT 当成自己的、用 `RuoYiJWTSecret` 验签失败而直接 401
2. **取 token**：从 `Authorization` header 取，同时支持 `Bearer xxx` 前缀和裸 JWT
3. **快速判别**：`strings.Count(tokenString, ".") != 2` 就返回 `errNoRuoYiJWT`（不是 JWT，让后面的鉴权方式接手）
4. **验签**：HMAC 算法（拒绝其它 signing method），密钥用 `common.RuoYiJWTSecret`
5. **取用户名**：claims 里先找 `userName`（sa-token 默认 key），兼容 fallback 到 `username`
6. **查用户**：
   - `PlatformIsolationEnabled()` 为真 → `model.GetUserByUsernameAndPlatform(username, RequestPlatformID(c))`（**与 platform_id 隔离联动，见第 7.6 节**）
   - 否则 → `model.GetUserByUsername(username)`
7. **成功后**：设置 `fromRuoYi = true`，并从 user 取 `username/role/id/status/group` 灌进 context

#### ⚠️ 三个极易被合并破坏的关键点

1. **`errNoRuoYiJWT` 是"这不是 RuoYi 请求"的哨兵，不是错误**
   调用处必须写成 `else if err != nil && err != errNoRuoYiJWT { 拒绝 }`。如果把 `errNoRuoYiJWT` 也当错误直接 401，**所有非 RuoYi 的正常登录都会挂**。

2. **`fromRuoYi` 标志位控制 `New-Api-User` 头的强制校验**
   `middleware/auth.go` 约 line 208 有 `if !fromRuoYi && !fromAccessJWT { ...要求 New-Api-User 头... }`。RuoYi JWT 模式**和 default 前端 access_token JWT 模式**下都**不强制**这个头（Java 端跳转、default 前端都不带）。删掉这个判断 → Java 跳转 / default 前端全部 400/401。

3. **platform_id 锁定语义**
   `middleware/auth.go` 约 line 295 的注释说明：「仅对 access_token 生效：session 为内部登录、RuoYi JWT 已在解析时按平台锁定」。也就是说 RuoYi 路径的平台隔离**在 `tryRuoYiJWTAuth` 内部就做完了**，外层不要重复施加，否则会误伤。

#### 合并 origin/main 时的处理原则

官方在 `31d70fca3 refactor(auth): replace dashboard sessions with stateless tokens (#6329)` 里**整段重写了 `middleware/auth.go`，把 RuoYi JWT 分支全部删除**（把 `gin-contrib/sessions` 换成无状态 token + `user_sessions` 表）。

**处理方式：`middleware/auth.go` 与 `middleware/ruoyi_auth.go` 一律取 main-alpha 版（`--ours`）。**

官方的会话管理能力已在 **2026-08-07 以 A2 完整版补做**（见下方 [5.7](#57-a2-双前端无状态-token-鉴权体系%EF%BC%88access_token--flow_token--login_session2026-08-07-补做)），当时是在 main-alpha 版 `middleware/auth.go` 基础上**叠加**、而非采用官方重写版，因此下列约束已全部满足并需在后续合并中继续守住：
- [x] 在 auth 链路里保留 `tryRuoYiJWTAuth` 分支（现位于 access_token JWT 层**之后**、char32 令牌之前）
- [x] 保留 `errNoRuoYiJWT` 哨兵语义
- [x] 保留 `fromRuoYi`（及新增 `fromAccessJWT`）对 `New-Api-User` 头的豁免
- [x] 保留 platform_id 在 RuoYi 路径内部锁定
- [x] **真机验证**：从 Java 管理端跳转进 Go 界面免登录成功
- [x] **真机验证**：普通用户密码登录、API key 调用、access_token 调用、default 前端登录都不受影响

⚠️ 官方版 `service/auth_session.go` / `service/auth_token.go` 与我们的**同名文件语义不同**（我们的是自研，不是官方那套 `user_sessions` 表）。后续合并若遇官方这些文件，**一律取 ours**，不要用官方版覆盖。

### 5.7 A2 双前端无状态 token 鉴权体系（access_token + flow_token + login_session，2026-08-07 补做）

**用途**：官方新前端 `web/default` 走"token bundle + 无状态 access_token + 持久化登录会话"这套鉴权（登录响应要含 `access_token` / `token_type` / `access_expires_at` / `user{id 为整数}` / `session{sid,...}`）。fork 原来的扁平 cookie 登录喂不出这个结构，导致 default 前端登录后**无限刷新**。A2 完整版补齐了这套，同时**保留 classic 的扁平 cookie 登录**与 **RuoYi SSO**。

#### 文件与位置（均为 main-alpha 独有，合并遇官方同名文件一律取 ours）

| 位置 | 内容 |
|---|---|
| `model/login_session.go` | **整个文件独有**。`login_session` 表 + CRUD（多设备管理 / 远程下线）。时间字段 Unix 秒，`RevokedAt==0` 表示有效。已在 `model/main.go` AutoMigrate 注册 |
| `service/auth_token.go` | **整个文件独有**（与官方同名文件语义完全不同）。签发/校验两种 HS256 JWT：`access_token`（载 uid+sid，TTL 15min）、2FA `flow_token`（载 pending_uid，TTL 5min），均用 `common.SessionSecret` 签名，带唯一 subject `new-api-access` / `new-api-2fa-flow` |
| `controller/auth_session.go` | **整个文件独有**。`RefreshAuth`（POST /api/user/auth/refresh，靠 cookie/`X-Auth-Session` 头恢复会话、复签 token）、`LogoutAuth`、`GetLoginSessions`、`RevokeLoginSessionBySid`、`RevokeOtherLoginSessions` |
| `controller/user.go` | `setupLogin` 改为同时吐"扁平字段(classic 兼容, id 为字符串)+ token bundle(default, user.id 为整数)"；`buildAuthBundle` / `buildAuthUser`；cookie 里额外存 `sid` 供首屏刷新回退；Login 的 2FA 分支下发 `flow_token` |
| `controller/twofa.go` | `Verify2FALogin` 优先用 `flow_token` 解析待验用户，缺省回退 classic 的 cookie `pending_user_id` |
| `middleware/auth.go` | `authHelper` 里新增 `tryAccessTokenJWTAuth` 层 + `fromAccessJWT` 标志位 |
| `router/api-router.go` | `POST /api/user/auth/refresh`、`POST /api/user/auth/logout`；selfRoute 下 `GET /sessions`、`DELETE /sessions/:sid`、`POST /sessions/revoke-others` |
| `middleware/access_token_jwt_auth_test.go` | 回归测试：RuoYi 启用 + cookie+bearer 无 New-Api-User 时应 200 |

#### 🔴 鉴权层顺序（合并时最易被破坏，必须守住）

`authHelper` 里判定顺序**必须**是：

1. **access_token JWT（本体系）** —— `tryAccessTokenJWTAuth`，**无条件最先判定**，先于 cookie、先于 RuoYi
2. **cookie session**（classic）
3. **RuoYi JWT**（Java SSO）
4. **char32 系统管理令牌**（`ValidateAccessToken`）

两个曾经踩过的坑（都会表现为 default 前端登录后无限刷新 / 接口全 401）：

1. **access_token 必须先于 RuoYi**：本体系 JWT 是三段 HS256、用 `SessionSecret` 签名；RuoYi 层会把任意三段 JWT 当自己的、用 `RuoYiJWTSecret` 验签失败即直接拒绝。放在 RuoYi 之后 → 永远拿不到判定机会。靠唯一 subject `new-api-access` 与 RuoYi/系统令牌区分，非本体系 JWT 返回 `errNotAccessJWT` 正常回退。
2. **access_token 必须先于 cookie**：`setupLogin` 给 default 前端也写了 cookie，浏览器同源会自动带上；若 cookie 层抢先接管，而 default 前端不发 `New-Api-User` 头 → 撞该头校验 401。所以有合法 access_token 时以它为准（`fromAccessJWT=true`，跳过 New-Api-User）。

#### 环境变量依赖

- 复用 `SESSION_SECRET` 作为 access_token/flow_token 的签名密钥。**多副本部署（k8s 多 pod）必须给所有 pod 注入同一个 `SESSION_SECRET`**，否则 A pod 签的 token 到 B pod 验签失败 → 401（cookie 若走 Redis session 则不受影响，故只有 A2 token 会暴露这个问题）。

#### 前端切换开关（与 4.5.4 联动）

- `theme.frontend`（options 表，值 `default`/`classic`，默认 classic）决定服务哪套 dist。default 走 A2 token bundle，classic 走扁平 cookie，两者从同一个 `setupLogin` 出口分流。
- ⚠️ 官方有个 retired migration `normalizeRetiredThemeOption()`（`model/frontend_option_migration.go`）会**每次启动强制把 `theme.frontend` 归一成 default**。本 fork **未接线**（无调用点），保留双前端切换能力。合并时若有人把 `MigrateRetiredFrontendOptions` 接进启动流程，classic 会被强制改回 default，**必须拒绝接线**。

### 5.4 MYSQL_PREPARE_STMT 环境变量
- **位置**：`model/main.go chooseDB`
- **默认值**：`false`（官方默认 true）
- **原因**：MySQL 在高并发下 MaxOpenConns × unique_queries > 16382 会超 `max_prepared_stmt_count`，禁用 PrepareStmt 是安全默认
- **合并时注意**：官方总想改成 true，必须坚守 false

### 5.5 QuotaDataStream Redis Stream 配额数据流
- **环境变量**：`QUOTA_DATA_STREAM_ENABLED`（默认 true）、`QUOTA_DATA_STREAM_CONSUMER_COUNT`、`QUOTA_DATA_STREAM_BATCH_SIZE` 等
- **入口**：`model/StartQuotaDataStreamWorkers()`（在 `main.go` 里注入）
- **fallback**：`QUOTA_DATA_STREAM_ENABLED=false` 时回落到 `model.UpdateQuotaData()`
- **合并时注意**：`ensureQuotaStreamEventIDOnLog(log)` 是 `CreateLog` 里的必要步骤，会给 consume log 加 quota_stream_event_id

### 5.6 StartTaskClearTask
- **位置**：`service/` 里的 `StartTaskClearTask`
- **用途**：清理 tasks 表里过期的 `fail_reason` payloads
- **合并时注意**：`main.go` 里必须调用 `service.StartTaskClearTask()`

---

## 六、数据库 schema

### 6.1 ByteHouse(CnchMergeTree) 作为日志库
- **DSN 前缀**：`clickhouse://`
- **建表**：**手动执行 `docs/bytehouse_logs.sql`**，Go 端 `migrateLOGDB` 跳过 AutoMigrate（ByteHouse 不支持 `ENGINE = MergeTree()`，必须用 CnchMergeTree）
- **合并时注意**：官方 `migrateClickHouseLogDB` 里用 MergeTree 建表，在 ByteHouse 上会失败，所以 `migrateLOGDB` 入口就 skip
- **DELETE 拒绝**：`model/log.go DeleteOldLog` 对 ClickHouse 直接返回 error（"ByteHouse 不支持删除日志操作"）

### 6.2 varchar 长度扩展迁移
- **函数**：`model/main.go` 的 `migrateTaskIDColumnLength` / `migrateLogRequestIdColumnLength`
- **迁移**：`tasks.task_id` varchar(191) → 512（Veo/Vertex base64 operation name）；`logs.request_id` varchar(64) → 512
- **仅对 MySQL/PostgreSQL 生效**，SQLite 不强制 varchar 长度

### 6.3 User.Group 列宽 varchar(64)
- 上次合并按主人指示接受 main 的 64（原来 256）
- **不要**回改为 256

### 6.4 与 Java 后端共享表
- **共享表**：users、tokens、logs、channels、abilities
- **不直接 HTTP 调用**，通过 MySQL 表协作
- **合并时注意**：修改这些表的字段/索引前，要考虑 Java 后端（Gravitex-API-End 项目）是否也要同步

---

## 七、Log 表相关

### 7.1 LogType 常量扩展
- **位置**：`model/log.go`
- **main-alpha 独有**：`LogTypeRetryFail = 7`（"重试"）、`LogTypeTest = 8`（测试）
- **官方**：`LogTypeLogin = 7`
- **实际映射**：main-alpha 把 `LogTypeLogin` 挪到 9，避免和 RetryFail 冲突
- **合并时注意**：官方引入新 LogType 常量时要重新编号避免冲突

### 7.2 Log content 面向用户 + 英文
- **位置**：`model/log.go` `formatUserLogs`
- **原则**：Content 在 Expenses 页直接渲染给用户看，用自然英文句子、不重复字段、不暴露内部概念
- **删除的 admin-only 字段**：`admin_info`、`audit_info`、`reject_reason`、`stream_status`、`official_quota`、`official_video_price_per_second`

### 7.3 GetTimeString 用 UTC+8 时区
- **位置**：`common/utils.go`
- **官方**：`time.Now().UTC()`
- **main-alpha**：`time.Now().In(time.FixedZone("UTC+8", 8*60*60))`
- **合并时注意**：不要被 main 改回 UTC

---

## 七点五、OperLog 操作日志审计（模型定价 / 渠道 / 用户分组）

**用途**：所有对模型定价、用户分组、渠道配置、工具定价的关键改动，保存时弹**确认对话框**，让运维填写"本次改动摘要（自动生成）+ 备注（人工填写）"，写入 `oper_log` 表。Java 后端定时把未推送的记录推到飞书群。这套是**审计追溯**用，不是普通业务日志。

### 7.5.1 后端

- **表**：`oper_log`（Go 端写入，Java 端只读）
  - 字段：`id / oper_type / content / remark / operator / created_at / pushed`
  - `oper_type` 取值：`模型价格` / `用户分组` / `渠道配置` / `工具定价`
- **推送任务历史表**：`t_oper_log_push_job_log`（Java 端写入，Go 端 AutoMigrate 建表）
- **文件**：
  - `model/oper_log.go` — OperLog 结构 + CreateOperLog / GetOperLogsPaged / MarkPushed 等
  - `model/oper_log_push_job_log.go` — Java 推送任务日志结构
  - `controller/oper_log.go` — CreateOperLog / ListOperLogs handler
- **路由**：`router/api-router.go` 里的 `operLogRoute := apiRouter.Group("/oper-log")`
  - `POST /api/oper-log/` — 创建（前端确认对话框调用）
  - `GET /api/oper-log/?oper_type=xxx&page=x` — 分页查询（渠道日志抽屉调用）

### 7.5.2 前端触发点（**仅 classic 前端有**，default 前端未实现）

| 界面 | 触发方式 | oper_type |
|---|---|---|
| `/console/setting?tab=ratio` **模型倍率**保存 | 弹 `OperLogConfirmModal` 让用户确认改动+填备注，然后一起提交 | `模型价格` |
| `/console/setting?tab=ratio` **分组倍率**保存 | 同上 | `用户分组` |
| `/console/setting?tab=ratio` **工具定价**保存 | 同上 | `工具定价` |
| `/console/channel` **渠道管理** → 顶部"日志"按钮 | 打开 `ChannelLogModal` 侧边抽屉，查看历史 + 手动添加一条 | `渠道配置` |

### 7.5.3 前端文件

- `web/classic/src/components/oper-log/OperLogConfirmModal.jsx` — 保存前确认对话框
- `web/classic/src/components/oper-log/operLogApi.js` — API 封装
- `web/classic/src/pages/Setting/Ratio/ModelRatioSettings.jsx` — 模型倍率保存流程
- `web/classic/src/pages/Setting/Ratio/GroupRatioSettings.jsx` — 分组倍率保存流程
- `web/classic/src/pages/Setting/Ratio/ToolPriceSettings.jsx` — 工具定价保存流程
- `web/classic/src/components/table/channels/modals/ChannelLogModal.jsx` — 渠道日志抽屉
- `web/classic/src/components/table/channels/ChannelsActions.jsx` — "日志"按钮入口

### 7.5.4 OperLogConfirmModal 里的 FIELD_LABELS 字段清单

前端保存时自动生成的"改动摘要"依赖这份字段映射，涉及的所有字段名：

| 字段 key | 中文标签 |
|---|---|
| ModelPrice | 模型固定价格 |
| ModelRatio | 模型倍率 |
| CacheRatio | 提示缓存倍率 |
| CreateCacheRatio | 缓存创建倍率 |
| CompletionRatio | 模型补全倍率 |
| ImageRatio | 图片输入倍率 |
| AudioRatio | 音频倍率 |
| AudioCompletionRatio | 音频补全倍率 |
| ImageCompletionRatio | 图片补全倍率 |
| VideoRatio | 视频倍率 |
| VideoCompletionRatio | 视频补全倍率 |
| ImageModelPricePerImage | 按张计费每张价格 |
| VideoModelPricePerSecond | 按秒计费每秒价格 |
| ExposeRatioEnabled | 暴露倍率接口 |
| billing_setting.billing_mode | 计费模式（阶梯计费） |
| billing_setting.billing_expr | 计费表达式（阶梯计费） |
| tool_price_setting.prices | 工具调用价格 |
| GroupRatio | 分组倍率 |
| UserUsableGroups | 用户可选分组 |
| GroupGroupRatio | 分组特殊倍率 |
| group_ratio_setting.group_special_usable_group | 分组特殊可用分组 |
| AutoGroups | 自动分组 |
| DefaultUseAutoGroup | 默认使用 auto 分组 |

**⚠️ 合并 main 时**：如果新增了 ratio/pricing 字段（比如未来官方加个新的倍率字段），要**同步补充**到这份 `FIELD_LABELS` 里，否则改动摘要里会显示裸 key 而不是中文名，运维看不懂。

### 7.5.5 Java 后端联动（Gravitex-API-End）

- Java 端有定时任务扫描 `oper_log` 表里 `pushed=false` 的记录 → 推送到飞书群 → 标记 `pushed=true`
- 推送任务本身的运行历史写入 `t_oper_log_push_job_log`（表名带 `t_` 前缀是 Java 端 RuoYi 风格）
- **合并时注意**：如果修改 `oper_log` 表字段/含义，要同步告知 Java 团队

### 7.5.6 常见误区

- 这个 `oper_log` 表**不是** `logs` 表（后者是消费日志 `type=LogTypeManage` 记录）——两套系统并存
- `logs` 表里的 `LogTypeManage` 记录是**后端埋点自动**记的（比如 UpdateChannel handler 里的 `recordManageAudit`），不需要用户确认
- `oper_log` 表是**运维手动确认+填备注**才写入的，用于合规/审计追溯
- 两套系统各自独立，都不能合并/替换

---

## 七点六、platform_id 多平台数据隔离

**用途**：同一套 Go 服务同时服务多个平台（gravitex.ai / tennda.ai / gravitex.cn 等），用 `platform_id` 把用户/token/日志数据互相隔离，A 平台的客户看不到 B 平台的数据。

- **开关**：环境变量 `PLATFORM_ISOLATION_ENABLED`（默认跟随 `RUOYI_AUTH_ENABLED`）
- **文件**：`middleware/platform.go` + `middleware/platform_test.go`
- **context key**：`platform_id`
- **隔离范围（重要）**：只对**客户流量**生效 —— API key / access_token 访问 `/api/token` / RuoYi JWT。**内部登录和 session 放行**，否则管理端会看不到数据
- **配套**：`sys_user` 软删过滤（Java 端删除的用户不能再通过 platform 查询漏出来）
- **合并时注意**：官方没有这个概念，`middleware/platform.go` 是纯新增文件不会冲突；但 `common/init.go` 里的 `PlatformIsolationEnabled = GetEnvOrDefaultBool(...)` 那行容易在冲突时被丢

---

## 七点七、企业账号体系（主账号 / 子账号）

**用途**：企业客户有一个主账号 + 多个子账号，主账号不能自己建 apikey（必须由 Java 管理端下发），子账号的模型范围受企业 `allowedModels` 收敛。

### 数据表（Java 端写入，Go 端只读 + AutoMigrate 建表）
- `t_enterprise_user` — `model/enterprise.go` 的 `EnterpriseUser`
- `t_enterprise_info` — `model/enterprise.go` 的 `EnterpriseInfo`

### 关键函数
- `model.IsEnterpriseApikeyRestrictedOwner(userId)` — 判断是否是"受限的企业主账号"
- `model.GetEnterpriseInfoByUserId(userId)` — 取企业信息
- `controller.capTokenLimitsForEnterpriseSubAccount(...)` — 按企业 allowedModels 收敛子账号 token 的模型/厂商范围

### 行为约束
- **拦截**：企业主账号创建/修改 apikey 直接拒绝
- **fail-open**：守卫查询出错时**放行不阻断**（`037fe0901`），避免 DB 抖动导致企业客户全线不可用
- **告警**：子账号敏感操作回调 Java 告警（`service/sensitive_op_notify.go` 的 `postEnterpriseAlert`）
  - apikey 未设 IP 白名单事件告警
  - apikey 日消费阈值告警

**合并时注意**：`model/task_cas_test.go` 的 AutoMigrate 列表和 truncateTables 里必须包含 `EnterpriseUser` / `EnterpriseInfo` / `t_enterprise_user` / `t_enterprise_info`，否则企业相关测试全挂

---

## 七点八、允许用户欠费继续使用（AllowNegativeBalance）

**用途**：管理员给特定用户开白名单，钱包额度 <= 0 时不拦截请求（先用后付 / 大客户信用额度场景）。

- **字段**：`dto.UserSetting.AllowNegativeBalance`（`dto/user_settings.go`）
- **helper**：`service.IsNegativeBalanceAllowed(c)`（`service/negative_balance.go`）
- **放行点**：**6 处**钱包扣费预检查都要判断这个开关（改动扣费链路时别漏）
- **保护**：`UpdateUserSetting` 显式保留 `AllowNegativeBalance`，防止用户自己调接口清掉授权
- **管理入口**：`UpdateUser` 支持 admin 修改（白名单字段）；classic 前端 `EditUserModal.jsx` 有"允许透支使用"开关
- **设计文档**：见 commit `c6fab6838` 引入的设计文档

**合并时注意**：官方合并如果重写了扣费预检查逻辑，6 处放行判断很容易被漏掉 → 表现是白名单用户欠费后被拦

---

## 七点九、Seedance 2.0 官方镜像（BytePlus 原生 API 兼容层）

**用途**：让客户可以用**火山/BytePlus 官方 SDK 的原始请求格式**直接打到我们的网关，请求/响应体字节级一致，客户端零改造迁移。

### 路由（`router/video-router.go`）
| 路径 | 说明 |
|---|---|
| `POST /api/v3/contents/generations/tasks` | 提交视频生成任务 |
| `GET /api/v3/contents/generations/tasks/:id` | 查询任务 |
| `DELETE /api/v3/contents/generations/tasks/:id`（cancel router） | 取消任务 |
| `POST /api/v3/seedance` | 素材库 Action 分发入口 |

中间件链：`SeedanceOfficialMirror()` → `TokenAuth()` → `AssetResolveChannel()` → `Distribute()`

### 文件
- `middleware/seedance_official_mirror.go` — 请求改写中间件（把官方格式转成内部格式）
- `controller/seedance_official_video.go` — 视频生成镜像处理器（含取消端点、错误响应体原样透传、查询响应体原样透传）
- `controller/seedance_official_asset.go` — 素材库 Action 分发（创建/更新/删除同步写本地表）
- 对应 `_test.go` 文件都要保留

### 关键行为
- **响应体原样透传**：错误和查询响应都不做包装，保持和官方字节一致
- **model_mapping 重定向**：官方镜像路径也要应用渠道的 model_mapping（`830af253c`）
- **素材库用户隔离**：`ListAssets` / `ListAssetGroups` 的 `GroupIds` 过滤条件与**用户名下分组取交集**，不是直接覆盖（`b7bc636f7`）—— 否则 A 用户能看到 B 用户的素材
- **CreateVisualValidateSession**：H5 链接强制中文，且中文改写迁移到 `Result` 内嵌 `H5Link`（不能破坏前端 Result 信封解析）
- **素材库配额时序**：见 `bad39dff3`

### doubao 适配器配套
`relay/channel/task/doubao/` 里新增了：原始请求体透传分支、原始响应体透传分支、取消任务上游调用

**合并时注意**：这一整套是 main-alpha 独有的大功能（20+ commit），官方完全没有。所有 `seedance_official_*` 文件和 `/api/v3/*` 路由都不能丢

---

## 七点十、本轮（2026-07 前后）其它 main-alpha 独有改动

### 渠道 / 协议
| 功能 | 位置 | 说明 |
|---|---|---|
| 腾讯云 TokenHub 渠道 | `constant/channel.go` 的 `ChannelTypeTencentTokenHub = 61`、`APITypeTencentTokenHub`、`relay/channel/tokenhub/` 包 | 支持 OpenAI + Claude 双协议原生透传（`/v1/messages` + `x-api-key` + `anthropic-version`）、embeddings、completions。**⚠️ Base URL 必须是 `https://tokenhub.tencentcloudmaas.com`**（已确认正确）；官方 `relay/channel/tencent/dispatch.go` 用的是 `tokenhub.tencentmaas.com`，**不要让官方覆盖我们的域名**。官方那套是"腾讯渠道按 key 格式分流"，与我们的独立渠道并存不冲突 |
| SeedanceGateway（川益网关）渠道 | `ChannelTypeSeedanceGateway = 62`、`relay/channel/task/seedancegateway/` | 网关模型适配 |
| 渠道请求头支持用户传 + 配置默认 | `model/channel.go` 的 `HeaderOverride` / `GetHeaderOverride()` | 客户端可传自定义 header，渠道侧可配默认值 |
| `anthropic_beta_target` override | `dto/channel_settings.go`、`relay/channel/claude/adaptor.go` 的 `ResolveBetaTarget` | Anthropic 类型渠道但上游实际是 Bedrock/Vertex 时，显式指定按哪个白名单过滤（可选值 `''`/`bedrock`/`bedrock-converse`/`vertex`/`direct`） |
| Azure 模型特定 Responses API 版本 | `dto/channel_settings.go` 的 `AzureModelResponsesVersions`、`relay/channel/openai/adaptor.go:143` | 和普通 API 版本 `AzureModelApiVersions` 独立配置 |
| 腾讯云 / TokenHub 加入 stream_options 白名单 | `streamSupportedChannels` | |
| AWS Bedrock InvokeModel 对 Claude 4.5+ 放行 structured outputs | `relay/channel/aws/` | |
| Claude 媒体 URL 输入支持 | `relay/channel/claude/media_source.go` | image/document URL 自动转 base64；Vertex 与 Anthropic 兼容渠道都要补；下载失败**降级透传**不报错；下载请求带浏览器 UA/Accept 头 |
| 渠道亲和性滑动 TTL 修复 | `middleware/distributor.go` 的 `MarkChannelAffinityUsed` | 修复同优先级渠道无法负载均衡 |
| 上游连接中断 body 回退重试 | `common/body_storage.go`、`relay/channel/api_request.go` | `do request failed` 日志显示真实原因且不暴露 URL |

### 计费
| 功能 | 位置 | 说明 |
|---|---|---|
| GPT-5.6 显式 prompt cache + cache_write_tokens | `dto/openai_response.go`、`service/text_quota.go`、`service/tiered_settle.go`、`service/relayconvert/responses_to_chat.go` | **`UsageFromResponsesUsage` 里必须透传 `CacheWriteTokens`**，流式和非流式都走这个函数 |
| Gemini Omni 视频输入模态计费 + token 持久化 | `relay/channel/gemini/`、`controller/task_video.go`、`service/task_billing.go` | `has_video_input` 字段 |
| realtime WSS 计费完善 | `relay/channel/openai/relay_realtime.go` | 记录缓存文本/音频 token，补全音频/图片缓存倍率，日志记录**会话总用量**，修复 `CachedTokensDetails` 跨轮累积 |
| dola-seedream-5-0-pro-260628 图片计费 | `relay/helper/image_billing.go` | |
| veo 4k 分辨率解析修复 | `relay/channel/task/gemini/adaptor.go:797`、`billing.go` | 原来 `4k` 被解析成 `4kp` 导致误按 720p 计价 |
| veo-3.1-generate-001 配置 | | |
| web 搜索工具计费展示口径 | | 改为**折后美元** |
| gpt-image 参数校验交上游 | | 支持 `quality=auto` 等合法值；生图空 prompt 由 500 改 400；Gemini 无图时响应带模型文字说明 |

### 模型管理
| 功能 | 位置 | 说明 |
|---|---|---|
| 模型厂商（Vendor）管理 | `model/vendor_meta.go` 的 `Vendor`、`model/pricing.go` | 新增/编辑模型时保存厂商；返回模型时带厂商；按厂商限制模型可见范围 |
| token 支持 vendor 限制 | `model/token.go` 的 vendor limits | 与企业子账号收敛联动 |

### 通知 / 告警
| 功能 | 位置 | 说明 |
|---|---|---|
| 额度预警 webhook 默认指向 Java | `service/quota.go` 的 `DefaultQuotaWarningWebhookURL` | 地址可用环境变量覆盖 |
| 邮件通知文案改造（含日限额） | `common/email.go` / notify 相关 | wpr 多次迭代 |
| 子账号敏感操作回调 Java 告警 | `service/sensitive_op_notify.go` | |

### 运维
| 功能 | 位置 | 说明 |
|---|---|---|
| 请求追踪响应头 `X-Api-Request-Id` | `middleware/request-id.go` | |
| readiness / liveness probe initial delay 增大 | k8s 部署配置 | |

---

## 七点十一、与官方共存的"混合体"文件（阶段2 后新增，最易被后续合并破坏）

这些文件里 main-alpha 的改动和官方的重构**交织在一起**，不是简单的"保留 ours"或"取 theirs"。后续合并时必须逐项核对，否则会静默丢功能或引入计费错误。

### 7.11.1 `types/price_data.go` —— 官方封装 + main-alpha 字段

- 官方把 `OtherRatios` 公开字段改成私有 `otherRatios` + 一套访问器（带 NaN/Inf 过滤）
- **main-alpha 必须保留的**：`VideoRatio float64` 字段（视频倍率）
- **调用点写法**（官方私有化后唯一正确的用法）：

| 需求 | 写法 |
|---|---|
| 读全部 | `pd.OtherRatios()` （方法，返回副本） |
| 读单值 | `pd.OtherRatios()["n"]` |
| 判存在 | `pd.HasOtherRatio("n")` |
| 整体赋值 | `pd.ReplaceOtherRatios(map[string]float64{...})` |
| 单键写入 | `pd.AddOtherRatio(k, v)` |
| 连乘应用 | `pd.ApplyOtherRatiosToFloat(v)` / `ApplyOtherRatiosToDecimal(v)` |
| 反向除掉 | `pd.RemoveOtherRatiosFromFloat(v)` |

⚠️ `model.TaskBillingContext.OtherRatios` 是**另一个结构体**的公开字段，不受影响，别误改。

调用点分布：`service/text_quota.go`、`relay/image_handler.go`、`relay/relay_task.go`、`relay/channel/task/ali/adaptor.go`、`relay/channel/task/vertex/adaptor.go`

### 7.11.2 `dto/openai_response.go` —— 三方字段并集

`InputTokenDetails` 里同时有：
- 官方的 `CacheWriteTokens` + `CacheCreationTokensTotal()` 方法
- **main-alpha 独有**：`CachedTokensDetails *CachedTokensDetails`（realtime WSS 计费用）、`VideoTokens int`（视频输入计费用）

⚠️ **绝对不能把 `CachedCreationTokens` 和 `CacheWriteTokens` 相加** —— 必须用 `CacheCreationTokensTotal()` 取 max。main-alpha 曾经用加法，上游两个字段都上报时会**重复收费**，阶段2 已修（`service/tiered_settle.go:67`、`service/text_quota.go:223`）。

### 7.11.3 `dto/gemini.go` —— 官方 UnmarshalJSON 的 aux 陷阱

官方给 `GeminiChatResponse` 加了 `HasUsageMetadata bool` + 自定义 `UnmarshalJSON`，用一个 **aux struct 影子结构**来区分"上游没返回 usageMetadata"和"返回了但全 0"。

⚠️ **aux 会 shadow 整个 `GeminiChatResponse`**：给 `GeminiChatResponse` 加任何字段，**必须同步加进 aux 并在下面 copy**，否则 unmarshal 时该字段被静默丢弃。

main-alpha 的 `ResponseId string` 已按此规则加进 aux（阶段2 处理）。检查方法：`grep -c ResponseId dto/gemini.go` 应 ≥ 3（struct 定义 + aux 定义 + copy 赋值）。

### 7.11.4 `relay/channel/task/ali/adaptor.go` —— 整体保留 main-alpha

- **21 个模型**（`relay/channel/task/ali/constants.go` 的 `ModelList`），官方只 5 个：
  - **15 个 wan**：wan2.7 t2v/i2v/r2v + wan2.6 全系 5 个 + wan2.5 preview 2 个 + wan2.2 flash/plus + wan2-1 + wanx2.1 turbo/plus
  - **6 个 happyhorse**：1.0 和 1.1 各 t2v/i2v/r2v（1.1 于 2026-08-06 由 wpr 新增）
  - ⚠️ 合并时 `ModelList` 一律**取 ours 再补官方新增**，不能反过来
- **计费引擎**：`AdjustBillingOnSubmit` / `AdjustBillingOnComplete` / `BillingResolutionKeyFromParams` / `ParseBillingResolutionFromSize` / `ParseBillingResolutionKeyFromUpstreamJSON`，官方完全没有
- 官方的 `normalizeWan27I2VInput` / `firstTaskImage` / `secondTaskImage` / `firstNonEmpty` 是同功能的另一套实现，**混用会破坏计费**
- 处理方式：`git checkout --ours relay/channel/task/ali/adaptor.go`（连 `adaptor_test.go` 一起）

### 7.11.5 `relay/channel/task/kling/adaptor.go` —— 整体保留 main-alpha

官方新增的 `FinalUnitDeduction` 计费依赖它独有的响应结构字段；main-alpha 有自己的 `AdjustBillingOnComplete` 计费路径。处理方式同上。

### 7.11.6 `relay/channel/openai/relay_responses{,_compact}.go` —— 保留分项计费

官方只保留 `CachedTokens` / `CacheWriteTokens`，**main-alpha 需要的 `TextTokens` / `AudioTokens` / `ImageTokens` 分项和 `info.SetUpstreamResponsesField("usage", ...)` 都要保留** —— 这是分项计费和账单回写的数据来源。

### 7.11.7 `relay/image_handler.go` —— `n` 倍率兜底

官方把 `n` 倍率设置挪到各 adaptor 自己做（`relay/channel/ali/image.go` 等）。main-alpha 在 `image_handler.go` 保留了一个**兜底**：

```go
if info.PriceData.UsePrice && info.PriceData.ImagePerImagePricing == nil {
    if !info.PriceData.HasOtherRatio("n") {   // adaptor 没设才兜底
        info.PriceData.AddOtherRatio("n", float64(imageN))
    }
}
```

条件是 `!HasOtherRatio("n")`，adaptor 设了就不覆盖，纯安全网。**删掉会导致没有自设 n 的 adaptor 少收多图费用**。
`ImagePerImagePricing == nil` 判断也是 main-alpha 独有（按张计费已含实际张数和输入成本，不该再乘 n）。

### 7.11.8 `service/download.go` —— SSRF 防护 + 浏览器 UA 必须共存

```go
req, _ := http.NewRequest(http.MethodGet, originUrl, nil)
setBrowserLikeHeaders(req)                    // main-alpha：很多站点拒绝 Go 默认 UA 返回 403
return GetSSRFProtectedHTTPClient().Do(req)   // 官方：防内网探测
```

⚠️ 官方版是 `GetSSRFProtectedHTTPClient().Get(originUrl)`（没有 UA），**直接取官方会让 Claude 媒体 URL 下载大面积 403**。

### 7.11.9 `model/log.go` —— gopool 异步配额回退

`RecordConsumeLog` 里 `common.DataExportEnabled` 分支必须用 `gopool.Go(func(){ LogQuotaData(...) })` **异步**。官方改成了同步调用，会阻塞请求。

⚠️ 保留异步时记得 import `"github.com/bytedance/gopkg/util/gopool"` —— 官方改同步时把这个 import 删了。

### 7.11.10 `model/channel_cache.go` / `model/model_meta.go` —— 函数并存

| main-alpha 独有 | 官方新增 | 关系 |
|---|---|---|
| `GetChannelsByGroupAndModelPrefix` / `getChannelsByGroupAndModelPrefixFromDB` | `filterChannelsByRequestPathAndModel`（原 `...ByRequestPath` 升级为按模型过滤） | 并存 |
| `GetVendorNameFromModel` / `GetVendorIdFromModel` / `GetEnabledVendorIdFromModel` | `parseModelStatusFilter` / `parseModelSyncFilter` / `SearchModels(keyword, vendor, status, syncOfficial string, offset, limit)` | 并存；`GetVendorModelCounts` 已改为收 `status string` 复用 `parseModelStatusFilter`，保证列表与计数过滤语义一致 |

### 7.11.11 `relay/channel/claude/relay-claude.go` + `relay/channel/gemini/relay-gemini.go` —— 暂时整体保留

官方已把这两个文件改成 `relayconvert` 的薄包装（9 行 / 11 行），是 relaykit 重构（断崖③）的前奏。main-alpha 分别有 987 行 / 1130 行定制：

- claude：prompt cache 自动注入、媒体 URL 转 base64、anthropic-beta 过滤、thinking、`FormatClaudeResponseInfo` 带 `info *relaycommon.RelayInfo` 首参
- gemini：`CHZ-PATCH(gemini-usage-fix)` 图文分离计费、Omni 视频计费、`SetUpstreamResponsesField("usageMetadata", ...)`、`hasImagePart` 追踪

**连带文件**（依赖 main-alpha 的 `buildUsageFromGeminiMetadata(metadata 值类型, ...)` 签名，也要一起保留）：
`relay/channel/gemini/relay-gemini-native.go`、`relay/channel/gemini/relay_responses.go`

⚠️ 官方的 `buildUsageFromGeminiResponse` 依赖一整条新辅助链（`geminiResponseUsageText` / `patchGeminiZeroCompletionUsage` / `geminiResponseInlineImageCount` / `attachEstimatedGeminiBillingUsage` / `dto.NewEstimatedGeminiChatBillingUsage`），且它的 `buildUsageFromGeminiMetadata` 收**指针**而 main-alpha 收**值**。要迁移就得整条链一起迁 —— 计划在阶段4 随 relaykit 一并处理。

### 7.11.12 🔴 `setting/operation_setting/status_code_ranges.go` —— **main-alpha 删掉了官方的 504/524 硬编码不重试**

这是一处**反向偏离**：不是 main-alpha 加了东西，而是 main-alpha **删掉了官方的东西**。
这类最容易在下次合并时被官方悄悄带回来，务必每次检查。

**官方的行为**：硬编码一张 `alwaysSkipRetryStatusCodes = {504, 524}` 表，
`ShouldRetryByStatusCode()` 一开头就查它，命中直接返回 false ——
**即使管理员在后台把 504/524 配进了重试范围也不生效**。

**main-alpha 的行为**（2026-08-06 由 wpr 改）：把这张表和
`IsAlwaysSkipRetryStatusCode()` 整个删掉，504/524 是否重试**完全由后台
`AutomaticRetryStatusCodes` 配置决定**。业务上有些上游 504 是瞬时的，值得重试。

✅ **默认行为没有改变**：默认区间 `{500-503}`、`{505-523}`、`{525-599}` 本来就
**跳过了 504 和 524**，所以不配置的话表现和官方一致。区别只在于
**现在管理员可以把 504/524 配进重试范围**，官方是配了也不生效。
（两个测试看似矛盾——`TestShouldRetryByStatusCode` 手动设了 `500-599` 断言 True、
`TestShouldRetryByStatusCode_DefaultMatchesLegacyBehavior` 用默认区间断言 False——
其实正是在验这件事，不要以为哪个写错了。）

**必须同时保留的 3 处**：

| 位置 | main-alpha 的样子 |
|---|---|
| `setting/operation_setting/status_code_ranges.go` | **没有** `alwaysSkipRetryStatusCodes` 变量、**没有** `IsAlwaysSkipRetryStatusCode()` 函数；`ShouldRetryByStatusCode()` 直接 `return shouldMatchStatusCodeRanges(...)`，开头不做 skip 判断 |
| `controller/relay.go` 的 `shouldRetryTaskRelay` | 5xx 分支里 504/524 走 `operation_setting.ShouldRetryByStatusCode(code)`，其余 5xx 仍 `return true` |
| `status_code_ranges_test.go` | `TestShouldRetryByStatusCode` 里 504/524 断言是 **True**；**没有** `TestIsAlwaysSkipRetryStatusCode` |

⚠️ **合并官方时**：只要 diff 里出现 `alwaysSkipRetryStatusCodes` 或
`IsAlwaysSkipRetryStatusCode`，一律**取 ours**（即删掉）。
快速验证：`grep -c IsAlwaysSkipRetryStatusCode setting/operation_setting/*.go` 应为 **0**。

### 7.11.13 `controller/channel-test.go` —— 渠道测试补 requestId

main-alpha 在构造测试用 `c.Request` 之后主动调一次 `middleware.RequestId()(c)`，
让渠道测试的日志也带 requestId，方便排查。官方没有这行。

```go
c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, requestPath, nil)
middleware.RequestId()(c)   // ← main-alpha 独有，必须在 c.Request 初始化之后
```

⚠️ 顺序不能颠倒 —— 中间件是基于 `c.Request.Context()` 派生新 ctx 再 `WithContext` 回去的，
`c.Request` 为 nil 时会 panic。官方若又改了 `c.Request` 的构造方式（这次就从手工
`&http.Request{}` 换成了 `httptest.NewRequestWithContext`），**取官方的构造 + 保留这行**。

---

## 八、依赖包

| 依赖 | 版本 | 用途 |
|---|---|---|
| `github.com/byteplus-sdk/byteplus-go-sdk-v2` | v1.0.59 | BytePlus 素材库 |
| `gorm.io/driver/clickhouse` | v0.7.0 | ByteHouse/ClickHouse 日志库 |
| `gorm.io/driver/mysql` | v1.6.0 | (main-alpha 版本更新) |
| `gorm.io/gorm` | v1.30.0 | (main-alpha 版本更新) |

---

## 九、合并 main 时的检查清单（每次必做）

- [ ] `model/user.go` 的 `MarshalJSON` 还在，Id 输出为 string
- [ ] `model/log.go` 的 `MarshalJSON` 还在，Id + UserId 都输出为 string
- [ ] `model/log.go` 的 `SumUsedQuota` 用 `Row().Scan()`，不用 `Scan(&stat)`
- [ ] `main.go` 里 `service.StartTaskClearTask()` 还在
- [ ] `main.go` 里 `relay.CompleteVideoTaskOnUpstreamSuccessFn`、`relay.MergeVideoTaskDataWithUpstreamResponseFn` 注入还在
- [ ] `router/api-router.go` 里 `assetAdminRoute` 还在
- [ ] `middleware/restrict_api_domain.go` 还在，且 `main.go` server.Use 里挂载
- [ ] 🔴 **RuoYi JWT SSO 完整性（第 5.3 节，最高优先级）**：
  - `middleware/ruoyi_auth.go` 文件还在，`tryRuoYiJWTAuth` + `errNoRuoYiJWT` 都在
  - `middleware/auth.go` 里 **2 处**调用 `tryRuoYiJWTAuth` 都在（用户链 + 管理员链）
  - 调用处写法是 `else if err != nil && err != errNoRuoYiJWT`（哨兵不当错误）
  - `fromRuoYi` 标志位在，且 `if !fromRuoYi { 要求 New-Api-User 头 }` 判断在
  - `common/init.go` 里 `RuoYiAuthEnabled`、`RuoYiJWTSecret` 读取还在
  - 快速验证：`grep -c tryRuoYiJWTAuth middleware/auth.go` 应为 2
- [ ] `common/init.go` 里 `QUOTA_DATA_STREAM_*` 环境变量读取还在
- [ ] `model/main.go chooseDB` MySQL 分支用 `MYSQL_PREPARE_STMT` env，默认 false
- [ ] `model/main.go migrateLOGDB` 对 ClickHouse skip AutoMigrate
- [ ] `dto/channel_settings.go` 的 `ByteplusAssetAK/SK/Region/ProjectName` / `EnableModerationQuery` / `AzureModelApiVersions` 还在
- [ ] `controller/channel_authz.go` 的 `channelNonSensitiveFields` 包含 `"cost_discount"`
- [ ] `common/utils.go GetTimeString` 用 UTC+8
- [ ] 🔴 **双前端 workspace 结构完整（第 4.5 节）**：
  - `web/package.json` 仍是 workspace 根（含 `"workspaces": ["default", "classic"]` + `catalog`）
  - `web/default/` 和 `web/classic/` 两个目录都在
  - `main.go` 里 **4 处** `//go:embed`：`web/default/dist`、`web/default/dist/index.html`、`web/classic/dist`、`web/classic/dist/index.html`
  - `router/web-router.go` 的 `ThemeAssets` + `NewThemeAwareFS(defaultFS, classicFS)` + `GetTheme() == "classic"` 分支都在
  - `setting/system_setting/theme.go` 还在（官方已删除）
  - `Dockerfile` 两个 builder stage（`builder` + `builder-classic`）都在
  - 快速验证：`grep -c 'go:embed' main.go` 应为 4（+1 个 buildFS 共 5，视实际）；`grep -c classic Dockerfile` 应 ≥ 4
- [ ] web/classic 前端 `/console/setting?tab=ratio` 界面所有 15 个字段（见 4.1）
- [ ] `model/log.go` 的 `LogTypeRetryFail=7`、`LogTypeTest=8`、`LogTypeLogin=9`（编号别被官方覆盖）
- [ ] OperLog 系统（第 7.5 节）完整保留：
  - `model/oper_log.go` / `model/oper_log_push_job_log.go` 还在
  - `controller/oper_log.go` 还在，路由 `/api/oper-log/` 挂载
  - classic 前端 `components/oper-log/OperLogConfirmModal.jsx` 还在，且 4 处触发点（模型倍率/分组倍率/工具定价/渠道日志按钮）都能触发确认对话框
  - `FIELD_LABELS` 里的 23 个字段没被删（若官方新增倍率字段，同步补进 FIELD_LABELS）
- [ ] platform_id 隔离（第 7.6 节）：`middleware/platform.go` 在，`common/init.go` 里 `PlatformIsolationEnabled` 读取还在
- [ ] 企业账号体系（第 7.7 节）：`model/enterprise.go` 在，`model/task_cas_test.go` 的 AutoMigrate/truncate 含 Enterprise 表
- [ ] AllowNegativeBalance（第 7.8 节）：`dto/user_settings.go` 字段在，`service/negative_balance.go` 在，**4 处扣费预检查放行判断都在**（`service/quota.go` / `service/billing_session.go` ×2 / `relay/relay_task.go`）。原为 6 处，阶段2 官方 `116004fd4` 的 BillingSession 重构删掉了 `service/pre_consume_quota.go`，其中 2 处由 `billing_session.go` 承接，非丢失
- [ ] Seedance 官方镜像（第 7.9 节）：`middleware/seedance_official_mirror.go`、`controller/seedance_official_{video,asset}.go` 在，`router/video-router.go` 里 `/api/v3/contents/generations` + `/api/v3/seedance` 路由挂载
- [ ] 渠道类型常量未被覆盖：`ChannelTypeTencentTokenHub = 61`、`ChannelTypeSeedanceGateway = 62`（官方若新增渠道类型可能撞号，需重新编号）
- [ ] TokenHub Base URL 仍为 `https://tokenhub.tencentcloudmaas.com`（`constant/channel.go` 第 129 行附近），**没被官方的 `tokenhub.tencentmaas.com` 覆盖**
- [ ] `relay/channel/tokenhub/` 包还在（Claude 双协议支持，官方只有 OpenAI 兼容）
- [ ] `AnthropicBetaTarget` / `AzureModelResponsesVersions` 在 `dto/channel_settings.go`，且消费方（`relay/channel/claude/adaptor.go`、`relay/channel/openai/adaptor.go`）还在
- [ ] `service/relayconvert/responses_to_chat.go` 的 `UsageFromResponsesUsage` 里 `CacheWriteTokens` 透传还在（GPT-5.6 缓存写入计费）
- [ ] `relay/channel/claude/media_source.go` 在（Claude 媒体 URL 转 base64）
- [ ] 🔴 **504/524 重试改由配置决定（第 7.11.12 节，反向偏离，最易被官方带回来）**：
  - `grep -c IsAlwaysSkipRetryStatusCode setting/operation_setting/*.go` 应为 **0**
  - `grep -c alwaysSkipRetryStatusCodes setting/operation_setting/*.go` 应为 **0**
  - `controller/relay.go` 的 `shouldRetryTaskRelay` 里 504/524 走 `ShouldRetryByStatusCode`
  - `status_code_ranges_test.go` 里 504/524 断言是 `require.True`
- [ ] `controller/channel-test.go` 里 `middleware.RequestId()(c)` 还在，且在 `c.Request` 初始化**之后**（第 7.11.13 节）
- [ ] `relay/channel/task/ali/constants.go` 的 `ModelList` 有 **21 个模型**（15 wan + 6 happyhorse），官方只 5 个
- [ ] `common/body_storage.go` 在（上游中断 body 回退重试）
- [ ] `model/vendor_meta.go` 在（模型厂商管理）
- [ ] 全仓库无残留旧 DB API：`grep -rn "common.UsingSQLite\|common.UsingMySQL\|common.UsingPostgreSQL\|common.UsingClickHouse\|common.LogSqlType" --include="*.go" .` 应为空
- [ ] Go build 通过
- [ ] `go test ./model/... ./relay/channel/claude/...` 关键回归通过：
  - `TestSumUsedQuotaDoesNotResetQuotaByRpmTpmScan`
  - `TestLogMarshalJSONIDsAreString`
  - `TestFormatClaudeResponseInfo_*`
  - `TestStreamServerToolUse*`
- [ ] 前端资源包文件数**没被官方大删除静默吞掉**（第 4.5.4bis 节）：
  - `git ls-files web/classic | wc -l` 应 ≈ **432**（不是几十个）
  - `git ls-files web/default | wc -l` 应 ≈ **960**（不是个位数）
  - 若被删：`git checkout origin/main-alpha -- web/classic web/default` 恢复
- [ ] （如涉及）前端 `bun run build` 通过

---

## 十、未列举 / 待补充

主人明确说过"有些我也忘记了没有列举出来"。每次合并中如果发现新的 main-alpha 独有点被覆盖了，都要补到这个文档。补充方式：

1. 在对应的一级章节下新增小节
2. 或在下面这个「新增记录」表里先粗略记，后续再整理

### 新增记录（草稿区）

| 日期 | 发现点 | 位置 | 说明 |
|---|---|---|---|
| 2026-08-07 | A2 双前端无状态 token 鉴权 | 见 [5.7](#57-a2-双前端无状态-token-鉴权体系%EF%BC%88access_token--flow_token--login_session2026-08-07-补做) | login_session 表 / auth_token service / auth_session controller / setupLogin token bundle / 2FA flow_token / 鉴权层顺序（access_token→cookie→RuoYi→char32）。合并遇官方同名 auth_session.go / auth_token.go 一律取 ours |
| 2026-08-07 | classic 渠道启停/编辑适配官方新接口 | `web/classic/src/hooks/channels/useChannelsData.jsx`、`.../modals/EditChannelModal.jsx` | 官方把渠道状态改成独立接口 `POST /api/channel/:id/status`，并在 `UpdateChannel` 加了"请求体带 status 即拒绝"校验。classic 已适配：启停改走 `POST /:id/status`（成功回调按 action 回填 status，不再取 res.data.data.status）、编辑保存提交前 `delete localInputs.status`。合并若回退这两个 classic 文件会导致渠道无法启停/编辑保存 |
