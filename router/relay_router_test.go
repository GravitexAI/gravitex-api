package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListModelsSupportsOpenAIAndGeminiAuthentication(t *testing.T) {
	setupRelayRouterTestDB(t)

	user := model.User{
		Username: "models-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         user.Id,
		Key:            "modelstestkey",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)

	engine := gin.New()
	SetRelayRouter(engine)

	tests := []struct {
		name           string
		path           string
		headerName     string
		expectedObject string
		expectedField  string
	}{
		{
			name:           "OpenAI bearer token",
			path:           "/v1/models",
			headerName:     "Authorization",
			expectedObject: "list",
			expectedField:  "data",
		},
		{
			name:          "Gemini API key header",
			path:          "/v1/models",
			headerName:    "x-goog-api-key",
			expectedField: "models",
		},
		{
			name:          "Gemini API key query",
			path:          "/v1/models?key=modelstestkey",
			expectedField: "models",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.headerName != "" {
				value := "modelstestkey"
				if test.headerName == "Authorization" {
					value = "Bearer " + value
				}
				request.Header.Set(test.headerName, value)
			}

			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var payload map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.Contains(t, payload, test.expectedField)
			assert.NotContains(t, payload, "error")
			if test.expectedObject != "" {
				assert.Equal(t, test.expectedObject, payload["object"])
			}
		})
	}
}

func TestListModelsAllowsExhaustedTokenOnlyForCatalog(t *testing.T) {
	setupRelayRouterTestDB(t)

	user := model.User{
		Username: "exhausted-models-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	tokens := []model.Token{
		{UserId: user.Id, Key: "exhaustedmodelkey", Status: common.TokenStatusExhausted, ExpiredTime: -1},
		{UserId: user.Id, Key: "enabledzeromodelkey", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 0},
		{UserId: user.Id, Key: "expiredmodelkey", Status: common.TokenStatusExhausted, ExpiredTime: common.GetTimestamp() - 1},
		{UserId: user.Id, Key: "disabledmodelkey", Status: common.TokenStatusDisabled, ExpiredTime: -1},
	}
	for i := range tokens {
		require.NoError(t, model.DB.Create(&tokens[i]).Error)
	}

	engine := gin.New()
	SetRelayRouter(engine)
	tests := []struct {
		name       string
		method     string
		path       string
		headerName string
		key        string
		wantStatus int
	}{
		{name: "OpenAI catalog", path: "/v1/models", headerName: "Authorization", key: "exhaustedmodelkey", wantStatus: http.StatusOK},
		{name: "enabled token with zero quota transitions to exhausted", path: "/v1/models", headerName: "Authorization", key: "enabledzeromodelkey", wantStatus: http.StatusOK},
		{name: "Gemini catalog with API key header", path: "/v1/models", headerName: "x-goog-api-key", key: "exhaustedmodelkey", wantStatus: http.StatusOK},
		{name: "Gemini catalog with API key query", path: "/v1/models?key=exhaustedmodelkey", key: "exhaustedmodelkey", wantStatus: http.StatusOK},
		{name: "Gemini v1beta catalog", path: "/v1beta/models", headerName: "x-goog-api-key", key: "exhaustedmodelkey", wantStatus: http.StatusOK},
		{name: "Gemini OpenAI-compatible catalog", path: "/v1beta/openai/models", headerName: "Authorization", key: "exhaustedmodelkey", wantStatus: http.StatusOK},
		{name: "expired token", path: "/v1/models", headerName: "Authorization", key: "expiredmodelkey", wantStatus: http.StatusUnauthorized},
		{name: "disabled token", path: "/v1/models", headerName: "Authorization", key: "disabledmodelkey", wantStatus: http.StatusUnauthorized},
		{name: "model detail", path: "/v1/models/gpt-4o", headerName: "Authorization", key: "exhaustedmodelkey", wantStatus: http.StatusUnauthorized},
		{name: "billable relay", method: http.MethodPost, path: "/v1/chat/completions", headerName: "Authorization", key: "exhaustedmodelkey", wantStatus: http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			method := test.method
			if method == "" {
				method = http.MethodGet
			}
			var request *http.Request
			if method == http.MethodPost {
				request = httptest.NewRequest(method, test.path, strings.NewReader(`{"model":"gpt-4o"}`))
			} else {
				request = httptest.NewRequest(method, test.path, nil)
			}
			if test.headerName != "" {
				value := test.key
				if test.headerName == "Authorization" {
					value = "Bearer " + value
				}
				request.Header.Set(test.headerName, value)
			}
			engine.ServeHTTP(recorder, request)
			require.Equal(t, test.wantStatus, recorder.Code)
			if test.key == "enabledzeromodelkey" {
				token, err := model.GetTokenByKey(test.key, true)
				require.NoError(t, err)
				assert.Equal(t, common.TokenStatusExhausted, token.Status)
			}
		})
	}
}

func setupRelayRouterTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalIsMasterNode := common.IsMasterNode
	originalRedisEnabled := common.RedisEnabled
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.RedisEnabled = false
	common.SQLitePath = fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}, &model.Ability{}))

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.IsMasterNode = originalIsMasterNode
		common.RedisEnabled = originalRedisEnabled
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}
