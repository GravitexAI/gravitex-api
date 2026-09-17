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

func TestAssetChannelSelectionByType(t *testing.T) {
	for _, cacheEnabled := range []bool{true, false} {
		name := "sqlite"
		if cacheEnabled {
			name = "cache"
		}
		t.Run(name, func(t *testing.T) {
			previousDB, previousCache := DB, common.MemoryCacheEnabled
			channelSyncLock.Lock()
			previousChannels, previousGroups := channelsIDM, group2model2channels
			channelsIDM = make(map[int]*Channel)
			group2model2channels = nil
			channelSyncLock.Unlock()
			common.MemoryCacheEnabled = cacheEnabled
			t.Cleanup(func() {
				DB, common.MemoryCacheEnabled = previousDB, previousCache
				channelSyncLock.Lock()
				channelsIDM, group2model2channels = previousChannels, previousGroups
				channelSyncLock.Unlock()
			})

			priority10, priority20 := int64(10), int64(20)
			channels := []*Channel{
				{Id: 1, Type: constant.ChannelTypeDoubaoVideo, Status: 1, Group: "vip", Models: "", Priority: &priority10},
				{Id: 2, Type: constant.ChannelTypeDoubaoVideo, Status: 1, Group: "default,vip", Models: "seedance-2-5", Priority: &priority20},
				{Id: 3, Type: constant.ChannelTypeDoubaoVideo, Status: 1, Group: "vip", Models: "custom-model", Priority: &priority10},
				{Id: 4, Type: constant.ChannelTypeOpenAI, Status: 1, Group: "vip", Models: "seedance-2-0"},
				{Id: 5, Type: constant.ChannelTypeDoubaoVideo, Status: 2, Group: "vip", Models: "seedance-2-0"},
				{Id: 6, Type: constant.ChannelTypeDoubaoVideo, Status: 3, Group: "vip", Models: "seedance-2-0"},
				{Id: 7, Type: constant.ChannelTypeDoubaoVideo, Status: 1, Group: "vip_plus"},
			}
			if cacheEnabled {
				channelSyncLock.Lock()
				for _, channel := range channels {
					channelsIDM[channel.Id] = channel
				}
				channelSyncLock.Unlock()
			} else {
				db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
				DB = db
				var sqliteVersion string
				require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&sqliteVersion).Error)
				t.Logf("SQLite %s", sqliteVersion)
				require.NoError(t, db.AutoMigrate(&Channel{}))
				require.NoError(t, db.Create(&channels).Error)
			}

			for _, tc := range []struct {
				group string
				ids   []int
			}{
				{"vip", []int{2, 1, 3}},
				{"default", []int{2}},
				{"missing", []int{}},
			} {
				selected, err := GetAssetSupportedChannelsByGroup(tc.group)
				require.NoError(t, err)
				ids := make([]int, 0, len(selected))
				for _, channel := range selected {
					ids = append(ids, channel.Id)
				}
				assert.Equal(t, tc.ids, ids, "group=%s", tc.group)
			}
			for _, tc := range []struct {
				id        int
				supported bool
			}{
				{1, true}, {2, true}, {3, true}, {4, false},
				{5, false}, {6, false}, {999, false},
			} {
				assert.Equal(t, tc.supported, IsAssetSupportedChannel(tc.id), "channel=%d", tc.id)
			}
		})
	}
}
