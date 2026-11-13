# Go 后端升级回归测试手册

> 首次整理：2026-11-13，来源是官方 401 commit 合并（`main-alpha-merge-20261111`）在测试环境的实测。
> 用途：每次 Go 升级上生产前，按本手册在测试环境回归，避免漏测导致生产事故。
> 测试脚本在同目录 `scripts/`；每次测完把结果追加到文末「历次测试记录」。

---

## 一、先定测试档位

| 场景 | 档位 | 大约耗时 |
|---|---|---|
| 每周小更新，改动集中在少数文件，不涉及计费、转发主流程、鉴权、任务 | **A 冒烟档**：第三节标 A 的项目，加上第六节「改动目录 → 必测项」里命中的项 | 15–20 分钟 |
| 隔了很久才更新、合并官方、或改动涉及计费 / relay / 鉴权 / 任务 / 数据库结构 | **B 全量档**：第三节全部项目，再加第四节「上次没覆盖」里的项 | 半天 |

拿不准就跑 B。判断依据：

```bash
git diff --stat <上次上线的commit>..<本次要上线的commit> | tail -1   # 改动规模
git diff --name-only <上次>..<本次> | cut -d/ -f1-2 | sort | uniq -c | sort -rn   # 改动集中在哪些目录
```

改动超过 50 个文件，或命中 `service/*quota*`、`service/*billing*`、`relay/`、`middleware/`、`model/` 中任意一个，一律按 B 档。

---

## 二、测试环境准备

### 2.1 测试机

| 项 | 值 |
|---|---|
| 机器 | `47.237.80.104`，SSH 私钥 `ali_gravitex.pem`，root |
| Go 程序 | `/workplace/gravitex_llm`，端口 3000，配置 `/workplace/.env`，日志 `/workplace/logs/api-*.log` |
| 前端 | maas：`http://<机器>/`；system：`http://<机器>:5666/`；两者都经 nginx `/llm-api` 转发到 Go |
| 数据库 | 主库与 ADB 日志库连接串在 `.env` 的 `SQL_DSN` / `LOG_SQL_DSN`，用 `scripts/db.sh main\|log` 连，免明文密码 |
| 注意 | 测试环境 `ENABLE_TASK_POLLING=false`：视频任务没有后台轮询，状态靠客户端 GET 实时查询推进 |

### 2.2 部署步骤

```bash
# 1. 本地构建（与 Dockerfile 一致：CGO 关闭 + PGO）
CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 go build \
  -pgo="$(go list -m -f '{{.Dir}}' github.com/MoeForge/moejs)/default.pgo" \
  -ldflags "-s -w -X 'github.com/QuantumNous/new-api/common.Version=test-$(git rev-parse --short HEAD)'" \
  -o gravitex_llm_test .

# 2. 测试机单连接上传只有 ~50KB/s：压缩后切 4MB 分片并行 scp，传完核对 sha256
gzip -c gravitex_llm_test > gl.gz && split -b 4m gl.gz gl.part.
for p in gl.part.*; do (for i in 1 2 3; do scp -q -i ali_gravitex.pem $p root@47.237.80.104:/workplace/merge_upload/ && break; sleep 2; done) & done; wait
shasum -a256 gl.gz        # 与测试机 cat gl.part.* > gl.gz && sha256sum gl.gz 比对

# 3. 测试机上：备份 → 替换 → 启动
cd /workplace
cp -p gravitex_llm gravitex_llm.bak_$(date +%Y%m%d_%H%M)
merge_upload/db.sh main dump tokens options channels abilities users > merge_upload/bak_$(date +%Y%m%d).sql
gunzip -c merge_upload/gl.gz > gravitex_llm.new && chmod +x gravitex_llm.new
kill $(pgrep -f "^./gravitex_llm"); sleep 3
mv gravitex_llm.new gravitex_llm && (setsid nohup ./gravitex_llm > /dev/null 2>&1 &)
curl -s localhost:3000/api/status | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['version'])"   # 确认版本号
```

### 2.3 回滚

```bash
cd /workplace && kill $(pgrep -f "^./gravitex_llm"); sleep 3
cp -p gravitex_llm.bak_<时间> gravitex_llm && (setsid nohup ./gravitex_llm > /dev/null 2>&1 &)
# 数据有问题时再导回备份：merge_upload/db.sh main < merge_upload/bak_<日期>.sql
```

### 2.4 测试账号与渠道

- 测试令牌：用户 `chz`（id 666888）名下的 `auto` 分组不限额令牌。系统访问令牌用同一用户的（角色 10 = 管理员，非 root）。具体值不写进仓库，找负责人要。
- 测试前先确认渠道可用：

```sql
-- 各类模型是否有启用的渠道
select id,name,type,`group`,left(models,120) from channels
where status=1 and (models like '%seedance%' or models like '%claude%' or models like '%gemini%' or models like '%gpt%');
```

---

## 三、测试项清单

「档」列：A = 冒烟档必跑，B = 全量档才跑。脚本都在 `scripts/`，在测试机上跑（直连 `localhost:3000`）。

### 3.1 启动与数据库

| 编号 | 档 | 测试项 | 方法 | 预期 |
|---|---|---|---|---|
| S01 | A | 进程启动、版本号 | `/api/status` 的 `version` | 等于本次构建的版本号 |
| S02 | A | 启动日志无异常 | `grep -E "\[ERR\]\|panic\|FATAL" logs/api-<最新>.log` | 无；`jimeng`/`kling` 插件 `route_conflict` 被拒是正常现象（alpha 原生路由优先） |
| S03 | B | 自动建表加列 | 部署前后 `show tables` 对比；新列 `show columns from <表>` | 新表已建；共享表（users/tokens/logs/channels/abilities）只出现升级说明里列出的变更 |
| S04 | B | 价格设置能正常保存 | 把测试库真实 `ModelRatio` 等价格项原样保存一次（system 设置页点保存，或用 root 调 `PUT /api/option/`） | 全部成功。官方新加了校验：必须是 JSON 对象、数值非负 |

### 3.2 文本调用（`scripts/t_text.py`）

```bash
python3 t_text.py <sk-令牌> r_text.json
```

| 编号 | 档 | 测试项 | 预期 |
|---|---|---|---|
| T01–T02 | A | GPT `/v1/chat/completions` 非流式 / 流式 | 200，有 usage |
| T03–T04 | B | GPT `/v1/responses` 非流式 / 流式 | 200 |
| T05 | B | GPT 走 `/v1/messages`（跨格式） | 200 |
| T06–T07 | A | **Claude 走 `/v1/chat/completions`** 非流式 / 流式 | 200，usage 有 `usage_source: anthropic` |
| T08–T09 | A | Claude `/v1/messages` 非流式 / 流式 | 200 |
| T10–T12 | B | Claude 缓存：5m 写入、5m 读取、1h 写入 | usage 里 `ephemeral_5m` / `ephemeral_1h` 正确 |
| T13 | B | Claude 联网搜索 `web_search_20250305` | 200，日志 `tool_surcharges` 有 web_search |
| T14–T15 | A | Gemini `/v1/chat/completions` 非流式 / 流式 | 200 |
| T16–T17 | A | Gemini `/v1beta/models/x:generateContent` / `:streamGenerateContent?alt=sse` | 200 |
| T18 | B | Gemini 走 `/v1/messages` | 200 |
| T19 | B | Gemini `google_search` | 200，日志 `tool_surcharges` 有 google_search |

### 3.3 计费对账（`scripts/recon.py`）—— 最重要

```bash
Q0=$(./db.sh main -N -e "select quota from users where id=666888"); T0=$(date +%s)
python3 t_text.py <sk-令牌> r_text.json
python3 recon.py r_text.json 666888 $T0 $Q0
```

| 编号 | 档 | 核对项 | 计算方法（quota，$1 = 500000） | 预期 |
|---|---|---|---|---|
| B01 | A | **余额变化 = 日志扣费合计** | recon.py 最后一行 | 一致 ✅（测试期间不要有别的请求用同一用户） |
| B02 | A | GPT 表达式计费 | 表达式 `p*4 + c*20 + ...` 算出的是每百万 token 的美元价，quota = 表达式值 / 2 | 例：p=22 c=5 → (88+100)/2 = 94 |
| B03 | A | Claude 倍率计费 | `(p + c×补全倍率 + 缓存读×0.1 + 缓存写×写入倍率) × 模型倍率 × 分组倍率` | 例：p=40 c=4，分组 0.55 → (40+20)×0.55 = 33 |
| B04 | B | Claude 5m / 1h 缓存写入倍率 | 5m 写入 ×1.25，1h 写入 ×2，读取 ×0.1 | 日志 `cache_creation_ratio_5m=1.25`、`cache_creation_ratio_1h=2` |
| B05 | B | Claude 搜索加价 | 每次 $10/千次 × 分组倍率 | 例：1 次 = 0.01×500000×0.55 = 2750 |
| B06 | B | Gemini 搜索加价 | 按查询数，每次 $14/千次 | 例：2 次查询 = 14000 |
| B07 | B | 重试日志 | 渠道失败重试时写 type=7，content 形如 `[重试1/8 545] status_code=403 ...` | 有重试时出现，额度为 0 |

### 3.4 令牌接口（`scripts/t_token.sh`）

```bash
bash t_token.sh <系统访问令牌> 666888
```

| 编号 | 档 | 测试项 | 预期 |
|---|---|---|---|
| K01 | A | `GET /api/token/` 列表 | success，分页字段齐全 |
| K02 | B | `GET /api/token/auto-groups` | 返回分组列表 |
| K03 | A | `POST /api/token/` 创建 | success |
| K04 | B | `GET /api/token/search`、`GET /api/token/:id` | 能查到 |
| K05 | A | `POST /api/token/:id/key` 取完整 key | 返回 key |
| K06 | A | `PUT /api/token/` 修改 | success，字段生效 |
| K07 | A | 新 key 调用后令牌额度扣减 | `remain_quota` 减少、`used_quota` 增加 |
| K08 | B | `/api/usage/token`、`/v1/dashboard/billing/subscription` | 返回令牌额度 |
| K09 | A | 禁用（`PUT ?status_only=true`）后调用 | 返回 `Invalid token` |
| K10 | A | 删除后查询、调用 | 查询 `record not found`，调用 `Invalid token` |
| K11 | A | **`allow_user_quota_query` 默认值** | 创建时不传 → false；`/api/usage/token` 不返回 `user_quota` |
| K12 | B | `allow_user_quota_query` 修改 | 改 true → 用量接口有 `user_quota`；修改时不传 → 保持原值；改 false → 没有 |

### 3.5 视频 Seedance（`scripts/t_video.py`，会产生真实上游费用，用最便宜参数）

```bash
python3 t_video.py <sk-令牌> r_video.json
```

| 编号 | 档 | 测试项 | 预期 |
|---|---|---|---|
| V01 | A | 平台接口提交 `seedance-2-0-mini`（480p 4s 无音频） | 返回 `task_id`，status=queued |
| V02 | B | 平台接口提交 `seedance-2-0-mini-NSFW` | 同上 |
| V03 | A | `GET /v1/video/generations/:id` 查询到完成 | 状态变 SUCCESS，有 `video_url`（约 1 分钟） |
| V04 | B | 官方镜像 `POST /api/v3/contents/generations/tasks`（content 里引用 `asset://`） | 返回 `{"id":...}` |
| V05 | B | 镜像 `GET /api/v3/contents/generations/tasks/:id` | 官方原样格式，status=succeeded |
| V06 | B | 镜像 `DELETE` 已完成任务 | 返回 `{}`，再查返回 `task_not_exist` |
| V07 | A | **视频扣费** | 日志 type=2「视频任务成功」，`tokens × 单价`。例：40594 × $3.5/M = 71039；余额变化等于日志合计 |

查任务和日志：

```sql
select task_id,status,progress,quota,channel_id from tasks where task_id in (...);
-- 日志库
select type,model_name,quota,completion_tokens,content from logs where user_id=666888 and created_at>=<开始时间戳>;
```

### 3.6 素材库（`scripts/t_video.py` 里一起跑）

| 编号 | 档 | 测试项 | 预期 |
|---|---|---|---|
| A01–A04 | B | 镜像 `POST /api/v3/seedance?Action=` CreateAssetGroup / GetAssetGroup / UpdateAssetGroup / ListAssetGroups | 官方信封格式 `ResponseMetadata` + `Result` |
| A05 | A | 镜像 CreateAsset（公网图片 URL） | 返回 `Result.Id` |
| A06 | A | 镜像 GetAsset 轮询 | 约 10 秒变 `Active` |
| A07–A08 | B | 镜像 ListAssets / UpdateAsset | 成功 |
| A09 | B | 平台 `GET /v1/assets/:id` 能查到镜像建的素材 | 两套接口数据互通 |
| A10–A11 | A | 镜像 DeleteAsset / DeleteAssetGroup | 成功 |
| A12 | A | 平台 `POST /v1/asset-groups`、`GET /v1/asset-groups` | 成功 |
| A13 | A | 平台 `POST /v1/assets`、`GET /v1/assets?group_id=`、`GET /v1/assets/:id` | 素材变 active |
| A14 | A | 平台 `DELETE /v1/assets/:id`、`DELETE /v1/asset-groups/:id` | `deleted: true` |

> 注意：素材库镜像接口的调用文档 `docs/byteplus/seedance-2.0-official-api-mirror-usage.md` 写的是 `/ark/seedance/v3`，**实际路由是 `/api/v3/seedance`**。按文档调用会拿到前端 HTML 页面（HTTP 200），容易误判为成功。
> 测试图片不要用库里客户的素材，用公网图片，例如 Wikimedia 风景图。

### 3.7 前端 maas / system 接口

两个前端都经 nginx `/llm-api` 调 Go，鉴权头是 `Authorization: Bearer <若依token>` + `New-API-User`。用访问令牌模拟管理员（非 root）：

```bash
H1="Authorization: <系统访问令牌>"; H2="New-Api-User: 666888"
curl -s -H "$H1" -H "$H2" -H "Accept-Language: zh-CN" "http://localhost:3000/api/channel/?p=1&page_size=3"
```

| 编号 | 档 | 接口 | 预期 |
|---|---|---|---|
| F01 | A | nginx 转发：`curl -H "Host: <机器IP>" http://127.0.0.1/llm-api/api/status`、`http://127.0.0.1:5666/llm-api/api/status` | 两个都返回 Go 的 status JSON |
| F02 | A | `/api/channel/?p=1&page_size=3` | success，有 items/total/type_counts |
| F03 | A | `/api/log/?p=1&page_size=2`、`/api/log/stat?type=2` | success |
| F04 | B | `/api/user/?p=1`、`/api/data/?start_timestamp=&end_timestamp=` | success |
| F05 | B | `/api/group/`、`/api/models/?p=1`、`/api/pricing` | success |
| F06 | A | `/api/user/self/groups`、`/api/user/models` | success |
| F07 | B | `/api/subscription/admin/plans` | success |
| F08 | B | `/api/asset-admin/groups?channel_id=<seedance渠道>` | success（不带 channel_id 会报 `channel_id is required`，正常） |
| F09 | B | `/api/option/` | 非 root 返回「无权进行此操作」（正常）；root 能读 |
| F10 | B | 报错语言：带 `Accept-Language: zh-CN` 调一个会报错的接口（如创建名称超长的令牌） | 返回中文报错 |
| F11 | B | **真机点页面**：maas 登录、建 key、看消费记录、视频创作；system 渠道列表、日志、用户、设置页保存 | 页面正常，无英文报错、无字段缺失 |

### 3.8 日志与 IP

| 编号 | 档 | 测试项 | 预期 |
|---|---|---|---|
| L01 | A | 消费日志写入 ADB | `select ... from logs where user_id=... and created_at>=...` 能查到；消费日志 `content` 本来就为空，正常 |
| L02 | B | IP 记录 | 只在用户开启「记录 IP」时写；经 nginx 的请求是真实 IP；在测试机上直连 localhost 测试时是 `::1`，正常 |
| L03 | B | 重试日志、视频异步结算日志 | IP 为空，是原有行为 |
| L04 | B | 日志 `other` 关键字段 | `request_path`、`billing_mode`、`group_ratio`、`admin_info.use_channel` 等齐全 |

---

## 四、上次没覆盖、下次大更新必须补测

2026-11-13 这一轮因为测试环境缺渠道，或者无法主动触发，下面这些没测到。**下次 B 档要优先补上**：

| 项目 | 为什么高风险 | 怎么测 |
|---|---|---|
| Gemini OAuth Bearer 渠道（broker 自动刷新 token） | alpha 新功能，测试环境没配这种渠道 | 配一个 OAuth 渠道，测调用和拉模型列表 |
| 上游换模型标记 | 只有上游真换了模型才会触发 | 看生产日志 `upstream_model_fallback` 字段是否出现 |
| 视频实时查询漏退款修复（`5369f5e86`） | 涉及用户资金 | 按 `20261111/合并后待开发清单.md` 1.4 节手动验证 |
| 按秒计费视频（Veo、kling、jimeng 等） | 计费逻辑和 Seedance 不同 | 各提交一个最短任务，核对扣费 |
| 阿里生图 `parameters.n`、`prompt_extend` 加价 | 合并时手工改过（透传模式） | 透传渠道和非透传渠道各生 2 张，核对张数和扣费 |
| gpt-image / Gemini 生图 / Seedream | 按张、按 token 计费路径多 | 各调一次，核对日志 |
| 企业子账号令牌（模型 / 厂商限额上限） | 令牌接口有企业子账号的额度上限逻辑 | 用子账号创建和修改令牌 |
| Java 侧联动：导出账单、日账单、告警 | Java 直接读 logs / tokens / channels 表 | 在 system 导出账单，检查新增字段不影响导出 |
| 多节点 / 集群节点 | 生产是 k8s 多副本加集群节点 | 集群节点只放行转发接口，令牌管理接口可用 |
| 多 key 渠道自动禁用和恢复 | 官方改了规则：key 全部禁用后整个渠道变禁用，恢复后自动启用 | 用一个多 key 测试渠道模拟 key 失效 |

---

## 五、改动目录 → 必测项

小更新时，按改动命中的目录挑测试项。

| 改动路径 | 必测 |
|---|---|
| `service/*quota*`、`service/*billing*`、`service/text_quota.go`、`setting/ratio_setting/`、`setting/billing_setting/`、`pkg/billingexpr/` | 3.3 全部 + 3.2 全部 |
| `relay/channel/claude/` | T06–T13、B03–B05 |
| `relay/channel/gemini/`、`relay/channel/vertex/` | T14–T19、B06 |
| `relay/channel/openai/`、`relay/*responses*` | T01–T05、B02 |
| `relay/channel/task/`、`controller/task*`、`service/task_*`、`relay/relay_task.go` | 3.5 全部 |
| `controller/*asset*`、`controller/seedance*`、`router/video-router.go` | 3.6 全部 |
| `controller/token.go`、`model/token*.go`、`middleware/auth.go` | 3.4 全部 |
| `middleware/ruoyi_auth.go`、`router/`、`controller/channel*.go`、`controller/option.go` | 3.7 全部 |
| `model/log*.go`、`model/main.go`、`model/*.go` 里有 gorm 结构变更 | 3.1 S03 + 3.8 全部，并通知 Java 侧 |
| `go.mod`、`Dockerfile` | S01–S02 + A 档全部 |

---

## 六、历次测试记录

### 2026-11-13 · 官方 401 commit 合并（`merge-c72a49724`），B 档

| 分组 | 结果 |
|---|---|
| 3.1 启动与数据库 | ✅ 启动正常；价格设置用测试库真实数据原样保存，11 项全部通过新校验 |
| 3.2 文本调用 | ✅ 19/19 返回 200 |
| 3.3 计费对账 | ✅ 逐条按公式手算一致；余额变化 39551 = 日志合计 |
| 3.4 令牌接口 | ✅ 全部通过 |
| 3.5 视频 | ✅ 3 个任务都成功；扣费 3 × 71039 = 余额变化 |
| 3.6 素材库 | ✅ 镜像 10 个 Action + 平台简化接口全部通过 |
| 3.7 前端接口 | ✅ 接口层全部正常；F11 真机点页面未做 |
| 3.8 日志与 IP | ✅ 和合并前行为一致 |

测试中发现并修复的问题：
- 修改令牌接口改不了 `allow_user_quota_query`：合并遗漏，`66dac422e` 已修。
- 新建令牌默认开启 `allow_user_quota_query`，会让 maas 用户新建的 key 都能看到账户余额：改为默认关闭，`c72a49724`。

测试中发现的非合并问题：
- 素材库镜像文档路径写错（见 3.6 注意事项）。
- 渠道 545 返回 403（`error code: 1010`），会自动重试到其他渠道。

<!-- 后续测试按上面格式追加 -->
