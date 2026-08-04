package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupSeedanceCancelTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
}

func TestRelayTaskCancel_QueuedTask_MarksCancelledLocally(t *testing.T) {
	setupSeedanceCancelTestDB(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	base := upstream.URL
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeDoubaoVideo, Key: "sk-upstream", BaseURL: &base}).Error)
	task := &model.Task{TaskID: "cgt-cancel-me", UserId: 55, ChannelId: 1, Status: model.TaskStatusQueued, Platform: "54"}
	require.NoError(t, model.DB.Create(task).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 55)
	c.Params = gin.Params{{Key: "id", Value: "cgt-cancel-me"}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v3/contents/generations/tasks/cgt-cancel-me", nil)

	RelayTaskCancel(c)

	require.Equal(t, http.StatusOK, w.Code)

	updated, exist, err := model.GetByTaskId(55, "cgt-cancel-me")
	require.NoError(t, err)
	require.True(t, exist)
	assert.Equal(t, model.TaskStatusCancelled, updated.Status)
}

func TestRelayTaskCancel_TerminalTask_DeletesLocalRecord(t *testing.T) {
	setupSeedanceCancelTestDB(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	base := upstream.URL
	require.NoError(t, model.DB.Create(&model.Channel{Id: 2, Type: constant.ChannelTypeDoubaoVideo, Key: "sk-upstream", BaseURL: &base}).Error)
	task := &model.Task{TaskID: "cgt-done", UserId: 55, ChannelId: 2, Status: model.TaskStatusSuccess, Platform: "54"}
	require.NoError(t, model.DB.Create(task).Error)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 55)
	c.Params = gin.Params{{Key: "id", Value: "cgt-done"}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v3/contents/generations/tasks/cgt-done", nil)

	RelayTaskCancel(c)

	require.Equal(t, http.StatusOK, w.Code)

	_, exist, err := model.GetByTaskId(55, "cgt-done")
	require.NoError(t, err)
	assert.False(t, exist)
}

func TestRelayTaskCancel_TaskNotFound_Returns404(t *testing.T) {
	setupSeedanceCancelTestDB(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 55)
	c.Params = gin.Params{{Key: "id", Value: "does-not-exist"}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v3/contents/generations/tasks/does-not-exist", nil)

	RelayTaskCancel(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
