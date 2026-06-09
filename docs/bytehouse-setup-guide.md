# BytePlus ByteHouse 开通与配置指南

> 编写日期：2026-06-05
> 用途：指导在 BytePlus 平台开通 ByteHouse 云数据仓库，并为 Gravitex 万级并发日志场景选型

---

## 一、BytePlus 平台注册与服务开通

### 1.1 注册 BytePlus 账号

1. 访问 [BytePlus 官网](https://www.byteplus.com)
2. 点击右上角 **Sign up**
3. 完成邮箱/手机号注册 + 账单信息验证

### 1.2 开通 ByteHouse 云数仓（CDW）

1. 登录后，访问 [ByteHouse 产品首页](https://www.byteplus.com/en/product/bytehouse)
2. 点击 **Get started (CDW)**
3. 点击 **Authorize now**，授权 ByteHouse 跨服务访问私有网络（VPC）、云监控等产品
4. 进入配置页面，按以下步骤逐项填写

### 1.3 支持的地域

| 地域 | Region Code | 说明 |
|------|------------|------|
| 中国香港 | cn-hongkong | 推荐，延迟最低（如服务部署在东南亚/香港） |
| 柔佛（马来西亚） | ap-southeast-1 | |
| 雅加达（印尼） | ap-southeast-3 | |

> 选择地域时，建议与 Go 后端服务部署在同一区域，降低网络延迟。

---

## 二、创建集群与虚拟仓库（详细步骤）

### Step 1：选择地域（Region）

在开通页面选择离业务服务器最近的地域。

### Step 2：配置计算资源（虚拟仓库 Virtual Warehouse）

| 配置项 | 说明 |
|--------|------|
| **计费方式** | 包年包月（Pre-paid）或 按量付费（Post-paid） |
| **规格型号** | 从 XS 到 XL，详见下方规格表 |
| **仓库名称** | 自定义，如 `gravitex-log-warehouse` |
| **描述** | 可选 |

### Step 3：配置本地磁盘（可选）

- 最低 100 GiB
- 用途：临时写入缓冲 + 读缓存加速
- 费用：$0.214/GiB/月（包月）

> **建议：开启本地磁盘。** 日志高频写入场景下，本地磁盘可作为写入缓冲，提升入库性能。

### Step 4：配置高级功能（可选）

| 功能 | 用途 | 费用 |
|------|------|------|
| Full Text Search | 全文检索 | 当前 $0 |
| GIS | 地理信息 | 当前 $0 |
| Vector Search | 向量搜索 | 当前 $0 |

> 日志场景一般不需要开启这些。

### Step 5：配置自动暂停/启动

| 配置项 | 说明 |
|--------|------|
| **Auto Start** | 默认开启，查询时自动唤醒仓库 |
| **Auto Pause** | 空闲 N 分钟后自动暂停（节省按量计费成本） |

> **注意：** 日志场景是 7x24 持续写入，**建议关闭 Auto Pause**，避免暂停导致写入失败。

### Step 6：配置存储与网络

| 配置项 | 说明 |
|--------|------|
| **存储** | 按量付费，最低 100 GiB |
| **VPC** | 绑定与业务服务器相同的 VPC |
| **子网** | 选择对应子网 |
| **安全组** | 放通以下端口 |

#### 必须放通的端口

| 协议 | 端口 | 用途 |
|------|------|------|
| **TCP** | 19000 | ClickHouse 原生协议（推荐） |
| **HTTP** | 8123 | HTTP 接口 |
| **MySQL** | 3306 | MySQL 兼容协议 |

#### IP 白名单

如果开启了 IP 白名单限制，需要添加 ByteHouse 服务 IP：

| 地域 | 白名单 IP |
|------|----------|
| 香港 | 150.5.146.60, 150.5.168.25 |
| 柔佛 | 101.47.140.89, 101.47.11.124 |
| 雅加达 | 163.7.17.61, 163.7.18.196 |

### Step 7：确认并开通

勾选服务协议，点击 **Activate ByteHouse**。

---

## 三、获取连接信息

开通完成后，在控制台获取连接参数：

1. 进入 **Tenant Management（租户管理）**
2. 找到 **Connection Information（连接信息）**
3. 记录以下关键信息：

| 参数 | 获取位置 | 示例 |
|------|---------|------|
| **Host** | 连接信息页，公网/私网域名 | `xxx.bytehouse.cloud` |
| **Port** | MySQL 协议固定 **3306**，原生协议 **19000** | |
| **Username** | IAM 用户默认 `bytehouse`；数据库用户格式 `{accountId}::{username}` | `bytehouse` |
| **Password** | 在连接信息页生成 **API Key**，以 `.` 分割：前缀为用户名，后缀为密码 | |
| **Virtual Warehouse ID** | 虚拟仓库管理页 | `vw-xxx` |

### 连接字符串格式

#### MySQL 协议（端口 3306）
```
# Go 后端 DSN 格式
{apiKeyPrefix}:{apiKeySuffix}@tcp({host}:3306)/{database}

# Java JDBC 格式
jdbc:mysql://{host}:3306/{database}?user={apiKeyPrefix}&password={apiKeySuffix}
```

#### ClickHouse 原生协议（端口 19000）
```
# GORM ClickHouse 驱动 DSN 格式
clickhouse://{username}:{password}@{host}:19000/{database}?dial_timeout=10s&read_timeout=20s
```

### 设置默认虚拟仓库

连接后需指定虚拟仓库（MySQL 协议下不能在建连时设置）：

```sql
SET virtual_warehouse = 'your-warehouse-id';
```

---

## 四、规格选型：万级并发场景

### 4.1 虚拟仓库规格与价格

#### Generic 通用型（CPU:内存 = 1:4）

| 规格 | CPU | 内存 | 缓存 | 包月价格 | 按量价格 |
|------|-----|------|------|---------|---------|
| **XS** | 4 核 | 16 GiB | 200 GiB | $280/月 | $0.56/小时 |
| **S** | 8 核 | 32 GiB | 200 GiB | $560/月 | $1.12/小时 |
| **M** | 16 核 | 64 GiB | 200 GiB | $1,120/月 | $2.24/小时 |
| **L** | 32 核 | 128 GiB | 200 GiB | $2,240/月 | $4.48/小时 |
| **XL** | 64 核 | 256 GiB | 200 GiB | $4,480/月 | $8.96/小时 |

#### Performance 性能型（CPU:内存 = 1:8）

| 规格 | CPU | 内存 | 缓存 | 包月价格 |
|------|-----|------|------|---------|
| **M Perf** | 16 核 | 128 GiB | 200 GiB | $1,520/月 |
| **L Perf** | 32 核 | 256 GiB | 200 GiB | $3,040/月 |
| **XL Plus** | 64 核 | 512 GiB | 1000 GiB | $6,080/月 |

#### 存储费用

| 类型 | 价格 | 说明 |
|------|------|------|
| **热存储**（EBS 云盘） | $0.214/GiB/月 | 本地磁盘，写入缓冲 |
| **冷存储**（对象存储） | $0.034/GiB/月 | 主数据存储，按需付费 |

### 4.2 万级并发场景需求分析

#### 业务指标估算

| 指标 | 估算值 | 说明 |
|------|--------|------|
| **并发用户数** | 10,000+ | 在线用户 |
| **API 调用频率** | ~2-5 次/分钟/用户 | 正常使用频率 |
| **峰值写入 QPS** | 300-800 条/秒 | 10000 x 5 / 60 = ~833 |
| **日均写入量** | 1000 万 - 3000 万条/天 | 日志记录 |
| **单条日志大小** | ~500 字节 | 含 other JSON 字段 |
| **日增数据量** | ~5-15 GB/天（未压缩） | |
| **月增数据量** | ~150-450 GB/月 | |
| **半年数据量** | ~0.9-2.7 TB | 保留 6 个月 |

#### 写入特性

ClickHouse/ByteHouse 的写入关键特点：

| 特性 | 说明 |
|------|------|
| **批量写入最佳** | 推荐每批 1,000-100,000 行，单行 INSERT 性能极差 |
| **同步写入** | 每秒约 1 次 INSERT 请求（批次内可含数万行） |
| **异步写入** | 服务端缓冲，支持更高频率，但有丢失风险 |
| **写入吞吐** | 16 核节点可达 **50-100 万行/秒**（批量写入） |
| **压缩率** | 列式存储，日志数据通常可达 **8-15:1** 压缩比 |

### 4.3 推荐配置

#### 起步配置（当前阶段）

| 组件 | 配置 | 月费用（包月） |
|------|------|---------------|
| **虚拟仓库** | **S（8 核 32 GiB）** | $560 |
| **本地磁盘** | 200 GiB（写入缓冲） | $43 |
| **冷存储** | 500 GiB（起步） | $17 |
| **合计** | | **~$620/月** |

> 适用场景：当前日志量 < 1000 万条/天，写入 QPS < 200

#### 万级并发推荐配置

| 组件 | 配置 | 月费用（包月） |
|------|------|---------------|
| **虚拟仓库** | **M（16 核 64 GiB）** | $1,120 |
| **本地磁盘** | 500 GiB（写入缓冲 + 热查询缓存） | $107 |
| **冷存储** | 1000 GiB | $34 |
| **合计** | | **~$1,261/月** |

> 适用场景：1000-3000 万条/天，写入 QPS 300-800，复杂聚合查询

#### 高负载配置（峰值预留）

| 组件 | 配置 | 月费用（包月） |
|------|------|---------------|
| **虚拟仓库** | **L（32 核 128 GiB）** | $2,240 |
| **本地磁盘** | 1000 GiB | $214 |
| **冷存储** | 2000 GiB | $68 |
| **合计** | | **~$2,522/月** |

> 适用场景：> 3000 万条/天，大量并发聚合查询，DataV 大屏实时统计

### 4.4 费用对比：ByteHouse vs MySQL

| 对比项 | MySQL（RDS 等价配置） | ByteHouse |
|--------|---------------------|-----------|
| 16 核 64 GiB 实例 | ~$800-1200/月 | ~$1,120/月 |
| 1TB 存储 | ~$100-200/月 | ~$34/月（冷存储压缩后更小） |
| 聚合查询性能 | 基准 | **10-60x 更快** |
| 存储压缩 | 无 | **8-15:1** |
| 弹性伸缩 | 需手动迁移 | 随时调整规格 |
| 按月总计（估） | ~$1,000-1,400 | ~$1,154-1,261 |

> **结论：** 同等配置下 ByteHouse 费用与 MySQL 相当甚至更低（得益于压缩存储），但聚合分析性能提升 10-60 倍。

---

## 五、高并发写入方案设计

### 5.1 核心问题

当前 Go 后端是**每次 API 调用立即 INSERT 一条日志**到 MySQL。ClickHouse 不擅长高频单条 INSERT，需要改为**批量写入**。

### 5.2 推荐方案：内存缓冲 + 定时批量刷入

```
API 请求 → Go 后端 → 写入内存 Buffer（channel / slice）
                            ↓
                   定时批量 flush（每 1-5 秒，或满 1000-10000 条）
                            ↓
                   ByteHouse 批量 INSERT
```

#### Go 代码架构示例

```go
type LogBuffer struct {
    mu       sync.Mutex
    buffer   []*Log
    maxSize  int           // 批量阈值，如 5000
    interval time.Duration // 刷新间隔，如 3 秒
    ticker   *time.Ticker
    done     chan struct{}
}

func NewLogBuffer(db *gorm.DB, maxSize int, interval time.Duration) *LogBuffer {
    lb := &LogBuffer{
        buffer:   make([]*Log, 0, maxSize),
        maxSize:  maxSize,
        interval: interval,
        ticker:   time.NewTicker(interval),
        done:     make(chan struct{}),
    }
    go lb.flushLoop(db)
    return lb
}

func (lb *LogBuffer) Add(log *Log) {
    lb.mu.Lock()
    lb.buffer = append(lb.buffer, log)
    shouldFlush := len(lb.buffer) >= lb.maxSize
    lb.mu.Unlock()
    if shouldFlush {
        lb.Flush(db)
    }
}

func (lb *LogBuffer) flushLoop(db *gorm.DB) {
    for {
        select {
        case <-lb.ticker.C:
            lb.Flush(db)
        case <-lb.done:
            lb.Flush(db) // 退出前最后一次 flush
            return
        }
    }
}

func (lb *LogBuffer) Flush(db *gorm.DB) {
    lb.mu.Lock()
    if len(lb.buffer) == 0 {
        lb.mu.Unlock()
        return
    }
    batch := lb.buffer
    lb.buffer = make([]*Log, 0, lb.maxSize)
    lb.mu.Unlock()

    // 批量写入
    if err := db.CreateInBatches(batch, len(batch)).Error; err != nil {
        // 失败重试或降级到 MySQL
        common.SysLog(fmt.Sprintf("ByteHouse batch insert failed: %v, %d logs lost", err, len(batch)))
    }
}
```

#### 关键参数建议

| 参数 | 推荐值 | 说明 |
|------|--------|------|
| **批量阈值** | 3,000 - 10,000 条 | ClickHouse 推荐的最小批次 |
| **刷新间隔** | 3 - 5 秒 | 平衡延迟和吞吐 |
| **内存上限** | 50,000 条 | 防止 OOM |
| **失败策略** | 降级写 MySQL 或持久化到本地文件 | 保证日志不丢失 |

### 5.3 写入格式优化

使用 ClickHouse 原生协议时，数据格式选择影响性能：

| 格式 | 性能 | 适用场景 |
|------|------|---------|
| **Native** | 最快 | GORM ClickHouse 驱动默认 |
| **RowBinary** | 快 | 手动构造高性能写入 |
| **JSONEachRow** | 中 | 调试/小规模数据 |
| **CSV/TSV** | 慢 | 不推荐用于高频写入 |

### 5.4 ClickHouse 异步插入（Async Insert）

ByteHouse 支持异步插入模式，适合无法在客户端批量聚合的场景：

```sql
-- 开启异步插入
SET async_insert = 1;
SET wait_for_async_insert = 0;  -- 不等待（更快，但有丢失风险）
-- 或
SET wait_for_async_insert = 1;  -- 等待确认（更安全）

-- 配置服务端缓冲
SET async_insert_max_data_size = 10485760;  -- 10MB 缓冲区
SET async_insert_busy_timeout_ms = 3000;    -- 3 秒超时
```

> **注意：** 异步插入的 `wait_for_async_insert = 0` 模式下，客户端无法感知写入错误，**不推荐用于计费日志**。建议使用客户端批量聚合方案。

---

## 六、建表与分区策略

### 6.1 推荐建表 DDL

```sql
CREATE DATABASE IF NOT EXISTS gravitex_logs;

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
    -- Java 后端扩展字段
    official_quota    Int64       DEFAULT 0,
    cost_quota        Int64       DEFAULT 0,
    system_quota      Int64       DEFAULT 0,
    user_quota        Int64       DEFAULT 0,
    platform_profit   Int64       DEFAULT 0
)
ENGINE = CnchMergeTree()
ORDER BY (created_at, type, user_id)
PARTITION BY toYYYYMM(toDateTime(created_at))
TTL toDateTime(created_at) + INTERVAL 6 MONTH
SETTINGS index_granularity = 8192;
```

### 6.2 分区策略说明

| 策略 | 说明 | 优势 |
|------|------|------|
| **按月分区** | `PARTITION BY toYYYYMM(toDateTime(created_at))` | 适合日志场景，方便按月清理 |
| **TTL 自动过期** | `TTL ... + INTERVAL 6 MONTH` | 数据自动清理，无需手动 DELETE |
| **ORDER BY** | `(created_at, type, user_id)` | 匹配最常用的查询模式 |

### 6.3 索引策略

| 索引类型 | 建议 |
|---------|------|
| **主键排序键** | `(created_at, type, user_id)` — 时间范围 + 类型 + 用户是最常见查询组合 |
| **跳数索引** | 对 `model_name`、`request_id` 添加 minmax 或 set 索引 |
| **不建议** | 不需要像 MySQL 那样创建大量 B-Tree 索引 |

```sql
-- 可选：为高频查询字段添加跳数索引
ALTER TABLE gravitex_logs.logs ADD INDEX idx_model model_name TYPE minmax GRANULARITY 4;
ALTER TABLE gravitex_logs.logs ADD INDEX idx_request_id request_id TYPE minmax GRANULARITY 4;
```

---

## 七、连接验证步骤

### 7.1 通过 MySQL CLI 快速验证

```bash
# 使用 MySQL 客户端连接（端口 3306）
mysql -h <your-host> -P 3306 -u <api-key-prefix> -p<api-key-suffix>

# 连接后指定虚拟仓库
SET virtual_warehouse = 'your-warehouse-id';

# 创建测试库表
CREATE DATABASE IF NOT EXISTS test_db;
USE test_db;

CREATE TABLE test_db.test_log (
    id Int64,
    message String,
    created_at Int64
) ENGINE = CnchMergeTree() ORDER BY id;

-- 插入测试数据
INSERT INTO test_db.test_log VALUES (1, 'hello', 1717545600);

-- 查询验证
SELECT * FROM test_db.test_log;
```

### 7.2 通过 Go 代码验证

```go
package main

import (
    "fmt"
    "gorm.io/driver/clickhouse"
    "gorm.io/gorm"
)

func main() {
    dsn := "clickhouse://bytehouse:your-password@your-host:19000/test_db"
    db, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
    if err != nil {
        panic(err)
    }

    // 设置虚拟仓库
    db.Exec("SET virtual_warehouse = 'your-warehouse-id'")

    // 测试查询
    var count int64
    db.Table("test_db.test_log").Count(&count)
    fmt.Println("rows:", count)
}
```

---

## 八、监控与运维

### 8.1 ByteHouse 控制台监控

开通后可在控制台查看：
- **查询负载** — QPS、查询延迟、慢查询
- **写入吞吐** — 写入行数/秒、写入数据量
- **存储用量** — 热存储、冷存储使用量
- **虚拟仓库状态** — CPU/内存利用率

### 8.2 关键告警指标

| 指标 | 告警阈值 | 说明 |
|------|---------|------|
| 写入延迟 P99 | > 5s | 批量写入超时 |
| 查询延迟 P99 | > 10s | 复杂聚合查询过慢 |
| 存储用量 | > 80% 预算 | 需扩容或清理数据 |
| 虚拟仓库 CPU | > 80% 持续 5 分钟 | 需升配 |
| Buffer 积压 | > 50,000 条 | 写入跟不上 |

### 8.3 规格调整

ByteHouse 支持随时调整虚拟仓库规格（升级/降级），无需数据迁移：

1. 进入 **Manage clusters > Cluster list**
2. 点击对应集群的 **Show more > Change specifications**
3. 选择新规格，确认即可

> 变配期间会有短暂的服务中断（秒级），建议在低峰期操作。

---

## 九、总结与行动清单

### 开通步骤 Checklist

- [ ] 1. 注册 BytePlus 账号并完成账单验证
- [ ] 2. 开通 ByteHouse CDW，选择地域（推荐香港）
- [ ] 3. 选择虚拟仓库规格（起步 S，万级并发 M）
- [ ] 4. 开启本地磁盘（200-500 GiB）
- [ ] 5. 关闭 Auto Pause（日志场景需持续在线）
- [ ] 6. 配置 VPC + 安全组，放通 19000/8123/3306 端口
- [ ] 7. 获取连接信息（Host、API Key、Warehouse ID）
- [ ] 8. 执行建库建表 DDL
- [ ] 9. 使用 MySQL CLI 或 Go 代码验证连接
- [ ] 10. Go 后端改造：单条 INSERT → 批量 Buffer 写入

### 预算预估

| 阶段 | 配置 | 月费用 |
|------|------|--------|
| **试运行** | S（8C32G）+ 200G 磁盘 + 500G 存储 | ~$620 |
| **万级并发** | M（16C64G）+ 500G 磁盘 + 1TB 存储 | ~$1,261 |
| **峰值扩展** | L（32C128G）+ 1TB 磁盘 + 2TB 存储 | ~$2,522 |

### 参考链接

| 资源 | 链接 |
|------|------|
| ByteHouse 快速入门 | https://docs.byteplus.com/en/docs/bytehouse/docs-quick-start |
| 规格与定价 | https://docs.byteplus.com/en/docs/bytehouse/specifications-and-pricing |
| 定价规则（企业版） | https://docs.byteplus.com/en/docs/bytehouse/pricing-rules |
| MySQL Go 驱动 | https://docs.byteplus.com/en/docs/bytehouse/mysql-go-driver |
| ClickHouse Go 驱动 | https://docs.byteplus.com/en/docs/bytehouse/docs-clickhouse-go-driver |
| 连接信息获取 | https://docs.byteplus.com/en/docs/bytehouse/docs-connectivity |
| 网络设置 | https://docs.byteplus.com/en/docs/bytehouse/network-settings |
| ClickHouse 写入最佳实践 | https://clickhouse.com/docs/best-practices/selecting-an-insert-strategy |
