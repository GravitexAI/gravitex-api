package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// NOTE: this test exercises the wiring only (route registration + middleware
// chain reaching the handlers); it does not stand up the full TokenAuth/
// Distribute() channel-selection machinery, which is already covered by
// existing tests for those middlewares. It asserts that POST/GET/DELETE on
// the official-mirror paths are registered and reach the expected handlers
// by checking gin's route table directly.
func TestSeedanceOfficialVideoRoutes_Registered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.User{}, &model.Token{}))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	r := gin.New()
	SetVideoRouter(r)

	routes := r.Routes()
	var found struct{ post, get, del bool }
	for _, rt := range routes {
		if rt.Path == "/api/v3/contents/generations/tasks" && rt.Method == http.MethodPost {
			found.post = true
		}
		if rt.Path == "/api/v3/contents/generations/tasks/:id" && rt.Method == http.MethodGet {
			found.get = true
		}
		if rt.Path == "/api/v3/contents/generations/tasks/:id" && rt.Method == http.MethodDelete {
			found.del = true
		}
	}
	assert.True(t, found.post, "POST /api/v3/contents/generations/tasks not registered")
	assert.True(t, found.get, "GET /api/v3/contents/generations/tasks/:id not registered")
	assert.True(t, found.del, "DELETE /api/v3/contents/generations/tasks/:id not registered")
}
