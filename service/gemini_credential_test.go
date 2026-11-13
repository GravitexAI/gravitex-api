package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGeminiBroker 起一个 https broker，按调用次数返回 ya29.<n>，记录调用次数与收到的认证头
type fakeGeminiBroker struct {
	server  *httptest.Server
	calls   atomic.Int64
	status  atomic.Int64
	body    func(n int64) string
	authHdr atomic.Value
}

func newFakeGeminiBroker(t *testing.T, body func(n int64) string) *fakeGeminiBroker {
	t.Helper()
	broker := &fakeGeminiBroker{body: body}
	broker.status.Store(http.StatusOK)
	broker.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := broker.calls.Add(1)
		broker.authHdr.Store(r.Header.Get("X-Api-Key"))
		w.WriteHeader(int(broker.status.Load()))
		_, _ = w.Write([]byte(broker.body(n)))
	}))
	t.Cleanup(broker.server.Close)

	previous := httpClient
	httpClient = broker.server.Client()
	t.Cleanup(func() { httpClient = previous })
	return broker
}

func (b *fakeGeminiBroker) key() string {
	return fmt.Sprintf(`{"broker_url":%q,"headers":{"X-Api-Key":"broker-secret"},"user_project":"proj-1"}`, b.server.URL)
}

func TestResolveGeminiAuthByKeyType(t *testing.T) {
	tests := []struct {
		name      string
		keyType   dto.GeminiKeyType
		rawKey    string
		wantName  string
		wantValue string
	}{
		{name: "empty type keeps api key header", keyType: "", rawKey: "AIza-1", wantName: "x-goog-api-key", wantValue: "AIza-1"},
		{name: "api_key", keyType: dto.GeminiKeyTypeAPIKey, rawKey: "AIza-2", wantName: "x-goog-api-key", wantValue: "AIza-2"},
		{name: "oauth_token sends bearer", keyType: dto.GeminiKeyTypeOAuthToken, rawKey: " ya29.static ", wantName: "Authorization", wantValue: "Bearer ya29.static"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := ResolveGeminiAuth(1, tt.rawKey, tt.keyType, "")
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, auth.HeaderName)
			assert.Equal(t, tt.wantValue, auth.HeaderValue)
		})
	}
}

func TestGeminiAuthApplyRemovesAPIKeyForOAuth(t *testing.T) {
	header := http.Header{}
	header.Set("x-goog-api-key", "should-be-removed")
	GeminiAuth{HeaderName: "Authorization", HeaderValue: "Bearer ya29.a", UserProject: "proj-1"}.Apply(header)

	assert.Empty(t, header.Get("x-goog-api-key"))
	assert.Equal(t, "Bearer ya29.a", header.Get("Authorization"))
	assert.Equal(t, "proj-1", header.Get("x-goog-user-project"))
}

func TestParseGeminiOAuthBrokerConfigRejectsInvalid(t *testing.T) {
	tests := []struct {
		name   string
		rawKey string
	}{
		{name: "not json", rawKey: "ya29.plain"},
		{name: "http url", rawKey: `{"broker_url":"http://broker.example.com/token"}`},
		{name: "missing url", rawKey: `{"headers":{"a":"b"}}`},
		{name: "bad method", rawKey: `{"broker_url":"https://broker.example.com/token","method":"DELETE"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseGeminiOAuthBrokerConfig(tt.rawKey)
			require.Error(t, err)
		})
	}
}

func TestParseGeminiOAuthBrokerResponseFormats(t *testing.T) {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		body       string
		wantTokens []string
		wantExpiry time.Time
	}{
		{
			name:       "oauth token endpoint style",
			body:       `{"access_token":"ya29.a","expires_in":1800,"token_type":"Bearer"}`,
			wantTokens: []string{"ya29.a"},
			wantExpiry: now.Add(1800 * time.Second),
		},
		{
			name:       "iam credentials style",
			body:       `{"accessToken":"ya29.b","expireTime":"2026-11-12T10:20:00Z"}`,
			wantTokens: []string{"ya29.b"},
			wantExpiry: time.Date(2026, 11, 12, 10, 20, 0, 0, time.UTC),
		},
		{
			name:       "token group",
			body:       `{"tokens":["ya29.c"," ya29.d ",""],"expire_time":"2026-11-12T10:30:00Z"}`,
			wantTokens: []string{"ya29.c", "ya29.d"},
			wantExpiry: time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC),
		},
		{
			name:       "missing expiry defaults to 30 minutes",
			body:       `{"access_token":"ya29.e"}`,
			wantTokens: []string{"ya29.e"},
			wantExpiry: now.Add(30 * time.Minute),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := parseGeminiOAuthBrokerResponse([]byte(tt.body), now)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTokens, set.tokens)
			assert.True(t, tt.wantExpiry.Equal(set.expiresAt), "expiry %v", set.expiresAt)
		})
	}

	_, err := parseGeminiOAuthBrokerResponse([]byte(`{"expires_in":1800}`), now)
	require.Error(t, err)
}

func TestGeminiOAuthBrokerCachesUntilRefreshMargin(t *testing.T) {
	expiresIn := atomic.Int64{}
	expiresIn.Store(1800)
	broker := newFakeGeminiBroker(t, func(n int64) string {
		return fmt.Sprintf(`{"access_token":"ya29.%d","expires_in":%d}`, n, expiresIn.Load())
	})

	auth, err := ResolveGeminiAuth(9101, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.1", auth.HeaderValue)
	assert.Equal(t, "proj-1", auth.UserProject)
	assert.Equal(t, "broker-secret", broker.authHdr.Load())

	auth, err = ResolveGeminiAuth(9101, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.1", auth.HeaderValue, "fresh token must come from cache")
	assert.EqualValues(t, 1, broker.calls.Load())

	// 剩余有效期不足 150 秒的 token 每次都会触发刷新
	expiresIn.Store(100)
	InvalidateGeminiOAuthToken(broker.key(), "ya29.1")
	_, err = ResolveGeminiAuth(9101, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	auth, err = ResolveGeminiAuth(9101, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.3", auth.HeaderValue)
	assert.EqualValues(t, 3, broker.calls.Load())
}

func TestGeminiOAuthBrokerConcurrentRefreshIsSingleFlight(t *testing.T) {
	release := make(chan struct{})
	broker := newFakeGeminiBroker(t, func(n int64) string {
		<-release
		return fmt.Sprintf(`{"access_token":"ya29.%d","expires_in":1800}`, n)
	})

	var wg sync.WaitGroup
	results := make([]string, 20)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			auth, err := ResolveGeminiAuth(9102, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
			if err == nil {
				results[i] = auth.HeaderValue
			}
		}(i)
	}
	// broker 收到第一次调用后放行；没赶上 single-flight 的协程会命中刷新后的缓存，同样不会再调 broker
	require.Eventually(t, func() bool { return broker.calls.Load() == 1 }, 5*time.Second, 10*time.Millisecond)
	close(release)
	wg.Wait()

	assert.EqualValues(t, 1, broker.calls.Load())
	for _, value := range results {
		assert.Equal(t, "Bearer ya29.1", value)
	}
}

func TestGeminiOAuthBrokerRoundRobinsTokenGroup(t *testing.T) {
	broker := newFakeGeminiBroker(t, func(n int64) string {
		return `{"tokens":["ya29.a","ya29.b"],"expires_in":1800}`
	})

	var got []string
	for i := 0; i < 4; i++ {
		auth, err := ResolveGeminiAuth(9103, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
		require.NoError(t, err)
		got = append(got, auth.BearerToken())
	}
	assert.Equal(t, []string{"ya29.a", "ya29.b", "ya29.a", "ya29.b"}, got)
	assert.EqualValues(t, 1, broker.calls.Load())
}

func TestInvalidateGeminiOAuthTokenOnlyDropsMatchingSet(t *testing.T) {
	broker := newFakeGeminiBroker(t, func(n int64) string {
		return fmt.Sprintf(`{"access_token":"ya29.%d","expires_in":1800}`, n)
	})

	_, err := ResolveGeminiAuth(9104, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)

	// 其他协程已经换过的旧 token 报 401，不能把当前新组冲掉
	InvalidateGeminiOAuthToken(broker.key(), "ya29.stale")
	auth, err := ResolveGeminiAuth(9104, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.1", auth.HeaderValue)

	InvalidateGeminiOAuthToken(broker.key(), "ya29.1")
	auth, err = ResolveGeminiAuth(9104, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.2", auth.HeaderValue)
}

func TestGeminiOAuthBrokerFailureFallsBackToUnexpiredToken(t *testing.T) {
	broker := newFakeGeminiBroker(t, func(n int64) string {
		return fmt.Sprintf(`{"access_token":"ya29.%d","expires_in":100}`, n)
	})

	auth, err := ResolveGeminiAuth(9105, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.1", auth.HeaderValue)

	// token 已进入刷新余量但尚未过期，broker 故障时继续用旧 token
	broker.status.Store(http.StatusBadGateway)
	auth, err = ResolveGeminiAuth(9105, broker.key(), dto.GeminiKeyTypeOAuthBroker, "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer ya29.1", auth.HeaderValue)

	// 没有任何可用 token 时把 broker 错误透出（换一份配置，避开上面的缓存）
	otherKey := fmt.Sprintf(`{"broker_url":%q}`, broker.server.URL+"/other")
	_, err = ResolveGeminiAuth(9106, otherKey, dto.GeminiKeyTypeOAuthBroker, "")
	require.ErrorContains(t, err, "status 502")
}

func TestInferGeminiKeyType(t *testing.T) {
	assert.Equal(t, dto.GeminiKeyTypeOAuthBroker, InferGeminiKeyType(` {"broker_url":"https://b"}`))
	assert.Equal(t, dto.GeminiKeyTypeOAuthToken, InferGeminiKeyType("ya29.a0Af"))
	assert.Equal(t, dto.GeminiKeyTypeAPIKey, InferGeminiKeyType("AIzaSy"))
	assert.Equal(t, dto.GeminiKeyTypeAPIKey, InferGeminiKeyType("AQ.Ab8"))
}

func TestResolveGeminiTaskAuthPrefersCurrentKeyForExpiredSnapshotToken(t *testing.T) {
	tests := []struct {
		name       string
		channelKey string
		taskKey    string
		wantName   string
		wantValue  string
	}{
		{name: "api key snapshot is reused", channelKey: "AIza-new", taskKey: "AIza-old", wantName: "x-goog-api-key", wantValue: "AIza-old"},
		{name: "snapshot ya29 is replaced by current channel token", channelKey: "ya29.new", taskKey: "ya29.old", wantName: "Authorization", wantValue: "Bearer ya29.new"},
		{name: "empty snapshot falls back to channel key", channelKey: "AIza-new", taskKey: "", wantName: "x-goog-api-key", wantValue: "AIza-new"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &model.Channel{Id: 7, Key: tt.channelKey}
			auth, err := ResolveGeminiTaskAuth(channel, tt.taskKey)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, auth.HeaderName)
			assert.Equal(t, tt.wantValue, auth.HeaderValue)
		})
	}
}
