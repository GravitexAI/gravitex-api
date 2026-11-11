package channel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	common2 "github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoRequestCapturesUpstreamRequestIdFromChannelHeader(t *testing.T) {
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		configuredName  string
		responseHeaders map[string]string
		want            string
	}{
		{
			name:            "default reads X-Api-Request-Id",
			responseHeaders: map[string]string{common2.RequestIdKey: "newapi-id", "X-Request-Id": "te-id"},
			want:            "newapi-id",
		},
		{
			name:            "configured header wins",
			configuredName:  " x-request-id ",
			responseHeaders: map[string]string{common2.RequestIdKey: "newapi-id", "X-Request-Id": "te-id"},
			want:            "te-id",
		},
		{
			name:            "missing configured header clears previous attempt id",
			configuredName:  "x-request-id",
			responseHeaders: map[string]string{},
			want:            "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.responseHeaders {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			ctx.Set(common2.UpstreamRequestIdKey, "stale-id-from-previous-attempt")

			req, err := http.NewRequest(http.MethodPost, upstream.URL, nil)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ChannelOtherSettings: dto.ChannelOtherSettings{UpstreamRequestIdHeader: tt.configuredName},
			}}

			resp, err := doRequest(ctx, req, info)
			require.NoError(t, err)
			_ = resp.Body.Close()

			assert.Equal(t, tt.want, ctx.GetString(common2.UpstreamRequestIdKey))
		})
	}
}
