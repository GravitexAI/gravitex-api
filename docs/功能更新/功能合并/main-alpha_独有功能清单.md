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

### 5.3 RuoYi JWT 认证
- **环境变量**：`RUOYI_AUTH_ENABLED`、`RUOYI_JWT_SECRET`
- **位置**：`common/init.go` 读取环境变量，`middleware/auth.go` 中验证 JWT
- **用途**：接受 Java 后端（RuoYi-Plus）发的 JWT token，实现 Java ↔ Go 双向 SSO

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
| 腾讯云 TokenHub 渠道 | `constant/channel.go` 的 `ChannelTypeTencentTokenHub = 61`、`APITypeTencentTokenHub` | 支持 OpenAI + Claude 双协议原生透传 |
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
- [ ] `common/init.go` 里 `RuoYiAuthEnabled`、`RuoYiJWTSecret`、`QUOTA_DATA_STREAM_*` 环境变量读取还在
- [ ] `model/main.go chooseDB` MySQL 分支用 `MYSQL_PREPARE_STMT` env，默认 false
- [ ] `model/main.go migrateLOGDB` 对 ClickHouse skip AutoMigrate
- [ ] `dto/channel_settings.go` 的 `ByteplusAssetAK/SK/Region/ProjectName` / `EnableModerationQuery` / `AzureModelApiVersions` 还在
- [ ] `controller/channel_authz.go` 的 `channelNonSensitiveFields` 包含 `"cost_discount"`
- [ ] `common/utils.go GetTimeString` 用 UTC+8
- [ ] web/classic 前端 `/console/setting?tab=ratio` 界面所有 15 个字段（见 4.1）
- [ ] `model/log.go` 的 `LogTypeRetryFail=7`、`LogTypeTest=8`、`LogTypeLogin=9`（编号别被官方覆盖）
- [ ] OperLog 系统（第 7.5 节）完整保留：
  - `model/oper_log.go` / `model/oper_log_push_job_log.go` 还在
  - `controller/oper_log.go` 还在，路由 `/api/oper-log/` 挂载
  - classic 前端 `components/oper-log/OperLogConfirmModal.jsx` 还在，且 4 处触发点（模型倍率/分组倍率/工具定价/渠道日志按钮）都能触发确认对话框
  - `FIELD_LABELS` 里的 23 个字段没被删（若官方新增倍率字段，同步补进 FIELD_LABELS）
- [ ] platform_id 隔离（第 7.6 节）：`middleware/platform.go` 在，`common/init.go` 里 `PlatformIsolationEnabled` 读取还在
- [ ] 企业账号体系（第 7.7 节）：`model/enterprise.go` 在，`model/task_cas_test.go` 的 AutoMigrate/truncate 含 Enterprise 表
- [ ] AllowNegativeBalance（第 7.8 节）：`dto/user_settings.go` 字段在，`service/negative_balance.go` 在，**6 处扣费预检查放行判断都在**
- [ ] Seedance 官方镜像（第 7.9 节）：`middleware/seedance_official_mirror.go`、`controller/seedance_official_{video,asset}.go` 在，`router/video-router.go` 里 `/api/v3/contents/generations` + `/api/v3/seedance` 路由挂载
- [ ] 渠道类型常量未被覆盖：`ChannelTypeTencentTokenHub = 61`、`ChannelTypeSeedanceGateway = 62`（官方若新增渠道类型可能撞号，需重新编号）
- [ ] `AnthropicBetaTarget` / `AzureModelResponsesVersions` 在 `dto/channel_settings.go`，且消费方（`relay/channel/claude/adaptor.go`、`relay/channel/openai/adaptor.go`）还在
- [ ] `service/relayconvert/responses_to_chat.go` 的 `UsageFromResponsesUsage` 里 `CacheWriteTokens` 透传还在（GPT-5.6 缓存写入计费）
- [ ] `relay/channel/claude/media_source.go` 在（Claude 媒体 URL 转 base64）
- [ ] `common/body_storage.go` 在（上游中断 body 回退重试）
- [ ] `model/vendor_meta.go` 在（模型厂商管理）
- [ ] 全仓库无残留旧 DB API：`grep -rn "common.UsingSQLite\|common.UsingMySQL\|common.UsingPostgreSQL\|common.UsingClickHouse\|common.LogSqlType" --include="*.go" .` 应为空
- [ ] Go build 通过
- [ ] `go test ./model/... ./relay/channel/claude/...` 关键回归通过：
  - `TestSumUsedQuotaDoesNotResetQuotaByRpmTpmScan`
  - `TestLogMarshalJSONIDsAreString`
  - `TestFormatClaudeResponseInfo_*`
  - `TestStreamServerToolUse*`
- [ ] （如涉及）前端 `bun run build` 通过

---

## 十、未列举 / 待补充

主人明确说过"有些我也忘记了没有列举出来"。每次合并中如果发现新的 main-alpha 独有点被覆盖了，都要补到这个文档。补充方式：

1. 在对应的一级章节下新增小节
2. 或在下面这个「新增记录」表里先粗略记，后续再整理

### 新增记录（草稿区）

| 日期 | 发现点 | 位置 | 说明 |
|---|---|---|---|
| _(留空待补)_ | | | |
