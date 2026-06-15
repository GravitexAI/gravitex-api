# 邮箱与 Webhook 通知设置参考文档

本文只保留你当前需要迁移的部分：

1. 如何获取用户当前的邮件通知 / Webhook 通知设置
2. 如何保存用户的邮件通知 / Webhook 通知设置
3. 这些设置在前端和后端分别位于哪里

不包含后续“通知发送、额度判断、定时任务”等逻辑。

另外，考虑到你的另一个 Java 项目不在同一个文件夹，本文统一使用当前项目的绝对路径，方便你直接对照源码。

---

## 1. 结论

这套“邮件通知 / Webhook 通知设置”使用的是同一张用户主表。

主表模型定义在：

- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go`

关键字段：

- `Email`：用户账号邮箱  
  位置：`/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:31`

- `Setting`：用户个性化设置 JSON  
  位置：`/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:50`

`Setting` 这个 JSON 字段里包含：

- `notify_type`
- `notification_email`
- `webhook_url`
- `webhook_secret`
- 以及其他个人设置

设置对象定义在：

- `/Users/kocdehub/workspace/gocode/gravitex-api/dto/user_settings.go`
- 位置：`3-19` 行

其中和你当前迁移直接相关的字段是：

- `NotifyType`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/dto/user_settings.go:4`

- `WebhookUrl`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/dto/user_settings.go:6`

- `WebhookSecret`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/dto/user_settings.go:7`

- `NotificationEmail`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/dto/user_settings.go:8`

---

## 2. 前端部分

前端这里可以拆成两段看：

1. 获取当前用户已有设置
2. 保存用户修改后的设置

---

## 3. 前端获取设置

### 3.1 获取来源

前端不是单独调“通知设置接口”来读，而是从当前用户信息里的 `setting` 字段读取。

主要代码在：

- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx`

### 3.2 读取逻辑

方法位置：

- `useEffect(...)`
- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:182-203`

这段逻辑做了什么：

1. 判断 `userState?.user?.setting` 是否存在
2. `JSON.parse(userState.user.setting)`
3. 把 JSON 中的字段映射到前端状态 `notificationSettings`

和邮件 / Webhook 相关的字段映射是：

- `settings.notify_type -> warningType`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:186`

- `settings.webhook_url -> webhookUrl`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:188`

- `settings.webhook_secret -> webhookSecret`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:189`

- `settings.notification_email -> notificationEmail`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:190`

### 3.3 前端页面输入项

通知设置表单组件在：

- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx`

邮件输入框位置：

- `notificationSettings.warningType === 'email'` 分支
- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:489-503`

Webhook 输入框位置：

- `notificationSettings.warningType === 'webhook'` 分支
- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:506-544`

这里具体包含：

1. `notificationEmail`
   `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:491-497`

2. `webhookUrl`
   `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:509-531`

3. `webhookSecret`
   `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:534-543`

### 3.4 前端字段变更入口

表单变化统一通过：

- `handleFormChange(field, value)`
- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:227-230`

再往上交给：

- `handleNotificationSettingChange(type, value)`
- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:498-507`

这一步只是把表单值写回 React 状态，并不会立即提交后端。

---

## 4. 前端保存设置

### 4.1 保存方法

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx`

方法：

- `saveNotificationSettings()`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:509-535`

### 4.2 请求地址

保存时调用：

- `PUT /api/user/setting`

对应代码：

- `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:511`

### 4.3 提交字段

和邮件 / Webhook 迁移相关的字段是：

- `notify_type`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:512`

- `webhook_url`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:516`

- `webhook_secret`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:517`

- `notification_email`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:518`

你如果在 Java 项目里复刻前端，可直接参考这一层的请求体结构。

---

## 5. 后端部分

后端同样拆成两段：

1. 获取当前用户已有设置
2. 保存用户新的设置

---

## 6. 后端获取设置

### 6.1 获取当前用户接口

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go`

方法：

- `GetSelf(c *gin.Context)`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:374-425`

### 6.2 返回 `setting` 原始 JSON

在 `GetSelf(...)` 中，后端直接把用户表中的 `setting` 字段返回给前端：

- `"setting": user.Setting`
- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:414`

也就是说，前端读取通知设置的根数据源，不是单独的通知配置接口，而是：

- `GET /api/user/self`

返回数据中的：

- `data.setting`

### 6.3 用户设置反序列化方法

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go`

方法：

- `GetSetting()`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:81-89`

职责：

1. 读取 `user.Setting`
2. 把 JSON 字符串反序列化成 `dto.UserSetting`

如果你在 Java 里参考，可以对应成：

- `UserEntity.settingJson`
- `UserSettingDTO`
- `ObjectMapper.readValue(...)`

### 6.4 带缓存的设置获取方法

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go`

方法：

- `GetUserSetting(id int, fromDB bool)`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:861-896`

职责：

1. 优先从 Redis 读取 `setting`
2. 取不到再查数据库
3. 返回 `dto.UserSetting`

如果你后续在 Java 项目中也想让“用户设置读取”独立复用，这个方法很适合作为参考蓝图。

---

## 7. 后端保存设置

### 7.1 路由入口

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/router/api-router.go`

路由：

- `selfRoute.PUT("/setting", controller.UpdateUserSetting)`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/router/api-router.go:101`

这就是前端 `PUT /api/user/setting` 的后端入口。

### 7.2 请求结构

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go`

结构体：

- `type UpdateUserSettingRequest struct`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1138-1150`

与你迁移直接相关的字段：

- `QuotaWarningType string  json:"notify_type"`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1139`

- `WebhookUrl string json:"webhook_url,omitempty"`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1141`

- `WebhookSecret string json:"webhook_secret,omitempty"`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1142`

- `NotificationEmail string json:"notification_email,omitempty"`  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1143`

### 7.3 保存方法

文件：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go`

方法：

- `UpdateUserSetting(c *gin.Context)`

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1153-1293`

下面只保留邮件通知和 Webhook 通知相关逻辑。

### 7.4 通知类型校验

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1160-1164`

逻辑：

- `notify_type` 必须是项目支持的通知类型之一
- 你如果只迁移 Email/Webhook，可以在 Java 中只保留：
  - `email`
  - `webhook`

### 7.5 Webhook 参数校验

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1172-1183`

逻辑：

1. 当 `notify_type == webhook`
2. `webhook_url` 不能为空
3. `webhook_url` 必须是合法 URL

### 7.6 邮件参数校验

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1185-1192`

逻辑：

1. 当 `notify_type == email`
2. 如果传了 `notification_email`
3. 至少要包含 `@`

当前实现比较轻，只做了基础邮箱格式判断。

### 7.7 构建设置对象

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1246-1253`

这里创建了：

- `settings := dto.UserSetting{...}`

其中和你当前有关的核心字段是：

- `NotifyType`
- `QuotaWarningThreshold`

### 7.8 写入 Webhook 设置

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1255-1261`

逻辑：

1. 如果 `notify_type == webhook`
2. 写入 `settings.WebhookUrl`
3. 如果有密钥，再写入 `settings.WebhookSecret`

### 7.9 写入邮件通知设置

位置：

- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1263-1266`

逻辑：

1. 如果 `notify_type == email`
2. 且 `notification_email` 非空
3. 写入 `settings.NotificationEmail`

### 7.10 序列化并更新数据库

相关方法有两层。

第一层，序列化：

- `user.SetSetting(settings)`
- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1286`

实际序列化方法在：

- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go`
- `SetSetting(setting dto.UserSetting)`
- `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:92-99`

职责：

1. 把 `dto.UserSetting` 转成 JSON
2. 赋值给 `user.Setting`

第二层，持久化：

- `user.Update(false)`
- `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1287`

这里最终把新的 `setting` JSON 更新回用户表。

---

## 8. 你在 Java 项目里真正需要抄的最小集合

如果你只迁移“邮件通知设置 + Webhook 通知设置”，最小需要保留下面这些结构。

### 8.1 前端

获取：

- 从当前用户接口返回的 `setting` JSON 中解析：
  - `notify_type`
  - `notification_email`
  - `webhook_url`
  - `webhook_secret`

保存：

- 调用 `PUT /api/user/setting`
- 提交：
  - `notify_type`
  - `notification_email`
  - `webhook_url`
  - `webhook_secret`

### 8.2 后端

读取：

- 用户表里有一个 `setting_json` 或 `setting` 字段
- 反序列化成 `UserSettingDTO`

保存：

1. 绑定请求 DTO
2. 校验 `notify_type`
3. 校验 `notification_email`
4. 校验 `webhook_url`
5. 回写到用户 `setting` JSON

---

## 9. 精确代码索引

### 前端

- 读取当前设置  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:182-203`

- 保存设置方法  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:509-535`

- 邮件通知输入框  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:489-503`

- Webhook 输入框  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:506-544`

- 表单字段变化入口  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/personal/cards/NotificationSettings.jsx:227-230`

- React 状态更新入口  
  `/Users/kocdehub/workspace/gocode/gravitex-api/web/src/components/settings/PersonalSetting.jsx:498-507`

### 后端

- 当前用户信息接口  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:374-425`

- 返回 `setting` 原始 JSON  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:414`

- 保存接口 DTO  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1138-1150`

- 保存接口主逻辑  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1153-1293`

- Webhook 校验  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1172-1183`

- 邮件校验  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1185-1192`

- 写入 Webhook 设置  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1255-1261`

- 写入邮件设置  
  `/Users/kocdehub/workspace/gocode/gravitex-api/controller/user.go:1263-1266`

- 设置 JSON 反序列化  
  `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:81-89`

- 设置 JSON 序列化  
  `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:92-99`

- 带缓存读取用户设置  
  `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:861-896`

- 用户表主模型  
  `/Users/kocdehub/workspace/gocode/gravitex-api/model/user.go:23-55`

- 用户设置 DTO  
  `/Users/kocdehub/workspace/gocode/gravitex-api/dto/user_settings.go:3-19`

---

## 10. 迁移建议

如果你在另一个 Java 项目中只做“设置层迁移”，我建议直接照着下面的最小设计来：

1. 用户表保留一个 `setting_json` 字段
2. 定义 `UserSettingDTO`
3. DTO 先只保留：
   - `notifyType`
   - `notificationEmail`
   - `webhookUrl`
   - `webhookSecret`
4. 做一个 `GET /api/user/self`
   返回用户基础信息 + `setting`
5. 做一个 `PUT /api/user/setting`
   专门保存通知设置

这样最接近当前项目结构，也最容易迁移。
