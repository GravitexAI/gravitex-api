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
	"github.com/QuantumNous/new-api/setting/system_setting"
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
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=NotARealAction&Version=2024-01-01", strings.NewReader(`{}`))

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "InvalidAction")
}

func TestSeedanceOfficialAssetDispatch_ListAssets_ForwardsBodyAndReturnsRawResponse(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	require.NoError(t, model.InsertUserAssetGroup(&model.UserAssetGroup{
		UserId: 1, ChannelId: 1, GroupId: "group-1", GroupType: model.GroupTypeAIGC,
	}))

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
	reqBody := `{"Filter":{"SortBy":"CreateTime","SortOrder":"Desc"},"PageNumber":1,"PageSize":10}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=ListAssets&Version=2024-01-01", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ListAssets", capturedAction)
	filter := capturedBody["Filter"].(map[string]interface{})
	assert.Equal(t, "CreateTime", filter["SortBy"], "SortBy must be forwarded verbatim, not dropped")
	assert.Equal(t, []string{"group-1"}, filter["GroupIds"], "GroupIds must be restricted to the user's own groups")
	assert.Contains(t, w.Body.String(), `"TotalCount":0`)
}

func TestSeedanceOfficialAssetDispatch_ListAssetGroups_NoLocalGroups_ReturnsEmptyWithoutCallingUpstream(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	called := false
	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		called = true
		return nil, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 1)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=ListAssetGroups&Version=2024-01-01", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, called, "must not call upstream when the user owns no asset groups")
	assert.Contains(t, w.Body.String(), `"TotalCount":0`)
}

func TestSeedanceOfficialAssetDispatch_CreateAssetGroup_PersistsLocally(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		assert.Equal(t, service.ByteplusGroupTypeAIGC, body["GroupType"])
		return map[string]interface{}{"Id": "group-20260710-abcde"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=CreateAssetGroup&Version=2024-01-01",
		strings.NewReader(`{"Name":"角色A","Description":"desc"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)

	group, err := model.GetUserAssetGroupByUserIdAndGroupId(42, "group-20260710-abcde")
	require.NoError(t, err)
	assert.Equal(t, "角色A", group.Name)
	assert.Equal(t, "aigc", group.GroupType)
}

func TestSeedanceOfficialAssetDispatch_CreateAsset_PersistsLocallyAndUsableByCheckUserOwnsAssets(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"Id": "asset-20260710-xyz"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	reqBody := `{"GroupId":"group-1","URL":"https://cdn.example.com/face.jpg","AssetType":"Image","Name":"face.jpg"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=CreateAsset&Version=2024-01-01", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)

	asset, err := model.GetUserAssetByUserIdAndVirtualId(42, "asset-20260710-xyz")
	require.NoError(t, err)
	assert.Equal(t, "group-1", asset.GroupId)
	assert.Equal(t, "asset://asset-20260710-xyz", asset.AssetUrl)
	assert.Equal(t, "Image", asset.AssetType)

	notOwned, err := model.CheckUserOwnsAssets(42, []string{"asset-20260710-xyz"})
	require.NoError(t, err)
	assert.Empty(t, notOwned, "asset created via the mirror endpoint must be recognized as owned by the user")
}

func TestSeedanceOfficialAssetDispatch_UpdateAsset_UpdatesLocalName(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	require.NoError(t, model.InsertUserAsset(&model.UserAsset{
		UserId: 42, ChannelId: 1, VirtualId: "asset-upd-1", AssetUrl: "asset://asset-upd-1", Filename: "old.jpg", AssetType: "Image", Status: "active",
	}))

	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{"Id": "asset-upd-1"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=UpdateAsset&Version=2024-01-01",
		strings.NewReader(`{"Id":"asset-upd-1","Name":"new.jpg"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)
	asset, err := model.GetUserAssetByUserIdAndVirtualId(42, "asset-upd-1")
	require.NoError(t, err)
	assert.Equal(t, "new.jpg", asset.Filename)
}

func TestSeedanceOfficialAssetDispatch_DeleteAsset_RemovesLocalRecord(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	require.NoError(t, model.InsertUserAsset(&model.UserAsset{
		UserId: 42, ChannelId: 1, VirtualId: "asset-del-1", AssetUrl: "asset://asset-del-1", AssetType: "Image", Status: "active",
	}))

	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=DeleteAsset&Version=2024-01-01",
		strings.NewReader(`{"Id":"asset-del-1"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)
	_, err := model.GetUserAssetByUserIdAndVirtualId(42, "asset-del-1")
	assert.Error(t, err, "record should no longer exist")
}

func TestSeedanceOfficialAssetDispatch_DeleteAssetGroup_RemovesLocalRecordAndCascadesAssets(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	require.NoError(t, model.InsertUserAssetGroup(&model.UserAssetGroup{
		UserId: 42, ChannelId: 1, GroupId: "group-del-1", GroupType: "aigc", Name: "g",
	}))
	require.NoError(t, model.InsertUserAsset(&model.UserAsset{
		UserId: 42, ChannelId: 1, GroupId: "group-del-1", VirtualId: "asset-in-group", AssetUrl: "asset://asset-in-group", AssetType: "Image", Status: "active",
	}))

	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		return map[string]interface{}{}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=DeleteAssetGroup&Version=2024-01-01",
		strings.NewReader(`{"Id":"group-del-1"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)
	_, err := model.GetUserAssetGroupByUserIdAndGroupId(42, "group-del-1")
	assert.Error(t, err)
	_, err = model.GetUserAssetByUserIdAndVirtualId(42, "asset-in-group")
	assert.Error(t, err, "cascade delete must remove assets belonging to the deleted group")
}

func TestSeedanceOfficialAssetDispatch_CreateVisualValidateSession_ForcesCallbackURL(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	var capturedBody map[string]interface{}
	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		capturedBody = body
		return map[string]interface{}{"BytedToken": "tok-1", "H5Link": "https://byteplus.example/verify", "CallbackURL": body["CallbackURL"]}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=CreateVisualValidateSession&Version=2024-01-01",
		strings.NewReader(`{"CallbackURL":"https://attacker.example/steal"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)
	expected := strings.TrimRight(system_setting.ServerAddress, "/") + VisualValidateCallbackPath
	assert.Equal(t, expected, capturedBody["CallbackURL"], "client-supplied CallbackURL must be overridden, not forwarded")
	assert.Contains(t, w.Body.String(), "BytedToken")
}

func TestSeedanceOfficialAssetDispatch_CreateVisualValidateSession_QuotaExceeded_Returns403WithoutCallingUpstream(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	withByteplusAssetGroupLimit(t, 0)

	upstreamCalled := false
	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		upstreamCalled = true
		return map[string]interface{}{"BytedToken": "tok-should-not-be-issued"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=CreateVisualValidateSession&Version=2024-01-01",
		strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "QuotaExceeded")
	assert.False(t, upstreamCalled, "quota check must block the H5 session from ever being issued, not just the later sync")
}

func TestSeedanceOfficialAssetDispatch_GetVisualValidateResult_PersistsLivenessFaceGroup(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)

	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		assert.Equal(t, "GetVisualValidateResult", action)
		return map[string]interface{}{"GroupId": "group-face-20260710-abcde"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=GetVisualValidateResult&Version=2024-01-01",
		strings.NewReader(`{"BytedToken":"tok-1"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	require.Equal(t, http.StatusOK, w.Code)
	group, err := model.GetUserAssetGroupByUserIdAndGroupId(42, "group-face-20260710-abcde")
	require.NoError(t, err)
	assert.Equal(t, model.GroupTypeLivenessFace, group.GroupType)
}

func withByteplusAssetGroupLimit(t *testing.T, limit int) {
	t.Helper()
	// Force the lazy env-override read once so a direct assignment below isn't
	// clobbered by GetByteplusAssetGroupLimit's first-call ENV lookup.
	system_setting.GetByteplusAssetGroupLimit()
	original := system_setting.ByteplusAssetGroupLimit
	system_setting.ByteplusAssetGroupLimit = limit
	t.Cleanup(func() { system_setting.ByteplusAssetGroupLimit = original })
}

func TestSeedanceOfficialAssetDispatch_CreateAssetGroup_QuotaExceeded_Returns403WithoutCallingUpstream(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	withByteplusAssetGroupLimit(t, 0)

	upstreamCalled := false
	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		upstreamCalled = true
		return map[string]interface{}{"Id": "group-should-not-be-created"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=CreateAssetGroup&Version=2024-01-01",
		strings.NewReader(`{"Name":"角色A"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "QuotaExceeded")
	assert.False(t, upstreamCalled, "quota check must block the upstream call, not just the local sync")
}

func TestSeedanceOfficialAssetDispatch_GetVisualValidateResult_QuotaExceeded_Returns403WithoutCallingUpstream(t *testing.T) {
	setupSeedanceAssetTestDB(t)
	newSeedanceAssetChannel(t, 1)
	withByteplusAssetGroupLimit(t, 0)

	upstreamCalled := false
	original := seedanceAssetRawAction
	seedanceAssetRawAction = func(cfg service.ByteplusAssetConfig, action string, body map[string]interface{}) (map[string]interface{}, error) {
		upstreamCalled = true
		return map[string]interface{}{"GroupId": "group-should-not-be-created"}, nil
	}
	t.Cleanup(func() { seedanceAssetRawAction = original })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 42)
	c.Set(string(constant.ContextKeyUsingGroup), seedanceAssetTestGroup)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v3/seedance?Action=GetVisualValidateResult&Version=2024-01-01",
		strings.NewReader(`{"BytedToken":"tok-1"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SeedanceOfficialAssetDispatch(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "QuotaExceeded")
	assert.False(t, upstreamCalled, "quota check must block the upstream call, not just the local sync")
}
