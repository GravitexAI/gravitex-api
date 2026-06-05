# ByteHouse 迁移开发计划

> 编写日期：2026-06-05
> 基于：`bytehouse-feasibility-report.md` 方案 B（ClickHouse 原生驱动）
> 范围：Go 后端 `logs` 表迁移至 ByteHouse，Java 后端适配

---

## ⚠️ 零、基于真实代码核实的修正与待决策（2026-06-05 增补）

> 本节是对原计划的事实核对结果。原文多处标注"约 line XXX""需确认"，说明编写时未完全核对代码。
> 下面 A 类问题（事实性错误）已在对应章节就地修正；B 类问题（高风险决策）**必须先由负责人拍板，再进入开发**，不可盲目并发。

### A. 已核实的事实性错误（影响实现，已就地修正相关章节）

| # | 原文说法 | 真实情况（核实依据） | 处理 |
|---|---------|--------------------|------|
| A1 | M3.1：`SumUsedToken()` 用 `ifnull(sum(quota),0)` | **错**。`SumUsedToken`(log.go:578) 聚合的是 `ifnull(sum(prompt_tokens),0)+ifnull(sum(completion_tokens),0)`（token 数，非 quota）。真正按 `sum(quota)` 聚合的是文档**漏掉**的 `SumUsedQuota`(log.go:522)，且它写的是 `sum(quota) quota`，**没有 ifnull 包裹** | 已重写 M3.1 |
| A2 | M3.1/M3.2 把 quota 聚合当成主要兼容点 | `SumUsedQuota` 还内含第二条 raw SQL `sum(prompt_tokens)+sum(completion_tokens) tpm`、`count(*) rpm`，并用 `logGroupCol` 拼列名、`LIKE ? ESCAPE '!'`。这些都要一并验证 ClickHouse 兼容性 | 已在 M3.1 补充 |
| A3 | `chooseDB(envName string, isLogDB bool)` | 真实签名是 `chooseDB(envName string, isLog bool)`（main.go:118），参数名不同；`DeleteOldLog` 真实签名是 `DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int)`，文档漏了 `ctx`，且内部是 **for 循环分批删** | 见 M1/M3.2 |
| A4 | 待删的 `task_id`：Go 未用但保留 | Go `Log` struct **根本没有 `task_id` 字段**，只有 `RequestId`（log.go:38）。Java `Logs.java` 有 `taskId`(126) 但所有 SQL 都不读写它 | 建表 DDL 是否保留 `task_id` 见 B4 |
| A5 | S1：删 `oem_id`/`oem_subsidy`，"Go/Java 均未使用" | Go struct 和 Java 实体类里**都不存在**这两个字段，代码零引用。也就是说"删字段"对代码是 no-op；只有 MySQL 物理表里可能还有列+索引（需 DBA 在真实库确认是否真存在） | 见 B3 |
| A6 | S2：5 个 quota 字段 Java "20+ 处引用" | 实际 **64 处**：40 处 getter（BillingDetailService 24、LogsController 6、BillingReportServiceImpl 5、EnterpriseLogController 1…）+ 24 处 setter（UserInvoiceServiceImpl 10、BillingDetailService 5、PricingCalculationService 4、BillingReportServiceImpl 5）。**注意有 24 处 setter = 有代码在写这些字段**（发票/定价计算），不是单纯只读 | 见 B3 |
| A7 | M7：只需内存化 `channels` + `t_enterprise_user` | `t_enterprise_user` 被 **8 个查询**方法 JOIN（企业日志列表/分页/keysetcount/maxId 等全依赖），`channels` 1 处。内存化方案必须覆盖全部 8 处，否则企业日志功能直接坏 | 已在 M7 补充清单 |
| A8 | `CreateLog` 只是 `LOG_DB.Create(log)` | `CreateLog`(log.go:68) 还含日志打印；写日志的真正入口是 `RecordConsumeLog`(266)、`RecordErrorLog`(200)、`RecordTaskBillingLog`(354)。批量写入改造要在 `CreateLog` 这一层做才能覆盖全部入口（方向对，但需确认这三个入口都最终走 `CreateLog`） | 见 M2 |
| A9 | M2 只改 `CreateLog` 即可覆盖全部日志写入 | **不够**。`RecordTaskBillingLog`(log.go:379) **绕过 `CreateLog`**，直接 `LOG_DB.Create(log)`。批量写入拦截点不唯一，M2 必须让它也走统一入口 | 已在 M2 补充 |
| A10 | （删字段数据依据） | `cost_quota`/`system_quota`/`user_quota`/`platform_profit` 这 4 个不仅 DB 列全 0，**Go 端 `other` JSON 也根本没写这 4 个 key**（只写了 `official_quota`，RecordConsumeLog:295）。即它们是无数据源的死字段，Java 现状读出来就是 0/null，删除对账单数字无影响 | 见 B3/S2 |

### B. 高风险决策点 —— 已于 2026-06-05 拍板

- **B1 ✅ Java 也写 logs，但只 INSERT 不 UPDATE。**
  已核实 Java 写 logs 的 3 个入口：`UserAmountController.insertAmountChangeLog`（充值/管理日志 type=1/3）、`UsersServiceImpl.allocateQuota`（团队配额 from/to 日志），**均只 set 标准字段（userId/type/quota/content/username/createdAt/modelName），不碰任何待删字段**。
  **决策：** ① `LogsMapper` 整体加 `@DS("clickhouse")`，`insertByBo` 也写 ByteHouse（充值/管理日志频率低，单条 INSERT 可接受）；② 脚手架的 `updateByBo`/`deleteWithValidByIds` 禁用（不再支持手改/删日志）。

- **B2 ✅ 不双写，直接切 ByteHouse。**
  MySQL 历史数据由负责人手动迁移到 ByteHouse。M2 的 `LogBuffer` 单写 ByteHouse 即可，无需双写过渡；回滚靠改 `LOG_SQL_DSN` 回 MySQL。失败策略仍建议「告警 + 落本地文件兜底」，但不强制双写。

- **B3 ✅ 5 个 quota 字段 + oem + task_id 一起删，ByteHouse 不建这些列。**
  安全性已核实（见 A6/A10）：4 个字段是无数据源死字段（删后保持 0，行为不变）；`official_quota` 走 `other` JSON（已有值，Java 改统一读 JSON，数值不变）。
  **需做：** Java `Logs.java` 删 5 个字段 + 重构 64 处引用（40 getter / 24 setter）。getter：`officialQuota` 改读 `other` JSON；其余 4 个直接视为 0 或移除展示。setter（写内存 VO/计算结构，非写 DB）：去掉对已删字段的赋值。

- **B4 ✅ 不保留 `task_id`。** ByteHouse 建表 DDL 删除 `task_id` 列（M4 已更新）。

### C. 开发节奏与并发计划（B 已拍板，进入开发）

真正能安全并行的是两条独立代码线 + 一份 DDL：

- **线 1（Go，独立目录 `gravitex-api/`）** —— 顺序做（都改 `model/`，不可内部并发）：
  M1 驱动接入 → M2 批量写入（含 A9：`RecordTaskBillingLog` 也走统一拦截点）→ M3 SQL 兼容（`ifnull`→`COALESCE`、`LIKE ESCAPE`、`ORDER BY id`→`created_at`、`DeleteOldLog`）。
- **线 2（Java，独立目录 `Gravitex-API-End/`）** —— 顺序做（都改 `gravitex-api` 模块）：
  S2 删字段重构 → M5 双数据源 + `@DS("clickhouse")`（含禁用 update/delete）→ M6 SQL 适配（`JSON_VALUE`→`JSONExtract`/`simpleJSONExtract`、`ifnull`→`COALESCE`）→ M7 JOIN 内存化（覆盖 8 处 `t_enterprise_user` + 1 处 `channels`）。
- **线 3（DDL）** —— 独立产出 `M4` 建表脚本（不含 oem/quota/task_id），交负责人在 ByteHouse 执行。

线 1 ‖ 线 2 ‖ 线 3 三者并行；线内串行。`M8 联调`最后串（依赖 ByteHouse 实例 + 连接凭证，需负责人提供）。

---

## 一、开发任务总览

### 主任务

| # | 任务模块 | 说明 | 预估工时 |
|---|---------|------|---------|
| M1 | Go 后端 — ClickHouse 驱动集成 | 添加 `gorm.io/driver/clickhouse`，`chooseDB()` 新增 `clickhouse://` 分支 | 4-6h |
| M2 | Go 后端 — 批量写入改造 | 单条 INSERT 改为内存缓冲 + 批量 flush | 6-8h |
| M3 | Go 后端 — SQL 兼容性修复 | `ifnull` → `COALESCE`，`DeleteOldLog` 适配，AutoMigrate 替换 | 3-4h |
| M4 | ByteHouse — 建库建表 | 建 `logs` 表，CnchMergeTree 引擎，永久 TTL（不自动删除） | 1h |
| M5 | Java 后端 — 双数据源配置 | 添加 ClickHouse 数据源，统计查询切到 ByteHouse | 4-6h |
| M6 | Java 后端 — SQL 语法适配 | `JSON_VALUE` → ClickHouse JSON 函数，聚合函数适配 | 3-4h |
| M7 | Java 后端 — 跨表 JOIN 内存化 | `channels`、`t_enterprise_user` 数据加载到内存，应用层关联 | 6-8h |
| M8 | 联调测试 | Go 写入 → ByteHouse → Java 读取全链路验证 | 4-6h |

### 次任务（字段清理）

| # | 任务 | 说明 | 预估工时 |
|---|------|------|---------|
| S1 | 删除 `oem_id`、`oem_subsidy` 字段 | Go/Java 均未使用，连同索引一起删除 | 0.5h |
| S2 | 清理 `official_quota`、`cost_quota`、`system_quota`、`user_quota`、`platform_profit` | Java 有引用但 DB 值全为 0，需重构 Java 代码改为从 `other` JSON 读取 | 4-6h |

### 总预估

| 阶段 | 工时 |
|------|------|
| 主任务（M1-M8） | 30-42h（约 4-6 个工作日） |
| 次任务（S1-S2） | 4.5-6.5h（约 1 个工作日） |
| **合计** | **约 5-7 个工作日** |

---

## 二、主任务开发步骤

### M1：Go 后端 — ClickHouse 驱动集成

#### 步骤 1.1：添加依赖

```bash
cd gravitex-api
go get gorm.io/driver/clickhouse
```

#### 步骤 1.2：添加数据库类型常量

**文件：** `common/database.go`

```go
// 新增常量
const DatabaseTypeClickHouse = "clickhouse"
```

当前代码：
```go
const (
    DatabaseTypeMySQL      = "mysql"
    DatabaseTypeSQLite     = "sqlite"
    DatabaseTypePostgreSQL = "postgres"
)
var UsingClickHouse = false  // 已预留，未使用
```

改动：无需新增常量（`UsingClickHouse` 已存在），只需在初始化时设置它为 `true`。

#### 步骤 1.3：`chooseDB()` 添加 ClickHouse 分支

**文件：** `model/main.go`（`chooseDB()` 函数，约 line 118）

当前逻辑：
```go
func chooseDB(envName string, isLogDB bool) *gorm.DB {
    dsn := os.Getenv(envName)
    if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
        // PostgreSQL
    } else if strings.HasPrefix(dsn, "local") {
        // SQLite
    } else {
        // MySQL（默认）
    }
}
```

新增分支：
```go
func chooseDB(envName string, isLogDB bool) *gorm.DB {
    dsn := os.Getenv(envName)
    if strings.HasPrefix(dsn, "clickhouse://") {
        // ClickHouse / ByteHouse 原生协议
        db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{
            PrepareStmt: false,  // ClickHouse 不支持 prepared statements
        })
        if err != nil {
            common.SysLog("failed to connect to ClickHouse: " + err.Error())
            os.Exit(1)
        }
        if isLogDB {
            common.UsingClickHouse = true
            common.LogSqlType = DatabaseTypeClickHouse
        }
        return db
    }
    // ... 原有分支不变
}
```

需要在文件顶部添加 import：
```go
import "gorm.io/driver/clickhouse"
```

#### 步骤 1.4：`initCol()` 添加 ClickHouse 列名引用格式

**文件：** `model/main.go`（`initCol()` 函数，约 line 28）

ClickHouse 使用反引号 `` ` `` 引用列名（与 MySQL 相同），所以当前 MySQL 分支的逻辑可以复用。但需要确认 `common.UsingClickHouse` 时设置正确的引用格式：

```go
func initCol() {
    if common.UsingPostgreSQL {
        commonGroupCol = `"group"`
        commonKeyCol = `"key"`
    } else {
        // MySQL / SQLite / ClickHouse 都用反引号
        commonGroupCol = "`group`"
        commonKeyCol = "`key`"
    }
}
```

> 如果当前 `initCol()` 只有 `UsingPostgreSQL` 分支和 else 分支，则无需改动。

#### 步骤 1.5：`InitLogDB()` 跳过 ClickHouse 不兼容的初始化

**文件：** `model/main.go`（`InitLogDB()` 函数，约 line 217）

需要跳过以下 ClickHouse 不兼容的操作：
- `checkMySQLChineseSupport()` — 查询 `information_schema`，ClickHouse 不支持
- `migrateLogRequestIdColumnLength()` — `ALTER COLUMN` 语法不同

```go
func InitLogDB() {
    // ... 现有逻辑
    
    if common.UsingClickHouse {
        common.SysLog("Log DB: using ClickHouse, skip MySQL-specific initialization")
        // 跳过 checkMySQLChineseSupport、migrateLogRequestIdColumnLength
        return
    }
    
    // 原有 MySQL/PostgreSQL/SQLite 初始化
    checkMySQLChineseSupport()
    migrateLogRequestIdColumnLength()
}
```

#### 步骤 1.6：`migrateLOGDB()` 适配 ClickHouse

**文件：** `model/main.go`（`migrateLOGDB()` 函数，约 line 384）

GORM ClickHouse 驱动的 AutoMigrate 支持有限。推荐方案：**ClickHouse 时跳过 AutoMigrate，改为手动建表**。

```go
func migrateLOGDB() {
    if common.UsingClickHouse {
        common.SysLog("ClickHouse log DB: skip AutoMigrate, use manual DDL")
        return
    }
    // 原有 AutoMigrate 逻辑
    err := LOG_DB.AutoMigrate(&Log{})
    // ...
}
```

手动建表 DDL 见 M4 步骤。

---

### M2：Go 后端 — 批量写入改造

> **核心改动**：每次 API 调用不再立即 INSERT 一条日志，而是写入内存 Buffer，定时/满量批量 flush 到 ByteHouse。

#### 步骤 2.1：创建 LogBuffer 组件

**新文件：** `model/log_buffer.go`

```go
package model

import (
    "fmt"
    "sync"
    "time"
    
    "gorm.io/gorm"
    "your-project/common"
)

// LogBuffer 日志批量写入缓冲区
type LogBuffer struct {
    mu       sync.Mutex
    buffer   []*Log
    maxSize  int           // 批量阈值，推荐 5000
    interval time.Duration // 刷新间隔，推荐 3-5 秒
    ticker   *time.Ticker
    done     chan struct{}
    db       *gorm.DB
}

// NewLogBuffer 创建日志缓冲区并启动后台 flush 协程
func NewLogBuffer(db *gorm.DB, maxSize int, interval time.Duration) *LogBuffer {
    lb := &LogBuffer{
        buffer:   make([]*Log, 0, maxSize),
        maxSize:  maxSize,
        interval: interval,
        ticker:   time.NewTicker(interval),
        done:     make(chan struct{}),
        db:       db,
    }
    go lb.flushLoop()
    return lb
}

// Add 添加一条日志到缓冲区
func (lb *LogBuffer) Add(log *Log) {
    lb.mu.Lock()
    lb.buffer = append(lb.buffer, log)
    shouldFlush := len(lb.buffer) >= lb.maxSize
    lb.mu.Unlock()
    if shouldFlush {
        lb.Flush()
    }
}

func (lb *LogBuffer) flushLoop() {
    for {
        select {
        case <-lb.ticker.C:
            lb.Flush()
        case <-lb.done:
            lb.Flush() // 退出前最后一次 flush
            return
        }
    }
}

// Flush 将缓冲区数据批量写入 ByteHouse
func (lb *LogBuffer) Flush() {
    lb.mu.Lock()
    if len(lb.buffer) == 0 {
        lb.mu.Unlock()
        return
    }
    batch := lb.buffer
    lb.buffer = make([]*Log, 0, lb.maxSize)
    lb.mu.Unlock()

    if err := lb.db.CreateInBatches(batch, len(batch)).Error; err != nil {
        common.SysLog(fmt.Sprintf("ByteHouse batch insert failed: %v, %d logs lost", err, len(batch)))
        // 可选：降级写 MySQL 或持久化到本地文件
    }
}

// Close 关闭缓冲区（优雅停机时调用）
func (lb *LogBuffer) Close() {
    lb.ticker.Stop()
    close(lb.done)
}
```

#### 步骤 2.2：修改 CreateLog 调用方式

**文件：** `model/log.go`

当前：
```go
func CreateLog(log *Log) {
    LOG_DB.Create(log)
}
```

改为：
```go
// 全局 LogBuffer 实例（在 InitLogDB 中初始化）
var GlobalLogBuffer *LogBuffer

func CreateLog(log *Log) {
    if common.UsingClickHouse && GlobalLogBuffer != nil {
        GlobalLogBuffer.Add(log)
    } else {
        LOG_DB.Create(log)
    }
}
```

#### 步骤 2.3：在 InitLogDB 中初始化 LogBuffer

**文件：** `model/main.go`

```go
func InitLogDB() {
    // ... 初始化 LOG_DB 连接
    
    if common.UsingClickHouse {
        // 初始化批量写入缓冲区
        GlobalLogBuffer = model.NewLogBuffer(LOG_DB, 5000, 5*time.Second)
        common.SysLog("ClickHouse LogBuffer initialized: maxSize=5000, interval=5s")
    }
}
```

#### 步骤 2.4：优雅停机时 flush

在程序退出时（`main.go` 或 graceful shutdown 逻辑中）调用：
```go
if model.GlobalLogBuffer != nil {
    model.GlobalLogBuffer.Close()
}
```

#### 步骤 2.5：关键参数配置

| 参数 | 推荐值 | 说明 |
|------|--------|------|
| `maxSize` | 5,000 条 | ClickHouse 推荐批量 1,000-100,000 |
| `interval` | 3-5 秒 | 平衡写入延迟和吞吐 |
| 内存上限 | 50,000 条 | 防止 OOM（可在 Add 中检查） |
| 失败策略 | 日志告警 + 丢弃 | 或降级写 MySQL（需维护双连接） |

---

### M3：Go 后端 — SQL 兼容性修复

#### 步骤 3.1：raw SQL 聚合兼容（已按真实代码重写）

> 原文此处把 `SumUsedToken` 写成 `ifnull(sum(quota),0)` 是错的（见 A1）。真实情况是两个不同函数：

**① `SumUsedQuota()`（log.go:522）—— 真正按 quota 聚合，当前无 ifnull**

真实代码：
```go
tx := LOG_DB.Table("logs").Select("sum(quota) quota")
// 第二条 raw SQL：
rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")
// 还用到：logGroupCol+" = ?"、model_name LIKE ? ESCAPE '!'
```
ClickHouse 兼容性：`sum()`/`count(*)` 兼容；`logGroupCol`（反引号引 `group`）与 MySQL 一致，OK；
**需重点验证 `LIKE ? ESCAPE '!'`** —— ClickHouse 的 `LIKE` 不支持自定义 `ESCAPE` 字符，此处可能需要按 `common.UsingClickHouse` 改写转义逻辑（用 `sanitizeLikePattern` 配合 ClickHouse 默认 `\` 转义）。

**② `SumUsedToken()`（log.go:578）—— 按 token 聚合，当前已用 ifnull**

真实代码：
```go
tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
```
ClickHouse **不认识小写 `ifnull`**（它的函数是驼峰 `ifNull` 或 `coalesce`）。

**统一改造方向（同时满足 MySQL/PG/SQLite/ClickHouse 四库，符合项目 Rule 2）：**
```go
// 把所有 ifnull(x,0) 统一改为 COALESCE(x,0)，四库都支持
// SumUsedToken：
Select("COALESCE(sum(prompt_tokens),0) + COALESCE(sum(completion_tokens),0)")
// SumUsedQuota：sum(quota) 本就无 ifnull，ClickHouse 下 NULL 会变 0，建议显式 COALESCE(sum(quota),0) 保持语义一致
```
> ⚠️ 改完必须在 MySQL 上回归一次，确认聚合结果与改造前一致——这是计费报表数字，不能错。

#### 步骤 3.2：`DeleteOldLog()` 适配

**文件：** `model/log.go`（`DeleteOldLog()` 函数，约 line 599）

当前：
```go
func DeleteOldLog(targetTimestamp int64, limit int) (int64, error) {
    result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
    return result.RowsAffected, result.Error
}
```

ClickHouse 适配：
```go
func DeleteOldLog(targetTimestamp int64, limit int) (int64, error) {
    if common.UsingClickHouse {
        // ClickHouse lightweight delete（异步执行）
        // 注意：不支持 LIMIT，一次性删除所有符合条件的数据
        sql := fmt.Sprintf("ALTER TABLE logs DELETE WHERE created_at < %d", targetTimestamp)
        result := LOG_DB.Exec(sql)
        return 0, result.Error  // ClickHouse 异步删除，无法返回准确的 RowsAffected
    }
    result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
    return result.RowsAffected, result.Error
}
```

> **注意**：由于用户要求 TTL 永久不删，`DeleteOldLog()` 实际不会被调用。但代码仍需保留兼容分支，以防未来策略变更。

#### 步骤 3.3：布尔值映射

ClickHouse 中布尔值用 `UInt8`（0/1），与 MySQL 一致。GORM ClickHouse 驱动会自动处理 `bool` ↔ `UInt8` 映射。确认 `commonTrueVal`/`commonFalseVal` 在 ClickHouse 下的值：

```go
// model/main.go initCol() 中
if common.UsingPostgreSQL {
    commonTrueVal = "true"
    commonFalseVal = "false"
} else {
    // MySQL / SQLite / ClickHouse 都用 1/0
    commonTrueVal = "1"
    commonFalseVal = "0"
}
```

#### 步骤 3.4：`GetAllLogs()` / `GetUserLogs()` 兼容性检查

这两个函数使用 GORM 方法（`Where`、`Order`、`Offset`、`Limit`、`Find`、`Count`），GORM ClickHouse 驱动支持这些操作，理论上无需改动。但需要验证：

1. `ORDER BY id DESC` — ClickHouse 主键是 `(created_at, type, user_id)`，按 `id` 排序会全表扫描，建议改为 `ORDER BY created_at DESC`
2. `COUNT(*)` — ClickHouse 支持但语义略有不同（`count()` 函数）

```go
// 建议：ClickHouse 下将 ORDER BY id DESC 改为 ORDER BY created_at DESC
if common.UsingClickHouse {
    tx = tx.Order("created_at desc")
} else {
    tx = tx.Order("id desc")
}
```

---

### M4：ByteHouse — 建库建表

#### 步骤 4.1：创建数据库

```sql
CREATE DATABASE IF NOT EXISTS gravitex_logs;
```

#### 步骤 4.2：建表 DDL（永久 TTL，不自动删除）

```sql
CREATE TABLE gravitex_logs.logs (
    id                Int64,
    user_id           Int64,
    created_at        Int64,
    type              Int32,
    content           String,
    username          LowCardinality(String),
    token_name        LowCardinality(String),
    model_name        LowCardinality(String),
    quota             Int32       DEFAULT 0,
    prompt_tokens     Int32       DEFAULT 0,
    completion_tokens Int32       DEFAULT 0,
    use_time          Int32       DEFAULT 0,
    is_stream         UInt8,
    channel_id        Int32,
    channel_name      String      DEFAULT '',
    token_id          Int32       DEFAULT 0,
    `group`           LowCardinality(String),
    ip                String      DEFAULT '',
    request_id        String      DEFAULT '',
    other             String      DEFAULT '',
    task_id           String      DEFAULT ''
)
ENGINE = CnchMergeTree()
ORDER BY (created_at, type, user_id)
PARTITION BY toYYYYMM(toDateTime(created_at))
SETTINGS index_granularity = 8192;
```

> **与之前方案的区别：**
> - **无 TTL 子句** — 数据永久保留，不自动删除
> - **删除了废弃字段** — `oem_id`、`oem_subsidy`、`official_quota`、`cost_quota`、`system_quota`、`user_quota`、`platform_profit`（详见次任务分析）
> - **保留 `task_id`** — 虽 Go 端未使用，但 Java 端可能有引用（需确认）
> - 引擎使用 `CnchMergeTree()` — ByteHouse 云原生引擎

#### 步骤 4.3：可选跳数索引

```sql
-- 为高频查询字段添加跳数索引（按需）
ALTER TABLE gravitex_logs.logs ADD INDEX idx_model model_name TYPE minmax GRANULARITY 4;
ALTER TABLE gravitex_logs.logs ADD INDEX idx_request_id request_id TYPE minmax GRANULARITY 4;
```

#### 步骤 4.4：设置虚拟仓库

连接后需指定虚拟仓库：
```sql
SET virtual_warehouse = 'your-warehouse-id';
```

---

### M5：Java 后端 — 双数据源配置

#### 步骤 5.1：添加 ClickHouse 依赖

**文件：** `Gravitex-API-End/ruoyi-modules/gravitex-api/pom.xml`

```xml
<!-- ClickHouse JDBC Driver -->
<dependency>
    <groupId>com.clickhouse</groupId>
    <artifactId>clickhouse-jdbc</artifactId>
    <version>0.6.0</version>
    <classifier>all</classifier>
</dependency>

<!-- MyBatis-Plus ClickHouse 支持（如需要） -->
<!-- 或使用动态数据源方式 -->
```

#### 步骤 5.2：配置双数据源

**文件：** `application.yml` 或 `application-dev.yml`

```yaml
spring:
  datasource:
    # 主数据源（MySQL）— 保持不变
    dynamic:
      primary: master
      datasource:
        master:
          url: jdbc:mysql://mysql-host:3306/gravitex?useUnicode=true&characterEncoding=utf8&serverTimezone=Asia/Shanghai
          username: root
          password: xxx
          driver-class-name: com.mysql.cj.jdbc.Driver
        # 新增：ClickHouse 日志数据源
        clickhouse:
          url: jdbc:clickhouse://bytehouse-host:19000/gravitex_logs
          username: bytehouse
          password: your-api-key
          driver-class-name: com.clickhouse.jdbc.ClickHouseDriver
          # 设置虚拟仓库
          connection-properties:
            virtual_warehouse: your-warehouse-id
```

#### 步骤 5.3：LogsMapper 指定数据源

使用 RuoYi-Plus 的 `@DS` 注解切换数据源：

```java
@Mapper
@DS("clickhouse")  // 指定使用 ClickHouse 数据源
public interface LogsMapper extends BaseMapperPlus<Logs, LogsVo> {
    // ... 现有方法
}
```

> **注意**：如果 LogsMapper 中有些方法仍需访问 MySQL（如 UPDATE/INSERT），需要拆分为两个 Mapper 或使用 `@DS` 方法级注解。

---

### M6：Java 后端 — SQL 语法适配

#### 步骤 6.1：`JSON_VALUE` → ClickHouse JSON 函数

**文件：** `LogsMapper.java`（`selectDatavKpiStats` 方法）

当前 MySQL 语法：
```sql
JSON_VALUE(other, '$.user_group_ratio' RETURNING DECIMAL(20,6) DEFAULT 0 ON EMPTY DEFAULT 0 ON ERROR)
```

ClickHouse 等价写法：
```sql
toFloat64OrZero(JSONExtractString(other, 'user_group_ratio'))
```

改动示例：
```java
@Select("SELECT " +
    "  toFloat64OrZero(JSONExtractString(other, 'user_group_ratio')) as userGroupRatio, " +
    "  toFloat64OrZero(JSONExtractString(other, 'system_profit_ratio')) as systemProfitRatio, " +
    "  ... " +
    "FROM logs WHERE ...")
List<Map<String, Object>> selectDatavKpiStats(...);
```

#### 步骤 6.2：`ifnull` → `ifNull` / `COALESCE`

ClickHouse 支持 `ifNull()` 和 `COALESCE()`，将 MySQL 的 `ifnull` 改为 `ifNull`（大小写敏感）或直接用 `COALESCE`。

#### 步骤 6.3：聚合函数检查

| MySQL 函数 | ClickHouse 兼容性 | 备注 |
|-----------|------------------|------|
| `COUNT(*)` | 支持 | 无 |
| `SUM()` | 支持 | 无 |
| `AVG()` | 支持 | 无 |
| `GROUP BY` | 支持 | 无 |
| `IFNULL()` | 需改为 `ifNull()` 或 `COALESCE` | 大小写敏感 |
| `IF(cond, a, b)` | 支持 | 或使用 `multiIf()` |
| `DATE_FORMAT()` | **不支持** | 用 ClickHouse 日期函数替代 |
| `FROM_UNIXTIME()` | 支持 | `toDateTime(ts)` |

#### 步骤 6.4：`DATE_FORMAT` 适配（如存在）

```sql
-- MySQL
DATE_FORMAT(FROM_UNIXTIME(created_at), '%Y-%m-%d')

-- ClickHouse
formatDateTime(toDateTime(created_at), '%Y-%m-%d')
-- 或
toString(toDate(toDateTime(created_at)))
```

---

### M7：Java 后端 — 跨表 JOIN 内存化

> **核心问题**：ByteHouse 中的 `logs` 表无法直接 JOIN MySQL 中的 `channels` 和 `t_enterprise_user` 表。

#### 步骤 7.1：`channels` 表数据加载到内存

**分析**：`channels` 表数据量小（通常几十到几百条），适合全量缓存。

**实现方案**：

```java
/**
 * Channel 内存缓存服务
 * 启动时全量加载 channels 表到内存，定时刷新
 */
@Service
public class ChannelCacheService {
    
    @Autowired
    private ChannelsMapper channelsMapper;
    
    // channel_id -> Channel 映射
    private volatile Map<Long, Channel> channelMap = new ConcurrentHashMap<>();
    
    @PostConstruct
    @Scheduled(fixedRate = 60000) // 每 60 秒刷新一次
    public void refreshCache() {
        List<Channel> channels = channelsMapper.selectList();
        Map<Long, Channel> newMap = channels.stream()
            .collect(Collectors.toMap(Channel::getId, Function.identity()));
        this.channelMap = new ConcurrentHashMap<>(newMap);
    }
    
    public Channel getById(Long channelId) {
        return channelMap.get(channelId);
    }
    
    public Map<Long, Channel> getAll() {
        return Collections.unmodifiableMap(channelMap);
    }
}
```

**改造 `selectChannelUsageStats`**：

原 SQL（跨表 JOIN）：
```sql
SELECT c.name as channel_name, SUM(l.quota) as total_quota
FROM logs l LEFT JOIN channels c ON c.id = l.channel_id
WHERE ...
GROUP BY l.channel_id
```

改为（只查 logs，应用层关联）：
```java
// 1. 从 ByteHouse 查询 logs 聚合（按 channel_id 分组）
@Select("SELECT channel_id, SUM(quota) as total_quota, COUNT(*) as call_count " +
    "FROM logs WHERE ... GROUP BY channel_id")
List<Map<String, Object>> selectChannelUsageStatsRaw(...);

// 2. 在 Service 层用内存中的 channels 数据关联
public List<ChannelUsageStat> getChannelUsageStats(...) {
    List<Map<String, Object>> rawStats = logsMapper.selectChannelUsageStatsRaw(...);
    Map<Long, Channel> channelMap = channelCacheService.getAll();
    
    return rawStats.stream().map(stat -> {
        Long channelId = (Long) stat.get("channel_id");
        Channel channel = channelMap.get(channelId);
        return new ChannelUsageStat(
            channel != null ? channel.getName() : "Unknown",
            (Long) stat.get("total_quota"),
            (Long) stat.get("call_count")
        );
    }).collect(Collectors.toList());
}
```

#### 步骤 7.2：`t_enterprise_user` 表数据加载到内存

**分析**：`t_enterprise_user` 数据量中等（取决于企业用户数），但查询频率不高。

```java
/**
 * 企业用户关系内存缓存
 */
@Service
public class EnterpriseUserCacheService {
    
    @Autowired
    private EnterpriseUserMapper enterpriseUserMapper;
    
    // user_id -> EnterpriseUser 映射
    private volatile Map<Long, EnterpriseUser> userMap = new ConcurrentHashMap<>();
    
    // enterprise_id -> List<user_id> 映射
    private volatile Map<Long, List<Long>> enterpriseUserIdsMap = new ConcurrentHashMap<>();
    
    @PostConstruct
    @Scheduled(fixedRate = 30000) // 每 30 秒刷新
    public void refreshCache() {
        List<EnterpriseUser> list = enterpriseUserMapper.selectList();
        
        Map<Long, EnterpriseUser> newUserMap = list.stream()
            .collect(Collectors.toMap(EnterpriseUser::getUserId, Function.identity(), (a, b) -> b));
        this.userMap = new ConcurrentHashMap<>(newUserMap);
        
        Map<Long, List<Long>> newEntMap = list.stream()
            .collect(Collectors.groupingBy(
                EnterpriseUser::getEnterpriseId,
                Collectors.mapping(EnterpriseUser::getUserId, Collectors.toList())
            ));
        this.enterpriseUserIdsMap = new ConcurrentHashMap<>(newEntMap);
    }
    
    /**
     * 获取企业下的所有 user_id 列表
     */
    public List<Long> getUserIdsByEnterpriseId(Long enterpriseId) {
        return enterpriseUserIdsMap.getOrDefault(enterpriseId, Collections.emptyList());
    }
}
```

**改造 `selectEnterpriseLogsByPage`**：

原 SQL（跨表 JOIN）：
```sql
SELECT l.* FROM logs l 
INNER JOIN t_enterprise_user eu ON eu.user_id = l.user_id
WHERE eu.enterprise_id = ? AND ...
ORDER BY l.created_at DESC
LIMIT ?, ?
```

改为（先查 user_id 列表，再查 logs）：
```java
public IPage<LogsVo> queryEnterpriseLogsPage(Long enterpriseId, LogsBo bo, PageQuery pageQuery) {
    // 1. 从内存缓存获取企业下的 user_id 列表
    List<Long> userIds = enterpriseUserCacheService.getUserIdsByEnterpriseId(enterpriseId);
    if (userIds.isEmpty()) {
        return new Page<>(); // 无企业成员，返回空
    }
    
    // 2. 用 user_id IN (...) 查询 ByteHouse logs 表
    LambdaQueryWrapper<Logs> wrapper = new LambdaQueryWrapper<>();
    wrapper.in(Logs::getUserId, userIds);
    // ... 其他条件
    
    return logsMapper.selectPage(pageQuery.build(), wrapper);
}
```

#### 步骤 7.3：缓存一致性保障

| 缓存 | 数据变化频率 | 刷新策略 | 可接受延迟 |
|------|------------|---------|-----------|
| `channels` | 极低（管理员手动操作） | 定时 60s + 管理端操作时主动刷新 | 1-2 分钟 |
| `t_enterprise_user` | 低（用户加入/退出企业） | 定时 30s + 操作时主动刷新 | 30 秒 |

---

### M8：联调测试

#### 步骤 8.1：Go 端写入验证

1. 配置 `LOG_SQL_DSN=clickhouse://bytehouse:password@host:19000/gravitex_logs`
2. 启动 Go 后端，确认日志输出 `ClickHouse LogBuffer initialized`
3. 发送几次 API 请求，等待 5 秒（flush 间隔）
4. 在 ByteHouse 中查询验证数据写入：
   ```sql
   SELECT count() FROM gravitex_logs.logs;
   SELECT * FROM gravitex_logs.logs ORDER BY created_at DESC LIMIT 10;
   ```

#### 步骤 8.2：Java 端读取验证

1. 配置双数据源
2. 启动 Java 后端
3. 验证管理端日志列表查询正常
4. 验证统计报表查询正常
5. 验证跨表 JOIN 内存化后的数据正确性

#### 步骤 8.3：性能基准测试

| 测试项 | 预期结果 |
|--------|---------|
| 批量写入吞吐 | > 10,000 条/秒 |
| 100w 日志聚合查询 | < 500ms |
| 1000w 日志聚合查询 | < 2s |
| 日志列表分页查询 | < 200ms |

---

## 三、次任务：字段清理

### 3.1 字段使用分析结果

| 字段 | Go 代码 | Java 代码 | DB 实际值 | 结论 |
|------|--------|----------|----------|------|
| `oem_id` | 未使用 | 未使用 | 全 NULL | **直接删除** |
| `oem_subsidy` | 未使用 | 未使用 | 全 0 | **直接删除** |
| `official_quota` | 未使用（Go 写入 `other` JSON） | 有引用，但 DB 值全为 0，有 fallback 读 `other` JSON | 全 0 | **删除字段 + 重构 Java 代码** |
| `cost_quota` | 未使用 | 有引用，DB 值全为 0 | 全 0 | **删除字段 + 重构 Java 代码** |
| `system_quota` | 未使用 | 有引用，DB 值全为 0 | 全 0 | **删除字段 + 重构 Java 代码** |
| `user_quota` | 未使用 | 有引用，但代码注释已标注"不可靠" | 全 0 | **删除字段 + 重构 Java 代码** |
| `platform_profit` | 未使用 | 有引用，DB 值全为 0 | 全 0 | **删除字段 + 重构 Java 代码** |

### 3.2 `oem_id`、`oem_subsidy` 清理（简单）

#### MySQL 当前表执行

```sql
-- 删除 oem_id 字段及其索引
ALTER TABLE logs DROP INDEX idx_logs_oem_id;
ALTER TABLE logs DROP COLUMN oem_id;

-- 删除 oem_subsidy 字段（无索引）
ALTER TABLE logs DROP COLUMN oem_subsidy;
```

#### ByteHouse 建表时直接不包含这两个字段

已在 M4 建表 DDL 中排除。

### 3.3 Quota 链字段清理（需重构 Java 代码）

#### 涉及的 Java 文件清单

| 文件 | 引用字段 | 改动说明 |
|------|---------|---------|
| `Logs.java`（实体类） | `officialQuota`, `costQuota`, `systemQuota`, `userQuota`, `platformProfit` | 删除这 5 个字段定义 |
| `LogsMapper.java` | `selectBillingReportData` 中 SELECT 这些字段 | 改为从 `other` JSON 提取或删除 |
| `LogsServiceImpl.java` | 导出逻辑中读取这些字段 | 改为从 `other` JSON 提取 |
| `BillingDetailService.java` | `log.getOfficialQuota()` 等 20+ 处引用 | 已有 fallback 逻辑，删除字段读取分支，统一走 `other` JSON |
| `BillingReportServiceImpl.java` | 聚合这些字段 | 改为从 `other` JSON 提取或计算 |
| `UserInvoiceServiceImpl.java` | 发票计算引用 | 已有注释说"user_quota 不可靠"，统一走 `quota` 字段 |
| `PricingCalculationService.java` | `PriceChain` 结构体定义 | 保留结构体（用于计算），但不持久化到 logs 表 |
| `LogsController.java` | 导出时 SELECT 这些字段 | 调整导出逻辑 |

#### 重构策略

这些字段的核心问题是：**Go 后端从未往这些列写值（全是默认 0），Java 的 `PricingCalculationService` 计算出了这些值但只存在 `other` JSON 中**。

重构方向：
1. **`BillingDetailService`** — 已有 fallback 逻辑 `if (officialQuota == null || officialQuota == 0L)` 从 `other` JSON 读取。删除数据库字段读取分支，统一走 `other` JSON。
2. **`LogsServiceImpl` 导出** — 将导出字段改为从 `other` JSON 中解析。
3. **`BillingReportServiceImpl` 聚合** — 统计查询改为从 `other` JSON 提取或直接用 `quota` 字段计算。
4. **`PricingCalculationService`** — `PriceChain` 保留作为内存计算结构，不持久化到独立列。

#### MySQL 表执行（在 ByteHouse 迁移之前或之后都可以）

```sql
-- 删除 quota 链字段（MySQL 当前表）
ALTER TABLE logs DROP COLUMN official_quota;
ALTER TABLE logs DROP COLUMN cost_quota;
ALTER TABLE logs DROP COLUMN system_quota;
ALTER TABLE logs DROP COLUMN user_quota;
ALTER TABLE logs DROP COLUMN platform_profit;
```

---

## 四、环境变量配置

### 4.1 Go 后端

```bash
# .env 或环境变量
LOG_SQL_DSN=clickhouse://bytehouse:your-api-key-suffix@your-host.bytehouse.cloud:19000/gravitex_logs
```

### 4.2 Java 后端

```yaml
# application.yml
spring:
  datasource:
    dynamic:
      datasource:
        clickhouse:
          url: jdbc:clickhouse://your-host.bytehouse.cloud:19000/gravitex_logs
          username: bytehouse
          password: your-api-key-suffix
          hikari:
            connection-init-sql: "SET virtual_warehouse = 'your-warehouse-id'"
```

---

## 五、开发顺序与依赖关系

```
阶段一：基础设施（Day 1）
├── S1: 删除 oem_id、oem_subsidy 字段（MySQL 表）
├── M4: ByteHouse 建库建表
└── M1: Go 后端 ClickHouse 驱动集成

阶段二：Go 端写入改造（Day 2-3）
├── M2: 批量写入 LogBuffer 改造
├── M3: SQL 兼容性修复
└── M8-a: Go 端写入验证

阶段三：Java 端适配（Day 3-5）
├── S2: Quota 链字段 Java 代码重构
├── M5: 双数据源配置
├── M6: SQL 语法适配
└── M7: 跨表 JOIN 内存化

阶段四：联调与上线（Day 5-7）
├── M8: 全链路联调测试
├── 性能基准测试
└── 灰度上线
```

### 依赖关系

| 任务 | 前置依赖 |
|------|---------|
| M2（批量写入） | M1（驱动集成） |
| M3（SQL 兼容） | M1（驱动集成） |
| M5（Java 双数据源） | M4（ByteHouse 建表） |
| M6（Java SQL 适配） | M5（双数据源） |
| M7（JOIN 内存化） | M5（双数据源） |
| M8（联调） | M2 + M3 + M6 + M7 |
| S2（Quota 重构） | 无前置依赖，可与阶段一并行 |

---

## 六、回滚方案

| 场景 | 回滚操作 |
|------|---------|
| Go 端写入异常 | 将 `LOG_SQL_DSN` 清空或改回 MySQL DSN，自动回退到单条 INSERT 模式 |
| Java 端查询异常 | 移除 `@DS("clickhouse")` 注解，回退到 MySQL 单数据源 |
| 数据不一致 | Go 端双写（MySQL + ByteHouse）过渡期，确认无误后再切单写 |
| 批量写入丢数据 | LogBuffer flush 失败时降级写 MySQL（需维护双连接） |

> **推荐上线策略**：先开启 Go 端双写（MySQL + ByteHouse），Java 端读取仍走 MySQL。验证 ByteHouse 数据完整后，Java 端切读 ByteHouse。最后关闭 MySQL 日志写入。

---

## 七、Checklist

- [ ] S1: MySQL 表删除 `oem_id`、`oem_subsidy` 字段及索引
- [ ] S2: Java 代码重构 quota 链字段（统一走 `other` JSON）
- [ ] S2: MySQL 表删除 `official_quota`、`cost_quota`、`system_quota`、`user_quota`、`platform_profit`
- [ ] M1: `go.mod` 添加 `gorm.io/driver/clickhouse`
- [ ] M1: `common/database.go` 确认 `UsingClickHouse` 变量
- [ ] M1: `model/main.go` — `chooseDB()` 添加 `clickhouse://` 分支
- [ ] M1: `model/main.go` — `InitLogDB()` 跳过 ClickHouse 不兼容操作
- [ ] M1: `model/main.go` — `migrateLOGDB()` 跳过 AutoMigrate
- [ ] M2: 新建 `model/log_buffer.go` — LogBuffer 组件
- [ ] M2: `model/log.go` — `CreateLog()` 适配批量写入
- [ ] M2: 优雅停机 flush 逻辑
- [ ] M3: `model/log.go` — `ifnull` → `COALESCE`
- [ ] M3: `model/log.go` — `DeleteOldLog()` ClickHouse 分支
- [ ] M3: `model/log.go` — `ORDER BY id` → `ORDER BY created_at`
- [ ] M4: ByteHouse 建库 `gravitex_logs`
- [ ] M4: ByteHouse 建表 `logs`（CnchMergeTree，永久 TTL）
- [ ] M5: Java `pom.xml` 添加 ClickHouse JDBC 依赖
- [ ] M5: Java `application.yml` 配置 ClickHouse 数据源
- [ ] M5: Java `LogsMapper` 添加 `@DS("clickhouse")`
- [ ] M6: Java `JSON_VALUE` → `JSONExtractFloat` / `toFloat64OrZero`
- [ ] M6: Java `ifnull` → `ifNull` / `COALESCE`
- [ ] M6: Java `DATE_FORMAT` → ClickHouse 日期函数（如有）
- [ ] M7: 新建 `ChannelCacheService` — channels 内存缓存
- [ ] M7: 新建 `EnterpriseUserCacheService` — 企业用户关系缓存
- [ ] M7: `selectChannelUsageStats` 改为应用层关联
- [ ] M7: `selectEnterpriseLogsByPage` 改为先查 user_id 再查 logs
- [ ] M8: Go 端写入验证
- [ ] M8: Java 端读取验证
- [ ] M8: 全链路联调
- [ ] M8: 性能基准测试
- [ ] 灰度上线
