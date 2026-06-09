# MySQL -> ByteHouse 日志数据库迁移可行性分析

> 分析日期：2026-06-05
> 分析范围：Go 后端（gravitex-api）的 `logs` 表从 MySQL 迁移到 ByteHouse/ClickHouse
> 分析依据：代码审计 + ByteHouse 官方文档 + ClickHouse 技术特性

---

## 一、现状分析

### 1.1 LOG_SQL_DSN 架构

Go 后端已预留了**日志数据库独立配置**的能力：

```
环境变量：LOG_SQL_DSN
初始化函数：model/main.go -> InitLogDB()
全局变量：model.LOG_DB (*gorm.DB)
类型标识：common.LogSqlType（当前仅支持 mysql / sqlite / postgres）
```

**初始化流程：**
1. `InitLogDB()` 检查 `LOG_SQL_DSN` 是否为空
2. 为空 → `LOG_DB = DB`（复用主库）
3. 非空 → 调用 `chooseDB("LOG_SQL_DSN", true)` 创建独立连接
4. `chooseDB()` 通过 DSN 前缀判断数据库类型：
   - `postgres://` / `postgresql://` → PostgreSQL
   - `local` → SQLite
   - 其他 → **MySQL（默认，当前线上使用）**

**当前状态：** `LOG_SQL_DSN` 未配置，日志写入主 MySQL 库。

### 1.2 logs 表操作全景

#### Go 后端（gravitex-api）— LOG_DB 操作清单

| 操作类型 | 函数/位置 | SQL 语义 | 频率 |
|---------|----------|---------|------|
| **INSERT** | `CreateLog()` | 单条插入 | 极高（每次 API 调用） |
| **INSERT** | `RecordLogWithAdminInfo()` | 单条插入 | 中 |
| **INSERT** | `RecordLogWithAdminInfoAndQuota()` | 单条插入 | 中 |
| **INSERT** | `RecordTopupLog()` | 单条插入 | 低 |
| **INSERT** | `RecordTaskBillingLog()` | 单条插入 | 低 |
| **SELECT** | `GetLogByTokenId()` | WHERE + ORDER BY + LIMIT | 低 |
| **SELECT** | `GetAllLogs()` | 多条件筛选 + COUNT + 分页 | 中（管理端） |
| **SELECT** | `GetUserLogs()` | 多条件筛选 + COUNT + 分页 | 中（客户端） |
| **SELECT** | `SumUsedQuota()` | SUM + COUNT + 聚合统计 | 中（RPM/TPM） |
| **SELECT** | `SumUsedToken()` | SUM + ifnull | 低 |
| **SELECT** | `SyncQuotaDataFromConsumeLogsByRequestId()` | WHERE + First + 聚合 | 低 |
| **DELETE** | `DeleteOldLog()` | WHERE created_at < X, 分批删除 | 低（定时清理） |
| **DDL** | `migrateLOGDB()` | AutoMigrate 建表 | 启动时一次 |
| **DDL** | `migrateLogRequestIdColumnLength()` | ALTER COLUMN 扩展长度 | 启动时一次 |
| **DDL** | `checkMySQLChineseSupport()` | 查询 information_schema | 启动时一次 |

#### Java 后端（Gravitex-API-End）— logs 表操作清单

| 操作类型 | 函数/位置 | SQL 语义 | 频率 |
|---------|----------|---------|------|
| **SELECT** | `queryPageList` (LambdaQueryWrapper) | 分页 + 多条件 + ORDER BY | 中（管理端） |
| **SELECT** | `queryList` (LambdaQueryWrapper) | 全量查询（导出） | 低 |
| **SELECT** | `selectQuotaStatFromLogs` | SUM(quota) 聚合 | 中 |
| **SELECT** | `selectQuotaRpmTpmFromLogs` | COUNT + SUM 聚合 | 高（实时统计） |
| **SELECT** | `selectBillingReportData` | 账单报表，含 ORDER BY | 低 |
| **SELECT** | `selectDatavKpiStats` | **JSON_VALUE** 解析 other 字段 + 多层子查询 | 中 |
| **SELECT** | `selectRealtimeRpmTpm` | COUNT + SUM，时间窗口聚合 | 高 |
| **SELECT** | `selectUserUsageStats` | GROUP BY + HAVING + CASE WHEN 聚合 | 低 |
| **SELECT** | `selectModelUsageStats` | GROUP BY + AVG + 聚合 | 低 |
| **SELECT** | `selectChannelUsageStats` | **LEFT JOIN channels** + GROUP BY | 低 |
| **SELECT** | `selectEnterpriseLogsByPage` | **INNER JOIN t_enterprise_user** + 分页 | 中 |
| **SELECT** | `countEnterpriseLogsByPage` | **INNER JOIN** + COUNT | 中 |
| **SELECT** | `selectEnterpriseLogsKeyset` | **INNER JOIN** + keyset 分页 | 低（导出） |
| **SELECT** | `countErrorsForAlarm` | WHERE + COUNT | 低 |
| **INSERT** | `insertByBo` | 单条插入 | 低 |
| **UPDATE** | `updateByBo` | 单条更新 | 极低 |
| **DELETE** | `deleteWithValidByIds` | 按 ID 删除 | 极低 |

### 1.3 代码中已预留的 ClickHouse 标识

```go
// common/database.go
var UsingClickHouse = false  // 已定义但从未使用
```

目前**没有任何代码引用** `UsingClickHouse`，也没有引入 `gorm.io/driver/clickhouse` 依赖。这是一个预留的占位变量。

---

## 二、ByteHouse / ClickHouse 技术特性

### 2.1 ByteHouse 简介

| 属性 | 说明 |
|------|------|
| 本质 | 基于 **ClickHouse** 的云原生数据仓库（OLAP） |
| 开发商 | 字节跳动（ByteDance） |
| 定位 | 大规模数据分析，列式存储，向量化执行 |
| 协议 | 同时支持 **ClickHouse 原生协议** 和 **MySQL 兼容协议** |
| MySQL 兼容 | 从 v2.4.0 起兼容 MySQL 5.7/8.0 常用语法 |
| Go 驱动 | 官方提供 MySQL Go Driver 和 ClickHouse Go Driver |
| GORM 支持 | `gorm.io/driver/clickhouse`（官方 GORM ClickHouse 驱动） |

### 2.2 核心差异：OLAP vs OLTP

| 特性 | MySQL (OLTP) | ByteHouse/ClickHouse (OLAP) |
|------|-------------|---------------------------|
| **写入模式** | 单行实时写入，毫秒级可见 | 批量写入最佳，单条 INSERT 性能差 |
| **UPDATE/DELETE** | 原生支持，实时生效 | 仅 lightweight delete/update，**异步执行** |
| **事务** | ACID 完整事务 | **无事务支持** |
| **外键** | 支持 | **不支持** |
| **索引** | B-Tree 主键索引 | 稀疏主键 + 跳数索引（不同概念） |
| **查询模式** | 点查优秀 | 全表扫描/聚合优秀 |
| **列式存储** | 行存储 | 列存储（压缩率高，分析快） |
| **information_schema** | 完整 | **部分支持或不支持** |
| **JSON 函数** | `JSON_VALUE`, `JSON_EXTRACT` | `JSONExtractFloat`, `JSONExtractString`（语法不同） |

### 2.3 ByteHouse MySQL 协议兼容性

ByteHouse 支持通过 MySQL 协议连接（端口通常 3306），这意味着：
- 可以使用 `go-sql-driver/mysql`（标准 MySQL Go 驱动）连接
- 可以使用 `gorm.io/driver/mysql`（GORM MySQL 驱动）连接
- 基本的 SELECT / INSERT / WHERE / ORDER BY / GROUP BY / LIMIT 语法兼容

**但有以下限制：**
- `UPDATE` 和 `DELETE` 走 lightweight mutations，**异步执行**，不立即生效
- `ALTER TABLE` 语法有限制
- `information_schema` 查询可能返回不同结果
- 部分 MySQL 特有函数不支持（如 `ifnull` 需用 `ifNull` 或 `COALESCE`）
- `JSON_VALUE(... RETURNING DECIMAL)` 是 MySQL 8.0.21+ 特有语法，**ByteHouse 不支持**

---

## 三、迁移方案对比

### 方案 A：MySQL 协议直连（最小改动）

> 思路：LOG_SQL_DSN 配置为 MySQL 协议 DSN 指向 ByteHouse，Go 后端继续使用 `gorm.io/driver/mysql`

**改动清单：**

| # | 改动项 | 文件 | 工作量 |
|---|--------|------|--------|
| 1 | 配置 `LOG_SQL_DSN` 为 ByteHouse MySQL 协议 DSN | .env / 环境变量 | 5 分钟 |
| 2 | `chooseDB()` 添加 ByteHouse 标识识别（如 DSN 含 `bytehouse` 关键字） | `model/main.go` | 0.5h |
| 3 | 设置 `common.UsingClickHouse = true` | `model/main.go` | 5 分钟 |
| 4 | 跳过 `checkMySQLChineseSupport()`（ByteHouse 无 information_schema） | `model/main.go` | 15 分钟 |
| 5 | `DeleteOldLog()` 改用 `ALTER TABLE ... DELETE` 或接受异步删除 | `model/log.go` | 1h |
| 6 | `SumUsedToken()` 中 `ifnull` → `COALESCE` | `model/log.go` | 5 分钟 |
| 7 | `AutoMigrate` 可能失败（ByteHouse DDL 限制），改为手动建表 | `model/main.go` | 2h |
| 8 | `migrateLogRequestIdColumnLength()` 适配 ClickHouse ALTER 语法 | `model/main.go` | 0.5h |
| 9 | Java 后端 LogsMapper 的 `JSON_VALUE` 改为 ClickHouse 的 `JSONExtractFloat` | `LogsMapper.java` | 2h |
| 10 | Java 后端跨表 JOIN 需确保 `channels`/`t_enterprise_user` 也在 ByteHouse 中 | Java 后端 | **阻塞问题** |

**优点：** 改动量最小（Go 后端约 5-6h），利用已有 LOG_SQL_DSN 架构
**缺点：**
- **Java 后端跨表 JOIN 是硬阻塞**：`selectChannelUsageStats` JOIN `channels`，`selectEnterpriseLogs*` JOIN `t_enterprise_user`，这些表在 MySQL 中，ByteHouse 无法直接 JOIN
- ByteHouse MySQL 协议兼容性不是 100%，可能遇到隐藏问题
- `JSON_VALUE(... RETURNING DECIMAL)` 在 ByteHouse 中需重写

### 方案 B：ClickHouse 原生驱动（推荐）

> 思路：引入 `gorm.io/driver/clickhouse`，使用 ClickHouse 原生协议（端口 9000），添加新的数据库类型分支

**改动清单：**

| # | 改动项 | 文件 | 工作量 |
|---|--------|------|--------|
| 1 | `go.mod` 添加 `gorm.io/driver/clickhouse` 依赖 | `go.mod` | 5 分钟 |
| 2 | `common/database.go` 添加 `DatabaseTypeClickHouse` 常量 | `common/database.go` | 5 分钟 |
| 3 | `chooseDB()` 添加 `clickhouse://` DSN 前缀分支 | `model/main.go` | 1h |
| 4 | `initCol()` 添加 ClickHouse 列名引用格式 | `model/main.go` | 15 分钟 |
| 5 | 跳过 `checkMySQLChineseSupport()` | `model/main.go` | 5 分钟 |
| 6 | `AutoMigrate` 替换为 ClickHouse 建表语句（MergeTree 引擎） | `model/main.go` | 3h |
| 7 | `DeleteOldLog()` 改用 `ALTER TABLE ... DELETE` 或 TTL 策略 | `model/log.go` | 2h |
| 8 | `ifnull()` → `COALESCE` / ClickHouse 兼容写法 | `model/log.go` | 15 分钟 |
| 9 | Java 后端添加 ClickHouse 数据源（独立 Datasource 配置） | `application.yml` + Java 配置类 | 4h |
| 10 | Java LogsMapper `JSON_VALUE` → ClickHouse `JSONExtractFloat` | `LogsMapper.java` | 2h |
| 11 | Java 跨表 JOIN 处理（见下方阻塞问题分析） | `LogsMapper.java` | 4h |
| 12 | Go 后端 `common.UsingClickHouse` 条件分支（布尔值映射等） | 多文件 | 2h |

**优点：**
- 原生协议性能更好，ClickHouse 特性完整可用
- GORM ClickHouse 驱动支持 AutoMigrate（有限）
- 社区成熟，`gorm.io/driver/clickhouse` 官方维护

**缺点：**
- 需要引入新依赖
- Java 后端需要配置双数据源（主库 MySQL + 日志库 ClickHouse）

### 方案 C：双写架构（MySQL 写入 + ByteHouse 分析，推荐长期方案）

> 思路：MySQL 保持写入（INSERT），异步同步到 ByteHouse，分析/统计查询走 ByteHouse

**架构：**
```
API 请求 → Go 后端 → INSERT logs (MySQL)
                         ↓  (异步同步：Canal / Flink CDC / 定时批量)
                     ByteHouse logs 表
                         ↓
              管理端统计/大屏/报表查询 → ByteHouse
              客户端实时日志列表查询 → MySQL
```

**改动清单：**

| # | 改动项 | 说明 | 工作量 |
|---|--------|------|--------|
| 1 | ByteHouse 建表 | ClickHouse MergeTree 引擎，按日分区 | 2h |
| 2 | 数据同步管道 | Canal/Flink CDC/自研批量同步 | 1-3 天 |
| 3 | Java 后端双数据源 | MyBatis 配置 ClickHouse 数据源 | 4h |
| 4 | Java 统计查询迁移 | 将聚合统计查询迁移到 ByteHouse | 1 天 |
| 5 | `JSON_VALUE` 改写 | → ClickHouse `JSONExtractFloat` | 2h |
| 6 | 跨表 JOIN 处理 | 使用字典表或宽表替代 JOIN | 1 天 |

**优点：**
- 风险最低：写入路径不变，不影响线上稳定性
- 统计查询性能提升显著（ClickHouse 聚合比 MySQL 快 10-100x）
- MySQL 日志表压力大幅降低（不再有重聚合查询）

**缺点：**
- 需要维护数据同步管道
- 存在数据延迟（秒级到分钟级）
- 基础设施成本增加

---

## 四、关键阻塞问题

### 4.1 跨表 JOIN（严重）

Java LogsMapper 中有 **多处跨表 JOIN**，如果 logs 表迁移到 ByteHouse，而 `channels` / `t_enterprise_user` 仍在 MySQL：

```sql
-- selectChannelUsageStats: logs LEFT JOIN channels
FROM logs l LEFT JOIN channels c ON c.id = l.channel_id

-- selectEnterpriseLogsByPage: logs INNER JOIN t_enterprise_user
FROM logs l INNER JOIN t_enterprise_user eu ON eu.user_id = l.user_id
```

**解决方案：**
1. **ClickHouse Dictionary**：将 `channels` 和 `t_enterprise_user` 创建为 ClickHouse 字典表（从 MySQL 同步）
2. **宽表/物化视图**：在 ByteHouse 中创建预关联的宽表
3. **应用层 JOIN**：在 Java 代码中分两次查询，然后在内存中关联
4. **ClickHouse MySQL 表引擎**：使用 `MySQL()` 表引擎直接查询 MySQL 表（性能一般）

### 4.2 JSON_VALUE 不兼容（中等）

`selectDatavKpiStats` 使用了 MySQL 8.0.21+ 的 `JSON_VALUE` 语法：

```sql
-- MySQL 8.0.21+
JSON_VALUE(other, '$.user_group_ratio' RETURNING DECIMAL(20,6) DEFAULT 0 ON EMPTY DEFAULT 0 ON ERROR)

-- ClickHouse 等价写法
toFloat64OrZero(JSONExtractString(other, 'user_group_ratio'))
-- 或
JSONExtractFloat(other, 'user_group_ratio')
```

**改动量：** 1 个方法，约 3 处 JSON_VALUE 调用，预计 2h。

### 4.3 DELETE 语义差异（中等）

`DeleteOldLog()` 使用 GORM 的分批删除：
```go
LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
```

ClickHouse/ByteHouse 的 lightweight delete 是**异步的**，且 `ALTER TABLE ... DELETE` 不支持 `LIMIT`。

**解决方案：**
1. 使用 ClickHouse **TTL** 特性自动过期删除（推荐）
2. 使用 `ALTER TABLE logs DELETE WHERE created_at < X`（异步，无 LIMIT）
3. 按分区删除：`ALTER TABLE logs DROP PARTITION ...`（如果使用按日分区）

### 4.4 AutoMigrate 不兼容（低）

GORM `AutoMigrate` 在 ClickHouse 上的支持有限：
- 可以创建表，但引擎默认可能不是最优
- `ALTER COLUMN` 在 ClickHouse 中语法不同
- 索引创建方式不同

**解决方案：** 为 ClickHouse 单独写建表 DDL，跳过 AutoMigrate。

```sql
CREATE TABLE logs (
    id          Int64,
    user_id     Int64,
    created_at  Int64,
    type        Int32,
    content     String,
    username    String,
    token_name  String,
    model_name  String,
    quota       Int32,
    prompt_tokens     Int32,
    completion_tokens Int32,
    use_time    Int32,
    is_stream   UInt8,
    channel_id  Int32,
    channel_name String,
    token_id    Int32,
    `group`     String,
    ip          String,
    request_id  String,
    other       String,
    official_quota Int64 DEFAULT 0,
    cost_quota     Int64 DEFAULT 0,
    system_quota   Int64 DEFAULT 0,
    user_quota     Int64 DEFAULT 0,
    platform_profit Int64 DEFAULT 0
) ENGINE = MergeTree()
ORDER BY (created_at, id)
PARTITION BY toYYYYMM(toDateTime(created_at))
TTL toDateTime(created_at) + INTERVAL 6 MONTH
SETTINGS index_granularity = 8192;
```

---

## 五、改动量评估汇总

### 方案 A：MySQL 协议直连

| 端 | 改动文件数 | 预估工时 | 风险 |
|----|-----------|---------|------|
| Go 后端 | 2-3 文件 | 4-5h | 中（兼容性未验证） |
| Java 后端 | 1-2 文件 | 4-6h | 高（跨表 JOIN 阻塞） |
| **合计** | **3-5 文件** | **1-1.5 天** | **高** |

### 方案 B：ClickHouse 原生驱动

| 端 | 改动文件数 | 预估工时 | 风险 |
|----|-----------|---------|------|
| Go 后端 | 3-5 文件 | 6-8h | 低 |
| Java 后端 | 3-5 文件 | 8-10h | 中（双数据源 + JOIN） |
| **合计** | **6-10 文件** | **2-2.5 天** | **中** |

### 方案 C：双写架构

| 端 | 改动文件数 | 预估工时 | 风险 |
|----|-----------|---------|------|
| Go 后端 | 0-1 文件 | 0-2h | 极低 |
| Java 后端 | 5-8 文件 | 1-2 天 | 低 |
| 数据管道 | 新建 | 1-3 天 | 中 |
| ByteHouse DDL | 新建 | 2h | 低 |
| **合计** | **5-10 文件 + 新组件** | **3-5 天** | **低** |

---

## 六、适合 ByteHouse 的场景评估

### 6.1 logs 表特征 vs ClickHouse 优势

| logs 表特征 | 是否适合 ClickHouse | 说明 |
|------------|-------------------|------|
| 写入模式：append-only | 非常适合 | 几乎无 UPDATE，INSERT 为主 |
| 数据量：持续增长 | 适合 | 列式存储压缩率高（10:1 以上） |
| 查询模式：聚合统计 | 非常适合 | SUM/COUNT/GROUP BY 是 ClickHouse 强项 |
| 查询模式：时间范围筛选 | 非常适合 | 时间分区 + 主键排序完美匹配 |
| 需要跨表 JOIN | 不太适合 | ClickHouse 的 JOIN 性能不如 MySQL |
| 需要单行 UPDATE | 不适合 | ClickHouse 不擅长 |
| 需要实时一致性删除 | 不适合 | lightweight delete 异步执行 |

### 6.2 性能收益预估

| 查询场景 | MySQL 耗时（估） | ByteHouse 耗时（估） | 提升 |
|---------|----------------|-------------------|------|
| 100w 日志 SUM/GROUP BY | 1-3s | 50-200ms | 10-20x |
| 1000w 日志聚合统计 | 10-30s | 100-500ms | 20-60x |
| 实时 RPM/TPM（60s 窗口） | 100-500ms | 10-50ms | 10x |
| 单用户日志分页查询 | 50-200ms | 20-100ms | 2-5x |

---

## 七、结论与建议

### 7.1 可行性结论

| 维度 | 评估 |
|------|------|
| **技术可行性** | **可行，但有阻塞** — 核心问题是跨表 JOIN 和 JSON_VALUE 语法不兼容 |
| **改动量** | Go 后端小（1-2 天），Java 后端中等（需要双数据源 + SQL 改写） |
| **风险等级** | 方案 A/B 中等偏高，方案 C 低 |
| **收益** | 聚合统计性能提升 10-60x，日志表存储压力大幅降低 |

### 7.2 推荐路线

**短期（1-2 周）：** 方案 B（ClickHouse 原生驱动）
1. Go 后端添加 ClickHouse 驱动支持，`LOG_SQL_DSN` 使用 `clickhouse://` 格式
2. Java 后端配置双数据源，统计查询走 ClickHouse
3. 跨表 JOIN 改为应用层关联或 ClickHouse Dictionary
4. `JSON_VALUE` 改写为 ClickHouse JSON 函数

**长期（1-2 月）：** 方案 C（双写架构）
1. MySQL 保持写入，CDC 同步到 ByteHouse
2. 所有统计/报表/大屏查询切到 ByteHouse
3. MySQL 日志表保留最近 7 天数据（热数据），ByteHouse 保留全量（冷数据）
4. 使用 TTL 自动清理过期数据

### 7.3 前置确认事项

| # | 确认项 | 影响 |
|---|--------|------|
| 1 | 使用的是**字节内部 ByteHouse** 还是开源 **ClickHouse**？ | 决定 DSN 格式和可用特性 |
| 2 | 日志表当前数据量级？（万/百万/千万/亿级） | 决定是否有必要迁移 |
| 3 | 日志保留策略？（永久 / N 天 / N 月） | 决定分区和 TTL 策略 |
| 4 | Java 后端是否支持配置双数据源？（RuoYi-Plus 框架支持情况） | 决定 Java 端改动复杂度 |
| 5 | 是否有基础设施团队支持数据同步管道？ | 决定方案 B vs 方案 C |

---

## 附录 A：ByteHouse Go 驱动连接示例

### MySQL 协议方式
```go
import "gorm.io/driver/mysql"

// DSN 格式: user:password@tcp(host:port)/database
dsn := "default:password@tcp(bytehouse-host:3306)/logs_db"
db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
```

### ClickHouse 原生协议方式
```go
import "gorm.io/driver/clickhouse"

// DSN format: clickhouse://user:password@host:port/database
dsn := "clickhouse://default:password@bytehouse-host:9000/logs_db"
db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
```

## 附录 B：ClickHouse 建表参考

```sql
CREATE TABLE IF NOT EXISTS logs (
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
    -- Java 后端扩展字段
    official_quota    Int64       DEFAULT 0,
    cost_quota        Int64       DEFAULT 0,
    system_quota      Int64       DEFAULT 0,
    user_quota        Int64       DEFAULT 0,
    platform_profit   Int64       DEFAULT 0
)
ENGINE = MergeTree()
ORDER BY (created_at, type, user_id)
PARTITION BY toYYYYMM(toDateTime(created_at))
TTL toDateTime(created_at) + INTERVAL 6 MONTH
SETTINGS index_granularity = 8192;
```

> 说明：
> - `LowCardinality(String)` 用于低基数列（username、model_name 等），提升压缩和查询性能
> - `ORDER BY (created_at, type, user_id)` 按查询频率最高的字段排序
> - `PARTITION BY` 按月分区，方便 TTL 过期和数据管理
> - `TTL` 自动清理 6 个月前的数据（可调整）
