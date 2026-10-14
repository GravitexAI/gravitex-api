package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// parseResolutionFromUpstreamBody reads the billing resolution from a video request.
func parseResolutionFromUpstreamBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var request map[string]interface{}
	if err := common.Unmarshal(body, &request); err != nil {
		return ""
	}
	if resolution, ok := request["resolution"].(string); ok && resolution != "" {
		return resolution
	}
	modelName, _ := request["model"].(string)
	if !strings.EqualFold(strings.TrimSpace(modelName), relaycommon.GeminiOmni11FlashPreview) {
		return ""
	}
	if formats, ok := request["response_format"].([]interface{}); ok && len(formats) > 0 {
		if format, ok := formats[0].(map[string]interface{}); ok {
			if resolution, ok := format["resolution"].(string); ok {
				return resolution
			}
		}
	}
	return ""
}
