package model

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCollectConfiguredRequestHeadersDefaultsToDisabled(t *testing.T) {
	got := collectConfiguredRequestHeaders(http.Header{
		"User-Agent": {"client"},
	}, dto.RequestHeadersLogSetting{})

	require.Empty(t, got)
}

func TestCollectConfiguredRequestHeadersSelectedPreservesOriginalHeaders(t *testing.T) {
	got := collectConfiguredRequestHeaders(http.Header{
		"User-Agent":   {"client"},
		"X-Request-ID": {"req-123"},
		"Cookie":       {"session=abc"},
	}, dto.RequestHeadersLogSetting{
		Enabled: true,
		Mode:    "selected",
		Headers: []string{"x-request-id", "COOKIE"},
	})

	require.Equal(t, map[string]string{
		"X-Request-ID": "req-123",
		"Cookie":       "session=abc",
	}, got)
}

func TestCollectConfiguredRequestHeadersAllPreservesEveryHeader(t *testing.T) {
	got := collectConfiguredRequestHeaders(http.Header{
		"Authorization": {"Bearer secret"},
		"User-Agent":    {"client"},
	}, dto.RequestHeadersLogSetting{
		Enabled: true,
		Mode:    "all",
	})

	require.Equal(t, map[string]string{
		"Authorization": "Bearer secret",
		"User-Agent":    "client",
	}, got)
}

func TestAppendConfiguredClientRequestHeadersUsesStoredUserSetting(t *testing.T) {
	setupUserUpdateTestState(t)

	user := User{Id: 91, Username: "header-log-user", Password: "password"}
	user.SetSetting(dto.UserSetting{RequestHeadersLog: &dto.RequestHeadersLogSetting{
		Enabled: true,
		Mode:    "selected",
		Headers: []string{"x-request-id"},
	}})
	require.NoError(t, DB.Create(&user).Error)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("X-Request-ID", "req-91")
	request.Header.Set("Authorization", "Bearer secret")
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	other := map[string]interface{}{}
	AppendConfiguredClientRequestHeaders(context, user.Id, other)

	require.Equal(t, map[string]string{"X-Request-Id": "req-91"}, other["client_request_headers"])
}
