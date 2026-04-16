package model

// UserAsset maps gateway users to upstream asset library resources (UpToken, VolcEngine, etc.).
// Each asset is bound to a specific channel instance via ChannelId, because different upstream
// accounts have isolated asset stores.
type UserAsset struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int    `json:"user_id" gorm:"index;not null"`
	TokenId     int    `json:"token_id"`
	ChannelId   int    `json:"channel_id" gorm:"index;default:0"`
	GroupId     string `json:"group_id" gorm:"type:varchar(128);index;default:''"` // BytePlus asset group ID (empty for uptoken)
	VirtualId   string `json:"virtual_id" gorm:"type:varchar(128);uniqueIndex;not null"`
	AssetUrl    string `json:"asset_url" gorm:"type:varchar(256)"`
	Url         string `json:"url" gorm:"type:text"`
	Filename    string `json:"filename" gorm:"type:varchar(256)"`
	ContentType string `json:"content_type" gorm:"type:varchar(64)"`
	SizeBytes   int64  `json:"size_bytes"`
	Status      string `json:"status" gorm:"type:varchar(20);default:'pending'"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (UserAsset) TableName() string {
	return "t_user_assets"
}

func InsertUserAsset(asset *UserAsset) error {
	return DB.Create(asset).Error
}

func GetUserAssetsByUserId(userId int) ([]UserAsset, error) {
	var assets []UserAsset
	err := DB.Where("user_id = ?", userId).Order("created_at DESC").Find(&assets).Error
	return assets, err
}

// GetUserAssetsByUserIdAndChannelIds returns assets belonging to the user across the given channels.
func GetUserAssetsByUserIdAndChannelIds(userId int, channelIds []int) ([]UserAsset, error) {
	var assets []UserAsset
	if len(channelIds) == 0 {
		return assets, nil
	}
	err := DB.Where("user_id = ? AND channel_id IN ?", userId, channelIds).Order("created_at DESC").Find(&assets).Error
	return assets, err
}

// GetUserAssetsByGroupId returns assets belonging to the user in the given BytePlus asset group.
func GetUserAssetsByGroupId(userId int, groupId string) ([]UserAsset, error) {
	var assets []UserAsset
	err := DB.Where("user_id = ? AND group_id = ?", userId, groupId).Order("created_at DESC").Find(&assets).Error
	return assets, err
}

func GetUserAssetByVirtualId(virtualId string) (*UserAsset, error) {
	var asset UserAsset
	err := DB.Where("virtual_id = ?", virtualId).First(&asset).Error
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func GetUserAssetByUserIdAndVirtualId(userId int, virtualId string) (*UserAsset, error) {
	var asset UserAsset
	err := DB.Where("user_id = ? AND virtual_id = ?", userId, virtualId).First(&asset).Error
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func DeleteUserAssetByVirtualId(virtualId string) error {
	return DB.Where("virtual_id = ?", virtualId).Delete(&UserAsset{}).Error
}

func UpdateUserAssetStatus(virtualId, status string) error {
	return DB.Model(&UserAsset{}).Where("virtual_id = ?", virtualId).Update("status", status).Error
}

func UpdateUserAssetFields(virtualId string, updates map[string]interface{}) error {
	return DB.Model(&UserAsset{}).Where("virtual_id = ?", virtualId).Updates(updates).Error
}

// CheckUserOwnsAssets verifies that all given virtual_ids belong to the specified user.
// Returns the list of virtual_ids that do NOT belong to the user.
func CheckUserOwnsAssets(userId int, virtualIds []string) ([]string, error) {
	if len(virtualIds) == 0 {
		return nil, nil
	}
	var owned []UserAsset
	err := DB.Select("virtual_id").Where("user_id = ? AND virtual_id IN ?", userId, virtualIds).Find(&owned).Error
	if err != nil {
		return nil, err
	}
	ownedSet := make(map[string]bool, len(owned))
	for _, a := range owned {
		ownedSet[a.VirtualId] = true
	}
	var notOwned []string
	for _, vid := range virtualIds {
		if !ownedSet[vid] {
			notOwned = append(notOwned, vid)
		}
	}
	return notOwned, nil
}

// GetAssetChannelIdByVirtualIds looks up the channel_id for the given virtual_ids owned by the user.
// Returns a map of virtualId -> channelId. Missing or not-owned assets are omitted.
func GetAssetChannelIdByVirtualIds(userId int, virtualIds []string) (map[string]int, error) {
	if len(virtualIds) == 0 {
		return nil, nil
	}
	var assets []UserAsset
	err := DB.Select("virtual_id, channel_id").Where("user_id = ? AND virtual_id IN ?", userId, virtualIds).Find(&assets).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]int, len(assets))
	for _, a := range assets {
		result[a.VirtualId] = a.ChannelId
	}
	return result, nil
}

// MigrateUserAssetChannelId sets the default channel_id for legacy assets (channel_id=0).
func MigrateUserAssetChannelId(defaultChannelId int) error {
	return DB.Model(&UserAsset{}).Where("channel_id = 0").Update("channel_id", defaultChannelId).Error
}

// AssetModelPrefix is the model name prefix used to identify channels that support the asset API.
// Only channels with at least one model matching this prefix (via abilities table) are considered.
const AssetModelPrefix = "seedance-2-0"

// GetAssetSupportedChannelsByGroup returns enabled channels that have seedance-2-0 models
// configured and are accessible by the specified group. Uses the same in-memory channel cache
// as Distribute() (with DB fallback), so priority/weight/enabled filtering is consistent.
// Returns channels sorted by priority DESC.
func GetAssetSupportedChannelsByGroup(group string) ([]*Channel, error) {
	return GetChannelsByGroupAndModelPrefix(group, AssetModelPrefix)
}

// IsAssetSupportedChannel checks if a channel has any seedance-2-0 model configured
// and the channel is present in the in-memory cache (i.e., enabled).
func IsAssetSupportedChannel(channelId int) bool {
	ch, err := CacheGetChannel(channelId)
	if err != nil || ch == nil {
		return false
	}
	// Check if channel has any seedance-2-0 model in its Models CSV
	for _, m := range ch.GetModels() {
		if len(m) >= len(AssetModelPrefix) && m[:len(AssetModelPrefix)] == AssetModelPrefix {
			return true
		}
	}
	return false
}
