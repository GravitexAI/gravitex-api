package gemini

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// geminiUpstreamCall 记录上游收到的一次请求
type geminiUpstreamCall struct {
	authorization string
	apiKey        string
	userProject   string
	body          string
}

// newGeminiOAuthFixture 起一个按调用次数发 ya29.<n> 的 https broker 和一个可编排状态码的上游
func newGeminiOAuthFixture(t *testing.T, upstreamStatus func(n int64) int) (brokerKey string, upstreamURL string, brokerCalls *atomic.Int64, upstreamCalls func() []geminiUpstreamCall) {
	t.Helper()
	previousInsecure := common.TLSInsecureSkipVerify
	common.TLSInsecureSkipVerify = true
	service.InitHttpClient()
	t.Cleanup(func() {
		common.TLSInsecureSkipVerify = previousInsecure
		service.InitHttpClient()
	})

	brokerCalls = &atomic.Int64{}
	broker := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := brokerCalls.Add(1)
		_, _ = fmt.Fprintf(w, `{"access_token":"ya29.%d","expires_in":1800}`, n)
	}))
	t.Cleanup(broker.Close)

	var mu sync.Mutex
	var calls []geminiUpstreamCall
	var upstreamCount atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, geminiUpstreamCall{
			authorization: r.Header.Get("Authorization"),
			apiKey:        r.Header.Get("x-goog-api-key"),
			userProject:   r.Header.Get("x-goog-user-project"),
			body:          string(body),
		})
		mu.Unlock()
		w.WriteHeader(upstreamStatus(upstreamCount.Add(1)))
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(upstream.Close)

	brokerKey = fmt.Sprintf(`{"broker_url":%q,"user_project":"proj-1"}`, broker.URL)
	return brokerKey, upstream.URL, brokerCalls, func() []geminiUpstreamCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]geminiUpstreamCall(nil), calls...)
	}
}

func newGeminiOAuthRelayInfo(channelID int, key string, keyType dto.GeminiKeyType, baseURL string) (*gin.Context, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayMode:       constant.RelayModeChatCompletions,
		OriginModelName: "gemini-4-carbon",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName:    "gemini-4-carbon",
			ChannelId:            channelID,
			ApiKey:               key,
			ChannelBaseUrl:       baseURL,
			ChannelOtherSettings: dto.ChannelOtherSettings{GeminiKeyType: keyType},
		},
	}
	return c, info
}

func TestGeminiSetupRequestHeaderByKeyType(t *testing.T) {
	tests := []struct {
		name          string
		keyType       dto.GeminiKeyType
		key           string
		wantAPIKey    string
		wantAuthValue string
	}{
		{name: "api key", keyType: dto.GeminiKeyTypeAPIKey, key: "AIza-1", wantAPIKey: "AIza-1"},
		{name: "legacy empty type", keyType: "", key: "AIza-2", wantAPIKey: "AIza-2"},
		{name: "oauth token", keyType: dto.GeminiKeyTypeOAuthToken, key: "ya29.static", wantAuthValue: "Bearer ya29.static"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, info := newGeminiOAuthRelayInfo(1, tt.key, tt.keyType, "https://generativelanguage.googleapis.com")
			header := http.Header{}
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, info))
			assert.Equal(t, tt.wantAPIKey, header.Get("x-goog-api-key"))
			assert.Equal(t, tt.wantAuthValue, header.Get("Authorization"))
		})
	}
}

func TestGeminiOAuthBrokerReplaysOnceAfter401(t *testing.T) {
	brokerKey, upstreamURL, brokerCalls, upstreamCalls := newGeminiOAuthFixture(t, func(n int64) int {
		if n == 1 {
			return http.StatusUnauthorized
		}
		return http.StatusOK
	})
	c, info := newGeminiOAuthRelayInfo(9201, brokerKey, dto.GeminiKeyTypeOAuthBroker, upstreamURL)

	resp, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader([]byte(`{"contents":[]}`)))
	require.NoError(t, err)
	httpResp := resp.(*http.Response)
	defer httpResp.Body.Close()

	assert.Equal(t, http.StatusOK, httpResp.StatusCode)
	assert.EqualValues(t, 2, brokerCalls.Load(), "401 must force a broker refresh")
	calls := upstreamCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "Bearer ya29.1", calls[0].authorization)
	assert.Equal(t, "Bearer ya29.2", calls[1].authorization)
	for _, call := range calls {
		assert.Empty(t, call.apiKey, "oauth requests must not carry x-goog-api-key")
		assert.Equal(t, "proj-1", call.userProject)
		assert.Equal(t, `{"contents":[]}`, call.body, "replayed request must resend the full body")
	}
}

func TestGeminiOAuthBrokerPersistent401IsReturnedAfterOneReplay(t *testing.T) {
	brokerKey, upstreamURL, _, upstreamCalls := newGeminiOAuthFixture(t, func(int64) int {
		return http.StatusUnauthorized
	})
	c, info := newGeminiOAuthRelayInfo(9202, brokerKey, dto.GeminiKeyTypeOAuthBroker, upstreamURL)

	resp, err := (&Adaptor{}).DoRequest(c, info, io.NopCloser(bytes.NewReader([]byte(`{"contents":[]}`))))
	require.NoError(t, err)
	httpResp := resp.(*http.Response)
	defer httpResp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, httpResp.StatusCode)
	calls := upstreamCalls()
	require.Len(t, calls, 2, "deterministic 401 must not be retried more than once")
	assert.Equal(t, `{"contents":[]}`, calls[1].body, "non-seekable body must be buffered for replay")
}
