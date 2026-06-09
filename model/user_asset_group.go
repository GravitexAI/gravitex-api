package model

// Asset group types correspond to BytePlus Ark `GroupType`.
//   - GroupTypeAIGC:         private virtual avatar library (existing default)
//   - GroupTypeLivenessFace: real-human portrait library (created via H5 liveness verification)
const (
	GroupTypeAIGC         = "aigc"
	GroupTypeLivenessFace = "liveness_face"
)

// UserAssetGroup maps gateway users to BytePlus AssetGroups.
// Each group belongs to a specific channel (upstream BytePlus account) and user.
type UserAssetGroup struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int    `json:"user_id" gorm:"index;not null"`
	ChannelId   int    `json:"channel_id" gorm:"index;not null"`
	GroupId     string `json:"group_id" gorm:"type:varchar(128);uniqueIndex;not null"` // e.g. "group-20260318033332-xxxxx"
	GroupType   string `json:"group_type" gorm:"type:varchar(32);default:'aigc';index"` // "aigc" or "liveness_face"
	Name        string `json:"name" gorm:"type:varchar(256)"`
	Description string `json:"description" gorm:"type:text"`
	ProjectName string `json:"project_name" gorm:"type:varchar(64);default:'default'"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (UserAssetGroup) TableName() string {
	return "t_user_asset_groups"
}

func InsertUserAssetGroup(group *UserAssetGroup) error {
	return DB.Create(group).Error
}

func GetUserAssetGroupsByUserIdAndChannelIds(userId int, channelIds []int) ([]UserAssetGroup, error) {
	var groups []UserAssetGroup
	if len(channelIds) == 0 {
		return groups, nil
	}
	err := DB.Where("user_id = ? AND channel_id IN ?", userId, channelIds).Order("created_at DESC").Find(&groups).Error
	return groups, err
}

// GetUserAssetGroupsByUserIdChannelIdsAndType returns asset groups filtered by user, channels and group_type.
// If groupType is empty, no type filter is applied.
func GetUserAssetGroupsByUserIdChannelIdsAndType(userId int, channelIds []int, groupType string) ([]UserAssetGroup, error) {
	var groups []UserAssetGroup
	if len(channelIds) == 0 {
		return groups, nil
	}
	q := DB.Where("user_id = ? AND channel_id IN ?", userId, channelIds)
	if groupType != "" {
		q = q.Where("group_type = ?", groupType)
	}
	err := q.Order("created_at DESC").Find(&groups).Error
	return groups, err
}

func GetUserAssetGroupByUserIdAndGroupId(userId int, groupId string) (*UserAssetGroup, error) {
	var group UserAssetGroup
	err := DB.Where("user_id = ? AND group_id = ?", userId, groupId).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func GetUserAssetGroupByGroupId(groupId string) (*UserAssetGroup, error) {
	var group UserAssetGroup
	err := DB.Where("group_id = ?", groupId).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func DeleteUserAssetGroupByGroupId(groupId string) error {
	// Delete assets belonging to this group first
	if err := DB.Where("group_id = ?", groupId).Delete(&UserAsset{}).Error; err != nil {
		return err
	}
	return DB.Where("group_id = ?", groupId).Delete(&UserAssetGroup{}).Error
}

// CountAssetsByGroupId returns the number of assets belonging to the given group.
func CountAssetsByGroupId(groupId string) (int64, error) {
	var count int64
	err := DB.Model(&UserAsset{}).Where("group_id = ?", groupId).Count(&count).Error
	return count, err
}

// CountUserAssetGroupsByChannel returns the number of asset groups the user has on the given channel,
// optionally filtered by group_type. Pass empty string for groupType to count all groups regardless of type.
func CountUserAssetGroupsByChannel(userId, channelId int, groupType string) (int64, error) {
	var count int64
	q := DB.Model(&UserAssetGroup{}).Where("user_id = ? AND channel_id = ?", userId, channelId)
	if groupType != "" {
		q = q.Where("group_type = ?", groupType)
	}
	err := q.Count(&count).Error
	return count, err
}

// AdminGroupQuery holds parameters for admin-side paginated group queries.
type AdminGroupQuery struct {
	ChannelId int
	GroupType string
	Page      int
	PageSize  int
}

// GetUserAssetGroupsByChannelIdPaged returns asset groups for a channel with
// optional group_type filter; ordered by created_at DESC. Page is 1-indexed.
func GetUserAssetGroupsByChannelIdPaged(q AdminGroupQuery) ([]UserAssetGroup, int64, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 20
	}
	db := DB.Model(&UserAssetGroup{}).Where("channel_id = ?", q.ChannelId)
	if q.GroupType != "" && q.GroupType != "all" {
		db = db.Where("group_type = ?", q.GroupType)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var groups []UserAssetGroup
	offset := (q.Page - 1) * q.PageSize
	err := db.Order("created_at DESC").Offset(offset).Limit(q.PageSize).Find(&groups).Error
	return groups, total, err
}

// UpdateUserAssetGroupName updates the Name and Description of a group by its group_id.
func UpdateUserAssetGroupName(groupId, name, description string) error {
	updates := map[string]interface{}{}
	if name != "" {
		updates["name"] = name
	}
	if description != "" {
		updates["description"] = description
	}
	if len(updates) == 0 {
		return nil
	}
	return DB.Model(&UserAssetGroup{}).Where("group_id = ?", groupId).Updates(updates).Error
}
