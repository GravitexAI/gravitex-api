# Gravitex 客户端 OSS 文件上传开发文档

## 一、背景与现状

### 1.1 架构概述

Gravitex 使用**后端代理上传**方式：前端将文件（或 URL/Base64）发送给 **Java 后端**（Gravitex-API-End），由 Java 后端调用 OSS SDK 上传到对象存储，返回公开访问 URL 和 ossId。

```
前端 (gravitex-api-cli)
    │ POST /resource/oss/upload（multipart）
    │ POST /resource/oss/uploadByBase64（JSON）
    │ POST /resource/oss/uploadByImageUrl（JSON）
    │ POST /resource/oss/uploadByVideoUrl（JSON）
    ▼
Java 后端 (Gravitex-API-End)
    │ AWS SDK v2 S3-compatible
    ▼
对象存储 (Aliyun OSS / Tencent COS / Minio / AWS S3 均支持)
```

Go 后端（gravitex-api）不直接上传 OSS，在视频任务完成时通过环境变量 `OSS_BASE64_ENDPOINT` 调用 Java 后端的 `/resource/oss/uploadByBase64` 接口。

### 1.2 当前问题

`gravitex-api-cli/services/uploadService.ts` 是**纯占位实现**——所有方法均 `throw new Error('未实现')`。

Chat 页已经在约 5 处调用了 uploadService（生成图片存 OSS、生成视频存 OSS 等），当前全部会抛出异常导致功能不可用。

**参考实现**：`nebula-lab-react/services/uploadService.ts`——与 Gravitex 完全相同的后端接口、相同的 `request` 客户端封装，可以直接参照复制。

---

## 二、Java 后端 API 接口

所有接口均位于 `Gravitex-API-End`，路径前缀 `/resource/oss`。

标注 `@SaIgnore` 的接口**不需要登录 token**（供 Go 后端等内部服务使用）；标注需认证的接口需携带 `Authorization: Bearer <token>` 请求头。

### 2.1 `POST /resource/oss/upload`（需认证）

上传本地文件（multipart/form-data）。

**请求**：

```
Content-Type: multipart/form-data
field: file（File 对象）
```

**响应**：

```json
{
  "code": 200,
  "msg": "操作成功",
  "data": {
    "url": "https://cdn.example.com/2024/01/01/xxx.jpg",
    "fileName": "xxx.jpg",
    "ossId": "123456"
  }
}
```

**使用场景**：用户主动上传文件（如头像、附件）。

---

### 2.2 `POST /resource/oss/uploadThumbnail`（@SaIgnore）

上传图片并自动压缩缩略图（≤800px 宽，0.8 质量）；视频文件直接透传不压缩。

**请求**：

```
Content-Type: multipart/form-data
field: file
```

**响应**：同 `/upload`。

---

### 2.3 `POST /resource/oss/uploadByBase64`（@SaIgnore）

将 Base64 data URI 上传到 OSS。

**请求**：

```json
{
  "base64Content": "data:image/png;base64,iVBORw0KGgoAAAANS...",
  "fileName": "image-1700000000.png",
  "extensionType": "png",
  "isPermanent": 1
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `base64Content` | string | 是 | 完整 data URI，含 `data:<mime>;base64,` 前缀 |
| `fileName` | string | 否 | 自定义文件名（不含扩展名部分）|
| `extensionType` | string | 否 | 文件扩展名（如 `png`、`jpg`、`mp4`）|
| `isPermanent` | int | 否 | 1=永久保存，0=临时（默认 1）|

**响应**：同 `/upload`。

**超时建议**：60 秒（Base64 图片）。

---

### 2.4 `POST /resource/oss/uploadByImageUrl`（@SaIgnore）

后端下载图片 URL 后上传到 OSS；若传入 data URI 则等同于 `/uploadByBase64`。

**请求**：

```json
{
  "url": "https://external.com/image.jpg",
  "extensionType": "jpg",
  "isPermanent": 1
}
```

**响应**：同 `/upload`。

**超时建议**：60 秒（需后端下载外部图片）。

---

### 2.5 `POST /resource/oss/uploadByVideoUrl`（@SaIgnore）

后端下载视频 URL 后上传到 OSS；支持 http URL 或 data URI，下载超时 300 秒。

**请求**：

```json
{
  "url": "https://external.com/video.mp4",
  "extensionType": "mp4",
  "isPermanent": 1
}
```

**响应**：同 `/upload`。

**超时建议**：120 秒（视频文件较大）。

---

### 2.6 `DELETE /resource/oss/{ossIds}`（需认证）

删除 OSS 资源（多个 ID 用逗号分隔）。

```
DELETE /resource/oss/123,456,789
```

---

## 三、前端实现

### 3.1 实现 `uploadService.ts`

将 `gravitex-api-cli/services/uploadService.ts` 替换为以下完整实现（参照 nebula-lab-react 的实现，适配 gravitex 的 `request` 客户端）：

```typescript
import { request } from '../lib/request';

/** OSS 上传最大重试次数 */
export const OSS_UPLOAD_MAX_RETRIES = 3;
/** OSS 上传重试间隔（毫秒） */
export const OSS_UPLOAD_RETRY_DELAY_MS = 1500;

export interface UploadResult {
  url: string;
  fileName: string;
  ossId: string;
}

/** 带重试的执行器：最多重试 3 次，每次失败后等 1500ms */
async function withOssRetry<T>(
  uploadFn: () => Promise<T>,
  context?: string
): Promise<T> {
  let lastError: unknown;
  for (let attempt = 1; attempt <= OSS_UPLOAD_MAX_RETRIES; attempt++) {
    try {
      const result = await uploadFn();
      return result;
    } catch (e) {
      lastError = e;
      if (import.meta.env?.DEV) {
        console.warn(
          `[OSS] 第 ${attempt}/${OSS_UPLOAD_MAX_RETRIES} 次上传失败`,
          context ?? '',
          e
        );
      }
      if (attempt < OSS_UPLOAD_MAX_RETRIES) {
        await new Promise((r) => setTimeout(r, OSS_UPLOAD_RETRY_DELAY_MS));
      }
    }
  }
  throw lastError;
}

export const uploadService = {
  /**
   * 判断是否为 base64 data URL
   */
  isDataUrl: (url: string) =>
    typeof url === 'string' && url.startsWith('data:'),

  /**
   * 解析 data URL，返回完整 data URL 和扩展名
   */
  parseDataUrl: (
    url: string
  ): { base64Content: string; extensionType: string } | null => {
    if (!url || !url.startsWith('data:')) return null;
    const commaIndex = url.indexOf(',');
    if (commaIndex === -1) return null;
    const mimePart = url.slice(5, commaIndex); // e.g. "image/png;base64"
    const mime = mimePart.split(';')[0].trim().toLowerCase();
    let extensionType = 'png';
    if (mime.startsWith('image/')) {
      const ext = mime.slice(6);
      extensionType = ext === 'jpeg' ? 'jpg' : ext;
    } else if (mime.startsWith('video/')) {
      extensionType = mime.slice(6) || 'mp4';
    }
    return { base64Content: url, extensionType };
  },

  /**
   * 上传文件（multipart/form-data）
   * POST /resource/oss/upload
   */
  uploadFile: (file: File) => {
    return request.upload<UploadResult>('/resource/oss/upload', file, {
      timeout: 120000,
    });
  },

  /**
   * 通过 Base64 data URI 上传
   * POST /resource/oss/uploadByBase64
   */
  uploadByBase64: (
    base64Content: string,
    fileName: string,
    extensionType: string
  ) => {
    return request.post<UploadResult>(
      '/resource/oss/uploadByBase64',
      { base64Content, fileName, extensionType },
      { timeout: 60000 }
    );
  },

  /**
   * 通过 Base64 上传（带最多 3 次重试）
   */
  uploadByBase64WithRetry: (
    base64Content: string,
    fileName: string,
    extensionType: string
  ) => {
    return withOssRetry(
      () => uploadService.uploadByBase64(base64Content, fileName, extensionType),
      `uploadByBase64 fileName=${fileName} len=${base64Content?.length ?? 0}`
    );
  },

  /**
   * 通过图片 URL 上传（data URL 自动转 Base64 接口）
   * POST /resource/oss/uploadByImageUrl
   */
  uploadByImageUrl: (imageUrl: string, extensionType: string) => {
    const parsed = uploadService.parseDataUrl(imageUrl);
    if (parsed) {
      const fileName = `image-${Date.now()}.${parsed.extensionType}`;
      return uploadService.uploadByBase64(
        parsed.base64Content,
        fileName,
        parsed.extensionType
      );
    }
    return request.post<UploadResult>(
      '/resource/oss/uploadByImageUrl',
      { url: imageUrl, extensionType },
      { timeout: 60000 }
    );
  },

  /**
   * 通过图片 URL 上传（带重试）
   */
  uploadByImageUrlWithRetry: (imageUrl: string, extensionType: string) => {
    return withOssRetry(
      () => uploadService.uploadByImageUrl(imageUrl, extensionType),
      `uploadByImageUrl len=${imageUrl?.length ?? 0}`
    );
  },

  /**
   * 通过视频 URL 上传（data URL 自动转 Base64 接口）
   * POST /resource/oss/uploadByVideoUrl
   */
  uploadByVideoUrl: (videoUrl: string, extensionType: string) => {
    const parsed = uploadService.parseDataUrl(videoUrl);
    if (parsed) {
      const fileName = `video-${Date.now()}.${parsed.extensionType}`;
      return uploadService.uploadByBase64(
        parsed.base64Content,
        fileName,
        parsed.extensionType
      );
    }
    return request.post<UploadResult>(
      '/resource/oss/uploadByVideoUrl',
      { url: videoUrl, extensionType },
      { timeout: 120000 }
    );
  },

  /**
   * 通过视频 URL 上传（带重试）
   */
  uploadByVideoUrlWithRetry: (videoUrl: string, extensionType: string) => {
    return withOssRetry(
      () => uploadService.uploadByVideoUrl(videoUrl, extensionType),
      `uploadByVideoUrl len=${videoUrl?.length ?? 0} ext=${extensionType}`
    );
  },

  /**
   * 删除 OSS 资源
   * DELETE /resource/oss/{ids}
   */
  deleteOssResource: (ids: string | number | string[]) => {
    const idsParam = Array.isArray(ids) ? ids.join(',') : String(ids);
    return request.delete<void>(`/resource/oss/${idsParam}`);
  },
};
```

### 3.2 Chat 页调用位置

Chat 页（`pages/Chat/index.tsx`）已在以下位置调用 uploadService，实现后即可生效：

| 行号（参考）| 场景 | 调用方法 |
|-----------|------|---------|
| ~3425 | AI 生成图片（Base64）存 OSS | `uploadByBase64WithRetry` |
| ~3438 | AI 生成图片（URL）存 OSS | `uploadByImageUrlWithRetry` |
| ~3477 | AI 生成视频（Base64 data URI）存 OSS | `uploadByBase64WithRetry` |
| ~3483 | AI 生成视频（HTTP URL）存 OSS | `uploadByVideoUrlWithRetry` |
| ~3563 | 另一处生成图片存 OSS | `uploadByBase64WithRetry` |

**约定**：Chat 页的逻辑是"先 OSS 上传成功，再写对话存储"。`uploadByBase64WithRetry` 失败（3 次）时应降级跳过 OSS（记录 log），避免因 OSS 失败导致整个对话丢失。

---

## 四、Go 后端 OSS 配置

Go 后端（gravitex-api）通过环境变量调用 Java 后端上传视频：

**环境变量**：

```bash
OSS_BASE64_ENDPOINT=http://java-backend:8080/resource/oss/uploadByBase64
```

若未设置此变量，`service.IsVideoOSSEnabled()` 返回 false，跳过 OSS 上传（视频 URL 直接存 tasks 表）。

**Go 端上传流程**（`service/oss_video.go`）：

1. 下载远端视频到内存（支持注入 Gemini GCS 鉴权头等）
2. 转为 Base64 data URI
3. `POST {OSS_BASE64_ENDPOINT}` → Java 后端 → OSS
4. 返回 OSS URL，写入 task 记录

---

## 五、Java 后端 OSS 配置

OSS 配置存储在数据库表 `sys_oss_config`，通过管理后台配置，**不需要修改代码**。

### 5.1 配置字段

| 字段 | 说明 | 示例（阿里云）|
|------|------|--------------|
| `configKey` | 配置标识 | `aliyun` |
| `accessKey` | AccessKey ID | `LTAI5t...` |
| `secretKey` | AccessKey Secret | `xxx...` |
| `bucketName` | Bucket 名称 | `my-bucket` |
| `endpoint` | OSS Endpoint | `oss-cn-hangzhou.aliyuncs.com` |
| `domain` | 自定义 CDN 域名（可选）| `cdn.example.com` |
| `region` | 区域 | `cn-hangzhou` |
| `isHttps` | 是否 HTTPS | `1`（是）/ `0`（否）|
| `accessPolicy` | 访问策略 | `1`（公开）/ `0`（私有）/ `2`（自定义）|
| `prefix` | 路径前缀（可选）| `gravitex/` |

### 5.2 支持的 OSS 提供商

Java 后端使用 AWS SDK v2（S3 协议），支持所有 S3 兼容的对象存储：

| 提供商 | endpoint 格式 |
|--------|--------------|
| 阿里云 OSS | `oss-cn-hangzhou.aliyuncs.com` |
| 腾讯云 COS | `cos.ap-guangzhou.myqcloud.com` |
| AWS S3 | `s3.amazonaws.com` |
| MinIO | `minio.yourdomain.com:9000` |
| Cloudflare R2 | `<accountid>.r2.cloudflarestorage.com` |

### 5.3 文件大小限制

Java 后端默认配置：

```yaml
spring.servlet.multipart.max-file-size: 500MB
spring.servlet.multipart.max-request-size: 500MB
```

---

## 六、上传流程图

### 6.1 前端上传图片/视频到 OSS

```
前端 Chat 页
  │
  │ AI 生成图片（Base64 data URI）
  │ uploadService.uploadByBase64WithRetry("data:image/png;base64,...", "img.png", "png")
  │
  ▼
POST /resource/oss/uploadByBase64
{
  base64Content: "data:image/png;base64,...",
  fileName: "img.png",
  extensionType: "png"
}
  │
  ▼
Java 后端 SysOssServiceImpl.uploadByBase64()
  → 解码 data URI → 写临时文件
  → OssFactory.instance().uploadSuffix(tempFile, "png")
  → AWS SDK S3AsyncClient.putObject()
  │
  ▼
OSS 存储（阿里云/COS/Minio...）
  │
  ▼
返回: { url: "https://cdn.example.com/xxx.png", ossId: "123456" }
  │
  ▼
前端存入对话消息 ossId，用于后续删除清理
```

### 6.2 Go 后端上传视频

```
视频任务轮询完成（task_status = succeed）
  │
  │ service.IsVideoOSSEnabled() = true（已配置 OSS_BASE64_ENDPOINT）
  │
  ▼
uploadVideoToOSS(ctx, channel, task, taskResult)
  │
  ├── ChannelType = Gemini → UploadVideoFromURL(gcsUrl, {"x-goog-api-key": key})
  ├── ChannelType = Azure/Sora → UploadVideoFromURL(remoteUrl, nil)
  └── ChannelType = VertexAI → UploadBase64ToOSS(dataUri, ...)
  │
  ▼
service.UploadBase64ToOSS(ctx, dataUri, fileName, ext)
  → POST {OSS_BASE64_ENDPOINT} → Java 后端
  │
  ▼
返回 ossUrl → 写入 task.Data
```

---

## 七、开发检查清单

### 前端（gravitex-api-cli）
- [ ] `services/uploadService.ts`：按第三章实现（替换占位代码）
- [ ] 验证 Chat 页图片生成后能正常上传 OSS（断开 console.error，检查 ossId 写入）
- [ ] 验证 Chat 页视频生成后能正常上传 OSS
- [ ] 验证对话删除时能正确调用 `deleteOssResource` 清理 OSS 资源

### Java 后端（Gravitex-API-End）
- [ ] 确认 `sys_oss_config` 表已配置有效的 OSS 凭证
- [ ] 确认 `/resource/oss/uploadByBase64`（@SaIgnore）接口已正常部署
- [ ] 确认文件大小上限满足需求（默认 500MB）

### Go 后端（gravitex-api）
- [ ] 设置环境变量 `OSS_BASE64_ENDPOINT=http://java-backend:8080/resource/oss/uploadByBase64`
- [ ] 验证视频任务完成后 OSS URL 正确写入 task 记录

---

## 八、注意事项

1. **认证问题**：`/uploadByBase64`、`/uploadByImageUrl`、`/uploadByVideoUrl`、`/uploadThumbnail` 均标注 `@SaIgnore`，前端调用这些接口**不需要传 Authorization token**，适合匿名上传 AI 生成内容。若需要用户归属，调用 `/upload`（需认证）。

2. **Base64 大小**：大型视频的 Base64 体积会是原文件的约 1.33 倍，注意请求体大小不能超过 Java 后端的 500MB 限制。对于超大文件（>300MB）建议先用 `/upload` 直接上传。

3. **重试策略**：`withOssRetry` 是 3 次重试、1500ms 间隔的线性重试，适合网络抖动。若 OSS 服务本身宕机，3 次重试后会抛出异常，Chat 页应捕获并降级（跳过 OSS，仍保存对话但不保留 ossId）。

4. **临时 vs 永久**：`isPermanent=0` 标记为临时文件，可被后台定时清理任务删除。AI 对话生成的内容（图片/视频）应传 `isPermanent=1` 或不传（默认 1）保证持久化。

5. **文件名冲突**：建议 fileName 加时间戳：`image-${Date.now()}.png`，避免覆盖同名文件。
