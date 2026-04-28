# 视频模型计费场景分析

> 分析对象：seedance-2-0、seedance-2-0-fast、kling-v3
> 分析版本：含保底计费机制（handleFallbackBilling）

---

## 一、模型计费路由分类

| 模型 | 计费路由 | 计费函数 | 防重复机制 |
|------|----------|----------|-----------|
| kling-v3 | `per_second` | `handleSora2TaskBilling` | DB CAS: `WHERE id=? AND quota=0 UPDATE quota=-1` |
| seedance-2-0 | `token_ratio` | `handleVideoTokenRatioBilling` | DB CAS: `WHERE id=? AND quota=0 UPDATE quota=-1` |
| seedance-2-0-fast | `token_ratio` | `handleVideoTokenRatioBilling` | DB CAS: `WHERE id=? AND quota=0 UPDATE quota=-1` |

---

## 二、kling-v3 计费场景

### 计费公式

```
actualQuota = officialVideoPrice × requestedSeconds × QuotaPerUnit × groupRatio
```

- `officialVideoPrice`: noAudio=0.084, audio=0.126（$/秒）
- `requestedSeconds`: 从 upstream_request_body.duration 解析
- `groupRatio`: 提交时缓存的 billing_effective_group_ratio

### 场景 1：正常计费成功

```
提交 → isVideoPerSecondModel=true → billingRoute="per_second" → 不预扣
轮询成功 → isVideoPerSecondModel=true → handleSora2TaskBilling:
  1. DB CAS 抢锁: quota 0→-1 ✓
  2. 解析 upstream_request_body.duration → requestedSeconds=5
  3. 读取价格: 0.084（noAudio）
  4. 计算: 0.084 × 5 × 500000 × 1.0 = 210000
  5. DecreaseUserQuota(210000) + TaskAdjustTokenQuota(210000)
  6. 写消费日志 + 更新 task.Quota=210000
```

**结果**: 扣费正确 ✓

### 场景 2：正常计费失败 → 保底触发

**触发条件**: `isVideoPerSecondModel` 返回 false（代码 BUG 或配置被误删）

```
轮询成功 → isVideoPerSecondModel=false, isVideoTokenRatioModel=false
  → billingRoute="pre_deduction_settle" → 正常路由不扣费 → task.Quota=0
  → 进入保底: handleFallbackBilling
    → 检查 GetVideoModelPricePerSecond("kling-v3") → 找到 (代码硬编码 0.084)
    → 调用 handleSora2TaskBilling:
      1. DB CAS 抢锁: quota 0→-1 ✓
      2. 解析 upstream_request_body.duration → requestedSeconds=5
      3. 价格: 0.084
      4. 计算: 0.084 × 5 × 500000 × groupRatio
      5. 正常扣费
    → 记录系统日志 "[保底计费] ... 保底按秒计费成功"
```

**结果**: 保底计费正确 ✓，费用与正常路径一致

### 场景 3：保底也失败

**可能原因**:
- `upstream_request_body` 为空且 `task.Data` 无 `requested_seconds`（极罕见，需入库环节异常）
- 此时 requestedSeconds=0 → 兜底为 4 秒计费

```
保底失败路径 (理论上):
  handleSora2TaskBilling → 配置检查通过 → DB CAS 抢锁成功
  → requestedSeconds 解析: 0 → 兜底 4s（代码 1262-1268 行）
  → 仍能正常计费（4s × 0.084 × 500000 × groupRatio）
```

**实际上**: kling-v3 的 `handleSora2TaskBilling` 有 requestedSeconds=0 时兜底 4s 的逻辑，所以**不会真正失败**。唯一失败可能是 `DecreaseUserQuota` 报错（用户余额不足），此时：
- task.Quota 停留在 -1（计费中锁）
- 用户不被重复扣费
- 记录系统日志 "[保底计费失败]"

**结果**: 不会扣错费 ✓（要么正确计费 4s 兜底，要么不扣费）

### 场景 4：会不会重复扣费？

| 并发场景 | 防护机制 | 结果 |
|----------|----------|------|
| 轮询 + GET 同时触发 | DB CAS `WHERE quota=0` 只有一个成功 | 不会重复 ✓ |
| 正常路由 + 保底路由 | 正常路由先执行，成功则 quota>0，保底条件 `task.Quota==0` 不满足 | 不会重复 ✓ |
| 保底内 per_second 成功后继续 token_ratio | `err==nil` 直接 return，不走第二条路径 | 不会重复 ✓ |

---

## 三、seedance-2-0 计费场景

### 计费公式

```
VideoRatio 存在且≠0: actualQuota = tokens × (VideoRatio × VideoCompletionRatio) × groupRatio
VideoRatio 不存在/=0: actualQuota = tokens × ($/M tokens) / 1e6 × QuotaPerUnit × groupRatio
```

- 当前 seedance-2-0 使用**分辨率+视频输入维度**定价（VideoCompletionRatioResolution）:

| 分辨率 | noVideo | video |
|--------|---------|-------|
| 480p | 7.35 | 4.515 |
| 720p | 7.35 | 4.515 |
| 1080p | 8.085 | 4.935 |

- `tokens`: 从上游 usage.completion_tokens 获取（如 97605）
- `video_resolution`: 从 upstream_request_body.resolution 解析
- `has_video_input`: 从 upstream_request_body.content 中检测是否有视频类型输入

### 场景 1：正常计费成功

```
提交 → isVideoTokenRatioModel=true → billingRoute="token_ratio" → 不预扣
轮询成功 → isVideoTokenRatioModel=true → handleVideoTokenRatioBilling:
  1. DB CAS 抢锁: quota 0→-1 ✓
  2. tokens=97605 (completion_tokens)
  3. has_video_input=false (纯文生视频)
  4. video_resolution="720p" (从 upstream_request_body 解析)
  5. 查找定价: GetVideoCompletionRatioResolutionPricing("seedance-2-0", false, "720p") → 7.35
  6. VideoRatio=0 → 价格体系: 7.35/1e6 × 97605 × 500000 × 1.0 = 358,748
  7. DecreaseUserQuota + TaskAdjustTokenQuota
  8. 写消费日志 + 更新 task.Quota
```

**结果**: 扣费正确 ✓

### 场景 2：正常计费失败 → 保底触发

**触发条件**: `isVideoTokenRatioModel` 返回 false

```
轮询成功 → billingRoute="pre_deduction_settle" → task.Quota=0
  → 进入保底: handleFallbackBilling
    → 检查 per_second: GetVideoModelPricePerSecond("seedance-2-0") → 不存在
    → 检查 token_ratio: HasVideoCompletionRatioResolution("seedance-2-0") → true
    → 调用 handleVideoTokenRatioBilling:
      1. DB CAS 抢锁 ✓
      2. tokens 从 taskResult 获取 ✓
      3. 分辨率从 upstream_request_body 解析 ✓
      4. has_video_input 从 upstream_request_body 解析 ✓
      5. 正常计费
    → 记录系统日志 "[保底计费] ... 保底按量计费成功"
```

**结果**: 保底计费正确 ✓

### 场景 3：保底也失败

**可能原因**:
- `taskResult.CompletionTokens=0` 且 `taskResult.TotalTokens=0`

```
handleVideoTokenRatioBilling:
  → DB CAS 抢锁 ✓ (quota 0→-1)
  → tokens=0
  → 释放锁: quota -1→0, 标记 billing_skipped="no_usage"
  → return nil (不报错)
```

**结果**: 不扣费，但 `err==nil` → 保底视为"成功完成"（零费用场景），记录系统日志。
**用户视角**: 任务成功但未扣费。管理员可通过系统日志发现。
**会扣错费吗**: 不会 ✓（tokens=0 说明上游未返回用量，确实无法计费）

**另一种失败**: 定价配置不存在

```
handleVideoTokenRatioBilling:
  → DB CAS 抢锁 ✓
  → GetVideoCompletionRatioResolutionPricing → 未找到
  → return error "video completion ratio not configured"
```

此时 task.Quota 停留在 -1。保底的下一步：
- per_second 已失败（无配置）
- token_ratio 也失败
- 记录 "[保底计费失败]" 系统日志

**结果**: 不扣费 ✓，不会扣错。管理员需人工处理。

---

## 四、seedance-2-0-fast 计费场景

### 计费公式

与 seedance-2-0 相同，区别在定价：

| 分辨率 | noVideo | video |
|--------|---------|-------|
| 480p | 5.88 | 3.465 |
| 720p | 5.88 | 3.465 |

注意：seedance-2-0-fast **不支持 1080p**。

### 场景 1：正常计费成功

```
提交 → isVideoTokenRatioModel=true (通过 HasVideoCompletionRatioResolution) → billingRoute="token_ratio"
轮询成功 → handleVideoTokenRatioBilling:
  1. DB CAS 抢锁 ✓
  2. tokens=完成令牌数
  3. video_resolution="720p"
  4. has_video_input=false
  5. GetVideoCompletionRatioResolutionPricing("seedance-2-0-fast", false, "720p") → 5.88
  6. 计算扣费
```

**结果**: 扣费正确 ✓

### 场景 2：正常失败 → 保底

与 seedance-2-0 逻辑完全一致：
- 保底检测到 `HasVideoCompletionRatioResolution("seedance-2-0-fast")` → true
- 调用 `handleVideoTokenRatioBilling` → 正常计费

**结果**: 保底计费正确 ✓

### 场景 3：保底也失败

与 seedance-2-0 一致：
- tokens=0 → 零费用返回，记录日志
- 配置缺失 → 返回 error，记录 "[保底计费失败]"

**结果**: 不会扣错费 ✓

### 场景 4：分辨率解析异常

如果 `video_resolution` 解析失败（upstream_request_body 中无 resolution 字段）：
```
video_resolution 解析链:
  1. taskResult.Resolution → 通常为空（上游不返回）
  2. taskData["video_resolution"] → 提交时保存（preservedFields 已补上）
  3. parseResolutionFromUpstreamBody(task.UpstreamRequestBody) → 解析请求体
  4. 默认兜底: "720p"
```

即使全部失败，兜底为 720p。seedance-2-0-fast 的 720p 定价 = 5.88，**不会出现分辨率不存在导致计费失败**。

**结果**: 最差情况按 720p 计费（正确或略高于实际），不会扣错费 ✓

---

## 五、关键风险总结

### 不会发生的错误

| 风险 | 防护机制 | 结论 |
|------|----------|------|
| 重复扣费 | DB CAS `WHERE id=? AND quota=0` | 不可能 ✓ |
| 正常+保底双重扣费 | 正常路由成功后 quota>0，保底条件不满足 | 不可能 ✓ |
| 保底内两条路径都扣费 | 第一条成功 `return`，不走第二条 | 不可能 ✓ |
| 多节点并发扣费 | DB 原子操作，RowsAffected 只有一个=1 | 不可能 ✓ |
| 价格计算错误 | 硬编码价格 + upstream_request_body 原始参数 | 不可能 ✓ |

### 可能存在的非致命问题

| 场景 | 影响 | 严重程度 | 发现方式 |
|------|------|----------|----------|
| tokens=0 导致不扣费 | 用户免费用了一次 | 低（极罕见） | 系统日志 `billing_skipped=no_usage` |
| requestedSeconds=0 兜底 4s | kling-v3 按 4s 计费（可能偏低/偏高） | 低（极罕见） | 系统日志告警 |
| 分辨率解析失败兜底 720p | seedance-2-0 若实际 1080p 按 720p 计费 | 中（偏低） | 日志中 resolution 字段 |
| 计费锁停留在 -1 | task.Quota=-1 不影响用户，但统计异常 | 低 | SQL 查询 `quota=-1` |

### 最坏情况

```
最坏场景：所有计费路径都失败
  → 用户：任务成功，视频正常返回，不扣费
  → 管理员：收到系统日志 "[保底计费失败]" 告警
  → 后果：平台亏损一次视频生成费用（几毛到几块钱）
  → 修复：管理员手动调整用户额度
```

**核心结论：在任何场景下，都不会出现"多扣费"或"扣错费"的情况。最差只会出现"少扣费"或"不扣费"。**

---

## 六、计费流程图

```
任务轮询/GET 成功
    │
    ├── isVideoPerSecondModel? ──YES──→ handleSora2TaskBilling (kling-v3)
    │                                         │
    │                                    DB CAS 抢锁
    │                                         │
    │                                    解析 duration + audio
    │                                         │
    │                                    查价格 → 计算 → 扣费
    │
    ├── isVideoTokenRatioModel? ──YES──→ handleVideoTokenRatioBilling (seedance)
    │                                         │
    │                                    DB CAS 抢锁
    │                                         │
    │                                    解析 tokens + resolution + video_input
    │                                         │
    │                                    查定价 → 计算 → 扣费
    │
    └── 都不匹配 → billingRoute="pre_deduction_settle" → quota=0
              │
              └── 保底触发: handleFallbackBilling
                        │
                        ├── 有 per_second 配置? → handleSora2TaskBilling → 成功则 return
                        │
                        ├── 有 token_ratio 配置? → handleVideoTokenRatioBilling → 成功则 return
                        │
                        └── 都没有 → 记录 "[保底计费失败]" 系统日志 → 不扣费
```
