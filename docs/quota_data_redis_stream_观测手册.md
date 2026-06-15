# quota_data Redis Stream 观测手册

本文档用于排查 `type=2` 消费日志通过 Redis Stream 异步更新 `quota_data` 的运行状态，覆盖本地调试、线上观测、常见故障与恢复动作。

当前实现里，幂等主键优先使用应用侧预生成的 `event_id`；`log_id` 主要用于辅助排查和历史兼容。

## 1. 开关与参数

打开新链路：

```bash
QUOTA_DATA_STREAM_ENABLED=true
```

常用参数：

- `QUOTA_DATA_STREAM_CONSUMER_COUNT`：单实例消费者协程数。
- `QUOTA_DATA_STREAM_BATCH_SIZE`：单次从 Stream 拉取的消息条数。
- `QUOTA_DATA_STREAM_BLOCK_MS`：消费者阻塞轮询等待时间，单位毫秒。
- `QUOTA_DATA_STREAM_PENDING_IDLE_MS`：Pending 消息超时后允许被重领的最小空闲时间，单位毫秒。
- `QUOTA_DATA_STREAM_RETRY_LIMIT`：单条消息最大重试次数，超过后进入 DLQ。
- `QUOTA_DATA_STREAM_BACKFILL_BATCH`：补偿扫描每批处理的日志条数。
- `QUOTA_DATA_STREAM_BACKFILL_INTERVAL_SECONDS`：补偿扫描执行间隔，单位秒。
- `QUOTA_DATA_STREAM_DONE_TTL_HOURS`：幂等完成标记的保留时长，单位小时。
- `QUOTA_DATA_STREAM_MAX_LEN`：Redis Stream 近似最大长度。

## 2. 涉及的 Redis Key / Stream

- 主 Stream：`stream:quota_data:v1`
- Consumer Group：`quota_data_workers`
- DLQ：`stream:quota_data:dlq:v1`
- 补偿扫描锁：`quota_data:stream:backfill:lock`
- 补偿扫描游标：`quota_data:stream:last_scan_log_id`
- 幂等完成标记：`quota_data:stream:done:{event_id}`
- 单消息处理锁：`quota_data:stream:lock:{event_id}`

## 3. 控制台日志说明

启动后常见日志前缀：

- `[QuotaStream][Producer]`
  生产端把消费日志写入 Stream。
- `[QuotaStream][Consumer]`
  消费端读取消息、更新 `quota_data`、ACK。
- `[QuotaStream][Reclaimer]`
  Pending 恢复器，负责认领超时未 ACK 的消息。
- `[QuotaStream][Backfill]`
  补偿扫描器，负责从 `logs` 重新补投漏掉的消费日志。
- `[QuotaStream][DLQ]`
  写入死信队列时的日志。

重点观察：

- `enqueue ok`
- `apply ok`
- `ack ok`
- `enqueue failed`
- `apply failed`
- `done mark failed`
- `move to dlq`

## 4. 本地观测

### 4.1 看应用日志

如果本地直接跑服务：

```bash
QUOTA_DATA_STREAM_ENABLED=true ./new-api
```

观察控制台中是否出现：

```text
[QuotaStream] stream workers enabled
[QuotaStream][Consumer] start consumer=...
[QuotaStream][Reclaimer] start consumer=...
[QuotaStream][Backfill] start leader loop
```

### 4.2 看 Stream 是否在增长

```bash
redis-cli XLEN stream:quota_data:v1
```

如果持续有消费日志写入，但长度始终是 `0` 或极小，需要结合应用日志看生产端是否有：

- `enqueue ok`
- `enqueue failed`

### 4.3 查看 Stream 最新消息

```bash
redis-cli XREVRANGE stream:quota_data:v1 + - COUNT 5
```

重点看字段：

- `event_id`
- `log_id`
- `user_id`
- `model_name`
- `quota`
- `token_used`
- `bucket_ts`
- `request_id`
- `event_source`

### 4.4 查看 Consumer Group 状态

```bash
redis-cli XINFO GROUPS stream:quota_data:v1
redis-cli XINFO CONSUMERS stream:quota_data:v1 quota_data_workers
```

关注字段：

- `consumers`
- `pending`
- `last-delivered-id`
- `idle`

### 4.5 查看 Pending 消息

```bash
redis-cli XPENDING stream:quota_data:v1 quota_data_workers
redis-cli XPENDING stream:quota_data:v1 quota_data_workers - + 20
```

如果 Pending 持续增长，通常意味着：

- 消费端 `applyQuotaStreamEvent(...)` 失败
- `done` 标记写入 Redis 失败
- 消费端实例异常退出，尚未被重领

### 4.6 查看 DLQ

```bash
redis-cli XLEN stream:quota_data:dlq:v1
redis-cli XREVRANGE stream:quota_data:dlq:v1 + - COUNT 10
```

重点看：

- `failed_message_id`
- `failed_consumer`
- `failed_reason`
- `retry_count`

## 5. 线上观测

### 5.1 核对补偿扫描是否在推进

```bash
redis-cli GET quota_data:stream:last_scan_log_id
```

如果线上持续有新的 `type=2` 日志，这个游标应当持续增长。

若长时间不变，优先排查：

- 主节点是否在跑补偿扫描器
- `quota_data:stream:backfill:lock` 是否被异常占用
- 应用日志里是否有 `[QuotaStream][Backfill] acquire lock failed`

### 5.2 看补偿锁

```bash
redis-cli GET quota_data:stream:backfill:lock
redis-cli TTL quota_data:stream:backfill:lock
```

正常现象：

- 有值：说明当前有某个实例在执行补偿扫描
- TTL 持续变化：说明锁在按周期轮换

异常现象：

- TTL 很长但游标不推进
- 锁一直是同一个异常节点且应用日志无变化

### 5.3 核对 `logs` 与 `quota_data`

建议按小时抽样核对：

```sql
SELECT
    (created_at DIV 3600) * 3600 AS hour_ts,
    SUM(quota) AS logs_quota
FROM logs
WHERE type = 2
  AND created_at >= ? AND created_at < ?
GROUP BY hour_ts
ORDER BY hour_ts;
```

```sql
SELECT
    created_at AS hour_ts,
    SUM(quota) AS quota_data_quota
FROM quota_data
WHERE created_at >= ? AND created_at < ?
GROUP BY created_at
ORDER BY created_at;
```

判断口径：

- 实时短时间内有小量差值是允许的，因为 Stream 是异步派生
- 如果补偿器正常推进，差值应当逐步收敛
- 如果差值持续扩大，优先看 Producer / Consumer / Pending / DLQ

## 6. 常见故障与排查顺序

### 6.1 `logs` 有，`quota_data` 没增长

排查顺序：

1. 看应用日志是否有 `[QuotaStream][Producer] enqueue ok`
2. 看 `XLEN stream:quota_data:v1`
3. 看 `XINFO GROUPS stream:quota_data:v1`
4. 看 `XPENDING stream:quota_data:v1 quota_data_workers`
5. 看补偿游标 `GET quota_data:stream:last_scan_log_id`

常见原因：

- Redis 不可用，生产端 `enqueue failed`
- 消费端更新 `quota_data` 失败
- done 标记写入失败，消息一直 Pending
- 补偿器没跑起来

### 6.2 Pending 越堆越多

重点排查：

- 应用日志中的 `apply failed`
- 应用日志中的 `done mark failed`
- 数据库连接是否异常
- Redis 是否出现高延迟或写失败

### 6.3 DLQ 开始出现消息

说明某些消息反复失败，超过了 `QUOTA_DATA_STREAM_RETRY_LIMIT`。

处理方式：

1. 先用 `XREVRANGE stream:quota_data:dlq:v1 + - COUNT 20` 看失败原因
2. 修复根因
3. 视情况把 DLQ 里的消息重新投回主 Stream，或人工按 `log_id` 回放

### 6.4 补偿游标不动

检查：

- 是否只有主节点在跑补偿任务
- `quota_data:stream:backfill:lock` 是否异常
- 是否没有新的 `type=2` 日志
- 应用日志里是否存在 `[QuotaStream][Backfill] query logs failed`

## 7. 手工恢复动作

### 7.1 仅重启消费者

适用场景：

- 生产正常
- Stream 有消息
- 消费者协程异常或实例卡死

动作：

- 重启应用实例
- 观察是否出现 `[QuotaStream][Consumer] start consumer=...`
- 观察 Pending 是否开始下降

### 7.2 依靠 Pending 恢复

适用场景：

- 某实例已经挂掉
- 但消息仍在 Pending

动作：

- 等待 `QUOTA_DATA_STREAM_PENDING_IDLE_MS` 超时
- 观察日志中 `Reclaimer` 是否开始重新处理

### 7.3 依靠补偿扫描最终补齐

适用场景：

- 某些 `CreateLog` 成功，但实时 `XADD` 失败

动作：

- 不需要立即人工干预
- 观察补偿游标是否继续推进
- 再抽样校对 `logs` 与 `quota_data`

## 8. 运行建议

- 新链路灰度时，先从低流量环境开启：
  - `QUOTA_DATA_STREAM_ENABLED=true`
  - `QUOTA_DATA_STREAM_CONSUMER_COUNT=1`
  - `QUOTA_DATA_STREAM_BATCH_SIZE=50`
- 观察 1 到 2 天后，再逐步提高并发与批次。
- 如果线上模型调用量较大，优先观察：
  - Stream 长度
  - Pending 数量
  - DLQ 数量
  - 补偿游标推进速度

## 9. 当前实现边界说明

当前实现遵循“不新增表”的约束，因此：

- 可做到最终一致性
- 可做到多实例下业务上基本不重复累计
- 可通过补偿扫描修复实时投递失败

但仍需明确：

- Redis 幂等标记与数据库更新不是单事务
- 理论上仍存在极小概率的重复累计窗口
- 因此这套方案是“高可用最终一致”，不是严格财务级 exactly-once
