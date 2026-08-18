# Auto 虚拟模型路由

在 `Distribute()` 拦截 `model=auto` / `auto:low|medium|high|max`，解析成真实对外模型名后再走原有白名单、选渠道、计费、同模型重试。  
配置存在 option `AutoRouter`（JSON），**默认关闭**。

计费按实际模型价。只作用于 `/v1/chat/completions`、`/v1/responses`、`/v1/messages`、`/pg/chat/completions`。

## 已做

- [x] 虚拟模型拦截：改 `modelRequest.Model` 一处，其余链路复用
- [x] 规则分类：agent / vision / code / translation / reasoning / general
- [x] 能力硬过滤：tools、vision、JSON；未标注视觉能力时按模型名启发
- [x] 成本档 + 管理员权重 + 任务偏好（过滤后只在偏好子集里选）
- [x] 会话黏性：`X-Session-Id` / `X-Auto-Session`，否则指纹取 system + 首条 user
- [x] 回包透明：`X-Auto-Model` / `X-Auto-Task` / `X-Auto-Tier`；日志 `admin_info` 带 `auto_original_model`
- [x] Token 白名单：含 `auto` 放行整池，否则按解析后的真实模型校验
- [x] 模型列表挂上 `auto`、`auto:low|medium|high|max`
- [x] Playground 换组发生在 Auto 解析之前
- [x] 热路径：body 用 gjson 扫 `[]byte`；分组模型走渠道内存缓存
- [x] option 读写：`AutoRouter` JSON，`setting/auto_router.go`

### 试用

```json
{
  "enabled": true,
  "default_tier": "medium",
  "stickiness_ttl": 1800,
  "tiers": {
    "low": ["deepseek-chat"],
    "medium": ["gpt-4o-mini", "deepseek-chat"],
    "high": ["claude-sonnet", "gpt-4o"],
    "max": ["claude-opus"]
  },
  "task_prefer": {
    "code": ["claude-sonnet", "deepseek-chat"],
    "vision": ["gpt-4o", "claude-sonnet"]
  },
  "weights": { "claude-sonnet": 10, "deepseek-chat": 5 },
  "capabilities": {
    "gpt-4o": { "tools": true, "vision": true, "json": true },
    "deepseek-chat": { "tools": true, "vision": false, "json": true }
  }
}
```

`tiers` 为空时，回退到当前分组（或 auto 分组）已启用模型。池内名字必须是对外模型名。

```bash
curl /v1/chat/completions \
  -H "Authorization: Bearer sk-..." \
  -H "X-Session-Id: conv-1" \
  -d '{"model":"auto","messages":[{"role":"user","content":"..."}]}'
```

也可用 `auto:high` 或请求头 `X-Cost-Tier: high`。

## 未做

- [ ] 跨模型回退：该模型渠道全 429/5xx 后，换候选列表下一个模型（同模型换渠道已有）
- [ ] 上下文窗口硬过滤：输入 tokens + `max_tokens` 超过窗口则排除
- [ ] 小模型分类器：规则低置信度时再调便宜小模型，结果缓存
- [ ] 观察质量分：用成功率 / 首 token 延迟 / 回退率自动调权重
- [ ] 后台 Auto 配置页与观察面板（现在只能写 option JSON）
- [ ] 按用户 / 分组覆盖成本档与候选池
- [ ] embeddings / audio / images / video 不套 Auto（已拒绝；无单独产品方案）
