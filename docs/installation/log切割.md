# 日志切割与归档说明（Linux + logrotate）

> 适用于 `gravitex-api`（基于 new-api）二进制部署到 Linux 服务器的场景。
>
> 部署路径假设：
> - 程序目录：`/workplace`
> - 日志目录：`/workplace/logs`
>
> 如实际部署路径不同，把本文中所有 `/workplace` / `/workplace/logs` 替换成对应路径即可。

---

## 0. 前置条件（务必先看，否则 logrotate 不会切到任何文件）

本文档中 logrotate 的匹配模式是 `api-*.log`，要求程序产生**前缀为 `api-`** 的日志文件。

但 `logger/logger.go` **默认硬编码生成的前缀是 `oneapi-`**：

```go
logPath := filepath.Join(*common.LogDir, fmt.Sprintf("oneapi-%s.log", time.Now().Format("20060102150405")))
```

因此使用本文档之前，请二选一：

- **方案 A（推荐，与本文档一致）**：把 `logger/logger.go` 中的 `"oneapi-%s.log"` 改成 `"api-%s.log"`，重新编译部署。之后程序产出的文件就是 `api-YYYYMMDDHHMMSS.log`，与本文档完全对应。
- **方案 B（保持代码不动）**：保留代码里的 `oneapi-` 前缀，但把本文档中所有 `api-*.log` / `api-` 字样**全部替换回 `oneapi-*.log` / `oneapi-`** 后再使用，否则 logrotate 配置生效但匹配不到任何文件，归档不会发生且不会报错。

> 是否切换前缀对程序运行**没有任何功能影响**，仅影响磁盘上文件名和 logrotate 匹配规则。本文档之后的内容默认你已选了方案 A。

---

## 1. 目标

按照运维最佳实践对 `/workplace/logs` 下的日志做以下处理：

| 时间 | 文件状态 |
|------|---------|
| 今天的日志 | 正常以 `.log` 形式存在主目录，程序持续追加写入 |
| 昨天及以前的日志 | 自动归档到 `archive/` 子目录，并以 `gzip` 压缩（`.gz`） |
| 6 个月（约 180 天）以前的归档 | 自动删除（保留份数可调） |

切割频率：**每天一次**（凌晨由 cron.daily 触发）。

---

## 2. 程序的日志机制（必读，否则会踩坑）

`gravitex-api` 的日志写入特点：

1. 启动参数 `--log-dir`（默认 `./logs`）指定日志目录。**不是环境变量**，写在 `Environment=LOG_DIR=...` 里无效。
2. 日志文件名格式：`api-YYYYMMDDHHMMSS.log`（默认代码为 `oneapi-`，本文按 §0 方案 A 已改为 `api-`），时间戳是"进程启动时刻"或"内部切换时刻"。
3. 进程启动后会持有该文件 fd 持续追加写入。
4. **没有按天自动新建文件**，也**不监听 SIGHUP / SIGUSR1 信号**。

由此得出两个关键约束：

- 切割工具必须使用 **`copytruncate`** 模式（先复制再清空原文件），不能用默认的 rename 模式，否则程序会写到一个已经"消失"的 inode 里，磁盘上看不到日志。
- 文件名虽然带时间戳，但只要进程不重启，今天就只会有一个 `.log` 文件在写入；logrotate 用通配符匹配 `api-*.log` 即可。

---

## 3. 推荐方案：`logrotate` + `gzip`

### 3.1 为什么用 gzip 而不是 tar

- 单文件归档的标准做法是 `gzip`（`.gz`），压缩比好、还能用 `zcat` / `zless` / `zgrep` 直接查日志，不用先解包。
- `tar` 是用来"打包多个文件"的，单个 `.log` 文件外面再套一层 `tar` 没有意义，反而让查询日志多一道解包动作。
- 如果有"按周/按月把多天的归档合成一个包上传到归档存储"的需求，再单独写脚本做 tar，详见 §6。

### 3.2 安装 logrotate（一般已自带）

```bash
# CentOS / RHEL / Anolis
sudo yum install -y logrotate

# Ubuntu / Debian
sudo apt install -y logrotate

# 验证
logrotate --version
```

### 3.3 创建配置文件

```bash
sudo vim /etc/logrotate.d/gravitex-api
```

写入以下内容：

```conf
/workplace/logs/api-*.log {
    daily
    rotate 180
    missingok
    notifempty
    copytruncate
    compress
    compresscmd /usr/bin/gzip
    compressoptions -9
    compressext .gz
    dateext
    dateformat -%Y%m%d
    dateyesterday
    olddir /workplace/logs/archive
    createolddir 0755 root root
    su root root
}
```

> 如果程序不是用 root 跑（例如你用了专门的 `www` 用户），把最后两行的 `root root` 改成对应用户和用户组，比如 `createolddir 0755 www www` 和 `su www www`。

### 3.4 配置项说明

| 配置项 | 含义 |
|--------|------|
| `/workplace/logs/api-*.log` | 匹配的日志文件，通配符方式（前缀对应 §0 方案 A） |
| `daily` | 每天切割一次（cron.daily 触发） |
| `rotate 180` | 保留 180 份归档（约 6 个月），超出的自动删除 |
| `missingok` | 文件不存在不报错 |
| `notifempty` | 空文件不切，避免产生空归档 |
| `copytruncate` | **必选**。复制原文件后清空原文件，兼容 Go 进程持有 fd 的写入方式 |
| `compress` | 启用压缩 |
| `compresscmd /usr/bin/gzip` | 压缩命令 |
| `compressoptions -9` | 最高压缩比（CPU 多消耗一点，磁盘省更多） |
| `compressext .gz` | 压缩文件后缀 |
| `dateext` | 归档名追加日期，而不是 `.1 .2 .3` |
| `dateformat -%Y%m%d` | 日期格式：`-20260511` |
| `dateyesterday` | 用昨天日期命名（凌晨执行时正好对应"昨天的日志"） |
| `olddir /workplace/logs/archive` | 把压缩后的归档移到子目录，保持主目录干净 |
| `createolddir 0755 root root` | `archive` 不存在时自动创建 |
| `su root root` | logrotate 操作时使用的用户/组 |

### 3.5 切割后的目录结构示例

```
/workplace/logs/
├── api-20260512083012.log          # 今天，程序正在写入
└── archive/
    ├── api-20260512083012.log-20260511.gz   # 昨天的归档
    ├── api-20260512083012.log-20260510.gz
    ├── api-20260512083012.log-20260509.gz
    └── ...                                    # 最多保留 180 份（约 6 个月）
```

> 注：`api-20260512083012.log` 这个文件名里的时间戳**不会变**（取决于程序启动时间），变的是后缀的 `-20260511` 这个日期标签。

---

## 4. 验证步骤

### 4.1 dry-run（不真正执行，只看会做什么）

```bash
sudo logrotate -d /etc/logrotate.d/gravitex-api
```

输出会描述"如果触发会做哪些动作"，无副作用，用于排查配置是否正确。

### 4.2 强制执行一次（验证效果）

```bash
sudo logrotate -fv /etc/logrotate.d/gravitex-api
```

执行后检查：

```bash
ls -lh /workplace/logs/
ls -lh /workplace/logs/archive/
```

应当看到：
- 主目录的 `api-*.log` 大小变得很小（被 truncate 了）。
- `archive/` 下出现了带日期后缀的 `.gz` 文件。
- 程序进程仍在正常运行，且仍在向那个 `api-*.log` 追加新日志。

### 4.3 验证 cron 是否会按天触发

logrotate 默认通过系统 `cron.daily` 调度，无需额外配置：

```bash
# 查看是否存在 cron.daily 调度脚本
ls -l /etc/cron.daily/logrotate

# 查看 logrotate 上次执行的状态
sudo cat /var/lib/logrotate/logrotate.status | grep gravitex-api
```

执行历史正常的话，应当能看到 `gravitex-api` 配置对应的最近执行日期。

---

## 5. 查看历史日志

由于历史日志已 gzip 压缩，**无需先解压**，直接用 `z` 系列命令查看：

```bash
# 查看
zcat /workplace/logs/archive/api-20260512083012.log-20260511.gz | less

# 翻页查看
zless /workplace/logs/archive/api-20260512083012.log-20260511.gz

# 搜索关键字
zgrep "ERROR" /workplace/logs/archive/api-20260512083012.log-20260511.gz

# 跨多个归档搜索
zgrep "ERROR" /workplace/logs/archive/api-*.gz
```
# 日志切割与归档说明（Linux + logrotate）

> 适用于 `gravitex-api`（基于 new-api）二进制部署到 Linux 服务器的场景。
>
> 部署路径假设：
> - 程序目录：`/workplace`
> - 日志目录：`/workplace/logs`
>
> 如实际部署路径不同，把本文中所有 `/workplace` / `/workplace/logs` 替换成对应路径即可。

---

## 0. 前置条件（务必先看，否则 logrotate 不会切到任何文件）

本文档中 logrotate 的匹配模式是 `api-*.log`，要求程序产生**前缀为 `api-`** 的日志文件。

但 `logger/logger.go` **默认硬编码生成的前缀是 `oneapi-`**：

```go
logPath := filepath.Join(*common.LogDir, fmt.Sprintf("oneapi-%s.log", time.Now().Format("20060102150405")))
```

因此使用本文档之前，请二选一：

- **方案 A（推荐，与本文档一致）**：把 `logger/logger.go` 中的 `"oneapi-%s.log"` 改成 `"api-%s.log"`，重新编译部署。之后程序产出的文件就是 `api-YYYYMMDDHHMMSS.log`，与本文档完全对应。
- **方案 B（保持代码不动）**：保留代码里的 `oneapi-` 前缀，但把本文档中所有 `api-*.log` / `api-` 字样**全部替换回 `oneapi-*.log` / `oneapi-`** 后再使用，否则 logrotate 配置生效但匹配不到任何文件，归档不会发生且不会报错。

> 是否切换前缀对程序运行**没有任何功能影响**，仅影响磁盘上文件名和 logrotate 匹配规则。本文档之后的内容默认你已选了方案 A。

---

## 1. 目标

按照运维最佳实践对 `/workplace/logs` 下的日志做以下处理：

| 时间 | 文件状态 |
|------|---------|
| 今天的日志 | 正常以 `.log` 形式存在主目录，程序持续追加写入 |
| 昨天及以前的日志 | 自动归档到 `archive/` 子目录，并以 `gzip` 压缩（`.gz`） |
| 6 个月（约 180 天）以前的归档 | 自动删除（保留份数可调） |

切割频率：**每天一次**（凌晨由 cron.daily 触发）。

---

## 2. 程序的日志机制（必读，否则会踩坑）

`gravitex-api` 的日志写入特点：

1. 启动参数 `--log-dir`（默认 `./logs`）指定日志目录。**不是环境变量**，写在 `Environment=LOG_DIR=...` 里无效。
2. 日志文件名格式：`api-YYYYMMDDHHMMSS.log`（默认代码为 `oneapi-`，本文按 §0 方案 A 已改为 `api-`），时间戳是"进程启动时刻"或"内部切换时刻"。
3. 进程启动后会持有该文件 fd 持续追加写入。
4. **没有按天自动新建文件**，也**不监听 SIGHUP / SIGUSR1 信号**。

由此得出两个关键约束：

- 切割工具必须使用 **`copytruncate`** 模式（先复制再清空原文件），不能用默认的 rename 模式，否则程序会写到一个已经"消失"的 inode 里，磁盘上看不到日志。
- 文件名虽然带时间戳，但只要进程不重启，今天就只会有一个 `.log` 文件在写入；logrotate 用通配符匹配 `api-*.log` 即可。

---

## 3. 推荐方案：`logrotate` + `gzip`

### 3.1 为什么用 gzip 而不是 tar

- 单文件归档的标准做法是 `gzip`（`.gz`），压缩比好、还能用 `zcat` / `zless` / `zgrep` 直接查日志，不用先解包。
- `tar` 是用来"打包多个文件"的，单个 `.log` 文件外面再套一层 `tar` 没有意义，反而让查询日志多一道解包动作。
- 如果有"按周/按月把多天的归档合成一个包上传到归档存储"的需求，再单独写脚本做 tar，详见 §6。

### 3.2 安装 logrotate（一般已自带）

```bash
# CentOS / RHEL / Anolis
sudo yum install -y logrotate

# Ubuntu / Debian
sudo apt install -y logrotate

# 验证
logrotate --version
```

### 3.3 创建配置文件

```bash
sudo vim /etc/logrotate.d/gravitex-api
```

写入以下内容：

```conf
/workplace/logs/api-*.log {
    daily
    rotate 180
    missingok
    notifempty
    copytruncate
    compress
    compresscmd /usr/bin/gzip
    compressoptions -9
    compressext .gz
    dateext
    dateformat -%Y%m%d
    dateyesterday
    olddir /workplace/logs/archive
    createolddir 0755 root root
    su root root
}
```

> 如果程序不是用 root 跑（例如你用了专门的 `www` 用户），把最后两行的 `root root` 改成对应用户和用户组，比如 `createolddir 0755 www www` 和 `su www www`。

### 3.4 配置项说明

| 配置项 | 含义 |
|--------|------|
| `/workplace/logs/api-*.log` | 匹配的日志文件，通配符方式（前缀对应 §0 方案 A） |
| `daily` | 每天切割一次（cron.daily 触发） |
| `rotate 180` | 保留 180 份归档（约 6 个月），超出的自动删除 |
| `missingok` | 文件不存在不报错 |
| `notifempty` | 空文件不切，避免产生空归档 |
| `copytruncate` | **必选**。复制原文件后清空原文件，兼容 Go 进程持有 fd 的写入方式 |
| `compress` | 启用压缩 |
| `compresscmd /usr/bin/gzip` | 压缩命令 |
| `compressoptions -9` | 最高压缩比（CPU 多消耗一点，磁盘省更多） |
| `compressext .gz` | 压缩文件后缀 |
| `dateext` | 归档名追加日期，而不是 `.1 .2 .3` |
| `dateformat -%Y%m%d` | 日期格式：`-20260511` |
| `dateyesterday` | 用昨天日期命名（凌晨执行时正好对应"昨天的日志"） |
| `olddir /workplace/logs/archive` | 把压缩后的归档移到子目录，保持主目录干净 |
| `createolddir 0755 root root` | `archive` 不存在时自动创建 |
| `su root root` | logrotate 操作时使用的用户/组 |

### 3.5 切割后的目录结构示例

```
/workplace/logs/
├── api-20260512083012.log          # 今天，程序正在写入
└── archive/
    ├── api-20260512083012.log-20260511.gz   # 昨天的归档
    ├── api-20260512083012.log-20260510.gz
    ├── api-20260512083012.log-20260509.gz
    └── ...                                    # 最多保留 180 份（约 6 个月）
```

> 注：`api-20260512083012.log` 这个文件名里的时间戳**不会变**（取决于程序启动时间），变的是后缀的 `-20260511` 这个日期标签。

---

## 4. 验证步骤

### 4.1 dry-run（不真正执行，只看会做什么）

```bash
sudo logrotate -d /etc/logrotate.d/gravitex-api
```

输出会描述"如果触发会做哪些动作"，无副作用，用于排查配置是否正确。

### 4.2 强制执行一次（验证效果）

```bash
sudo logrotate -fv /etc/logrotate.d/gravitex-api
```

执行后检查：

```bash
ls -lh /workplace/logs/
ls -lh /workplace/logs/archive/
```

应当看到：
- 主目录的 `api-*.log` 大小变得很小（被 truncate 了）。
- `archive/` 下出现了带日期后缀的 `.gz` 文件。
- 程序进程仍在正常运行，且仍在向那个 `api-*.log` 追加新日志。

### 4.3 验证 cron 是否会按天触发

logrotate 默认通过系统 `cron.daily` 调度，无需额外配置：

```bash
# 查看是否存在 cron.daily 调度脚本
ls -l /etc/cron.daily/logrotate

# 查看 logrotate 上次执行的状态
sudo cat /var/lib/logrotate/logrotate.status | grep gravitex-api
```

执行历史正常的话，应当能看到 `gravitex-api` 配置对应的最近执行日期。

---

## 5. 查看历史日志

由于历史日志已 gzip 压缩，**无需先解压**，直接用 `z` 系列命令查看：

```bash
# 查看
zcat /workplace/logs/archive/api-20260512083012.log-20260511.gz | less

# 翻页查看
zless /workplace/logs/archive/api-20260512083012.log-20260511.gz

# 搜索关键字
zgrep "ERROR" /workplace/logs/archive/api-20260512083012.log-20260511.gz

# 跨多个归档搜索
zgrep "ERROR" /workplace/logs/archive/api-*.gz
```

---

## 6. 可选：再做一层周/月级 tar 归档

只在"想把多天的 .gz 打成一个包，便于上传 OSS / 冷备份"时才需要。例子：每周日凌晨把上一周 7 天的 `.gz` 打成一个 tar，并删掉原 `.gz`。

```bash
sudo vim /etc/cron.weekly/gravitex-log-archive
```

```bash
#!/bin/bash
set -e

ARCHIVE_DIR=/workplace/logs/archive
WEEK_TAG=$(date -d "last week" +%Y-W%V)

cd "$ARCHIVE_DIR" || exit 0

# 找 7 天以前的 .gz，打成一个 tar.gz 后删除原文件
find . -maxdepth 1 -name 'api-*.log-*.gz' -mtime +7 -print0 \
  | tar --null -czf "weekly-${WEEK_TAG}.tar.gz" --remove-files -T -
```

```bash
sudo chmod +x /etc/cron.weekly/gravitex-log-archive
```

> 这一步**不是必须**的。日常排查日志只需要 `zgrep` 即可，加这一层 tar 反而会让查询时多一次解包。仅在有归档/上传需求时才使用。

---

## 7. 常见问题

### Q1：可以通过修改环境变量让程序自己按天切割吗？

不能。当前代码（`logger/logger.go`）**只**在日志条数超过 100 万行时才会切换文件，没有按日期切割的逻辑。日志目录是命令行参数 `--log-dir`，也不是环境变量。

如果一定要在程序内部完成按天切割 + 自动压缩，需要改代码引入 `gopkg.in/natefinch/lumberjack.v2` 或类似库，工作量较大且会与上游 new-api 仓库产生冲突，不推荐。**用 logrotate 是最合适的方案。**

### Q2：为什么必须用 `copytruncate`？

因为 Go 程序在启动时打开了 `api-*.log`（默认代码为 `oneapi-*.log`，参考 §0）文件并持有 fd，且没监听 SIGHUP 信号去重新打开文件。

- 默认的 logrotate 是 `rename + create`：把当前文件改名为 `xxx.log.1`，再新建 `xxx.log`。但 Go 进程持有的 fd 还指向那个被 rename 的旧 inode，新日志会写到一个文件名已经变成 `xxx.log.1` 的文件里——下次切割又会把它当成"已切过的旧文件"，逻辑混乱。
- `copytruncate` 是 `cp + truncate`：把当前文件复制一份到归档，然后用 `truncate(0)` 清空原文件。原文件的 inode 没变，Go 进程的 fd 仍然指向同一个 inode，会从偏移 0 重新开始追加写入。

代价：在 cp 和 truncate 之间的极短时间内可能丢失少量正在写入的日志（毫秒级，通常可忽略）。

### Q3：程序重启后会发生什么？

程序重启会以新的启动时间生成一个新的 `.log`，例如 `api-20260513090000.log`。原来的 `api-20260512083012.log` 会变成"无人写入"的旧文件。下次 logrotate 触发时，由于 `notifempty` 配置，空闲的旧文件不会被处理；但因为它**有内容且匹配通配符**，仍会被切割并归档到 `archive/`，结果是旧文件被压缩并按 `rotate 180` 规则保留——这正是我们想要的行为，无需额外处理。

### Q4：磁盘满了怎么应急？

```bash
# 立即手动跑一次切割 + 压缩
sudo logrotate -fv /etc/logrotate.d/gravitex-api

# 临时清掉 30 天以前的归档（按需调整天数）
sudo find /workplace/logs/archive -name 'api-*.gz' -mtime +30 -delete
```

如果空间长期不够，把配置里的 `rotate 180` 调小（例如 `rotate 90` 保留 3 个月、`rotate 30` 保留 1 个月），或者加个 cron 把超过 N 天的归档转存到对象存储。

---

## 8. 总结

- **唯一的代码改动：§0 把 `logger/logger.go` 的日志前缀 `oneapi-` 改成 `api-`**（一行字符串）；之后无需再改任何代码。如果不愿改代码，按 §0 方案 B 把本文配置中的 `api-*.log` 替换回 `oneapi-*.log` 也可以。
- **日志切割不需要环境变量**。日志目录用启动参数 `--log-dir /workplace/logs` 指定即可。
- **在 Linux 上配置 `/etc/logrotate.d/gravitex-api`**，使用 §3.3 的配置直接落地。
- 关键点是 `copytruncate`（兼容 Go fd）、`compress + .gz`（标准压缩）、`dateext + dateyesterday`（按昨天日期命名）、`olddir`（归档与运行目录隔离）、`rotate 180`（保留 6 个月）。
- 历史日志用 `zgrep` / `zless` 直接查询，无需解压。
- 单文件归档用 gzip 已最佳，仅在有归档/冷备需求时再加一层 tar 周打包（§6）。

---

## 6. 可选：再做一层周/月级 tar 归档

只在"想把多天的 .gz 打成一个包，便于上传 OSS / 冷备份"时才需要。例子：每周日凌晨把上一周 7 天的 `.gz` 打成一个 tar，并删掉原 `.gz`。

```bash
sudo vim /etc/cron.weekly/gravitex-log-archive
```

```bash
#!/bin/bash
set -e

ARCHIVE_DIR=/workplace/logs/archive
WEEK_TAG=$(date -d "last week" +%Y-W%V)

cd "$ARCHIVE_DIR" || exit 0

# 找 7 天以前的 .gz，打成一个 tar.gz 后删除原文件
find . -maxdepth 1 -name 'api-*.log-*.gz' -mtime +7 -print0 \
  | tar --null -czf "weekly-${WEEK_TAG}.tar.gz" --remove-files -T -
```

```bash
sudo chmod +x /etc/cron.weekly/gravitex-log-archive
```

> 这一步**不是必须**的。日常排查日志只需要 `zgrep` 即可，加这一层 tar 反而会让查询时多一次解包。仅在有归档/上传需求时才使用。

---

## 7. 常见问题

### Q1：可以通过修改环境变量让程序自己按天切割吗？

不能。当前代码（`logger/logger.go`）**只**在日志条数超过 100 万行时才会切换文件，没有按日期切割的逻辑。日志目录是命令行参数 `--log-dir`，也不是环境变量。

如果一定要在程序内部完成按天切割 + 自动压缩，需要改代码引入 `gopkg.in/natefinch/lumberjack.v2` 或类似库，工作量较大且会与上游 new-api 仓库产生冲突，不推荐。**用 logrotate 是最合适的方案。**

### Q2：为什么必须用 `copytruncate`？

因为 Go 程序在启动时打开了 `api-*.log`（默认代码为 `oneapi-*.log`，参考 §0）文件并持有 fd，且没监听 SIGHUP 信号去重新打开文件。

- 默认的 logrotate 是 `rename + create`：把当前文件改名为 `xxx.log.1`，再新建 `xxx.log`。但 Go 进程持有的 fd 还指向那个被 rename 的旧 inode，新日志会写到一个文件名已经变成 `xxx.log.1` 的文件里——下次切割又会把它当成"已切过的旧文件"，逻辑混乱。
- `copytruncate` 是 `cp + truncate`：把当前文件复制一份到归档，然后用 `truncate(0)` 清空原文件。原文件的 inode 没变，Go 进程的 fd 仍然指向同一个 inode，会从偏移 0 重新开始追加写入。

代价：在 cp 和 truncate 之间的极短时间内可能丢失少量正在写入的日志（毫秒级，通常可忽略）。

### Q3：程序重启后会发生什么？

程序重启会以新的启动时间生成一个新的 `.log`，例如 `api-20260513090000.log`。原来的 `api-20260512083012.log` 会变成"无人写入"的旧文件。下次 logrotate 触发时，由于 `notifempty` 配置，空闲的旧文件不会被处理；但因为它**有内容且匹配通配符**，仍会被切割并归档到 `archive/`，结果是旧文件被压缩并按 `rotate 180` 规则保留——这正是我们想要的行为，无需额外处理。

### Q4：磁盘满了怎么应急？

```bash
# 立即手动跑一次切割 + 压缩
sudo logrotate -fv /etc/logrotate.d/gravitex-api

# 临时清掉 30 天以前的归档（按需调整天数）
sudo find /workplace/logs/archive -name 'api-*.gz' -mtime +30 -delete
```

如果空间长期不够，把配置里的 `rotate 180` 调小（例如 `rotate 90` 保留 3 个月、`rotate 30` 保留 1 个月），或者加个 cron 把超过 N 天的归档转存到对象存储。

---

## 8. 总结

- **唯一的代码改动：§0 把 `logger/logger.go` 的日志前缀 `oneapi-` 改成 `api-`**（一行字符串）；之后无需再改任何代码。如果不愿改代码，按 §0 方案 B 把本文配置中的 `api-*.log` 替换回 `oneapi-*.log` 也可以。
- **日志切割不需要环境变量**。日志目录用启动参数 `--log-dir /workplace/logs` 指定即可。
- **在 Linux 上配置 `/etc/logrotate.d/gravitex-api`**，使用 §3.3 的配置直接落地。
- 关键点是 `copytruncate`（兼容 Go fd）、`compress + .gz`（标准压缩）、`dateext + dateyesterday`（按昨天日期命名）、`olddir`（归档与运行目录隔离）、`rotate 180`（保留 6 个月）。
- 历史日志用 `zgrep` / `zless` 直接查询，无需解压。
- 单文件归档用 gzip 已最佳，仅在有归档/冷备需求时再加一层 tar 周打包（§6）。
