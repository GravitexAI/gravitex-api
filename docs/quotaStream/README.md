# QuotaStream 架构与运维手册

> 面向同事的 QuotaStream 全景文档。介绍系统职责、五个角色、数据流、Redis Key、幂等机制、DLQ 处理与日常运维。
>
> 深度排查场景请配合姊妹文档 `docs/quota_data_redis_stream_观测手册.md`。

## 目录

- [1. 背景与设计目的](#1-背景与设计目的)
- [2. 整体架构](#2-整体架构)
- [3. 五个角色详解](#3-五个角色详解)
- [4. 关键数据流](#4-关键数据流)
- [5. Redis Key / Stream 清单](#5-redis-key--stream-清单)
- [6. 环境变量参数](#6-环境变量参数)
- [7. 幂等三道防线](#7-幂等三道防线)
- [8. 死信队列 (DLQ) 专题](#8-死信队列-dlq-专题)
- [9. 生产运维手册](#9-生产运维手册)
- [10. 常见故障场景](#10-常见故障场景)
- [11. 代码文件索引](#11-代码文件索引)

---

## 1. 背景与设计目的

### 1.1 涉及的两张表

| 表 | 定位 | 说明 |
|---|---|---|
| `logs` (type=2) | 消费事实表 | Source of Truth。每次用户调 LLM 或计费事件都插一行,金额/token 明细完整 |
| `quota_data` | 统计聚合表 | 按 `(user_id, username, model_name, created_at=小时整点, use_group, token_id, channel_id, node_name)` 聚合,前端 dashboard / 计费看板从这里查 |

### 1.2 老链路的问题

老链路在 `CreateLog` 之后同步执行 `UPDATE quota_data SET quota = quota + ?`:

- 高并发下行锁打架,写入长尾严重
- 应用重启 / 事务失败重放时容易**重复累加**,长期跑下来账对不上
- 全部同步逻辑挂在计费主路径上,一旦 DB 抖动,用户请求受影响

### 1.3 新链路的核心思想

- 每条 `type=2` log 触发一个事件,通过 Redis Stream 异步派生
- 消费时**按小时桶从 logs 表重新 SUM,整桶覆盖写回 quota_data** —— 不再增量累加
- 幂等标记 + bucket 锁 + backfill 兜底,保证最终一致

**语义:高可用最终一致 (eventual consistency),不是严格 exactly-once 财务级。**

---

## 2. 整体架构

```
┌────────────────────────────────────────────────────────────────────┐
│  用户请求 → CreateLog(type=2) → [ logs 表 ]  ← Source of Truth    │
└───────────────────────────────┬────────────────────────────────────┘
                                │ QueueConsumeLogToQuotaStream(log, source)
                                ▼
                     ┌──────────────────────┐
                     │  Producer (生产者)   │  4 处调用入口
                     │  XADD 主 Stream      │  → stream:quota_data:v1
                     └──────────┬───────────┘
                                │
      ┌─────────────────────────┴────────────────────────┐
      │                                                  │
      ▼                                                  ▼
┌────────────────────┐                          ┌──────────────────────┐
│ Consumer (消费者)  │                          │ Backfill (补偿扫描) │
│ XREADGROUP × N     │                          │ 只 master 节点跑     │
│ 检查 doneKey       │                          │ 扫 logs 游标,补投   │
│ 抢 bucket 锁       │                          │ 防 Redis 短挂漏投    │
│ 从 logs 重新 SUM   │                          └──────────────────────┘
│ 整桶覆盖 quota_data│
│ Lua 写 doneKey+ACK │
└──────┬─────────────┘
       │ apply 失败 / worker 挂 → 消息 Pending
       ▼
┌──────────────────────────────┐
│ Reclaimer (回收器)           │
│ 每实例 1 个协程 XAUTOCLAIM   │
│ 认回 idle 超时的 pending 消息 │
│ retry > 阈值 → 送 DLQ        │
└─────────────┬────────────────┘
              │
              ▼
     ┌─────────────────────────┐
     │ DLQ (死信)              │
     │ stream:quota_data:dlq:v1│
     │ 只写不读 → 人工处理     │
     └─────────────────────────┘
```

---

## 3. 五个角色详解

### 3.1 Producer(生产者)

**函数:** `QueueConsumeLogToQuotaStream(log *Log, source string)`
**入口位置(4 处):**

| 调用位置 | source 值 | 场景 |
|---|---|---|
| `model/log.go:514` | `record_consume_log` | 用户文本模型计费日志落库后 |
| `model/log.go:580` | `record_task_billing_log` | 通用任务计费(如绘图) |
| `controller/task_video.go:1552` | `video_billing_per_second` | 视频按秒计费 |
| `controller/task_video.go:1880` | `video_billing_token_ratio` | 视频按 token 比例计费 |

**行为:**
1. 从 log 构造 `quotaStreamEvent`(含 event_id、log_id、user、model、hour bucket_ts、quota 等)
2. `XADD stream:quota_data:v1MAXLEN ~ QUOTA_DATA_STREAM_MAX_LEN`

**注意:** Producer **不做重试**,失败仅打错误日志。补漏交给 Backfill。

### 3.2 Consumer(消费者)

**启动:** `runQuotaDataStreamConsumer(consumerName)`,每实例启动 `QUOTA_DATA_STREAM_CONSUMER_COUNT` 个协程。
**核心循环:**

```
for {
    msgs = XREADGROUP quota_data_workers <consumer> COUNT <batch> BLOCK <ms>
    for msg in msgs:
        processQuotaStreamMessage(msg)
}
```

**`processQuotaStreamMessage` 状态机:**

```
      解码消息
         │
         ▼
    doneKey 存在? ──是──► ACK,跳过(幂等命中)
         │否
         ▼
    Lua begin state:
      ├─ DONE                  → ACK 跳过
      ├─ ACQUIRED_BUCKET       → 进入 apply
      ├─ PROCESSING            → 本地重试 5 次 × 200ms
      ├─ BUCKET_BUSY_MARKED_DIRTY → 本地重试;5 次仍失败则放手,交给 reclaimer
      └─ 其他                  → 报错并返回
         │
         ▼
    apply:
      1. 等待 source log 在 logs 表可见(最多 5 × 200ms)
      2. 按 (user, model, hour) 从 logs 表 SELECT SUM(quota / token / count)
      3. UPSERT 整桶到 quota_data
      4. Lua finalize:写 doneKey (TTL=DONE_TTL_HOURS)、释放/续锁
         └─ 若 finalize 发现 dirty 标志被其他消息置位,继续下一轮 SUM 重算
      5. ACK
```

**关键点:**

- **整桶覆盖**:同一 (user, model, hour) 无论重放多少次,结果一致(sum 是幂等的)
- **bucket 锁 + dirty 标志**:同 hour 桶并发时,后来的 event 只把 dirty 置 1,让当前持锁 worker 多做一轮 SUM 就好,不阻塞并发
- **wait source log**:防止 log 在数据库还没提交、事件先到 Stream 的时序问题(ClickHouse 场景更明显)

### 3.3 Reclaimer(回收器)

**启动:** `runQuotaDataPendingReclaimer()`,每实例 1 个协程。

**行为:** 每 2 秒扫一次:

```
XAUTOCLAIM stream:quota_data:v1 quota_data_workers <reclaimer> MIN-IDLE <ms> 0-0
    ↓ Redis 不支持 XAUTOCLAIM 时降级
    XPENDING + 手动 XCLAIM

for each 认回的 message:
    retry = XPENDING 查询到的 delivery count
    if retry > QUOTA_DATA_STREAM_RETRY_LIMIT:
        → 写入 DLQ
        → ACK 从主 stream 移除
    else:
        → 重新走 processQuotaStreamMessage
```

**触发条件:** 消息被 delivered 后超过 `QUOTA_DATA_STREAM_PENDING_IDLE_MS` 毫秒仍未 ACK。典型原因:consumer 崩溃、apply 卡在数据库慢查、Lua finalize 失败。

### 3.4 Backfill(补偿扫描器)

**启动:** `runQuotaDataBackfillLoop()`,**仅 master 节点跑**(`common.IsMasterNode`),多实例互斥用 Redis SetNX 锁保证同一时刻只有一个 backfill 在工作。

**行为:** 每 `QUOTA_DATA_STREAM_BACKFILL_INTERVAL_SECONDS` 秒执行一次:

```
1. SetNX quota_data:stream:backfill:lock <NodeName> TTL=2×INTERVAL
   拿不到锁 → 让位
2. GET quota_data:stream:last_scan_log_id  ← 上次扫到哪
3. SELECT * FROM logs
   WHERE id > cursor AND type = 2
   ORDER BY id ASC
   LIMIT QUOTA_DATA_STREAM_BACKFILL_BATCH
4. 对每一条:
   - 非目标 log 直接跳过并前推游标
   - 目标 log → 构造 event,XADD 主 stream,source="backfill"
   - 前推游标
5. 更新游标
```

**作用:** 兜底 Producer 时 Redis 短暂不可用 / XADD 失败导致的漏投递。**不重复处理已投递过的**,靠 Consumer 端 doneKey 幂等保证。

**冷启动:** 首次启动时用当前 logs 表 max id 作为初始游标(`ensureQuotaDataBackfillCursorInitialized`),避免全表补投。

### 3.5 DLQ(死信队列)

见 [第 8 节](#8-死信队列-dlq-专题)。

---

## 4. 关键数据流

### 4.1 完整生命周期

```
[t0] 用户调 LLM
      ↓
[t0+ε] CreateLog(type=2) → logs 表 insert
      ↓ QueueConsumeLogToQuotaStream(log, "record_consume_log")
[t0+ε] XADD stream:quota_data:v1  { event_id, log_id, user, model, quota, bucket_ts, ... }
      ↓
[t1] Consumer XREADGROUP 收到
      ↓ doneKey 未命中
      ↓ Lua begin → ACQUIRED_BUCKET
      ↓
[t2] waitQuotaSourceLogReadyWithRetry: 确认 logs 表可以查到本条(防 ClickHouse 异步 flush)
      ↓
[t3] loadQuotaDataBucketAggFromLogs: SELECT SUM(quota, tokens, count(*))
      ↓
[t4] rewriteQuotaDataBucketExact: 事务内 UPSERT quota_data
      ↓
[t5] Lua complete: SET doneKey EX=DONE_TTL_HOURS×3600, DEL processingKey, 释放/续 bucketKey
      ↓ (如果期间被并发标了 dirty → 回到 [t3] 再来一轮,最多 20 轮)
[t6] XACK 从 pending 队列移除
      ↓
[t7] 消息生命结束
```

### 4.2 异常路径

| 异常点 | 后果 | 恢复机制 |
|---|---|---|
| Producer XADD 失败 | log 已入库但事件未入 stream | Backfill 按 log_id 游标补投 |
| Consumer XREADGROUP 失败 | Consumer 循环 sleep 2s 重试 | 自愈 |
| apply 中查 logs 失败(source log not visible) | 本地重试 5 × 200ms;仍失败返回 → 消息保留 pending | Reclaimer 认回重试 |
| apply 中 UPSERT 失败 | quota_data 未更新 → 消息 pending | Reclaimer 认回;RETRY_LIMIT 后进 DLQ |
| Lua finalize owner mismatch | 说明 bucket 锁被其他 worker 抢走 | 视为无害,消息 Pending 由 Reclaimer 或下次投递解决 |
| Consumer 进程崩溃 | 消息 Pending 无人 ACK | idle 超时后 Reclaimer 认回 |

---

## 5. Redis Key / Stream 清单

| Key / Stream | 类型 | 作用 | TTL |
|---|---|---|---|
| `stream:quota_data:v1` | Stream | 主队列 | MAXLEN 近似上限,老消息会被 XADD 挤出 |
| `stream:quota_data:dlq:v1` | Stream | 死信队列(只写不读) | 无(需人工清理) |
| Consumer Group `quota_data_workers` | Group | 主 Stream 的消费组 | 无 |
| `quota_data:stream:backfill:lock` | String | Backfill 分布式锁 | `2 × BACKFILL_INTERVAL_SECONDS` |
| `quota_data:stream:last_scan_log_id` | String | Backfill 游标(最后扫到的 log id) | 无 |
| `quota_data:stream:done:{hash_tag}:{event_id}` | String | 幂等完成标记 | `DONE_TTL_HOURS × 3600 秒` |
| `quota_data:stream:processing:{hash_tag}:{event_id}` | String | 单消息处理中锁 | `LOCK_TTL_SECONDS × 1000 毫秒` |
| `quota_data:stream:bucket_lock:{hash_tag}` | String | bucket 桶锁 | `LOCK_TTL_SECONDS × 1000 毫秒` |
| `quota_data:stream:bucket_dirty:{hash_tag}` | String | bucket 脏标志(触发再扫一轮) | `LOCK_TTL_SECONDS × 1000 毫秒` |

**hash_tag 格式:** `{user_id:model_name:bucket_ts}`
用于 Redis Cluster 场景保证同一 event 涉及的 4 个 key 落在同一 slot,避免 CROSSSLOT 错误。

---

## 6. 环境变量参数

| 变量名 | 含义 | 建议起始值 |
|---|---|---|
| `QUOTA_DATA_STREAM_ENABLED` | 总开关 | `true` |
| `QUOTA_DATA_STREAM_CONSUMER_COUNT` | 单实例消费者协程数 | 1(灰度)→ 4-8(稳定后) |
| `QUOTA_DATA_STREAM_BATCH_SIZE` | 单次 XREADGROUP 拉取条数 | 50 |
| `QUOTA_DATA_STREAM_BLOCK_MS` | XREADGROUP 阻塞等待时长(毫秒) | 5000 |
| `QUOTA_DATA_STREAM_PENDING_IDLE_MS` | Pending 消息认回门槛(毫秒) | 30000 |
| `QUOTA_DATA_STREAM_RETRY_LIMIT` | 单条消息进 DLQ 前的重试上限 | 5 |
| `QUOTA_DATA_STREAM_BACKFILL_BATCH` | Backfill 每批扫描 log 条数 | 200 |
| `QUOTA_DATA_STREAM_BACKFILL_INTERVAL_SECONDS` | Backfill 执行间隔(秒) | 5 |
| `QUOTA_DATA_STREAM_DONE_TTL_HOURS` | 幂等完成标记保留时长(小时) | 24 |
| `QUOTA_DATA_STREAM_MAX_LEN` | 主 Stream 近似最大长度 | 500000 |
| `QUOTA_DATA_STREAM_LOCK_TTL_SECONDS` | 各类 Redis 锁基础 TTL(秒) | 30 |

调优思路:

- **消费吞吐不够** → 提高 `CONSUMER_COUNT` 或 `BATCH_SIZE`
- **Pending 堆积** → 检查 `PENDING_IDLE_MS` 是否过大;检查数据库慢查
- **DLQ 出现消息** → 直接看 `failed_reason`,不要盲目调参数
- **Stream 长度暴涨** → 提高 `MAX_LEN` 或加快消费

---

## 7. 幂等三道防线

### 第一道:doneKey 完成标记

```go
doneKey := "quota_data:stream:done:{user:model:bucket}:event_id"
if EXISTS doneKey:
    ACK 跳过
```

TTL 内(默认 24 小时)重投递直接短路,不进入 apply 阶段。

### 第二道:bucket 锁 + dirty 标志

Lua 原子操作:

- 同 bucket 已被别的 worker 持有 → 当前 worker 不进入 apply,只把 dirty 置 1,让持锁者多做一轮 SUM
- 持锁者 finalize 时若发现 dirty=1 → 释放前再做一轮 SUM,直到 dirty 清零

**效果:** 同 hour 桶的并发 event 收敛到一次桶重算,不重复更新 quota_data。

### 第三道:整桶覆盖语义

`rewriteQuotaDataBucketExact` 用事务内**先删后插 / UPDATE-to-target-value**,不做 `+=`:

```
UPSERT quota_data
  SET count = <汇总的 count>,
      quota = <汇总的 quota>,
      token_used = <汇总的 token>
```

即使 doneKey 意外失效 + Backfill 重复投递,整桶最终值仍等于 logs 表实际 sum。

---

## 8. 死信队列 (DLQ) 专题

### 8.1 定位

`stream:quota_data:dlq:v1` 是最后兜底。**没有自动消费者,只写不读,必须人工介入。**

### 8.2 两个写入入口

| 触发位置 | 触发条件 | 代码位置 |
|---|---|---|
| `processQuotaStreamMessage` 解码失败 | Stream 消息字段无法 parse(脏数据) | `quota_stream.go:210` |
| `runQuotaDataPendingReclaimer` 重试超限 | 同一条消息 retry > `QUOTA_DATA_STREAM_RETRY_LIMIT` | `quota_stream.go:396-397` |

### 8.3 DLQ 消息字段

除原始 event 字段外,额外附带:

| 字段 | 说明 |
|---|---|
| `failed_message_id` | 主 Stream 里的 message id |
| `failed_consumer` | 最后一个失败的 consumer name |
| `failed_reason` | 失败原因字符串 |
| `retry_count` | 失败前的重试次数 |

### 8.4 人工处理流程

**步骤 1:看长度**

```bash
redis-cli XLEN stream:quota_data:dlq:v1
```

正常应为 0 或极小;持续增长即告警信号。

**步骤 2:看最近失败原因**

```bash
redis-cli XREVRANGE stream:quota_data:dlq:v1 + - COUNT 20
```

按 `failed_reason` 归类。常见原因:

- `source log not visible yet` → 数据库主从延迟 / ClickHouse flush 未跟上
- `bucket owner mismatch` → 锁续期时被抢占(通常是长时间卡住)
- `bucket repair rounds exceeded` → dirty 标志被反复置位,并发过高需要限流
- `parse xxx` / `decode failed` → Stream 里出现脏消息(应用版本不一致)

**步骤 3:修根因**

不同 reason 走不同路径,不要无脑重投。修完 DB / 修完 Redis 后:

- 大部分场景:等 Backfill 按 log_id 游标扫过就自动补回,**不用手动回放**
- 特殊场景:如果 log_id 已经小于当前游标(错过窗口),需要手动 XADD 到主 stream 或直接调 `rebuildQuotaDataBucketFromLogs` 修单个桶

**步骤 4:清空 DLQ**

处理完毕确认无问题后:

```bash
# 直接删掉整个 DLQ stream(下次自动创建)
redis-cli DEL stream:quota_data:dlq:v1

# 或只删已处理的 message
redis-cli XDEL stream:quota_data:dlq:v1 <message_id>
```

### 8.5 DLQ 告警配置建议

- `XLEN stream:quota_data:dlq:v1 > 0` → warning 告警
- `XLEN stream:quota_data:dlq:v1 > 100` → critical 告警,立即介入

---

## 9. 生产运维手册

### 9.1 日志已精简,靠 Redis / SQL 命令巡检

日常巡检**不依赖应用日志**,主要看 Redis 状态和 DB 数据。应用日志仅保留:

- 启动一次性事件(`start consumer=...`、`stream workers enabled`)
- 所有异常路径 `SysError`
- 关键告警(`move to dlq`、`unexpected begin state`、`abort state failed`)

### 9.2 一键健康检查(命令 cheat sheet)

```bash
# ── 生产端健康 ──
redis-cli XLEN stream:quota_data:v1                         # 主 Stream 长度
redis-cli XINFO STREAM stream:quota_data:v1                 # 首尾时间戳、entries 总数

# ── 消费端健康 ──
redis-cli XINFO GROUPS stream:quota_data:v1                 # consumers 数量、pending、last-delivered-id
redis-cli XINFO CONSUMERS stream:quota_data:v1 quota_data_workers   # 每个 consumer 的 idle、pending

# ── Pending 明细 ──
redis-cli XPENDING stream:quota_data:v1 quota_data_workers          # 汇总
redis-cli XPENDING stream:quota_data:v1 quota_data_workers - + 20   # 最近 20 条待 ACK

# ── Backfill 推进 ──
redis-cli GET quota_data:stream:last_scan_log_id            # 补偿游标(应持续增长)
redis-cli GET quota_data:stream:backfill:lock               # 当前持锁节点
redis-cli TTL quota_data:stream:backfill:lock               # 锁剩余 TTL(应在轮换)

# ── 死信 ──
redis-cli XLEN stream:quota_data:dlq:v1                     # 死信数(应为 0)
redis-cli XREVRANGE stream:quota_data:dlq:v1 + - COUNT 10   # 最近 10 条失败

# ── 幂等标记(排查用) ──
redis-cli KEYS 'quota_data:stream:done:*' | wc -l           # 大致规模(生产慎用 KEYS)
```

### 9.3 SQL 抽样核对 logs 与 quota_data

按小时抽样,验证聚合是否收敛:

```sql
-- logs 侧
SELECT
    (created_at DIV 3600) * 3600 AS hour_ts,
    SUM(quota) AS logs_quota,
    SUM(prompt_tokens + completion_tokens) AS logs_tokens,
    COUNT(*) AS logs_count
FROM logs
WHERE type = 2
  AND created_at >= ? AND created_at < ?
GROUP BY hour_ts
ORDER BY hour_ts;

-- quota_data 侧
SELECT
    created_at AS hour_ts,
    SUM(quota) AS quota_data_quota,
    SUM(token_used) AS quota_data_tokens,
    SUM(count) AS quota_data_count
FROM quota_data
WHERE created_at >= ? AND created_at < ?
GROUP BY created_at
ORDER BY created_at;
```

**判断标准:**

- 实时短窗口(最近 1-2 分钟)存在小差值 → 正常,Stream 是异步派生
- Backfill 正常推进的前提下,差值应逐步收敛到 0
- 差值持续扩大 → 走 [第 10 节故障排查](#10-常见故障场景)

---

## 10. 常见故障场景

### 10.1 logs 有,quota_data 不增长

**排查顺序:**

1. `XLEN stream:quota_data:v1` —— 是否有消息入队
2. `XINFO GROUPS stream:quota_data:v1` —— consumers 数量是否正常
3. `XPENDING stream:quota_data:v1 quota_data_workers` —— pending 是否堆积
4. `GET quota_data:stream:last_scan_log_id` —— Backfill 游标是否推进
5. 看应用错误日志(SysError)找根因

**常见原因:**

- Redis 不可用 → Producer XADD 失败(应用日志有 SysError)
- Consumer 进程全挂 / 未启动
- 数据库 UPSERT 失败(慢查询、锁死)
- Backfill 锁被异常节点占用

### 10.2 Pending 越堆越多

**重点排查:**

- 应用错误日志中 `apply failed` / `finalize failed`
- DB 连接数、慢查询、行锁
- Redis 高延迟或写失败
- Consumer 协程数是否被 `QUOTA_DATA_STREAM_CONSUMER_COUNT` 卡死

**应急处理:**

- 短期扩容 `QUOTA_DATA_STREAM_CONSUMER_COUNT`
- 缩短 `QUOTA_DATA_STREAM_PENDING_IDLE_MS`,让 Reclaimer 更快介入

### 10.3 DLQ 出现消息

见 [第 8.4 节](#84-人工处理流程)。

### 10.4 Backfill 游标不动

**检查:**

- 只有 master 节点跑 backfill,当前 master 是否健康
- `quota_data:stream:backfill:lock` 是否被卡住的节点异常占用
- 是否根本没有新 `type=2` log(低谷时段正常)
- 应用错误日志是否有 `query logs failed`

**应急处理:**

- 强制释放锁:`redis-cli DEL quota_data:stream:backfill:lock`(小心,只在确认异常时执行)
- 重启 master 节点

### 10.5 XLEN 主 Stream 长度暴涨

**原因:** 消费速度跟不上生产速度。

**处理:**

- 扩 `QUOTA_DATA_STREAM_CONSUMER_COUNT`
- 提 `QUOTA_DATA_STREAM_BATCH_SIZE`
- 检查 DB 是否是瓶颈
- 极端场景:调高 `QUOTA_DATA_STREAM_MAX_LEN`(注意老消息会被淘汰,靠 Backfill 兜底)

### 10.6 Redis 集群 CROSSSLOT 错误

Lua 脚本涉及多个 KEY 时,Redis Cluster 要求这些 KEY 落在同一 slot。项目里用 hash tag 保证:

```
{user_id:model_name:bucket_ts}
```

如果你在 Lua 里新增 KEY,**必须**沿用同一 hash tag,否则会 CROSSSLOT 报错。

---

## 11. 代码文件索引

| 文件 | 职责 |
|---|---|
| `model/quota_stream.go` | 核心实现:Producer、Consumer、Reclaimer、Backfill、Lua 脚本 |
| `model/usedata.go` | `loadQuotaDataBucketAggFromLogs` / `rewriteQuotaDataBucketExact` —— bucket 聚合与写入 |
| `model/log.go` | Producer 调用点 1、2 —— 文本消费、任务计费 |
| `controller/task_video.go` | Producer 调用点 3、4 —— 视频计费 |
| `main.go:106` | `StartQuotaDataStreamWorkers()` 启动入口 |
| `docs/quota_data_redis_stream_观测手册.md` | 姊妹文档:排查步骤更细的操作手册 |

### 核心函数速查

| 函数 | 位置 | 说明 |
|---|---|---|
| `StartQuotaDataStreamWorkers` | `quota_stream.go:79` | 启动 Consumer/Reclaimer/Backfill 协程 |
| `QueueConsumeLogToQuotaStream` | `quota_stream.go:110` | Producer 入口 |
| `runQuotaDataStreamConsumer` | `quota_stream.go:175` | Consumer 主循环 |
| `processQuotaStreamMessage` | `quota_stream.go:206` | 单消息处理状态机 |
| `applyQuotaStreamEvent` | `quota_stream.go:280` | apply 阶段:等待 log 可见 → SUM → 覆盖桶 |
| `runQuotaDataPendingReclaimer` | `quota_stream.go:358` | Reclaimer 主循环 |
| `runQuotaDataBackfillLoop` | `quota_stream.go:439` | Backfill 主循环(仅 master) |
| `runQuotaDataBackfillOnce` | `quota_stream.go:451` | Backfill 单轮扫描 |
| `moveQuotaStreamMessageToDLQ` | `quota_stream.go:590` | 写入死信队列 |

### Lua 脚本速查

| 脚本 | 位置 | 说明 |
|---|---|---|
| `quotaDataBeginEventProcessingLua` | `quota_stream.go:784` | 消息处理入口:检查 doneKey、抢 bucket 锁、置 dirty |
| `quotaDataCompleteEventProcessingLua` | `quota_stream.go:814` | apply 成功后:写 doneKey、释放/续锁、检查 dirty |
| `quotaDataFinalizeBucketRoundLua` | `quota_stream.go:842` | dirty 重算轮次的 finalize |
| `quotaDataAbortEventProcessingLua` | `quota_stream.go:864` | apply 失败时释放锁 |

---

## 附录 A:名词表

| 名词 | 定义 |
|---|---|
| bucket | 同一 (user_id, model_name, hour_ts) 组合下的 quota_data 行集合 |
| event | 一次 log 派生出的 Stream 消息,唯一标识为 event_id |
| pending | Stream 中已被 delivered 但尚未 ACK 的消息 |
| reclaim | Pending 消息被 Reclaimer 认领 |
| backfill | 补偿扫描,按 log_id 游标从 logs 表补投缺失的事件 |
| DLQ | Dead Letter Queue,死信队列,反复失败的消息终点 |
| dirty flag | bucket 上的脏标志,提示当前持锁 worker 需要再算一轮 |

## 附录 B:变更历史

- 2026-07-01:初版,涵盖架构、五个角色、DLQ、运维命令
