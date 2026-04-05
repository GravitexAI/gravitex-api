## RuoYi 统一登录与 Go 服务鉴权适配方案

### 1. 背景与目标

- **现状**
  - `gravitex-api`：Go 实现的 LLM 网关/业务服务，目前有自己的一套登录与鉴权方案。
  - `Gravitex-API-End`：基于 RuoYi 的 Java 后端，拥有完整的用户、角色、菜单、租户等管理与鉴权能力。
- **目标**
  - 客户端、管理端 **统一使用 RuoYi 的登录**。
  - **由 RuoYi 统一签发 Token**，作为整个系统的唯一登录凭证。
  - Go 服务不再自建登录体系，而是 **适配 RuoYi 的 Token 鉴权方式**，实现对同一 Token 的校验与用户信息解析。
  - 新的前端（管理端 + 客户端）只需要处理 “**一次登录 + 一份 Token**”，即可同时访问 Java 和 Go 接口。

---

### 2. 总体架构设计

#### 2.1 角色划分

- **认证 / 授权中心（Auth Center）**
  - 由 `Gravitex-API-End`（RuoYi）承担。
  - 职责：
    - 用户登录 / 退出；
    - 签发 Token；
    - 校验 Token；
    - 提供用户、角色、权限、租户、菜单等管理能力。

- **业务服务 A：Java（RuoYi 自身模块）**
  - 继续使用 RuoYi 原有的权限体系（拦截器 + 注解 + 权限标识）。
  - 直接消费 RuoYi 自己签发的 Token。

- **业务服务 B：Go LLM 服务（`gravitex-api`）**
  - 将当前的鉴权逻辑改造为 **信任 RuoYi 签发的 Token**。
  - 通过解析 / 校验该 Token，拿到用户 ID、角色、企业（租户）等信息后，再做自身的业务权限控制。

- **前端（未来新的管理端 + 客户端）**
  - 登录统一调用 RuoYi 的登录接口获取 Token。
  - 将 Token 存储在 Cookie / LocalStorage。
  - 调用 Java / Go 任意接口时，**统一携带这份 Token**。

#### 2.2 流程总览

1. 前端调用 RuoYi 登录接口，获取 Token。
2. 前端缓存 Token，并在后续请求的 Header（或 Cookie）中统一携带。
3. Java（RuoYi）通过自身拦截器校验 Token。
4. Go 服务通过适配逻辑校验同一 Token，并从中解析出用户上下文。

#### 2.3 用户模型与数据映射（当前设计）

基于当前两张表结构：

- **RuoYi 用户表 `sys_user`（Java 主账户表）**
  - 主键：`user_id bigint`（用户ID）
  - 账号：`user_name varchar(30)`（用户账号）
  - 密码：`password varchar(100)`（密码）
  - 余额：`amount decimal(20,4)`（用户余额）
  - 其它：部门、邮箱、手机号、状态、创建人等字段

- **Go 用户表 `users`（API 扩展表）**
  - 主键：`id bigint auto_increment`
  - 账号：`username varchar(191)`（唯一）
  - 密码：`password longtext`
  - 角色：`role bigint`
  - 状态：`status bigint`
  - 配额相关：`quota / used_quota / request_count / aff_*`
  - 绑定信息：`email / github_id / discord_id / oidc_id / wechat_id / telegram_id / linux_do_id`
  - 其它：`group / setting / remark / stripe_customer` 等

**推荐的用户中心与映射关系：**

- **用户中心归属**
  - `sys_user` 为**唯一“主用户表 / 账号表”**：负责登录账号、密码、基础状态、企业/部门信息。
  - `users` 为 **Go 业务扩展表**：负责额度、渠道、模型、第三方绑定等 LLM 业务属性。

- **一一映射关系**
  - 在 `sys_user` 中新增字段（若尚未新增）：
    - `api_id bigint comment 'Go users.id 对应的 API 用户ID'`
  - 映射规则：
    - `sys_user.user_id`：Java 侧主键，权限体系使用。
    - `sys_user.api_id = users.id`：一对一关联 Go 侧用户。
    - `sys_user.user_name = users.username`：账号保持一致且唯一。
  - 这样：
    - Java 通过 `sys_user` 管理账号 / 权限；
    - Go 通过 `users` 管理配额 / 渠道 / 模型等，依赖 `api_id` 做关联。

- **账号生命周期规则（推荐）**
  - 新建用户、修改用户名 / 密码、禁用用户等**都从 Java 管理端操作**：
    - 创建用户时：
      - 先插入 `sys_user`；
      - 再在 `users` 中插入一条记录（`username`、`email` 等保持一致）；
      - 将 `users.id` 回填到 `sys_user.api_id`。
    - 修改用户名 / 状态时：
      - 同步更新 `sys_user` 与 `users` 中对应字段；
    - 删除 / 停用用户：
      - `sys_user.del_flag / status` 控制登录权限；
      - `users.deleted_at / status` 控制 Go 业务可用性。
  - 密码的“唯一真相”放在 `sys_user.password`：
    - 登录、改密都只认 `sys_user`；
    - `users.password` 可以保留历史兼容，或定期同步，最终可以视为废弃字段。

- **Token 中的用户字段（建议）**
  - RuoYi 在签发 Token 时，建议在 Payload 中加入：
    - `sysUserId`：对应 `sys_user.user_id`
    - `apiUserId`：对应 `sys_user.api_id`（即 `users.id`）
    - `username`：对应 `sys_user.user_name` / `users.username`
    - `roles`：RuoYi 角色信息
    - `tenantId` / 企业ID 等
  - 这样 Go 收到 Token 后：
    - 直接使用 `apiUserId` 作为主键查询 `users` 表，无需额外跨库查询；
    - 同时可以从 Token 中拿到高层的角色 / 企业信息用于业务判断。

- **登录后缓存合并用户信息（可选）**
  - 登录成功后，Java 可在服务端缓存一份“合并用户对象”：
    - 基本信息：来自 `sys_user`
    - 业务信息：通过 `api_id` 查 `users` 合并进来
  - 缓存键可以使用 Token 或 `sysUserId`，Go 如需也可通过内部接口获取这份合并信息。

---

### 3. 登录与 Token 流程

#### 3.1 登录流程（管理端 / 客户端通用）

1. 用户在前端页面输入账号、密码（以及验证码等）。
2. 前端向 RuoYi 登录接口发起请求，例如：
   - `POST /login`
   - 请求体（示例）：`{ username, password, code, uuid }`（以当前 RuoYi 实现为准）。
3. RuoYi 校验通过后，返回：
   - `token`：登录凭证（可以是随机字符串，也可以是 JWT 字符串）。
   - 以及可选的用户信息：`user` / `roles` / `permissions` / `tenantId` 等。
4. 前端将 `token` 缓存：
   - **推荐 1：HttpOnly Cookie**
     - 后端通过 `Set-Cookie` 写入，如：`AUTH_TOKEN=<token>; HttpOnly; Secure; Domain=.example.com; Path=/`。
   - **推荐 2：localStorage**
     - 前端保存 `token`，通过拦截器在每次请求时写入 Header。

#### 3.2 携带 Token 访问后端

- **Java 接口（RuoYi 模块）**
  - 请求头示例（根据当前实现选择其一）：
    - `Authorization: Bearer <token>`
    - 或 `token: <token>`
  - RuoYi 自身过滤器 / 拦截器会从请求中提取 Token，完成校验和用户上下文构造。

- **Go 接口（`gravitex-api`）**
  - 采用与 Java **完全一致** 的 Token 携带方式（同一个 Header 字段）。
  - 由 Go 中间件读取 Token，按与 RuoYi 一致的规则完成校验与解析。

---

### 4. Token 规范（建议方案）

> 实际 Token 规范以当前 RuoYi 代码为准。本节给出一个推荐的统一规范，供后续实现 / 重构时对齐。

#### 4.1 使用 JWT 作为统一 Token（推荐）

- **Header**
  - `alg`: 签名算法，如 `HS256` / `RS256`。
  - `typ`: `"JWT"`。

- **Payload（示例）**
  - `sub`: 用户 ID（userId）。
  - `username`: 登录账号。
  - `roles`: 角色列表（如 `["admin", "tenant_admin"]`）。
  - `tenantId`: 企业 / 租户 ID。
  - `exp`: 过期时间（Unix 时间戳）。
  - `iat`: 签发时间。
  - 其他业务需要的字段（如组织 ID、邮箱等）。

- **Signature**
  - 使用统一密钥签名：
    - 对称：`HS256 + SECRET_KEY`；
    - 非对称：`RS256 + 私钥 / 公钥`。
  - **RuoYi 与 Go 必须共享同一套密钥配置**（或共享公钥）。

#### 4.2 传输方式

- **Header 推荐写法**
  - 标准：`Authorization: Bearer <jwt_token>`
  - 若需兼容 RuoYi 现有实现，也可以统一使用：`token: <jwt_token>`。

- **Cookie（可选但推荐）**
  - 后端在登录成功后写入 Cookie：
    - `AUTH_TOKEN=<jwt_token>; HttpOnly; Secure; Domain=.your-domain.com; Path=/`
  - JS 不直接操作 Cookie，浏览器自动携带到 Go / Java 接口。

---

### 5. Go 服务（`gravitex-api`）改造点

#### 5.1 统一 Token 获取中间件

在 Go 服务中增加一个全局中间件 / 拦截器，职责：

1. 从请求中读取 Token：
   - 优先从 Header 读取（`Authorization` 或 `token`）；
   - 如有需要，可兼容从 Cookie 读取（如 `AUTH_TOKEN`）。
2. 若 Token 为空：
   - 对非匿名接口返回 `401 未登录`；
   - 对匿名接口放行（可维护一个白名单列表）。
3. 将原始 Token 字符串放入 `context`，传递给后续处理逻辑。

#### 5.2 Token 校验与用户上下文构建

根据 RuoYi 现有实现，可选择两种适配方式：

##### 方式 A：Go 直接校验 JWT（推荐）

- 前提：RuoYi 改造 / 支持使用 JWT 作为登录 Token。
- Go 服务：
  - 在配置中写入与 RuoYi 一致的 `SECRET_KEY` / 公钥。
  - 使用 JWT 库进行解析与验签。
  - 从 Payload 中读取 `userId` / `username` / `roles` / `tenantId` 等字段。
  - 构建本地的 `CurrentUser` 结构体，并写入 `context`，供 Handler 使用。

优点：
- 无需每次请求远程调用 RuoYi，性能好、耦合度低。

##### 方式 B：Go 通过内部接口向 RuoYi 校验 Token

- RuoYi 提供内部校验接口（对外不暴露），示例：
  - `POST /internal/auth/validate`
  - 请求：`{ token: "<token>" }`
  - 响应：`{ valid: true/false, userId, username, roles, tenantId, ... }`
- Go 中间件：
  - 收到前端 Token 后，调用该接口完成校验。
  - 将接口返回的用户信息写入 `context`。

优点：
- Go 不需要知道签名密钥，密钥只保留在 RuoYi。

缺点：
- 每个请求多一次跨服务调用，性能稍差。

> 可先用方式 B 快速打通链路，后续稳定后升级到方式 A。

#### 5.3 业务 Handler 中使用用户信息

- 在 Go 的 Handler 中不再依赖原有的自定义 Token 解析逻辑。
- 统一通过 `context` 获取由中间件写入的 `CurrentUser` 信息，例如：
  - `CurrentUser.ID`
  - `CurrentUser.Username`
  - `CurrentUser.Roles`
  - `CurrentUser.TenantID`
- 如需更细粒度的权限控制，可与 RuoYi 中的权限标识对齐，或在 Token / 校验接口中增加权限集合字段。

#### 5.4 兼容与迁移

- **过渡期**：
  - Go 同时支持 “旧 Token 方案（自建） + 新的 RuoYi Token 方案”；
  - 可通过配置开关或 Header 标记区分。
- **最终目标**：
  - 所有前端与客户端全部改用 RuoYi 登录；
  - 关闭 Go 原有登录接口与旧 Token 校验逻辑；
  - 统一只保留 “RuoYi Token + Go 适配” 的链路。

---

### 6. 前端（管理端 / 客户端）适配要点

#### 6.1 登录统一

- 登录入口：
  - 统一调用 RuoYi 登录接口（例如 `POST /login`）。
- 登录成功：
  - 前端从响应中拿到 `token`；
  - 保存到 Cookie（后端写）或 localStorage。

#### 6.2 请求统一携带 Token

- 配置全局 HTTP 拦截器（如 Axios 拦截器）：
  - 在每次请求前，从 Cookie / localStorage 中读取 Token；
  - 写入请求头，例如 `Authorization: Bearer <token>` 或 `token: <token>`。
- 无论调用的是 Java 接口还是 Go 接口，**完全一致的 Token 携带方式**。

#### 6.3 退出登录

- 调用 RuoYi 的退出登录接口（如 `POST /logout`）。
- 前端清理本地缓存的 Token（localStorage 等）。
- 后端（RuoYi）视实现将 Token 加入黑名单 / 删除 Redis 记录等。

---

### 7. 实施步骤建议

#### 阶段一：现状梳理与设计确认

1. 在 `Gravitex-API-End` 中确认当前登录与 Token 方案：
   - 登录接口路径及请求 / 响应结构；
   - Token 字段名、格式（字符串 / JWT）、存储与校验逻辑；
   - 请求头中使用的字段名（如 `Authorization` 或 `token`）。
2. 根据现状决定：
   - 是直接在现有 Token 结构上扩展；
   - 还是引入 / 升级为标准 JWT 方案。

#### 阶段二：RuoYi 侧改造 / 完善

1. 若采用 JWT：
   - 登录成功后签发 JWT，并在拦截器中改为校验 JWT。
   - 设计好 Payload 字段与过期策略。
2. 若采用内部校验接口模式：
   - 增加 `/internal/auth/validate` 之类的内部接口。
   - 明确入参 / 出参的用户字段定义。

#### 阶段三：Go 服务适配

1. 新增统一 Token 读取中间件。
2. 根据选择的模式（直接校验 JWT / 内部校验接口）实现 Token 校验逻辑。
3. 将用户信息封装为统一结构并写入 `context`。
4. 逐步修改各业务 Handler，统一从 `context` 获取当前用户信息。
5. 过渡期内保留旧逻辑，并通过配置开关控制。

#### 阶段四：前端改造（新的管理端 + 客户端）

1. 登录全部切到 RuoYi 接口。
2. 在前端统一封装 Token 存取和请求头注入逻辑。
3. 验证 Java / Go 全部核心接口在新登录体系下能正常访问。

#### 阶段五：收尾与清理

1. 确认所有前端流量已切到新登录体系。
2. 下线 Go 旧登录接口与 Token 解析逻辑。
3. 整理最终文档与配置（包括本文件），作为统一鉴权方案的长期参考。

---

### 8. 设计原因与不推荐方案对比

#### 8.1 为什么要这样设计？

- **统一的“用户真相”和登录入口**
  - 把 `sys_user` 定义为唯一“主用户表 / 账号表”，所有账号、密码、启用状态、企业/部门信息都以它为准；
  - 登录、改密、锁定账号都只通过 RuoYi 管理端进行，避免“两边都能改密码”的混乱。
- **保留 Go 现有业务能力，改动最小**
  - Go 侧 `users` 已经承载了额度（`quota`）、使用量（`used_quota`、`request_count`）、推广（`aff_*`）、第三方登录绑定等大量业务字段；
  - 把 `users` 定位为 `sys_user` 的“扩展表”，通过 `sys_user.api_id = users.id` 做一一映射，可以最大程度复用现有逻辑和数据。
- **前端只需要“一次登录 + 一份 Token”**
  - 不论访问 Go 接口还是 Java 接口，前端只需要：
    - 统一打登录在 RuoYi；
    - 拿一份 Token，后续所有请求都带上这一份 Token；
  - 前端逻辑简单，不容易出错（比如“Java 已登录，Go 还要再登录一次”的问题）。
- **易于维护和扩展**
  - 以后业务如果扩展：
    - Java 增加新模块：直接复用 RuoYi 用户、角色、菜单体系；
    - Go 增加新功能：只要基于 `apiUserId` 做自己的业务权限即可；
  - 清晰的职责边界：**Java 负责账户与权限体系，Go 负责 LLM 业务与配额。**

#### 8.2 这样设计带来的具体好处

- **好处 1：数据一致性好管**
  - 用户名、密码、状态等敏感字段只在 `sys_user` 一处修改；
  - `users` 中只保留与 LLM 业务强相关的字段，减少“两个地方都要改”的风险。
- **好处 2：权限体系不被破坏**
  - RuoYi 自带的用户 / 角色 / 菜单 / 部门 / 数据权限体系完全保留；
  - 不需要把 RuoYi 的内部实现强行改成适配 Go 的用户表，升级维护压力小。
- **好处 3：迁移成本可控**
  - 只要建立 `sys_user.api_id = users.id` 的映射，并在创建 / 修改用户时同步两个表；
  - 大部分 Go 业务代码依旧围绕 `users.id` 工作，只是“获取当前用户”的方式换成从 Token 中拿 `apiUserId`。
- **好处 4：跨服务调用清晰**
  - Token 中明确地带上 `sysUserId`、`apiUserId`、`username` 等字段；
  - Java 侧按 `sysUserId` 和角色 / 权限判断，Go 侧按 `apiUserId` 和配额 / 渠道判断，互不干扰。

#### 8.3 如果不这样设计会有什么问题？

1. **继续保持“两套登录 / 两套用户中心”**
   - 问题：
     - 前端可能需要分别登录 Java 和 Go，两边登录状态不一致；
     - 用户改密码时需要改两次（Java + Go），很容易出现一个成功一个失败的情况；
     - 无法简单地在一个后台统一管理“用户 + 权限 + 菜单 + 配额”。
   - 后果：
     - 用户体验差、运维成本高；
     - 将来要做 SSO、审计、风控时会非常痛苦。

2. **让 Java 完全依赖 Go 的 `users` 表，放弃 RuoYi 自己的 `sys_user` 体系**
   - 问题：
     - RuoYi 现有的用户、角色、菜单、部门、岗位等所有模块，都基于 `sys_user` / `sys_role` / `sys_menu` 这一套数据模型；
     - 强行改成依赖 `users`，意味着要重写大量 Mapper / Service / Controller，几乎是“推倒重来”；
     - 将来一旦升级 RuoYi 版本，所有这些自定义改动又要重新适配。
   - 后果：
     - 开发周期长、风险高；
     - 升级困难，长期维护成本非常大。

3. **不建立 `api_id` 映射，只靠 `username` 关联两边**
   - 问题：
     - 用户名如果将来允许修改，会出现“Java 改了，Go 忘记同步”的情况；
     - 无法处理历史数据中存在重复用户名或需要合并账号的复杂场景；
     - 以可变字段做主关联键，本身就不稳。
   - 后果：
     - 之后做数据修复、排查 bug 很费劲；
     - 需要到处写“靠用户名模糊查 / 再手动修”的代码。

4. **Token 里不带 `apiUserId`，每次 Go 都去查 / 关联**
   - 问题：
     - Go 每次鉴权都要：
       - 先通过 Token 里的 `username` / `sysUserId` 间接查到 `sys_user`；
       - 再通过 `api_id` 找到 `users`；
     - 跨服务 / 跨库查询次数变多，链路复杂。
   - 后果：
     - 性能和可用性受影响（任一节点异常都会拖垮整条链路）；
     - 故障排查难度加大。

综上，这个设计的核心思想是：

- **一处“主真相”（`sys_user`） + 一处“业务扩展”（`users`）**；
- **一份 Token，同时服务 Java 和 Go 两侧**；
- **通过 `api_id` 把新旧系统、Java / Go 的职责隔离清楚，同时又打通数据与权限链路。**

---

### 9. 后续可补充内容（待与实现细节对齐）

在确认或实现完成以下信息后，可继续补充本文件：

- 实际使用的 RuoYi 登录接口请求 / 响应 JSON 示例；
- 最终确定的 Token 请求头字段名（`Authorization` / `token` 等）；
- RuoYi JWT 的具体 Payload 字段约定；
- Go 中间件示例代码片段；
- 内部 Token 校验接口（若启用）的请求 / 响应示例。

这样团队在阅读本文件时，可以直接看到 **从“登录 → Token → Java / Go 鉴权”的完整链路**，以及背后的设计理由和取舍，降低心智负担和后续接手成本。

