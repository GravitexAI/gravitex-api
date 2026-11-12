package gemini

import (
	"slices"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// geminiGroundingCounter accumulates Google Search grounding across one
// upstream response (every stream frame and candidate). Gemini 3 bills each
// unique non-empty search query ("we ignore the empty web search queries when
// counting unique queries", ai.google.dev/gemini-api/docs/google-search);
// Gemini 2.5 and older bill once per grounded prompt, counted here when the
// response carries any search query. Both quantities are recorded and the
// tool price table decides which one costs money for a model. Image-search
// queries count like web-search queries; Google documents no separate rule.
// Google documents no response field holding the billed count, so the unique
// query set is the best available evidence, not a guaranteed match.
type geminiGroundingCounter struct {
	queries map[string]struct{}
}

func (g *geminiGroundingCounter) observe(response *dto.GeminiChatResponse) {
	for _, candidate := range response.Candidates {
		metadata := candidate.GroundingMetadata
		if metadata == nil {
			continue
		}
		for _, query := range slices.Concat(metadata.WebSearchQueries, metadata.ImageSearchQueries) {
			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}
			if g.queries == nil {
				g.queries = make(map[string]struct{})
			}
			g.queries[query] = struct{}{}
		}
	}
}

func (g *geminiGroundingCounter) commit(info *relaycommon.RelayInfo) {
	if len(g.queries) == 0 {
		return
	}
	info.SetBillableToolCount(dto.BuildInToolGoogleSearch, len(g.queries))
	info.SetBillableToolCount(relaycommon.GoogleSearchGroundedPromptTool, 1)
}
