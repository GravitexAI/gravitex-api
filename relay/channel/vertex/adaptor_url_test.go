package vertex

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Vertex 与 Gemini Developer API 的 preview 版本命名互斥（v1beta1 vs v1beta），入站原生
// Gemini 路由只有 /v1beta/models/*，所以 Vertex 侧的 Gemini 请求必须换名到 v1beta1 —— 这是
// urlContext 与 googleSearch 组合等能力的开放版本。同渠道上的 Anthropic rawPredict 只有 v1。
func TestGetRequestUrlUsesGeminiPreviewVersion(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:     "fake-api-key",
			ApiVersion: "global",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				VertexKeyType: dto.VertexKeyTypeAPIKey,
			},
		},
	}

	cases := []struct {
		name        string
		requestMode int
		modelName   string
		suffix      string
		want        string
	}{
		{
			name:        "gemini uses v1beta1",
			requestMode: RequestModeGemini,
			modelName:   "gemini-3.5-flash",
			suffix:      "generateContent",
			want:        "https://aiplatform.googleapis.com/v1beta1/publishers/google/models/gemini-3.5-flash:generateContent?key=fake-api-key",
		},
		{
			name:        "gemini stream keeps alt=sse before key",
			requestMode: RequestModeGemini,
			modelName:   "gemini-3.5-flash",
			suffix:      "streamGenerateContent?alt=sse",
			want:        "https://aiplatform.googleapis.com/v1beta1/publishers/google/models/gemini-3.5-flash:streamGenerateContent?alt=sse&key=fake-api-key",
		},
		{
			name:        "imagen predict also uses v1beta1",
			requestMode: RequestModeGemini,
			modelName:   "imagen-4.0-generate-001",
			suffix:      "predict",
			want:        "https://aiplatform.googleapis.com/v1beta1/publishers/google/models/imagen-4.0-generate-001:predict?key=fake-api-key",
		},
		{
			name:        "claude stays on v1",
			requestMode: RequestModeClaude,
			modelName:   "claude-sonnet-4-5@20250929",
			suffix:      "rawPredict",
			want:        "https://aiplatform.googleapis.com/v1/publishers/anthropic/models/claude-sonnet-4-5@20250929:rawPredict?key=fake-api-key",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adaptor{RequestMode: tc.requestMode}
			got, err := a.getRequestUrl(info, tc.modelName, tc.suffix)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
