# Azure GPT-5.6-sol 与 OpenAI 官方 API 价格差异调研

> 调研日期：2026-09-18
> 结论：Azure 定价确实高于 OpenAI 官方 API，属于**官方已承认的价格更新滞后**，不是纯粹的界面显示 bug，也不是微软永久性差价策略。

## 一、价差对比

| 来源 | Input ($/1M) | Output ($/1M) |
|---|---|---|
| **OpenAI 官方 API**（2026-08-21 降价后） | $4.00 | $20.00 |
| **Azure OpenAI**（当前定价页 / 部分客户实际账单） | $5.00~$5.50（Data Zone 更高） | $30.00~$33.00 |

差价接近 25%~65%。社区反馈中，GPT-5.6 Luna/Terra 系列一度被多收甚至达 5 倍（Luna input $1.10 vs OpenAI $0.20）。

## 二、时间线与官方说明

1. **OpenAI 先降价**
   2026-08-21，OpenAI 官方公告将 GPT-5.6 Sol 降价：input 从 $5 降到 $4（-20%），output 从 $30 降到 $20（-33%），促销价至少持续到 2026-11-21。
   来源：[OpenAI Community 公告](https://community.openai.com/t/20-price-reduction-for-gpt-5-6-sol-api-codex-credits-and-chatgpt-work/1391726)

   > "Sol now costs $4 per million input tokens and $20 per million output tokens, 20% lower input pricing and 33% lower output pricing."

2. **微软官方承诺价格对齐**
   Azure 官方博客声明将同步降价：
   来源：[Azure Blog - GPT-5.6 now available in Microsoft Foundry](https://azure.microsoft.com/en-us/blog/gpt-5-6-now-available-in-microsoft-foundry/)

   > "Effective August 1st, Azure OpenAI customers will receive the same updated price decreases for GPT 5.6 Luna and Terra models recently announced by OpenAI. Azure is committed to continuing to price parity for applicable Azure OpenAI Services offerings."
   （该回复由 Microsoft 员工 Adam B 在 Microsoft Learn Q&A 中引用）

3. **但实际落地滞后**
   多位用户在官方 Q&A 中贴出真实账单，证明 8 月账单仍按旧的高价（例如 Sol $5.50/$33）计费，且定价页也未同步更新。
   来源：[Microsoft Learn Q&A - Pricing of OpenAI GPT 5.6 models (Luna & Terra)](https://learn.microsoft.com/en-us/answers/questions/5962521/pricing-of-openai-gpt-5-6-models-luna-terra)

   Microsoft 员工（Adam B）在该帖回复承认：

   > "It looks like some of the public pricing pages are not reflecting the updated pricing. There might just be a lag in getting those updated. If you all are still seeing differences in what is actually being billed to you, I would recommend opening a support ticket."

   另一位 Microsoft 员工（Thanh Nguyen）补充：

   > "Azure docs and Pricing Calc tool still show the old (high) pricing for Luna & Terra... Given Aug invoice has not been issued, MS usually 'fix' the issue before invoice date and the invoice should reflect the discounted pricing."

## 三、结论

- **不是纯 UI 显示错误**：真实账单当时确实按旧的高价收费（有用户贴出 8 月账单明细佐证）。
- **也不是永久性差价策略**：微软对外文档明确承诺与 OpenAI 价格对齐，这是**发布节奏滞后**——OpenAI 降价后，Azure 定价页 / 计量系统 / 账单系统更新存在数周延迟。
- 若使用 Azure GPT-5.6-sol 渠道计费，建议核对历史账单是否按旧价多收费，如有需走 Azure Support Ticket 要求按对齐后价格重新出账。

## 四、参考链接

- Azure OpenAI 官方定价页：https://azure.microsoft.com/en-us/pricing/details/azure-openai/
- OpenAI 官方定价页：https://developers.openai.com/api/docs/pricing
- OpenAI GPT-5.6 Sol 模型页（含最新价）：https://developers.openai.com/api/docs/models/gpt-5.6-sol
- OpenAI 降价公告：https://community.openai.com/t/20-price-reduction-for-gpt-5-6-sol-api-codex-credits-and-chatgpt-work/1391726
- Azure 官方博客（价格对齐承诺）：https://azure.microsoft.com/en-us/blog/gpt-5-6-now-available-in-microsoft-foundry/
- Microsoft Q&A（官方员工确认滞后 + 解决方案）：https://learn.microsoft.com/en-us/answers/questions/5962521/pricing-of-openai-gpt-5-6-models-luna-terra
