package gemini

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// markGeminiGoogleSearchCall records successful Google Search grounding for
// the request. A tools declaration alone is not evidence that Google Search
// ran; Gemini's grounding metadata is the provider response evidence.
func markGeminiGoogleSearchCall(c *gin.Context, response dto.GeminiChatResponse) {
	if c == nil {
		return
	}
	for _, candidate := range response.Candidates {
		if candidate.GroundingMetadata != nil && len(candidate.GroundingMetadata.WebSearchQueries) > 0 {
			c.Set("gemini_google_search_call", true)
			return
		}
	}
}
