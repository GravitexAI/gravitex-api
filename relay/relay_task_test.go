package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoFetchByIDRespBodyBuilder_RawMirror_CachedTerminalTask_ReturnsRawData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupRelayTaskTestDB(t)

	rawUpstreamJSON := `{"id":"cgt-xyz","model":"seedance-2-0","status":"succeeded","content":{"video_url":"https://example.com/v.mp4"},"billing_processed":true}`
	task := &model.Task{
		TaskID:    "cgt-xyz",
		Platform:  "54", // channel type 54 (ChannelTypeDoubaoVideo) resolves via GetTaskAdaptor to taskdoubao.TaskAdaptor, which implements OpenAIVideoConverter
		UserId:    77,
		ChannelId: 1,
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		Quota:     100,
		// Data must contain billing_processed:true — isVideoTaskBillingProcessed reads this flag
		// (not Quota) to decide that tryRealtimeFetch should skip the live upstream refetch.
		Data: []byte(rawUpstreamJSON),
	}
	require.NoError(t, model.DB.Create(task).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Type: 54}).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(common.KeySeedanceRawMirror, true)
	c.Set("id", 77)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/video/generations/cgt-xyz", nil)
	c.Params = gin.Params{{Key: "task_id", Value: "cgt-xyz"}}

	respBody, taskErr := videoFetchByIDRespBodyBuilder(c)
	require.Nil(t, taskErr)
	assert.JSONEq(t, rawUpstreamJSON, string(respBody))
}

func setupRelayTaskTestDB(t *testing.T) {
	t.Helper()
	// See existing setupModelListControllerTestDB in controller/model_list_test.go
	// for the established in-memory-sqlite fixture pattern this mirrors.
	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db := mustOpenTestSqlite(t, dsn)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.User{}))
}

func mustOpenTestSqlite(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}
