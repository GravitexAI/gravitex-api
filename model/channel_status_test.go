package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	if DB == nil {
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		db, err := gorm.Open(sqlite.Open("file:channel_status_test?mode=memory&cache=shared"), &gorm.Config{})
		require.NoError(t, err)
		DB = db
		LOG_DB = db
		initCol()
		require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))
	}
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM abilities")
		DB.Exec("DELETE FROM channels")
	})

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
	require.NoError(t, stale.saveStatusState(DB))

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

	require.NoError(t, channel.saveStatusState(DB))
}

func TestUpdateChannelStatusLeavesCacheUnchangedWhenDatabaseWriteFails(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{Name: "cache-status-write-failure", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	common.MemoryCacheEnabled = true
	InitChannelCache()
	require.NoError(t, DB.Exec("CREATE TRIGGER fail_channel_status_update BEFORE UPDATE ON channels BEGIN SELECT RAISE(ABORT, 'status write failed'); END").Error)
	t.Cleanup(func() {
		DB.Exec("DROP TRIGGER IF EXISTS fail_channel_status_update")
	})

	assert.False(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation"))

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.NotNil(t, cached)
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status)
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestUpdateChannelStatusRollsBackChannelWhenAbilityWriteFails(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{Name: "ability-status-write-failure", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, DB.Exec("CREATE TRIGGER fail_ability_status_update BEFORE UPDATE ON abilities BEGIN SELECT RAISE(ABORT, 'ability write failed'); END").Error)
	t.Cleanup(func() {
		DB.Exec("DROP TRIGGER IF EXISTS fail_ability_status_update")
	})

	assert.False(t, UpdateChannelStatus(channel.Id, "", common.ChannelStatusManuallyDisabled, "manual operation"))

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
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

func TestChannelUpdateCannotReviveDeletedChannelFromStaleSnapshot(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{
		Name:   "multi-key-delete-race",
		Key:    "key-a\nkey-b",
		Models: "shared-model",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.NoError(t, channel.Delete())

	stale.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusManuallyDisabled}
	err = stale.Update()
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, stored.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.False(t, ability.Enabled)
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

func TestDeleteDisabledChannelDoesNotDisableAbilitiesWhenChannelBecameActive(t *testing.T) {
	setupChannelStatusTest(t)
	channel := Channel{
		Name:   "delete-disabled-race",
		Key:    "key",
		Models: "shared-model",
		Group:  "default",
		Status: common.ChannelStatusManuallyDisabled,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	require.NoError(t, DB.Model(&Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", true).Error)
	require.NoError(t, DB.Exec("CREATE TRIGGER keep_channel_active AFTER UPDATE OF status ON channels WHEN NEW.status = -1 BEGIN UPDATE channels SET status = 1 WHERE id = NEW.id; END").Error)
	t.Cleanup(func() {
		DB.Exec("DROP TRIGGER IF EXISTS keep_channel_active")
	})

	_, err := DeleteDisabledChannel()
	require.NoError(t, err)
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)
}

func TestChannelTagStatusChangesKeepAbilitiesAligned(t *testing.T) {
	setupChannelStatusTest(t)
	tag := "tag-status"
	channel := Channel{
		Name:   "tag-status-channel",
		Key:    "key",
		Models: "shared-model",
		Group:  "default",
		Tag:    &tag,
		Status: common.ChannelStatusManuallyDisabled,
	}
	require.NoError(t, DB.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	require.NoError(t, EnableChannelByTag(tag))
	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.True(t, ability.Enabled)

	require.NoError(t, DisableChannelByTag(tag))
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	assert.False(t, ability.Enabled)
}

func TestGetChannelsByIdsExcludesDeletedChannels(t *testing.T) {
	setupChannelStatusTest(t)
	active := Channel{Name: "active-by-id", Status: common.ChannelStatusEnabled}
	deleted := Channel{Name: "deleted-by-id", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(&active).Error)
	require.NoError(t, DB.Create(&deleted).Error)
	require.NoError(t, deleted.Delete())

	channels, err := GetChannelsByIds([]int{active.Id, deleted.Id})
	require.NoError(t, err)
	require.Len(t, channels, 1)
	assert.Equal(t, active.Id, channels[0].Id)
}

func TestBatchSetChannelTagSkipsDeletedChannelsAndUpdatesActiveAbilities(t *testing.T) {
	setupChannelStatusTest(t)
	oldTag := "old-batch-tag"
	active := Channel{
		Name:   "active-batch-tag",
		Models: "shared-model",
		Group:  "default",
		Tag:    &oldTag,
		Status: common.ChannelStatusEnabled,
	}
	deleted := Channel{
		Name:   "deleted-batch-tag",
		Models: "shared-model",
		Group:  "default",
		Tag:    &oldTag,
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(&active).Error)
	require.NoError(t, DB.Create(&deleted).Error)
	require.NoError(t, active.AddAbilities(nil))
	require.NoError(t, deleted.AddAbilities(nil))
	require.NoError(t, deleted.Delete())

	newTag := "new-batch-tag"
	require.NoError(t, BatchSetChannelTag([]int{active.Id, deleted.Id}, &newTag))

	var storedActive Channel
	require.NoError(t, DB.First(&storedActive, active.Id).Error)
	assert.Equal(t, newTag, storedActive.GetTag())
	var activeAbility Ability
	require.NoError(t, DB.Where("channel_id = ? and model = ?", active.Id, "shared-model").First(&activeAbility).Error)
	assert.Equal(t, newTag, *activeAbility.Tag)
	assert.True(t, activeAbility.Enabled)

	var storedDeleted Channel
	require.NoError(t, DB.First(&storedDeleted, deleted.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, storedDeleted.Status)
	assert.Equal(t, oldTag, storedDeleted.GetTag())
	var deletedAbility Ability
	require.NoError(t, DB.Where("channel_id = ? and model = ?", deleted.Id, "shared-model").First(&deletedAbility).Error)
	assert.Equal(t, oldTag, *deletedAbility.Tag)
	assert.False(t, deletedAbility.Enabled)
}

func TestEditChannelByTagPreservesDeletedChannelsAndAbilities(t *testing.T) {
	setupChannelStatusTest(t)
	tag := "tag-edit-deleted"
	deletedChannel := Channel{
		Name:   "deleted-tag-channel",
		Key:    "deleted-key",
		Models: "old-model",
		Group:  "old-group",
		Tag:    &tag,
		Status: common.ChannelStatusEnabled,
	}
	activeChannel := Channel{
		Name:   "active-tag-channel",
		Key:    "active-key",
		Models: "old-model",
		Group:  "old-group",
		Tag:    &tag,
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, DB.Create(&deletedChannel).Error)
	require.NoError(t, DB.Create(&activeChannel).Error)
	require.NoError(t, deletedChannel.AddAbilities(nil))
	require.NoError(t, activeChannel.AddAbilities(nil))
	require.NoError(t, deletedChannel.Delete())

	newModels := "new-model"
	newGroup := "new-group"
	require.NoError(t, EditChannelByTag(tag, nil, nil, &newModels, &newGroup, nil, nil, nil, nil))

	var storedDeleted Channel
	require.NoError(t, DB.First(&storedDeleted, deletedChannel.Id).Error)
	assert.Equal(t, common.ChannelStatusDeleted, storedDeleted.Status)
	assert.Equal(t, "old-model", storedDeleted.Models)
	assert.Equal(t, "old-group", storedDeleted.Group)
	var deletedAbility Ability
	require.NoError(t, DB.Where("channel_id = ? and model = ? and "+commonGroupCol+" = ?", deletedChannel.Id, "old-model", "old-group").First(&deletedAbility).Error)
	assert.False(t, deletedAbility.Enabled)
	assert.ErrorIs(t, DB.Where("channel_id = ? and model = ?", deletedChannel.Id, "new-model").First(&Ability{}).Error, gorm.ErrRecordNotFound)

	var storedActive Channel
	require.NoError(t, DB.First(&storedActive, activeChannel.Id).Error)
	assert.Equal(t, newModels, storedActive.Models)
	assert.Equal(t, newGroup, storedActive.Group)
	var activeAbility Ability
	require.NoError(t, DB.Where("channel_id = ? and model = ? and "+commonGroupCol+" = ?", activeChannel.Id, newModels, newGroup).First(&activeAbility).Error)
	assert.True(t, activeAbility.Enabled)

	newTag := "renamed-active-tag"
	require.NoError(t, EditChannelByTag(tag, &newTag, nil, nil, nil, nil, nil, nil, nil))
	require.NoError(t, DB.First(&storedDeleted, deletedChannel.Id).Error)
	assert.Equal(t, tag, storedDeleted.GetTag())
	require.NoError(t, DB.First(&storedActive, activeChannel.Id).Error)
	assert.Equal(t, newTag, storedActive.GetTag())
	require.NoError(t, DB.Where("channel_id = ? and model = ? and "+commonGroupCol+" = ?", deletedChannel.Id, "old-model", "old-group").First(&deletedAbility).Error)
	assert.Equal(t, tag, *deletedAbility.Tag)
	require.NoError(t, DB.Where("channel_id = ? and model = ? and "+commonGroupCol+" = ?", activeChannel.Id, newModels, newGroup).First(&activeAbility).Error)
	assert.Equal(t, newTag, *activeAbility.Tag)
}
