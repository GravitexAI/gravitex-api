# 允许用户欠费继续使用 —— 设计文档

- 日期：2026-07-03
- 作者：caihongzhan
- 状态：待评审
- 分支：main-alpha-merge

## 一、目标

给管理员一个能力：把指定用户标记为「白名单」，用户在钱包额度 `<= 0` 或即将被扣成负数时，扣费流程继续放行，不再返回 403 "用户额度不足"。

## 二、范围与非目标

### 范围
- users 表用户级别的 `allow_negative_balance` 授权开关（管理员开启，用户不可自改）
- 覆盖钱包扣费路径的全部 6 处欠费预检查拦截点，全部放行
- 管理端 web/classic `/console/user` 用户编辑弹窗支持编辑该开关
- 无审计、无日志标记（用户视角完全无感知）

### 非目标（本次不做）
- 授信额度上限（无限欠费）
- 授信到期时间
- 订阅本身额度不足的放行（`billing_session.go:222/246` 是订阅未配置错误，与钱包无关）
- Java 后端联动（本次仅 Go 后端 + 管理端）
- 前端 web/default 侧的编辑入口（本次只做 web/classic）

## 三、数据模型

复用 `dto.UserSetting` JSON 字段（存在 `users.setting text` 列）。**不新增数据库列**，避免 DB 迁移。

### 3.1 `dto/user_settings.go`

在 `UserSetting` 结构体新增一个字段：

```go
type UserSetting struct {
    // ... 现有 13 个字段保持不变
    AllowNegativeBalance bool `json:"allow_negative_balance,omitempty"` // 管理员授权：允许该用户欠费继续使用（钱包额度 <= 0 时不拦截）
}
```

### 3.2 关键性质

- Setting JSON 由 `common.Unmarshal` 反序列化；缺省 bool 零值为 `false`，行为与现网一致
- Setting 已经在 `UserBase.Setting` 缓存中；`UserBase.WriteContext` 已把反序列化后的 `dto.UserSetting` 写入 `constant.ContextKeyUserSetting`
- 拦截点读值零额外开销

## 四、后端拦截点放行

统一模式：
```go
setting, _ := c.Get(string(constant.ContextKeyUserSetting))
allowNegative := false
if s, ok := setting.(dto.UserSetting); ok {
    allowNegative = s.AllowNegativeBalance
}

// 原判断：if userQuota <= 0 { return 403 }
if userQuota <= 0 && !allowNegative {
    return 403 ...
}
```

**建议封装成一个 helper**（避免 6 处各写一遍类型断言）：

```go
// service/pre_consume_quota.go (新增)
func isNegativeBalanceAllowed(c *gin.Context) bool {
    v, exists := c.Get(string(constant.ContextKeyUserSetting))
    if !exists {
        return false
    }
    s, ok := v.(dto.UserSetting)
    if !ok {
        return false
    }
    return s.AllowNegativeBalance
}
```

### 4.1 六个改动点

| # | 文件 | 现有判断 | 改法 |
|---|------|--------|-----|
| 1 | `service/pre_consume_quota.go:38` | `if userQuota <= 0 {` | `if userQuota <= 0 && !isNegativeBalanceAllowed(c) {` |
| 2 | `service/pre_consume_quota.go:41` | `if userQuota-preConsumedQuota < 0 {` | `if userQuota-preConsumedQuota < 0 && !isNegativeBalanceAllowed(c) {` |
| 3 | `service/billing_session.go:366` | `if userQuota <= 0 {` | 同 #1 |
| 4 | `service/billing_session.go:372` | `if userQuota-preConsumedQuota < 0 {` | 同 #2 |
| 5 | `service/quota.go:155` (PreWssConsumeQuota) | `if userQuota < quota {` | `if userQuota < quota && !isNegativeBalanceAllowed(ctx) {` |
| 6 | `relay/relay_task.go:244` | `if userQuota <= 0 {` | 同 #1（用 helper 需从 service 包引入或复用） |

**不改动**：
- `service/pre_consume_quota.go:45-64` TrustQuota 分支：欠费用户天然 `userQuota <= 0 < trustQuota`，走「不信任、需要预扣费」分支，扣费能顺利完成把 quota 打成负数
- `service/billing_session.go:222/246`：订阅本身额度不足，与钱包无关

### 4.2 允许扣成负数的正确性

- `users.quota` 字段类型 `int`（有符号），支持负数
- `IncreaseUserQuota` 校验 `< 0` 报错（`model/user.go:940`），但 `DecreaseUserQuota` 通过 `gorm.Expr("quota - ?", quota)` 减操作允许结果为负
- Redis 缓存的 `Quota` 字段用 `RedisHIncrBy` 原子减，也允许负数

## 五、后端管理接口

### 5.1 `controller/user.go` `UpdateUser` (line 604，管理员修改任意用户)

**当前实现**：显式白名单结构体 `updateUserRequest` 绑定字段，然后手工构造 `updatedUser := model.User{...}`。

**需要新增**：从请求解出 `setting` 并合并到 originUser 的 Setting 上。

```go
type updateUserRequest struct {
    // ... 现有字段
    Setting *dto.UserSetting `json:"setting,omitempty"` // 新增：允许 admin 更新用户 setting
}
```

处理逻辑（放在 `updatedUser.Edit(updatePassword)` 之前）：
```go
if req.Setting != nil {
    // 只合并 admin 应当能改的字段（当前只有 AllowNegativeBalance）
    current := originUser.GetSetting()
    current.AllowNegativeBalance = req.Setting.AllowNegativeBalance
    originUser.SetSetting(current)
    updatedUser.Setting = originUser.Setting
}
```

**关键：只合并 `AllowNegativeBalance`**，不允许 admin 通过这个接口改用户的通知/webhook 等偏好（保留用户自主偏好边界）。

### 5.2 `controller/user.go` `UpdateUserSetting` (line 1369，用户自改偏好)

**当前实现** (line 1463)：整个 setting 全量覆盖，仅显式保留 `UpstreamModelUpdateNotifyEnabled`。

**必须修改**：显式保留 `AllowNegativeBalance`（否则用户能自清 admin 授权 → 安全漏洞）。

```go
existingSettings := user.GetSetting()
upstreamModelUpdateNotifyEnabled := existingSettings.UpstreamModelUpdateNotifyEnabled
allowNegativeBalance := existingSettings.AllowNegativeBalance  // ← 新增

settings := dto.UserSetting{
    // ... 现有字段
    UpstreamModelUpdateNotifyEnabled: upstreamModelUpdateNotifyEnabled,
    AllowNegativeBalance:             allowNegativeBalance,  // ← 新增
}
```

### 5.3 `controller/user.go` `UpdateSelf` (line 848)

**无需修改**：只更新 `SidebarModules` 和 `Language`，然后 `SetSetting(currentSetting)`，`currentSetting` 是通过 `user.GetSetting()` 拿到的原 setting，`AllowNegativeBalance` 天然保留。

## 六、前端 web/classic

### 6.1 `EditUserModal.jsx`

**位置**：`web/classic/src/components/table/users/modals/EditUserModal.jsx`

**改动**：
1. `initFormData` 加 `setting: { allow_negative_balance: false }`
2. 初始化时从后端返回的 `user.setting`（JSON 字符串）解析并注入表单
3. 表单加一个 Semi Design `Switch`：
   - Label：`允许透支使用（欠费不拦截）`
   - 值绑定到 `formData.setting.allow_negative_balance`
   - 附带提示文字：`⚠ 开启后用户欠费也可继续调用，请谨慎使用`
4. 提交时序列化 setting 一起发送：
   ```js
   const payload = {
       ...formData,
       setting: {
           allow_negative_balance: !!formData.setting?.allow_negative_balance
       },
   };
   ```
5. 现有 `delete payload.quota` 保留，避免误发额度覆盖请求

### 6.2 后端返回

`UpdateUser` 的 GET 用户信息接口已经返回 `user.setting`（JSON 字符串），前端解析即可。若响应结构里没有，需要在 `controller/user.go` `GetUser` 里补上 `setting` 字段。

## 七、边界与失败模式

| 场景 | 行为 |
|-----|-----|
| Redis 缓存 miss，回退 DB 读 | UserBase 从 DB 组装（`GetUserCache`），Setting 依然通过 `WriteContext` 注入 Context |
| Setting JSON 反序列化失败 | `GetSetting` 返回零值 UserSetting，`AllowNegativeBalance=false`，行为 = 未开启（安全默认） |
| Context 里没有 UserSetting | Helper 返回 false，行为 = 未开启 |
| 用户恶意大量欠费 | 本次不做上限管控（B 方案定位就是 VIP 白名单，管理员开启前应做尽职调查） |
| 关闭开关后仍是负数 | 后续新请求继续被 `userQuota <= 0` 拦截，直到 admin 手动充值 |

## 八、测试计划

### 8.1 后端单测（`service/pre_consume_quota_test.go` 新建或补充）
- 表驱动测试 `isNegativeBalanceAllowed`：Context 无值 / 类型错 / bool=false / bool=true → 各返回预期值
- `PreConsumeQuota` 分支测试：
  - `userQuota=0, allowNegative=false` → 返回 403
  - `userQuota=0, allowNegative=true` → 返回 nil（放行）
  - `userQuota=100, preConsume=200, allowNegative=true` → 放行 + 扣成 -100

### 8.2 手动验证（QA）
1. Admin 后台 `/console/user` 编辑某测试账号，开启 "允许透支使用"
2. 用该账号的 token 发起 chat/completions 请求：
   - 额度充足时：正常返回
   - 手动把 quota 改到 0：请求仍能通过、扣完后 quota 为负
   - 关闭开关后再请求：返回 403
3. 用普通账号（未开启）测试：quota=0 → 403，行为不变
4. 用户自己在 `/console/setting` 保存偏好（如通知类型）：验证 `AllowNegativeBalance` 未被清零

## 九、不联动其他模块

- Java 后端（`Gravitex-API-End`）不涉及：Go 后端全权处理钱包扣费
- web/default 不涉及：本次仅 web/classic 管理端
- Redis / MySQL / PostgreSQL / SQLite：AutoMigrate 无变化（没加列）；三 DB 一致行为

## 十、回滚

- Setting 字段可保留，向后兼容（缺省 false = 未开启）
- 6 处拦截改回 `&& !isNegativeBalanceAllowed(c)` 之前的写法即可完全回滚
- 前端 Switch 移除即可

## 十一、Git 提交计划

按项目规范（`feat:chz`）：
1. `feat:chz 增加 allow_negative_balance 用户欠费白名单：dto/user_settings + 6 处扣费拦截放行`
2. `feat:chz UpdateUser/UpdateUserSetting 支持 allow_negative_balance 且用户不可自清`
3. `feat:chz web/classic 用户编辑弹窗增加「允许透支使用」开关`
