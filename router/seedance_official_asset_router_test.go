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

func TestSeedanceOfficialAssetRoute_Registered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}, &model.User{}, &model.Token{}, &model.UserAsset{}, &model.UserAssetGroup{}))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)

	r := gin.New()
	SetVideoRouter(r)

	found := false
	for _, rt := range r.Routes() {
		if rt.Path == "/api/v3/seedance" && rt.Method == http.MethodPost {
			found = true
		}
	}
	assert.True(t, found, "POST /api/v3/seedance not registered")
}
