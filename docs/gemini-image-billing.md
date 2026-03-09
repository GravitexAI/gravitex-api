# Gemini 图片模型计费（与 nebula 对齐说明）

gravitex 已实现与 nebula 一致的 **Gemini 返回图片** 计费逻辑，便于对账与排查。

---

## 1. 图片输出 token 来源（relay-gemini）

**位置**: `relay/channel/gemini/relay-gemini.go`

- **优先**: 从 `UsageMetadata.CandidatesTokensDetails` 按 `Modality` 拆分（大小写不敏感）：
  - `IMAGE` → `imageOutputTokens`
  - `TEXT` → `textOutputTokens`
- **回退**: 当 `imageOutputTokens == 0` 且 `CandidatesTokenCount > 0` 且模型名包含 `"image"` 时，将整段候选 token 计为图片：`imageOutputTokens = CandidatesTokenCount`，`textOutputTokens = 0`
- 流式 / 非流式均解析并写入：
  - `usage.CompletionTokenDetails.ImageTokens`、`TextTokens`
  - `c.Set("gemini_image_output_tokens", ...)`、`c.Set("gemini_text_output_tokens", ...)`
- **流式 completion 回退**: 若有图片（`imageCount > 0`）且 `usage.CompletionTokens == 0`，则 `usage.CompletionTokens = imageCount * 1400`（每张图按 1400 token 估）

---

## 2. 计费公式（compatible_handler）

**位置**: `relay/compatible_handler.go`

- **图片输出**: `imageCompletionQuota = geminiImageOutputTokens × effectiveImageOutputRatio`
- **文本输出**: `textCompletionQuota = textOutputTokens × completionRatio`（含 reasoning）
- **总 completion**: `completionQuota = textCompletionQuota + imageCompletionQuota`
- **总 quota**: `quotaCalculateDecimal = (promptQuota + completionQuota) × ratio`，其中 `ratio = modelRatio × groupRatio`，再叠加 tools/audio 等后取整为 `quota`

`geminiImageOutputTokens` 优先从 `ctx.GetInt("gemini_image_output_tokens")` 取，否则用 `usage.CompletionTokenDetails.ImageTokens`。

---

## 3. 图片输出倍率（effectiveImageOutputRatio）

**优先级**（与 nebula 一致）:

1. **ImageCompletionRatio** — `ratio_setting.GetImageCompletionRatio(OriginModelName)`，对应配置 `image_completion_ratio`
2. 未配置 → **ImageRatio**
3. 再未配置 → **ModelRatio**

实现: `setting/ratio_setting/model_ratio.go` 中 `GetImageCompletionRatio` 未命中时回退到 `GetCompletionRatio`。  
`relay/helper/price.go` 构建 `PriceData` 时已写入 `ImageCompletionRatio`。

---

## 4. 小结

| 项目 | gravitex 实现 |
|------|----------------|
| **量** | `gemini_image_output_tokens`：来自 CandidatesTokensDetails 的 IMAGE，或图片模型回退 CandidatesTokenCount |
| **价** | effectiveImageOutputRatio：ImageCompletionRatio → ImageRatio → ModelRatio |
| **公式** | 图片部分 quota = geminiImageOutputTokens × effectiveImageOutputRatio，与文本部分一起乘 modelRatio × groupRatio 等 |

若仍出现计费异常，建议抓一条具体请求：模型名、请求/响应中的 usage（含 CompletionTokenDetails）、实际扣费 quota 与预期，便于对照上述链路逐项排查。
