package model

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

const (
	requestHeadersLogModeAll      = "all"
	requestHeadersLogModeSelected = "selected"
)

func normalizeRequestHeadersLogSetting(setting dto.RequestHeadersLogSetting) dto.RequestHeadersLogSetting {
	if setting.Mode != requestHeadersLogModeAll && setting.Mode != requestHeadersLogModeSelected {
		setting.Mode = requestHeadersLogModeSelected
	}

	seen := make(map[string]struct{}, len(setting.Headers))
	headers := make([]string, 0, len(setting.Headers))
	for _, header := range setting.Headers {
		header = strings.ToLower(strings.TrimSpace(header))
		if header == "" {
			continue
		}
		if _, exists := seen[header]; exists {
			continue
		}
		seen[header] = struct{}{}
		headers = append(headers, header)
	}
	setting.Headers = headers
	return setting
}

func collectConfiguredRequestHeaders(headers http.Header, setting dto.RequestHeadersLogSetting) map[string]string {
	setting = normalizeRequestHeadersLogSetting(setting)
	if !setting.Enabled || len(headers) == 0 {
		return nil
	}

	selected := make(map[string]struct{}, len(setting.Headers))
	for _, header := range setting.Headers {
		selected[header] = struct{}{}
	}

	result := make(map[string]string)
	for name, values := range headers {
		if setting.Mode == requestHeadersLogModeSelected {
			if _, ok := selected[strings.ToLower(name)]; !ok {
				continue
			}
		}
		if len(values) == 0 {
			continue
		}
		result[name] = values[0]
	}
	return result
}

// AppendConfiguredClientRequestHeaders adds the original inbound client
// headers to a log's other payload when the target user enabled the feature.
// Missing, malformed, or legacy user settings remain a no-op.
func AppendConfiguredClientRequestHeaders(c *gin.Context, userId int, other map[string]interface{}) {
	if c == nil || c.Request == nil || userId == 0 || other == nil {
		return
	}
	setting, err := GetUserSetting(userId, false)
	if err != nil {
		return
	}
	if setting.RequestHeadersLog == nil {
		return
	}
	headers := collectConfiguredRequestHeaders(c.Request.Header, *setting.RequestHeadersLog)
	if len(headers) > 0 {
		other["client_request_headers"] = headers
	}
}
