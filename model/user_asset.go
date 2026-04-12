package model

// UserAsset maps gateway users to UpToken asset library resources.
// All assets live under one shared UpToken account; this table provides per-user isolation.
type UserAsset struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId      int    `json:"user_id" gorm:"index;not null"`
	TokenId     int    `json:"token_id"`
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

func InsertUserAsset(asset *UserAsset) error {
	return DB.Create(asset).Error
}

func GetUserAssetsByUserId(userId int) ([]UserAsset, error) {
	var assets []UserAsset
	err := DB.Where("user_id = ?", userId).Order("created_at DESC").Find(&assets).Error
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
