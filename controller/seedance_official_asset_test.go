package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

func setupSeedanceAssetTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.UserAsset{}, &model.UserAssetGroup{}, &model.Ability{}))

	// getByteplusEnabledChannels resolves channels through the in-memory channel
	// cache (model.GetAssetSupportedChannelsByGroup), not a direct DB query, so
	// the cache must be enabled and synced for the test channel to be visible.
	previousCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCacheEnabled })
}

// seedanceAssetTestGroup is the channel group used by both the test channel/ability
// fixtures and the dispatched request context, so getByteplusEnabledChannels resolves
// the fixture channel. "default" is used (rather than "") because GORM's
// `default:'default'` tag on Channel.Group silently rewrites an empty string to
// "default" on insert, so the Ability row must match that same value.
const seedanceAssetTestGroup = "default"

func newSeedanceAssetChannel(t *testing.T, id int) {
	t.Helper()
	settings := `{"byteplus_asset_ak":"ak","byteplus_asset_sk":"sk"}`
	require.NoError(t, model.DB.Create(&model.Channel{
		Id: id, Type: constant.ChannelTypeDoubaoVideo, Key: "sk-x",
		Status: common.ChannelStatusEnabled, Group: seedanceAssetTestGroup, Models: "seedance-2-0-official",
		OtherSettings: settings,
	}).Error)
	// InitChannelCache only pre-initializes group buckets that appear in the
	// abilities table, so an Ability row for the channel's group is required
	// for the channel to be visible to GetAssetSupportedChannelsByGroup.
	require.NoError(t, model.DB.Create(&model.Ability{
		Group: seedanceAssetTestGroup, Model: "seedance-2-0-official", ChannelId: id, Enabled: true,
	}).Error)
	model.InitChannelCache()
}

func TestSeedanceOfficialAssetDispatch_UnsupportedAction_Returns400(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/ark/seedance/v3?Action=NotARealAction&Version=2024-01-01", strings.NewReader(`{}`))

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "InvalidAction")
}

func TestSeedanceOfficialAssetDispatch_ListAssets_ForwardsBodyAndReturnsRawResponse(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	var capturedAction string
	var capturedBody map[string]interface{}
	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		capturedAction = action
		capturedBody = body
		return map[string]interface{}{
			"ResponseMetadata": map[string]interface{}{"Action": "ListAssets"},
			"Result":           map[string]interface{}{"Items": []interface{}{}, "TotalCount": 0},
		}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	reqBody := `{"Filter":{"GroupIds":["group-1"],"SortBy":"CreateTime","SortOrder":"Desc"},"PageNumber":1,"PageSize":10}`
	c.Request = httptest.NewRequest(http.MethodPost, "/ark/seedance/v3?Action=ListAssets&Version=2024-01-01", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ListAssets", capturedAction)
	filter := capturedBody["Filter"].(map[string]interface{})
	assert.Equal(t, "CreateTime", filter["SortBy"], "SortBy must be forwarded verbatim, not dropped")
	assert.Contains(t, w.Body.String(), `"TotalCount":0`)
}
