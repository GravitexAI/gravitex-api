package gemini

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 轮询拿不到渠道设置：ya29 token 必须走 Bearer，且不能再带 x-goog-api-key
func TestFetchTaskSendsBearerForOAuthTokenKey(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		wantAuth      string
		wantGoogleKey string
	}{
		{name: "oauth token", key: "ya29.poll", wantAuth: "Bearer ya29.poll"},
		{name: "api key", key: "AIza-poll", wantGoogleKey: "AIza-poll"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotAuth, gotGoogleKey string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				gotGoogleKey = r.Header.Get("x-goog-api-key")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer upstream.Close()

			taskID := markOmniTaskID("interaction-1")
			resp, err := (&TaskAdaptor{}).FetchTask(upstream.URL, tt.key, map[string]any{"task_id": taskID}, "")
			require.NoError(t, err)
			_ = resp.Body.Close()

			assert.Equal(t, tt.wantAuth, gotAuth)
			assert.Equal(t, tt.wantGoogleKey, gotGoogleKey)
		})
	}
}
