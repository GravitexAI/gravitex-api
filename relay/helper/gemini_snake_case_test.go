package helper

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiSnakeCaseMaxTokensValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		value   string
		invalid bool
	}{
		{"100", false}, {"0", false}, {fmt.Sprint(maxTokensLimit), false},
		{fmt.Sprint(maxTokensLimit + 1), true}, {"18446744073686646784", true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1beta/models/test:generateContent", strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}],"generation_config":{"max_output_tokens":`+tc.value+`}}`))
			c.Request.Header.Set("Content-Type", "application/json")
			req, err := GetAndValidateGeminiRequest(c)
			if tc.invalid {
				require.ErrorContains(t, err, "maxOutputTokens is invalid")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, req.GenerationConfig.MaxOutputTokens)
			assert.Equal(t, tc.value, fmt.Sprint(*req.GenerationConfig.MaxOutputTokens))
		})
	}
}
