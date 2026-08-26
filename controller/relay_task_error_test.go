package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRespondTaskError_RawMirror_WritesRawBodyVerbatim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(common.KeySeedanceRawMirror, true)

	rawUpstreamError := `{"error":{"code":"InvalidParameter","message":"the specified asset is not an image","param":"content[2].image_url.url","type":"BadRequest"}}`
	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    rawUpstreamError,
		StatusCode: http.StatusBadRequest,
		RawBody:    []byte(rawUpstreamError),
	}

	respondTaskError(c, taskErr)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, rawUpstreamError, w.Body.String())
}

func TestRespondTaskError_NonMirror_UsesPlatformShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	// common.KeySeedanceRawMirror intentionally NOT set — existing behavior must be unchanged.

	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    "some upstream error",
		StatusCode: http.StatusBadRequest,
		RawBody:    []byte(`{"error":{"code":"InvalidParameter"}}`), // present but must be ignored
	}

	respondTaskError(c, taskErr)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `"code":"fail_to_fetch_task"`)
	assert.NotContains(t, w.Body.String(), "InvalidParameter")
}

func TestRespondTaskError_LyriaRawMirror_WritesRawBodyVerbatim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("lyria_raw_mirror", true)
	raw := `{"error":{"code":"invalid_request","message":"provider error"}}`
	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    raw,
		StatusCode: http.StatusBadRequest,
		RawBody:    []byte(raw),
	}

	respondTaskError(c, taskErr)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, raw, w.Body.String())
}

func TestShouldRetryTaskRelay_LyriaRawMirrorDoesNotRetryProviderResponse(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("lyria_raw_mirror", true)
	taskErr := &dto.TaskError{StatusCode: http.StatusBadGateway}

	require.False(t, shouldRetryTaskRelay(c, 60, taskErr, 3))
}
