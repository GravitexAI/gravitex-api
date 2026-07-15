package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTokenModelAccessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Model{}, &model.Vendor{}))
	model.DB = db
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			require.NoError(t, sqlDB.Close())
		}
	})
	return db
}

func TestIsModelAllowedByTokenUsesModelOrVendorLimit(t *testing.T) {
	db := setupTokenModelAccessTestDB(t)
	vendor := model.Vendor{Name: "vendor-a", Status: 1}
	require.NoError(t, db.Create(&vendor).Error)
	require.NoError(t, db.Create(&model.Model{ModelName: "vendor-model", VendorID: vendor.Id, Status: 1}).Error)
	disabledModel := model.Model{ModelName: "disabled-model", VendorID: vendor.Id, Status: 1}
	require.NoError(t, db.Create(&disabledModel).Error)
	require.NoError(t, db.Model(&disabledModel).Update("status", 0).Error)

	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{"explicit-model": true})
	common.SetContextKey(ctx, constant.ContextKeyTokenVendorLimit, map[int]bool{vendor.Id: true})

	require.True(t, IsModelAllowedByToken(ctx, "explicit-model"))
	require.True(t, IsModelAllowedByToken(ctx, "vendor-model"))
	require.False(t, IsModelAllowedByToken(ctx, "disabled-model"))
	require.False(t, IsModelAllowedByToken(ctx, "other-model"))
}

func TestIsTokenModelAccessLimitedIncludesVendorLimit(t *testing.T) {
	ctx, _ := gin.CreateTestContext(nil)
	require.False(t, IsTokenModelAccessLimited(ctx))
	common.SetContextKey(ctx, constant.ContextKeyTokenVendorLimitEnabled, true)
	require.True(t, IsTokenModelAccessLimited(ctx))
}
