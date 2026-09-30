package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: ChannelInfo{},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]interface{}{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}

func TestCountChannelTagsIgnoresPaginationAppliedForPageQuery(t *testing.T) {
	setupChannelStatusTest(t)

	for _, tag := range []string{"tag-a", "tag-b", "tag-c"} {
		channel := Channel{Name: tag, Tag: &tag, Status: common.ChannelStatusEnabled}
		require.NoError(t, DB.Create(&channel).Error)
	}

	query := DB.Model(&Channel{}).Where("status = ?", common.ChannelStatusEnabled)
	page, err := GetPaginatedChannelTags(query, 1, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)

	total, err := CountChannelTags(query)
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
}

func TestSaveStatusStateAcceptsUnchangedUpdateForExistingChannel(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{Name: "unchanged-status", Status: common.ChannelStatusAutoDisabled}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, DB.Exec("CREATE TRIGGER ignore_channel_status_update BEFORE UPDATE ON channels BEGIN SELECT RAISE(IGNORE); END").Error)
	t.Cleanup(func() {
		DB.Exec("DROP TRIGGER IF EXISTS ignore_channel_status_update")
	})

	require.NoError(t, channel.saveStatusState())
}

func TestChannelSoftDeleteDisablesOnlyItsAbilities(t *testing.T) {
	setupChannelStatusTest(t)

	tag := "shared-tag"
	deletedChannel := Channel{Name: "delete-one", Key: "secret-a", Models: "shared-model,exclusive-model", Group: "default", Tag: &tag, Status: common.ChannelStatusEnabled}
	otherChannel := Channel{Name: "keep-one", Key: "secret-b", Models: "shared-model", Group: "default", Tag: &tag, Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(&deletedChannel).Error)
	require.NoError(t, DB.Create(&otherChannel).Error)
	require.NoError(t, deletedChannel.AddAbilities(nil))
	require.NoError(t, otherChannel.AddAbilities(nil))

	require.NoError(t, deletedChannel.Delete())
	assert.False(t, UpdateChannelStatus(deletedChannel.Id, "", common.ChannelStatusEnabled, "manual operation"))

	var stored Channel
	require.NoError(t, DB.First(&stored, deletedChannel.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, stored.Status)
	var deletedAbility Ability
	require.NoError(t, DB.Where("channel_id = ? and model = ?", deletedChannel.Id, "shared-model").First(&deletedAbility).Error)
	assert.False(t, deletedAbility.Enabled)
	var unaffectedAbility Ability
	require.NoError(t, DB.Where("channel_id = ? and model = ?", otherChannel.Id, "shared-model").First(&unaffectedAbility).Error)
	assert.True(t, unaffectedAbility.Enabled)
	assert.Contains(t, GetEnabledModels(), "shared-model")
	assert.NotContains(t, GetEnabledModels(), "exclusive-model")
	require.NoError(t, EnableChannelByTag("shared-tag"))
	require.NoError(t, DB.First(&stored, deletedChannel.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, stored.Status)
	require.NoError(t, DB.Where("channel_id = ? and model = ?", deletedChannel.Id, "shared-model").First(&deletedAbility).Error)
	assert.False(t, deletedAbility.Enabled)
}

func TestBatchDeleteChannelsSoftDeletesAndPreservesOtherChannels(t *testing.T) {
	setupChannelStatusTest(t)

	deletedChannel := Channel{Name: "batch-delete-one", Key: "secret-a", Models: "shared-model", Group: "default", Status: common.ChannelStatusEnabled}
	otherChannel := Channel{Name: "batch-keep-one", Key: "secret-b", Models: "shared-model", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(&deletedChannel).Error)
	require.NoError(t, DB.Create(&otherChannel).Error)
	require.NoError(t, deletedChannel.AddAbilities(nil))
	require.NoError(t, otherChannel.AddAbilities(nil))

	count, err := BatchDeleteChannels([]int{deletedChannel.Id})
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)

	var stored Channel
	require.NoError(t, DB.First(&stored, deletedChannel.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, stored.Status)
	var deletedAbility Ability
	require.NoError(t, DB.Where("channel_id = ?", deletedChannel.Id).First(&deletedAbility).Error)
	assert.False(t, deletedAbility.Enabled)
	var unaffectedAbility Ability
	require.NoError(t, DB.Where("channel_id = ?", otherChannel.Id).First(&unaffectedAbility).Error)
	assert.True(t, unaffectedAbility.Enabled)
}
