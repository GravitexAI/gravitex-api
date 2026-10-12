package helper

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetAndValidateClaudeRequestPreservesSafeguards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4-5",
		"messages":[{"role":"user","content":"hello"}],
		"safeguards":{"mode":"auto","actions":["bash"]}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	request, err := GetAndValidateClaudeRequest(c)
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"auto","actions":["bash"]}`, string(request.Safeguards))

	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	var output map[string]json.RawMessage
	require.NoError(t, common.Unmarshal(encoded, &output))
	require.JSONEq(t, `{"mode":"auto","actions":["bash"]}`, string(output["safeguards"]))
}
