# Azure 推理模型思维链跨区域回放报错调研

> 调研日期：2026-10-20
> 结论：跨区域调用后回放加密思维链（reasoning item）确实会报 400，但根因是**加密内容绑定到 Azure 资源/组织身份**，不是字面上的"区域"。微软与 OpenAI 官方文档均未说明该绑定规则，现有证据全部来自社区实测与网关厂商事故报告。

## 一、现象

同一会话中，首轮请求落在美东2（East US 2），后续请求被路由到其他区域后，携带上一轮 reasoning item 的请求直接失败：

```json
{
  "error": {
    "message": "The encrypted content for item rs_0d09d6e56879e76500699d6feee41c8197bd268aae76141f87 could not be verified. Reason: Encrypted content organization_id did not match the target organization.",
    "type": "invalid_request_error",
    "code": "invalid_encrypted_content"
  }
}
```

关键特征：

- **只有携带加密思维链的后续请求失败**，首轮请求和普通 Chat Completions 调用完全不受影响（这也是问题看起来"时好时坏"的原因）
- 部署越多（路由目标越多），失败概率越高，与路由命中率正相关

## 二、机制

Azure OpenAI 推理模型（o 系列 / GPT-5.x）走 Responses API 时，思维链以 reasoning item（`rs_...`）返回。当 `store=false`（stateless 模式，含零数据保留组织）时，每个 reasoning item 携带 `encrypted_content` 字段供下一轮回放。

绑定规则（从错误信息反推，非官方口径）：

1. **加密内容在密码学上绑定到创建它的组织/API key**
2. Azure 上每个区域的资源（resource）是独立的组织身份——美东2 的资源和西欧的资源各持各的加密密钥
3. 所以"换区域" = 换资源 = 换组织密钥 → 下一轮用新资源的密钥去解旧资源产出的 `encrypted_content`，解密失败，报 `organization_id did not match`

这也是**没有官方说法的根本原因**：官方维度上它不是"区域绑定"，而是"组织/资源绑定"。单资源部署的端点是固定区域的，跨区域必然跨资源，所以社区观察到的现象就表现为"区域变了就报错"。

## 三、证据来源

| 来源 | 内容 |
|---|---|
| [LiteLLM 事故报告 2026-02-24](https://docs.litellm.ai/blog/responses-api-encrypted-content-incident) | 最完整的定界：Azure East US → West Europe 跨区负载均衡，回放 `rs_...` 必失败，根因 org 绑定。修复方案为 `encrypted_content_affinity`：把产出该 item 的部署 ID 编码进 item ID 与 `encrypted_content` 前缀，回放时强制路由回同一部署（含流式事件包裹的补丁） |
| [LLM Gateway 路由文档](https://docs.llmgateway.io/features/routing#provider-bound-reasoning-stays-on-its-key) | 明确表述："OpenAI encrypted reasoning 会被另一个 OpenAI 组织或 **Azure resource** 拒绝"；网关必须记住哪把 key 产出了哪个加密 reasoning item / thought signature，回放该轮时绑定同一 provider 同一把 key |
| [OpenAI 社区帖 2025-11-18](https://community.openai.com/t/how-stateless-is-reasoning-encrypted-content/1366697) | 实测在多个 Azure OpenAI 实例间负载均衡即触发 "could not be verified"，证明官方文档宣称的"stateless 可移植"实际不成立 |
| [digitalapplied 六厂商普查 2026-09-02](https://www.digitalapplied.com/blog/reasoning-that-cannot-change-models-openai-google-anthropic) | 横向对比六厂商思维链绑定规则：OpenAI 绑 model family + org；Anthropic 绑模型代际 + 会话前缀（即 thinking signature）；Google 绑 thought signature。OpenAI 家族间切换会静默丢弃，Azure 跨资源则直接 400 |

## 四、官方文档的沉默

- OpenAI 文档把 `encrypted_content` 描述为"stateless、可跨请求携带"，**只字未提组织绑定**
- 微软 Learn 的 [Responses API 文档](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/responses) 和 [reasoning 文档](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/reasoning) 同样未提及该约束
- 现有规则全部是开发者撞上 400 之后从错误信息反推的。社区定性：官方说 "portable"，实际行为 "organization-bound"，两者矛盾且无官方认领
- 补充视角：arXiv 2608.09867（2026-08）研究了跨用户回放加密思维链的攻击面，说明这套加密还有更深的绑定维度（账号/会话级），比"区域绑定"更严格

## 五、对网关的启示

1. **会话粘性是硬需求**：请求中带 `encrypted_content`（或 thinking signature）时，必须路由回产出它的那个渠道/key，无法通用负载均衡
2. **切区域时的降级方案**：
   - 丢弃 reasoning item 再切换——OpenAI 口径中 reasoning 只在工具调用闭环里必需，普通多轮丢弃只损失质量不报错
   - 或参考 LiteLLM 方案：把渠道指纹编码进 item ID / `encrypted_content` 前缀，回放时解码并固定路由
3. **与 Anthropic thinking signature 同构**：Anthropic 的 prefix mismatch 场景在 Azure 侧对应 org/资源 mismatch，报错文案不同但问题本质相同，网关侧处理思路可以统一

## 六、参考链接

- LiteLLM 事故报告：https://docs.litellm.ai/blog/responses-api-encrypted-content-incident
- LLM Gateway 路由文档（provider-bound reasoning）：https://docs.llmgateway.io/features/routing
- OpenAI 社区讨论（encrypted_content 可移植性实测）：https://community.openai.com/t/how-stateless-is-reasoning-encrypted-content/1366697
- digitalapplied 六厂商思维链绑定普查：https://www.digitalapplied.com/blog/reasoning-that-cannot-change-models-openai-google-anthropic
- 跨用户回放加密思维链研究（arXiv 2608.09867）：https://arxiv.org/html/2608.09867v1
- Azure OpenAI Responses API 官方文档：https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/responses
- Azure OpenAI 推理模型官方文档：https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/reasoning
