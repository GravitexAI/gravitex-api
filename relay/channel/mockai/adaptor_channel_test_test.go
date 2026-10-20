package mockai

import (
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkipLatencyUnderChannelTest 守护渠道测试 / 定时健康检查路径：
// 长时延档位不能让测试请求真实睡满，否则健康检查会按墙钟耗时误禁用渠道。
func TestSkipLatencyUnderChannelTest(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "non_stream"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			cfg := mockChatConfig()
			slow := mockTestProfile()
			slow.LatencyMs = dto.MockAIRange{Min: 30000, Max: 30000}
			slow.FirstTokenMs = dto.MockAIRange{Min: 20000, Max: 20000}
			cfg.Default = slow
			cfg.Models = map[string]dto.MockAIModelProfile{"mock-origin-model": slow}

			c, _, info := newMockRelayContext(t, cfg, stream)
			common.SetContextKey(c, constant.ContextKeyChannelTesting, true)

			start := time.Now()
			raw, err := (&Adaptor{}).DoRequest(c, info, nil)
			require.NoError(t, err)
			resp, ok := raw.(*http.Response)
			require.True(t, ok, "DoRequest must return *http.Response")
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.NotEmpty(t, body)
			assert.Less(t, time.Since(start), time.Second, "channel test must not sleep through the simulated latency")
		})
	}
}
