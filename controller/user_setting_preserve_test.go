package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Saving the notification form must not wipe settings owned by other
// endpoints (admin grants, sidebar, language, billing preference).
func TestUpdateUserSettingPreservesFieldsOutsideNotificationForm(t *testing.T) {
	previousDB := model.DB
	previousRedis := common.RedisEnabled
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.User{}))
	model.DB = database
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		common.RedisEnabled = previousRedis
	})

	user := &model.User{Username: "setting-preserve", Password: "password123", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	user.SetSetting(dto.UserSetting{
		NotifyType:            dto.NotifyTypeWebhook,
		QuotaWarningThreshold: 100,
		WebhookUrl:            "https://hooks.example.com/old",
		WebhookSecret:         "old-secret",
		AllowNegativeBalance:  true,
		RequestHeadersLog:     &dto.RequestHeadersLogSetting{Enabled: true, Mode: "include", Headers: []string{"User-Agent"}},
		SidebarModules:        `{"chat":true}`,
		BillingPreference:     "wallet_first",
		Language:              "en",
	})
	require.NoError(t, model.DB.Create(user).Error)

	body := `{"notify_type":"email","quota_warning_threshold":200,"notification_email":"ops@example.com","record_ip_log":true}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/user/setting", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", user.Id)

	UpdateUserSetting(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	saved, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	setting := saved.GetSetting()

	// Form-owned fields follow the request; the previous webhook config is dropped.
	assert.Equal(t, dto.NotifyTypeEmail, setting.NotifyType)
	assert.Equal(t, float64(200), setting.QuotaWarningThreshold)
	assert.Equal(t, "ops@example.com", setting.NotificationEmail)
	assert.True(t, setting.RecordIpLog)
	assert.Empty(t, setting.WebhookUrl)
	assert.Empty(t, setting.WebhookSecret)

	// Fields managed elsewhere survive.
	assert.True(t, setting.AllowNegativeBalance)
	require.NotNil(t, setting.RequestHeadersLog)
	assert.True(t, setting.RequestHeadersLog.Enabled)
	assert.Equal(t, []string{"User-Agent"}, setting.RequestHeadersLog.Headers)
	assert.Equal(t, `{"chat":true}`, setting.SidebarModules)
	assert.Equal(t, "wallet_first", setting.BillingPreference)
	assert.Equal(t, "en", setting.Language)
}
