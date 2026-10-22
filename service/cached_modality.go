package service

import "github.com/QuantumNous/new-api/relaykit/dto"

// freshModalityTokens 是各模态中「未命中缓存」的 token 数，也就是真正需要按
// 该模态单价计费的部分。命中缓存的那部分统一按缓存价结算，不再按模态区分。
type freshModalityTokens struct {
	Image int
	Audio int
	Video int
}

// resolveFreshModalityTokens 把输入 token 先按是否命中缓存切开，再给出各模态
// 未命中缓存的部分。
//
// 之所以需要这一步：Gemini 的 promptTokensDetails 按模态拆的是**整个输入**
// （含已命中缓存的 token），而 cachedContentTokenCount 是另一把刀切同一批
// token。把两者当成互不相交的分区去依次扣减，会把重叠部分扣两次，使剩余量变
// 成负数，重叠的 token 则同时按缓存价和模态价各计费一次。
//
// 缓存的模态构成优先采信上游的 cacheTokensDetails。上游只列出部分模态时，缺
// 口（CachedTokens 减去已列出部分）按剩余模态在输入中的占比补齐；完全没有该
// 拆分时，整个缓存都按模态占比分摊。无论走哪条分支，各模态 fresh 之和都不会
// 超过未命中缓存的输入总量，这正是重复扣减无法再发生的原因。
//
// 模态同价时本函数的结果不影响金额（a×1 + b×1 恒等于 (a+b)×1），只有异价模型
// 才依赖这里的拆分精度。
func resolveFreshModalityTokens(details dto.InputTokenDetails, promptTokens int) freshModalityTokens {
	asReported := freshModalityTokens{
		Image: max(details.ImageTokens, 0),
		Audio: max(details.AudioTokens, 0),
		Video: max(details.VideoTokens, 0),
	}
	cachedTotal := details.CachedTokens
	if cachedTotal <= 0 {
		return asReported
	}

	// 四个模态并列参与缓存归属推断：文本虽然不单独计价，但它占掉的缓存额度会
	// 直接决定其余模态还能分到多少，漏掉它会让图片/音频被判定为缓存过多。
	totals := [4]int{details.TextTokens, details.ImageTokens, details.AudioTokens, details.VideoTokens}
	cached := [4]int{details.CachedTextTokens, details.CachedImageTokens, details.CachedAudioTokens, details.CachedVideoTokens}

	// 上游没有给出缓存的模态拆分，而各模态又都放得进未命中缓存的额度时，说明
	// 两者本就不重叠，不需要任何推断。这一条把推断限制在真正放不下的请求上，
	// 也让按模态单独统计、prompt_tokens 不含模态量的调用方保持原有口径。
	if cached == [4]int{} &&
		asReported.Image+asReported.Audio+asReported.Video <= promptTokens-cachedTotal {
		return asReported
	}

	gap := cachedTotal
	for i, c := range cached {
		if c < 0 {
			cached[i] = 0
			continue
		}
		if c > totals[i] {
			cached[i] = max(totals[i], 0)
		}
		gap -= cached[i]
	}

	if gap > 0 {
		// 只在上游没有点名的模态之间分配缺口：已给出的值是权威的，不能被稀释。
		unassigned := 0
		for i, c := range cached {
			if c == 0 && totals[i] > 0 {
				unassigned += totals[i]
			}
		}
		if unassigned > 0 {
			for i, c := range cached {
				if c != 0 || totals[i] <= 0 {
					continue
				}
				share := int(int64(gap) * int64(totals[i]) / int64(unassigned))
				cached[i] = min(share, totals[i])
			}
		}
	}

	return freshModalityTokens{
		Image: max(totals[1]-cached[1], 0),
		Audio: max(totals[2]-cached[2], 0),
		Video: max(totals[3]-cached[3], 0),
	}
}
