# 故障复盘：seedance-2 任务提交成功但 tasks 表无记录

| 项 | 内容 |
|---|---|
| 发生时间 | 2026-09-08 12:21:10（北京时间） |
| 丢失任务 | `cgt-20260908122110-zfnk2` |
| 受影响用户 | `user202630182358`（user_id `2034298460664471554`） |
| 模型 / 渠道 | `seedance-2-0-NSFW` / channel 81（BytePlus Ark） |
| 处理节点 | `iv-yeh2fkivi8sobosc2dzv`（服务器独立进程，**不是 k8s 集群 pod**） |
| 现象 | 用户拿到 task id（HTTP 200）→ 轮询我方接口报 `task_not_exist` → 直连 BytePlus 能查到该任务 |
| 结论 | **代码缺陷**。`tasks` 行入库失败被静默吞掉，任务和计费同时丢失 |
| 影响 | 该任务无记录、**未计费**（token-ratio 视频模型靠轮询成功才结算），当日该实例仅此 1 例 |
| 引入提交 | `694668c82`（2026-03-15，caihongzhan，`feat chz veo\veo fast视频`）—— 详见第四节 |
| 修复状态 | **已修复**（三层：截断算法识别 JSON 转义 / 入库前丢弃非法 JSON 列 / 先入库再回响应 + orphan 落盘）—— 详见第六节 |
| 丢失任务处置 | 不补录、不计费 |

---

## 一、生产日志原文

文件：`/workplace/logs/archive/20260908/api-20260908094758.log-20260908-13.gz`

```text
[INFO]  2026/09/08 - 12:21:10 | 202609081221101420090455W7UEpdcbz1PFVLo | [ChannelCostDiscount] channel=81 model=seedance-2-0-NSFW discount=1.000000 source=channel_or_model
[SYS]   2026/09/08 - 12:21:10 | [TaskSubmit] upstream request URL: https://ark.ap-southeast.bytepluses.com/api/v3/contents/generations/tasks
[DEBUG] 2026/09/08 - 12:21:10 | 202609081221101420090455W7UEpdcbz1PFVLo | http transport select: host=ark.ap-southeast.bytepluses.com protocol=auto shards=1 policy=auto|1
[DEBUG] 2026/09/08 - 12:21:10 | 202609081221101420090455W7UEpdcbz1PFVLo | http transport negotiated: host=ark.ap-southeast.bytepluses.com protocol=auto shards=1 policy=auto|1 negotiated=HTTP/2.0
[SYS]   2026/09/08 - 12:21:10 | insert task error: Error 3140 (22032): Invalid JSON text: "Missing a comma or '}' after an object member." at position 1062 in value for column 'tasks.upstream_request_body' at row 1.
```

### 逐行解读

| 日志行 | 对应代码 | 说明 |
|---|---|---|
| `[ChannelCostDiscount] channel=81 model=seedance-2-0-NSFW` | `relay/relay_task.go:214` | 渠道已选定、折扣已快照。**每次任务提交必打这一行**，所以它是"这次提交确实进入了 relay"的锚点 |
| `[TaskSubmit] upstream request URL: .../api/v3/contents/generations/tasks` | doubao adaptor `BuildRequestURL` | 准备调用上游 Ark 建任务接口 |
| `http transport select` / `negotiated=HTTP/2.0` | `service` HTTP 客户端 | 与 `ark.ap-southeast.bytepluses.com` 建连成功 |
| `insert task error: Error 3140 ...` | `controller/relay.go:695-696` | **上游已经建好任务、响应已经写回客户端之后**，往 `tasks` 表插入本地记录时被 MySQL 拒绝 |

> 注意日志里**没有**任何错误响应、没有重试、没有取消上游任务 —— 因为 `task.Insert()` 的错误只被 `common.SysError` 打了一行日志就结束了。

### 旁证

- `tasks` 表查不到 `cgt-20260908122110-zfnk2`；该用户 12:18:17 → 12:26:26 之间一条记录都没有
- ByteHouse `logs` 表按 `request_id / upstream_request_id` 查该 task id：**0 条** → 确实没计费
- 当日该实例：`[TaskSubmit] upstream request URL` 43 次、`tasks` 入库 31 条、`insert task error` **恰好 1 条**
- k8s 集群 4 个 pod（`6f4798d5b8-{5bmxw,8946z,94jrr,mj549}`）12:19–12:23 全时段扫描，`[ChannelCostDiscount]` **0 条** → 该用户流量走的是服务器独立进程

---

## 二、这个报错到底是什么意思

### 2.1 MySQL Error 3140 (22032)

```
Error 3140 (22032): Invalid JSON text: "Missing a comma or '}' after an object member."
at position 1062 in value for column 'tasks.upstream_request_body' at row 1.
```

拆开看：

- **`Error 3140` = `ER_INVALID_JSON_TEXT`**：往 MySQL 的 **`json` 类型列**写入的字符串不是合法 JSON，MySQL 直接拒绝整条 INSERT。
- **`tasks.upstream_request_body`**：出问题的列。它在 Go 侧是
  ```go
  // model/task.go:71
  UpstreamRequestBody json.RawMessage `json:"-" gorm:"column:upstream_request_body;type:json"`
  ```
  DDL 为 `` `upstream_request_body` json DEFAULT NULL ``。**`json` 列会做严格语法校验**（如果当初用 `longtext`，这条脏数据会被原样存下来，任务不会丢）。
- **`Missing a comma or '}' after an object member`**：JSON 解析器读完一个 `"key": value` 之后，期待 `,` 或 `}`，结果碰到了别的东西 → 说明**某个字符串值被提前闭合了**，后面的内容裸露在对象里。
- **`at position 1062`**：出错字节位置。这个数字非常关键 —— 它正好落在 `common/truncation.go` 的截断点上（见下节）。

### 2.2 为什么一条 INSERT 失败，用户却拿到了 task id

因为**响应比入库先发生**。`relay/channel/task/doubao/adaptor.go`：

```go
// DoResponse ── 本次走的是普通路径（非官方镜像，见 4.3）
info.PublicTaskID = dResp.ID
ov := dto.NewOpenAIVideo()
ov.ID, ov.TaskID = info.PublicTaskID, info.PublicTaskID
...
c.JSON(http.StatusOK, ov)          // ← 这里已经把 200 + {"id":"cgt-...","task_id":"cgt-..."} 写回客户端
return dResp.ID, responseBody, nil

// 官方镜像路由同理，只是换成原样透传：
// if c.GetBool(common.KeySeedanceRawMirror) { c.Data(http.StatusOK, "application/json", responseBody); ... }
```

然后才轮到 `controller/relay.go`：

```go
// controller/relay.go:693-698
if shouldPersistLyriaTask(...) {
    task := buildSubmittedTask(c, relayInfo, result, isPerSecondOrTokenRatio)
    if insertErr := task.Insert(); insertErr != nil {
        common.SysError("insert task error: " + insertErr.Error())   // ← 只打日志，不改响应、不重试、不取消上游
    }
}
```

时序：

```
上游建任务成功 ──► c.Data(200, {"id":"cgt-..."}) ──► 客户端已收到 id
                                                        │
                                              task.Insert() 失败（Error 3140）
                                                        │
                                                 SysError 一行日志，流程正常结束
```

结果：
- 用户手上有 id，我方 `tasks` 表没有 → 轮询 `videoFetchByIDRespBodyBuilder` → `GetByTaskId` 查不到 → `task_not_exist`
- 异步轮询 worker 靠 `tasks` 表驱动（`GetAllUnFinishSyncTasks`），也永远看不到它 → **不结算、不扣费**
- 上游那边任务照跑照收费 → 纯亏

---

## 三、根因：`TruncateBase64Content` 会产出结构非法的 JSON

### 3.1 数据是怎么变脏的

`buildSubmittedTask` 在把上游请求体存进任务行之前做了一次 base64 截断（避免图片/视频 base64 把库撑爆）：

```go
// controller/relay.go:816-818
if len(result.UpstreamBodyBytes) > 0 {
    task.UpstreamRequestBody = []byte(common.TruncateBase64Content(string(result.UpstreamBodyBytes)))
}
```

`common/truncation.go` 里三个截断函数都是**在 JSON 文本上做裸字符串扫描**，用"下一个双引号"当字符串结尾，**完全不认识 JSON 的转义引号 `\"`**：

| 函数 | 位置 | 判定字符串结尾的方式 |
|---|---|---|
| `truncateInlineDataBase64` | `common/truncation.go:78` | `strings.Index(content[quoteStartIndex:], "\"")` |
| `truncateDataImageBase64` | `common/truncation.go:192` | `strings.Index(content[base64StartIndex:], "\"")` |
| `truncateRawBase64Content` | `common/truncation.go:257` | `strings.Index(content[quoteIndex+1:], "\"")` |

本次触发的是第三个 `truncateRawBase64Content`（`maxBase64Length = 1000`）：

```go
// common/truncation.go:266-285
quotedContent := content[quoteIndex+1 : nextQuoteIndex]        // ← nextQuoteIndex 可能是一个「转义引号」
if len(quotedContent) > minBase64Length && isBase64String(quotedContent) {   // >2000 且 ≥80% base64 字符
    if len(quotedContent) > maxBase64Length {
        result.WriteString(quotedContent[:maxBase64Length])    // 只保留前 1000 字节
        result.WriteString("...[base64数据已截断，长度:")
        result.WriteString(fmt.Sprintf("%d", len(quotedContent)))
        result.WriteString("]")
    }
    result.WriteString("\"")          // ← 自己补一个结束引号，把字符串「关掉」
}
startIndex = nextQuoteIndex + 1       // ← 然后从那个「转义引号」之后继续拷贝原文
```

问题就在最后两步：
1. 它以为 `nextQuoteIndex` 是字符串真正的结束引号，于是**自己补了一个 `"` 把字符串闭合**；
2. 但那个引号其实是值内部的 `\"`，字符串在原文里还没结束；
3. 于是被截掉的那一段之后的原文内容，**全部裸露在 JSON 对象里**，不在任何字符串内。

另外 `isBase64String`（`common/truncation.go:292`）的判定过于宽松 —— 它只看"≥80% 的字符落在 base64 字母表"：

```go
base64Chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
return float64(base64CharCount)/float64(len(s)) > 0.8
```

**一段长英文提示词就会被误判成 base64**（字母 + 空格，空格占比通常 ~15%，字母占比 ~85% > 80%）。这正是本次的输入形态：`seedance-2-0-NSFW` 的英文提示词。
反过来，中文长提示词是安全的（中文字符不在字母表里，占比压到 80% 以下）—— 所以同一个用户 12:26:26 那条中文长提示词的任务（task 14952）就正常入库了。

### 3.2 最小复现（本地已验证）

```go
run  := strings.Repeat("ab", 1050)   // 2100 字节，100% base64 字符
body := `{"model":"m","content":[{"text":"` + run + `\"x\" tail","type":"text"}]}`
out  := common.TruncateBase64Content(body)
```

实际输出：

```
input : len=2161  valid=true
output: len=1100  valid=false   err=invalid character 'x' after object key:value pair
marker offset = 1033
output tail   = "...[base64数据已截断，长度:2101]\"x\\\" tail\",\"type\":\"text\"}]}"
prefix len    = 33            ← "text" 值的起始下标
```

输出长这样（`▼` 是被凭空补上的结束引号）：

```
{"model":"m","content":[{"text":"abab……(1000 字节)……...[base64数据已截断，长度:2101]"x\" tail","type":"text"}]}
                                                                                    ▲
                                                              字符串在这里被提前关掉，后面 x\" tail 变成裸内容
```

- Go 报 `invalid character 'x' after object key:value pair`
- MySQL 报 `Missing a comma or '}' after an object member`

**是同一件事的两种措辞。**

### 3.3 position 1062 对上了

```
marker 偏移 = 「字符串值的起始下标」 + maxBase64Length(1000)
```

| | 前缀长度 | 截断点偏移 |
|---|---|---|
| 复现用例 `{"model":"m","content":[{"text":"` | 33 | 1033 |
| **生产实际** | **62** | **1062** ← 报错 position |

62 字节的前缀完全符合真实请求体形态，例如
`{"model":"ep-20260420145750-ghkx9","ratio":"16:9","text":"` 一类。**数字精确吻合，根因确认。**

### 3.4 同一函数的另一类失败模式（顺带确认存在）

`quotedContent[:1000]` 是**按字节切**的，还会：
- 切在 `\n` / `\t` 的反斜杠和转义字符之间 → 留下孤立的 `\`，产生非法转义
- 切断多字节 UTF-8 字符 → 非法编码

本地同样复现为 `outValid=false`。虽然本次不是这个分支触发，但修的时候要一起解决。

### 3.5 触发条件矩阵（本地枚举验证）

对各种真实提示词形态跑 `TruncateBase64Content`，看输出是否还是合法 JSON：

| 提示词形态 | 是否被截断 | 输出 |
|---|---|---|
| 英文长串（无引号） | 是 | 合法 |
| **英文长串 + 引号出现在第 2000 字节之后** | **是** | **非法** ← 本次 |
| 英文长串 + 引号出现在第 2000 字节之前 | 是 | 合法（首段 <2000，不触发截断） |
| 英文长串 + 换行 `\n` | 是 | 合法 |
| 英文长串 + `&`（被转义成 `&`） | 是 | 合法 |
| 中文长串 | 否 | 合法 |
| 中文长串 + 引号 | 否 | 合法 |
| 中英混排长串 | 否 | 合法 |
| 空格占比高的英文长串 | 否 | 合法 |
| 短英文 | 否 | 合法 |

**唯一触发条件（三个必须同时满足）：**
1. 某个 JSON 字符串值 **> 2000 字节**
2. 该值 **≥ 80% 的字符落在 base64 字母表**（英文/字母数字为主；中文、大量空格都会把比例压到 80% 以下）
3. 该值里 **第一个引号出现在第 2000 字节之后**（引号在前面反而安全，因为首段不足 2000 不会进截断分支）

条件极窄 —— 这就是为什么代码写了 5 个多月才第一次踩到。

---

## 四、哪次提交引入的（git 考古）

### 4.1 结论

**`694668c82`（2026-03-15，caihongzhan，`feat chz veo\veo fast视频`）** 就是引入今天这个失败模式的提交。它在**同一个 commit 里**一次性凑齐了全部三个必要条件：

1. 新增字段 `UpstreamRequestBody json.RawMessage gorm:"column:upstream_request_body;type:json"`（强校验 JSON 列）
2. 新增调用 `task.UpstreamRequestBody = []byte(common.TruncateBase64Content(string(upstreamBodyBytes)))`
3. 把 `truncateRawBase64Content` 从"全坏"改成"只剩转义引号这个洞"

之后 **`common/truncation.go` 全历史只有 2 个 commit**（`69b64004f` 创建、`694668c82` 修改），也就是说 **2026-03-15 之后这段逻辑再没被动过**。当前生产行为 == 03-15 的行为。

### 4.2 为什么是 694668c82 而不是更早的 69b64004f

`69b64004f`（2026-03-09）创建 `common/truncation.go` 时，`truncateRawBase64Content` 的两个分支**都不补结束引号**：

```go
// 694668c82 之前
if len(quotedContent) > minBase64Length && isBase64String(quotedContent) {
    ...
    result.WriteString("]")          // ← 没有 result.WriteString("\"")
} else {
    result.WriteString(quotedContent) // ← 也没有结束引号
}
startIndex = nextQuoteIndex + 1       // ← 原文的结束引号被跳过
```

这个版本会**吃掉每一个字符串的结束引号**，对任何 JSON 输入都出非法结果。实测：

```
【普通请求体 {"model":"seedance-2-0-NSFW","duration":5}】
  03-09版   非法(invalid character 's' after object key)
  当前版     合法
【触发用例（英文长串 + 2000字节后有引号）】
  03-09版   非法(invalid character 't' after object key)
  当前版     非法(invalid character 'h' after object key:value pair)
```

也就是说 03-09 那版根本不可能和 `json` 列共存 —— 一接上去每条任务都会挂。它只存在了 6 天，期间没有任何调用点把它的输出写进数据库。`694668c82` 把它修到"大部分情况正确"的同时接上了 `json` 列，**修得刚好够用，只留了转义引号这一个洞**。

### 4.3 已排除的候选提交

| 候选 | 日期 / 作者 | 为什么排除 |
|---|---|---|
| `39fd1b3e4` doubao 适配器新增原始请求体透传分支 | 07-17 caihongzhan | 这是官方镜像路由（`/api/v3/contents/generations/tasks`）才走的 raw 透传。查库：raw 透传专属字段（`service_tier` / `execution_expires_after` / `output_format`）只出现在 **3 个 user、76 条任务**里（首现 2026-07-23，紧随该 commit），**本次用户 `2034298460664471554` 不在其中** → 本次请求走的是普通 `/v1/video/generations` 归一化路径，与该 commit 无关 |
| `09ae59472` seedance系列参数修改 | 08-20 king_15 | 只改了 `req.Duration == -1` 判断和 `MaxTaskDurationSeconds`（3600→36000），与请求体内容/长度/转义无关 |
| `d3c730e46` Lyria 3能力接入 | 08-26 king_15 | 把赋值语句从旧位置搬进 `buildSubmittedTask`，纯重构。`git blame` 指到它只是"最后一次触碰该行" |
| `892eb6e3c` dola-seedream-5-0-pro-260628计费信息 | 09-07 king_15 | 只新增了两个 md 文档，`git log -S` 是文档正文里提到符号导致的假命中 |
| `fb728fb55` / `13f87e8a6` | 04-06 / 04-18 caihongzhan | `type:json` 从 03-15 起就是 json，从没被改过；`13f87e8a6` 只是给 Save/Updates 加 `Omit` |
| `common/json.go` 相关 | — | `common.Marshal` 一直是 `encoding/json.Marshal`，转义策略（`"`→`\"`、`&`→`&`）从未变化 |

### 4.4 为什么"以前没这个问题"是对的

- 触发条件极窄（见 3.5），5 个多月没有用户踩到
- base64 截断本身**一直在正常工作**：库里 `upstream_request_body` 含 `...[base64数据已截断` 标记的行有 **6041 条，最早 2026-03-30**，全部合法入库
- 独立实例日志里 `insert task error` 的出现情况：**09-01 / 09-03 / 09-05 / 09-06 各 0 次，09-08 恰好 1 次**

所以准确说法是：**缺陷自 2026-03-15 起就在，2026-09-08 是第一次被真实输入触发**，不是最近某次改动改坏的。

---

## 五、代码定位明细

| # | 缺陷 | 当前位置 | **首次引入** | 后续触碰 |
|---|---|---|---|---|
| 1 | 截断算法在 JSON 文本上裸扫描、不识别 `\"` | `common/truncation.go:236-289`（`truncateRawBase64Content`）<br>同类问题：`:78`、`:192` | `694668c82`（03-15）加上"补回结束引号"后形成本失败模式；函数本体来自 `69b64004f`（03-09） | 无（全历史仅 2 个 commit） |
| 2 | `isBase64String` 判定过宽（≥80% 落在 base64 字母表就算 base64，长英文提示词被误判） | `common/truncation.go:292-309` | `69b64004f`（03-09） | 无 |
| 3 | 截断结果写进强校验的 `json` 列 | `model/task.go:71` `gorm:"type:json"` | `694668c82`（03-15） | `13f87e8a6`（04-18，仅加 `Omit`）；`fb728fb55`（04-06，仅格式/位置） |
| 4 | 任务提交路径调用截断、赋值前不校验 JSON 合法性 | `controller/relay.go:816-818` | `694668c82`（03-15）→ `cf183b943`（04-10）在当前架构重新落地 | `d3c730e46`（08-26，搬进 `buildSubmittedTask`，纯重构 → `git blame` 会指到它，但不是引入者） |
| 5 | `task.Insert()` 失败只 `SysError`，不改响应/不重试/不落 orphan | `controller/relay.go:695-696` | `cf183b943`（04-10） | `d3c730e46`（08-26 重构） |
| 6 | **本该防住这件事的机制是死代码**：`TaskSubmitDelayResponse` / `TaskSubmitResponseBody` 定义了、注释写明"DoResponse 不直接写响应，由 RelayTaskSubmit 在 task.Insert() 成功后写入"，但全仓库**没有任何一处 `c.Set(TaskSubmitDelayResponse, ...)`**，`TaskSubmitResponseBody` 也**只被写进 context、没有任何地方读出来回给客户端**；5 个 adaptor（gemini/vertex/azurevideo/uptoken/seedancegateway）的这个分支是空转，doubao 连分支都没有 | `relay/common/relay_info.go:38-40` | `694668c82`（03-15） | 无 |

> 注意：缺陷 1/3/4/6 都来自同一个 commit `694668c82`。也就是说**这次故障的四个必要条件是在一次提交里同时落地的**，`git blame` 指向 8 月的 `d3c730e46` 只是因为它最后碰过那几行。

补充（非本次故障，顺手发现的死代码）：`relay/relay_task.go:874` 的 `getUserRequestBody` 定义了但零调用方，所以 `tasks.user_request_body` 这一列**一直是 NULL**。

---

## 六、修复方案（已落地）

三层修复，全部已实现。`go build ./...` 通过，`relaykit` 独立构建通过，新增回归测试全绿。

### 6.1 根治：截断算法识别 JSON 转义（`common/truncation.go`）

新增两个辅助函数，三个截断函数全部改用它们：

```go
// indexJSONStringEnd 返回第一个「未被转义」的双引号下标，跳过值内部的 \"
func indexJSONStringEnd(s string) int

// truncateJSONStringPrefix 在不切断转义序列（\" \\ \uXXXX）和多字节 UTF-8 的
// 前提下取前缀 —— 半截字节同样会让结果变成非法 JSON
func truncateJSONStringPrefix(s string, max int) string
```

| 改动点 | 原来 | 现在 |
|---|---|---|
| `truncateInlineDataBase64` 找字符串结尾 | `strings.Index(..., "\"")` | `indexJSONStringEnd(...)` |
| `truncateDataImageBase64` 找字符串结尾 | 同上 | 同上 |
| `truncateRawBase64Content` 找字符串结尾 | 同上 | 同上 |
| 三处 + `truncateDataUrlBase64` 的截断切片 | `s[:maxLength]`（裸字节切） | `truncateJSONStringPrefix(...)` |
| `isBase64String` | 只看「≥80% 字符落在 base64 字母表」 | **先排除含空白字符的串**，长英文散文不再被误判成 base64 |

没有改成「Unmarshal → 截断 → Marshal」的结构化方案，原因：`encoding/json` 往返会重排对象键、并把大整数变成科学计数法（`1788841055` → `1.788841055e+09`），会改变已入库诊断数据的形态。修好扫描器的转义处理即可，且输出与历史数据保持一致。

### 6.2 兜底：诊断字段永远不许阻塞任务落库（`model/task.go`）

放在 `Task.Insert()` 里，覆盖所有入库入口（`controller/relay.go`、`controller/lyria_async.go`）：

```go
func (Task *Task) Insert() error {
	Task.dropInvalidJSONColumns()
	return DB.Create(Task).Error
}

// data / upstream_request_body 是 MySQL json 列，非法值会以 Error 3140 拒绝整条
// INSERT、任务行连带丢失。诊断字段绝不能阻塞任务落库 —— 宁可丢这一列。
func (Task *Task) dropInvalidJSONColumns()
```

配套在 `common/json.go` 新增 `IsValidJson([]byte) bool`（遵守 AGENTS.md「JSON 操作走 common 包装」的约定）。

**这一层是关键**：无论以后截断逻辑再出什么 bug，都不可能再把整条任务写没。

### 6.3 一致性：先入库再回响应 + orphan 落盘（方案 ①+②）

**① 先入库再回响应。** 各 adaptor 的 `DoResponse` 是直接往 `c.Writer` 写的，逐个改要动 13 个文件且容易漏。改为在 `controller.RelayTask` 入口统一缓冲响应，`task.Insert()` 成功后才真正下发：

```go
// controller/relay.go —— RelayTask 开头
var buffered nativeResponseWriter          // 复用 native_interactions 里已有的捕获 writer
buffered.ResponseWriter = c.Writer
c.Writer = &buffered
defer func() {
	c.Writer = buffered.ResponseWriter
	...
	c.Status(status)
	_, _ = c.Writer.Write(buffered.body.Bytes())
}()
```

同时把 `task.Insert()` **提到结算和日志之前**（原来是先结算再入库）。入库失败时：

```go
persistFailed = true
handleTaskPersistFailure(c, relayInfo, task, insertErr)   // 日志 + orphan 落盘 + 取消上游
buffered.reset()                                          // 丢弃已缓冲的 200 + task id
taskErr = service.TaskErrorWrapperLocal(..., http.StatusInternalServerError)
```

预扣费此时**还没结算**，由 `RelayTask` 顶部已有的 `defer` 走 `Billing.Refund` 原样退回，客户端拿到 500 可以安全重试。

顺序调整的安全性：`buildSubmittedTask` 只读 `relayInfo.PriceData` / `BillingSource` / `SubscriptionId` / `TokenId`，这些都由 `PreConsume → syncRelayInfo` 在 `RelayTaskSubmit` 内部（即 `DoResponse` 之前）写好，`SettleBilling` 不会改动它们 —— 入库内容与调整前完全一致。

**② orphan 落盘 + 取消上游**（新文件 `controller/task_orphan.go`）：

- `dumpOrphanTask` → 追加写 `failed_tasks.jsonl`（对齐 `model/log_buffer.go` 的 `failed_logs.jsonl`）。字段是按「手工补录一行 tasks 记录」挑的，不能直接 `Marshal(task)` —— `private_data` 带 `json:"-"`，那样会丢掉计费上下文。
- `cancelOrphanUpstreamTask` → 通过已有的 `TaskCancelAdaptor` 可选接口调上游 DELETE，用的是**本次提交实际使用的 baseURL/key**（`relayInfo.ChannelBaseUrl` / `relayInfo.ApiKey`），避免多 key 渠道取消到别的 key 上。平台不支持取消时只打 `logger.LogWarn`，落盘记录保证任务不会失去痕迹。
- 原来的 `common.SysError("insert task error: ")` 升级为带 request_id 的 `logger.LogError`，含 task_id / platform / channel / user。

### 6.4 顺手清掉的死代码

`TaskSubmitDelayResponse` / `TaskSubmitResponseBody` 这套「延迟写响应」机制全仓库没有任何一处 `c.Set`，`TaskSubmitResponseBody` 也只写不读，5 个 adaptor（gemini/vertex/azurevideo/uptoken/seedancegateway）的分支是空转；gemini/vertex 的那个分支还会返回未经 `markOmniTaskID` 标记的 task id（真跑起来是错的）。6.3 的缓冲方案已覆盖同一目标，为避免两套机制打架，把常量定义和 7 处死分支一并删除。

### 6.5 改动清单与验证

| 文件 | 改动 |
|---|---|
| `common/truncation.go` | +`indexJSONStringEnd`、+`truncateJSONStringPrefix`，4 处截断改用，`isBase64String` 排除空白串 |
| `common/truncation_test.go` | 新增：触发条件矩阵（输出必须仍是合法 JSON）、截断能力不回退、两个辅助函数的表驱动测试 |
| `common/json.go` | +`IsValidJson` |
| `model/task.go` | +`dropInvalidJSONColumns`，`Insert()` 调用 |
| `controller/relay.go` | 响应缓冲 + 入库提前 + 入库失败 500/退款/丢弃缓冲 |
| `controller/task_orphan.go` | 新文件：`handleTaskPersistFailure` / `dumpOrphanTask` / `cancelOrphanUpstreamTask` |
| `controller/native_interactions.go` | +`nativeResponseWriter.reset()` |
| `relay/common/relay_info.go`、5 个 task adaptor | 删除死代码 `TaskSubmitDelayResponse` / `TaskSubmitResponseBody` |

验证：
- `go build ./...` 通过；`cd relaykit && GOWORK=off go build ./...` 通过
- `go test ./common/` 全绿；新增回归测试在**未修复的 HEAD 上会判定为非法 JSON**（用临时 worktree 验证过），说明它守的是真缺陷
- 既存失败项与本次改动无关，已用干净 HEAD worktree 确认：`model`/`controller`/`relay` 若干测试文件本身编译不过（`taskSnapshot.PluginState`、`newTaskAPIRequest`、`getTaskAdaptorForRequest`、`nativeRouteBilling.Settle` 签名），`relay/channel/task/jsplugin` 的 `TestTaskAdaptorPreservesOpenAIVideoFailureSlotsAndOwnsLifecycle` 在 HEAD 上同样失败（`completed_at` 多出一个字段）

### 6.6 未做（本次范围外）

- `tasks.user_request_body` 一直是 NULL（`relay/relay_task.go` 的 `getUserRequestBody` 零调用方）—— 与本次故障无关，未动
- 上述既存编译不过的测试文件 —— 属于他人在改的范围，未动

### 附：为什么"没有 auto_increment 空洞"这条线索一开始把我带偏了

`tasks` 表 `id` 从 108 到 14979、共 14872 行，**完全连续、零空洞**。我据此一度以为 INSERT 根本没到 MySQL。实际是：MySQL 的 `json` 列语法校验发生在**分配 auto_increment 之前**，所以 Error 3140 失败的语句**不消耗自增值**。以后排查类似问题别把"无空洞"当成"没执行过 INSERT"的证据。

---

## 七、丢失任务的处置

**决定：不补录。** `cgt-20260908122110-zfnk2` 不再插入 tasks 表，该任务不计费。

（重建所需字段的考古结果保留在此，万一以后需要：`task_id=cgt-20260908122110-zfnk2`、`platform=54`、`user_id=2034298460664471554`、`group=default`、`channel_id=81`、`token_id=497`/`老王 voss`、`action=generate`、`submit_time=1788841270`、`quota=0`、`upstream_request_body` 不可恢复。上游 `execution_expires_after=172800`，2026-09-10 12:21 之后上游结果链接也会过期。）

## 八、遗留事项

1. **历史全量排查（未做）**：`insert task error` 在独立实例历史日志里可能还有别的受害任务。已抽查 09-01 / 09-03 / 09-05 / 09-06 各 0 次、09-08 恰好 1 次。全量要扫 `/workplace/logs/archive` 的 **354 GB / 312 个 gz**，会长时间压满生产机 CPU（排查中误起过一次全量扫描，load 冲到 5.4，已 kill 干净）。需要的话建议只扫最近 3 天或夜间限速跑。
2. **k8s 集群 pod 历史日志**：同样代码同样暴露面，单个归档动辄 5–8 GB，量级更大，同上。
3. **告警**：`failed_tasks.jsonl` 目前只落盘，没有接告警通道；`logger.LogError` 已带 request_id，可按需接入。
