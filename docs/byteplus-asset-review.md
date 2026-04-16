# BytePlus 官方素材库 API 集成 — 场景 Review

## 一、变更概览

| 文件 | 变更 |
|---|---|
| `go.mod` | 新增 `byteplus-go-sdk-v2 v1.0.59` |
| `dto/channel_settings.go` | 新增 4 个 AK/SK 配置字段 + 3 个 helper 方法 |
| `model/user_asset_group.go` | **新文件** — UserAssetGroup 模型 + CRUD |
| `model/user_asset.go` | 新增 `GroupId` 字段、`GetUserAssetsByGroupId` 查询 |
| `model/main.go` | AutoMigrate 增加 `UserAssetGroup` |
| `service/byteplus_asset.go` | **新文件** — BytePlus SDK 封装（7 个公开函数） |
| `controller/asset.go` | 双模式分发（Uptoken/BytePlus）；ListAssets/GetAsset/DeleteAsset 增加 BytePlus 分支 |
| `controller/asset_group.go` | **新文件** — 素材组 CRUD 控制器 |
| `router/video-router.go` | `UploadAsset` → `CreateAsset`；新增 3 条 asset-groups 路由 |
| `web/.../EditChannelModal.jsx` | 管理后台：渠道编辑增加 BytePlus AK/SK/Region/ProjectName 配置区域（仅 type=54 豆包视频） |

---

## 二、API 路由总览

| Method | Path | Handler | 说明 |
|---|---|---|---|
| POST | `/v1/assets` | `CreateAsset` | 双模式：`multipart/form-data` → Uptoken；`application/json` → BytePlus |
| GET | `/v1/assets` | `ListAssets` | 列出用户所有素材，支持 `?group_id=xxx` 过滤 |
| GET | `/v1/assets/:virtual_id` | `GetAsset` | 查看单个素材，刷新上游状态 |
| DELETE | `/v1/assets/:virtual_id` | `DeleteAsset` | 删除素材（上游+本地） |
| POST | `/v1/asset-groups` | `CreateAssetGroup` | 创建素材组（仅 BytePlus） |
| GET | `/v1/asset-groups` | `ListAssetGroups` | 列出素材组 |
| DELETE | `/v1/asset-groups/:group_id` | `DeleteAssetGroup` | 删除素材组（级联删除组内素材） |

---

## 三、用户场景走查

### 场景 1：管理员配置 BytePlus 渠道

**操作路径**：管理后台 → 渠道管理 → 编辑渠道（type=45 或 54）

**步骤**：
1. 打开渠道编辑弹窗
2. 看到"BytePlus 素材库配置"区域（仅 type=45/54 可见）
3. 填入 Access Key、Secret Key
4. 选择 Region（`ap-southeast-1` / `cn-north-1`）
5. 填写 Project Name（默认 `default`）
6. 保存

**数据流**：前端 → `handleChannelOtherSettingsChange` → `inputs.settings` JSON → 后端 `ChannelOtherSettings` → `HasByteplusAssetConfig()` 判断

**验证点**：保存后重新打开渠道，4 个字段应正确回显。

---

### 场景 2：用户通过 API 创建素材组

**请求**：
```bash
curl -X POST https://api.example.com/v1/asset-groups \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"name": "角色A", "description": "测试角色"}'
```

**流程**：
1. `TokenAuth` 验证 Token
2. 无指定 `channel_id` → `getByteplusEnabledChannels(group)` 自动选择第一个有 AK/SK 的渠道
3. **限额检查**：`CountUserAssetGroupsByChannel(userId, channelId)` ≥ 5 → 返回 403
4. `ByteplusCreateAssetGroup(cfg, name, desc)` 调用 BytePlus SDK
5. 本地 `InsertUserAssetGroup` 保存映射
6. 返回 `group_id`

**响应**：
```json
{"group_id": "group-20260416xxx", "name": "角色A", "description": "测试角色", "channel_id": 123}
```

---

### 场景 3：用户通过 API 上传素材（BytePlus 模式，URL 方式）

**请求**：
```bash
curl -X POST https://api.example.com/v1/assets \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{"url": "https://cdn.example.com/face.jpg", "group_id": "group-xxx", "asset_type": "Image"}'
```

**流程**：
1. `CreateAsset` 检测 Content-Type → 非 multipart → `createAssetByteplus`
2. 校验 group 归属：`GetUserAssetGroupByUserIdAndGroupId(userId, groupId)`
3. 通过 group 的 `ChannelId` 找到渠道 → `isByteplusAssetChannel` 校验
4. `ByteplusCreateAsset(cfg, groupId, url, "Image", name)`
5. 本地 `InsertUserAsset`，`Status = "pending"`
6. 返回 `virtual_id` 和 `asset_url`

**响应**：
```json
{"virtual_id": "asset-20260416xxx", "asset_url": "asset://asset-20260416xxx", "group_id": "group-xxx", "status": "pending"}
```

---

### 场景 4：用户通过 API 上传素材（Uptoken 模式，文件上传）

**请求**：
```bash
curl -X POST https://api.example.com/v1/assets \
  -H "Authorization: Bearer sk-xxx" \
  -F "file=@face.jpg"
```

**流程**：
1. `CreateAsset` 检测 Content-Type → `multipart/form-data` → `uploadAssetUptoken`
2. 从 `getAssetSupportedChannels(group)` 选择第一个可用渠道
3. 代理上传到 Uptoken 上游 `POST /v1/assets`
4. 本地保存 `UserAsset`（`GroupId` 为空）
5. 透传上游响应

**向后兼容**：与改动前行为完全一致。

---

### 场景 5：用户查看素材列表（混合模式）

**请求**：
```bash
# 全部素材
curl https://api.example.com/v1/assets -H "Authorization: Bearer sk-xxx"

# 按组过滤
curl "https://api.example.com/v1/assets?group_id=group-xxx" -H "Authorization: Bearer sk-xxx"
```

**流程**：
1. 获取 Token group 可访问的所有 asset-supporting 渠道
2. 查询 `user_assets` 表（按 `channel_id IN ?`，或按 `group_id = ?`）
3. 遍历 status 为 `pending`/`ready` 的素材，按渠道类型刷新：
   - BytePlus 渠道 → `ByteplusGetAsset(cfg, virtualId)` → 状态映射
   - Uptoken 渠道 → REST `GET /v1/assets/:id` → 直接比对
4. 更新本地状态
5. 返回带 `space_label` 的列表

---

### 场景 6：用户在视频生成中引用素材

**请求**：
```bash
curl -X POST https://api.example.com/v1/video/generations \
  -H "Authorization: Bearer sk-xxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "seedance-2-0-v2p0",
    "content": [
      {"type": "image_url", "image_url": {"url": "asset://asset-20260416xxx"}},
      {"type": "text", "text": "跳舞"}
    ]
  }'
```

**流程**：
1. `AssetResolveChannel` 中间件扫描 `content` 中的 `asset://` 引用
2. `GetAssetChannelIdByVirtualIds(userId, virtualIds)` 校验归属 + 获取 `channel_id`
3. 校验所有素材属于同一渠道
4. 设置 `specific_channel_id` 强制路由到该渠道
5. `Distribute` 中间件基于此分发到正确渠道
6. 上游 BytePlus/Uptoken 接收到 `asset://xxx` 引用，完成视频生成

---

### 场景 7：用户删除素材组

**请求**：
```bash
curl -X DELETE https://api.example.com/v1/asset-groups/group-xxx \
  -H "Authorization: Bearer sk-xxx"
```

**流程**：
1. 校验 group 归属
2. `ByteplusDeleteAssetGroup(cfg, groupId)` 上游删除（失败也继续）
3. `DeleteUserAssetGroupByGroupId(groupId)` 本地删除：先删 `user_assets` 中 `group_id = ?` 的记录，再删 `user_asset_groups` 记录

---

### 场景 8：用户删除单个素材

**请求**：
```bash
curl -X DELETE https://api.example.com/v1/assets/asset-xxx \
  -H "Authorization: Bearer sk-xxx"
```

**流程**：
1. 校验素材归属
2. 判断渠道类型：
   - BytePlus → `ByteplusDeleteAsset(cfg, virtualId)`，失败也继续
   - Uptoken → REST `DELETE /v1/assets/:id`，失败也继续（不再中断）
3. 本地 `DeleteUserAssetByVirtualId` 清理记录
4. 返回 `{"deleted": true, "virtual_id": "..."}`

---

## 四、资源限额

### 网关侧限制

| 维度 | 限制 | 常量 | 说明 |
|---|---|---|---|
| 素材组数量 | **5 个/用户/渠道** | `MaxAssetGroupsPerUserPerChannel` | 同一用户在同一渠道上最多创建 5 个素材组 |

超过限制时返回：
```json
HTTP 403
{"error": {"message": "Asset group limit reached (5 per channel)", "type": "invalid_request_error"}}
```

### BytePlus 上游 QPS 限制（文档标注）

| API | 限制 |
|---|---|
| CreateAssetGroup | 10 QPS |
| CreateAsset | 300 QPM |
| ListAssetGroups | 10 QPS |
| ListAssets | 10 QPS |
| GetAsset | 100 QPS |
| DeleteAsset | 10 QPS |
| DeleteAssetGroup | 5 QPS |

> BytePlus 文档未标注每账号素材组数量上限。当前网关设置 5 个/用户/渠道 作为防御性限制。
> 如需调整，修改 `controller/asset_group.go` 中的 `MaxAssetGroupsPerUserPerChannel` 常量。

### 管理后台配置范围

BytePlus 素材库 AK/SK 配置**仅在豆包视频渠道（type=54）**的编辑弹窗中显示。VolcEngine 渠道（type=45）不展示此配置。

---

## 五、发现的问题与修复

### 问题 1：删除素材组未级联删除组内素材 [已修复]

**问题**：`DeleteUserAssetGroupByGroupId` 只删除 `user_asset_groups` 表记录，组内的 `user_assets` 记录变成孤儿数据。

**影响**：用户删除素材组后，组内素材仍然存在于数据库中，`ListAssets` 会返回这些无法管理的孤儿素材。

**修复**：
```go
// model/user_asset_group.go
func DeleteUserAssetGroupByGroupId(groupId string) error {
    // 先删除组内素材
    if err := DB.Where("group_id = ?", groupId).Delete(&UserAsset{}).Error; err != nil {
        return err
    }
    return DB.Where("group_id = ?", groupId).Delete(&UserAssetGroup{}).Error
}
```

---

### 问题 2：ListAssets 缺少 group_id 过滤 [已修复]

**问题**：BytePlus 模式下素材按组管理，但 `GET /v1/assets` 只能返回全部素材，无法按组过滤。前端素材组内的素材列表需要此能力。

**修复**：增加 `?group_id=xxx` 查询参数支持。

```go
groupIdFilter := c.Query("group_id")
if groupIdFilter != "" {
    assets, err = model.GetUserAssetsByGroupId(userId, groupIdFilter)
} else {
    assets, err = model.GetUserAssetsByUserIdAndChannelIds(userId, channelIds)
}
```

---

### 问题 3：Uptoken DeleteAsset 上游失败时阻止本地清理 [已修复]

**问题**：Uptoken 删除路径中，如果 HTTP client 创建失败或请求构建失败，函数直接 `return`，跳过了本地 DB 删除。而 BytePlus 路径遇到上游错误后会继续删除本地记录。

**影响**：上游不可用时，用户无法删除本地素材记录，素材一直卡在列表里。

**修复**：将 Uptoken 删除路径改为"尽力删除上游，无论成败都删除本地"，与 BytePlus 路径行为一致。

---

### 问题 4 [待观察]：ListAssets 大量 pending 素材的 N+1 查询

**问题**：`ListAssets` 遍历所有 `pending`/`ready` 状态的素材，逐个调用 BytePlus SDK `GetAsset` 刷新状态。如果用户有大量 pending 素材，会触发 N 次 SDK 调用，响应时间线性增长。

**当前状态**：未修复。原因是当前素材量级较小（通常 < 20），实际影响有限。

**后续建议**：
- 短期：限制每次刷新的最大数量（如最多刷新 10 个）
- 中期：改用 `ByteplusListAssets` 批量获取指定组的素材状态
- 长期：后台 worker 定期轮询刷新状态，`ListAssets` 只读本地

---

### 问题 5 [待观察]：AssetResolveChannel 校验消息措辞不准确

**现象**：中间件错误消息写的是"素材必须来自同一素材组"，但实际校验逻辑是"素材必须来自同一渠道（channel_id）"。

**原因**：同一渠道下可以有多个素材组，同组素材自然同渠道，但跨组同渠道也应该合法。当前逻辑是正确的（校验 channel_id），只是消息不够准确。

**建议**：修改消息为"同一请求中的素材必须来自同一渠道"，或不修改（因为同一素材组是用户更容易理解的概念）。

---

### 问题 6 [设计说明]：admin 前端 SK 明文回显

**现象**：管理后台编辑渠道时，BytePlus Secret Key 以 `mode='password'` 输入框展示，但实际值存储在 `settings` JSON 中。重新打开编辑时，SK 会从 JSON 中回读并可以通过浏览器开发者工具看到。

**说明**：这与现有的 AWS SecretAccessKey、Vertex JSON 密钥等行为一致。`Channel.Key`（主密钥）也是同样的处理方式。这是管理后台的已有设计模式，不构成新增安全风险。

---

## 六、双模式兼容矩阵

| 操作 | Uptoken 渠道 | BytePlus 渠道 | 混合（同时存在） |
|---|---|---|---|
| POST /v1/assets (multipart) | 正常上传 | N/A（走 JSON 路径） | 按 Content-Type 分发 |
| POST /v1/assets (JSON) | N/A（走 multipart 路径） | URL 创建 | 按 Content-Type 分发 |
| GET /v1/assets | 返回 uptoken 素材 | 返回 byteplus 素材 | 合并返回，各自刷新 |
| GET /v1/assets?group_id= | 返回空（uptoken 素材无 group_id） | 按组过滤 | 按组过滤 |
| GET /v1/assets/:id | REST 刷新 | SDK 刷新 | 按渠道类型分发 |
| DELETE /v1/assets/:id | REST 删除+本地清理 | SDK 删除+本地清理 | 按渠道类型分发 |
| POST /v1/asset-groups | 不适用 | 创建 | 仅 BytePlus 渠道 |
| GET /v1/asset-groups | 返回空 | 返回组列表 | 仅返回 BytePlus 渠道的组 |
| DELETE /v1/asset-groups/:id | 不适用 | 删除+级联 | 仅 BytePlus 渠道 |
| 视频生成引用 asset:// | 路由到 uptoken 渠道 | 路由到 byteplus 渠道 | AssetResolveChannel 强制同渠道 |

---

## 七、状态映射参考

| BytePlus 状态 | 内部状态 (UserAsset.Status) | Uptoken 状态 | 说明 |
|---|---|---|---|
| Processing | `pending` | pending | 素材处理中 |
| Active | `active` | active | 可用于视频生成 |
| Failed | `failed` | — | 处理失败 |
| — | — | ready | Uptoken 独有的中间状态 |
