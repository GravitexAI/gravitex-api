package model

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	NameRuleExact = iota
	NameRulePrefix
	NameRuleContains
	NameRuleSuffix
)

type BoundChannel struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type Model struct {
	Id            int            `json:"id"`
	ModelName     string         `json:"model_name" gorm:"size:128;not null;uniqueIndex:uk_model_name_delete_at,priority:1"`
	Description   string         `json:"description" gorm:"type:text"`
	DescriptionEn string         `json:"description_en" gorm:"type:text"`
	DescriptionId string         `json:"description_id" gorm:"type:text"`
	Icon          string         `json:"icon" gorm:"type:varchar(128)"`
	IconURL       string         `json:"icon_url" gorm:"column:icon_url;type:varchar(128)"`
	Tags          string         `json:"tags" gorm:"type:varchar(255)"`
	TagsEn        string         `json:"tags_en" gorm:"type:varchar(255)"`
	TagsId        string         `json:"tags_id" gorm:"type:varchar(255)"`
	ShowTab       int            `json:"show_tab" gorm:"default:0"`
	Flag          int            `json:"flag" gorm:"type:int;default:0"`       // 1-新发布 2-最先进 3-火爆
	SortOrder     int            `json:"sort_order" gorm:"type:int;default:0"` // 越小优先级越高
	IsFeatured    int            `json:"is_featured" gorm:"column:is_featured;type:int;default:0"`
	VendorID      int            `json:"vendor_id" gorm:"index"`
	Endpoints     string         `json:"endpoints" gorm:"type:text"`
	ModelLimit    string         `json:"model_limit" gorm:"type:text"`
	Status        int            `json:"status" gorm:"default:1"`
	SyncOfficial  int            `json:"sync_official" gorm:"default:1"`
	CreatedTime   int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime   int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt     gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_model_name_delete_at,priority:2"`

	BoundChannels []BoundChannel `json:"bound_channels,omitempty" gorm:"-"`
	EnableGroups  []string       `json:"enable_groups,omitempty" gorm:"-"`
	QuotaTypes    []int          `json:"quota_types,omitempty" gorm:"-"`
	NameRule      int            `json:"name_rule" gorm:"default:0"`
	ModelNickName string         `json:"model_nick_name" gorm:"type:longtext"`
	MatchedModels []string       `json:"matched_models,omitempty" gorm:"-"`
	MatchedCount  int            `json:"matched_count,omitempty" gorm:"-"`
}

func (mi *Model) Insert() error {
	now := common.GetTimestamp()
	mi.CreatedTime = now
	mi.UpdatedTime = now

	// 保存原始值（因为 Create 后可能被 GORM 的 default 标签覆盖为 1）
	originalStatus := mi.Status
	originalSyncOfficial := mi.SyncOfficial

	// 先创建记录（GORM 会对零值字段应用默认值）
	if err := DB.Create(mi).Error; err != nil {
		return err
	}

	// 使用保存的原始值进行更新，确保零值能正确保存
	return DB.Model(&Model{}).Where("id = ?", mi.Id).Updates(map[string]interface{}{
		"status":        originalStatus,
		"sync_official": originalSyncOfficial,
	}).Error
}

func IsModelNameDuplicated(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&Model{}).Where("model_name = ? AND id <> ?", name, id).Count(&cnt).Error
	return cnt > 0, err
}

func (mi *Model) Update() error {
	mi.UpdatedTime = common.GetTimestamp()
	// 使用 Select 强制更新所有字段，包括零值
	return DB.Model(&Model{}).Where("id = ?", mi.Id).
		Select("model_name", "description", "description_en", "description_id", "icon", "icon_url", "tags", "tags_en", "tags_id", "show_tab", "flag", "sort_order", "is_featured", "vendor_id", "endpoints", "model_limit", "status", "sync_official", "name_rule", "model_nick_name", "updated_time").
		Updates(mi).Error
}

func UpdateModelFields(id int, updates map[string]any) error {
	return DB.Model(&Model{}).Where("id = ?", id).Updates(updates).Error
}

func (mi *Model) Delete() error {
	return DB.Delete(mi).Error
}

func GetVendorModelCounts(statusFilter int) (map[int64]int64, error) {
	var stats []struct {
		VendorID int64
		Count    int64
	}
	db := DB.Model(&Model{})
	if statusFilter >= 0 {
		db = db.Where("status = ?", statusFilter)
	}
	if err := db.
		Select("vendor_id as vendor_id, count(*) as count").
		Group("vendor_id").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(stats))
	for _, s := range stats {
		m[s.VendorID] = s.Count
	}
	return m, nil
}

func GetAllModels(offset int, limit int, statusFilter int) ([]*Model, error) {
	var models []*Model
	db := DB.Model(&Model{})
	if statusFilter >= 0 {
		db = db.Where("status = ?", statusFilter)
	}
	err := db.Order("id DESC").Offset(offset).Limit(limit).Find(&models).Error
	return models, err
}

func GetBoundChannelsByModelsMap(modelNames []string) (map[string][]BoundChannel, error) {
	result := make(map[string][]BoundChannel)
	if len(modelNames) == 0 {
		return result, nil
	}
	type row struct {
		Model string
		Name  string
		Type  int
	}
	var rows []row
	err := DB.Table("channels").
		Select("abilities.model as model, channels.name as name, channels.type as type").
		Joins("JOIN abilities ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ?", modelNames, true).
		Distinct().
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.Model] = append(result[r.Model], BoundChannel{Name: r.Name, Type: r.Type})
	}
	return result, nil
}

func normalizeLookupValues(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func GetPreferredModelOwnerChannelTypes(modelNames []string, groups []string) (map[string]int, error) {
	result := make(map[string]int)
	modelNames = normalizeLookupValues(modelNames)
	if len(modelNames) == 0 {
		return result, nil
	}

	type row struct {
		Model       string
		ChannelType int
	}
	var rows []row

	query := DB.Table("abilities").
		Select("abilities.model as model, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ?", modelNames, true, common.ChannelStatusEnabled).
		Order("COALESCE(abilities.priority, 0) DESC").
		Order("abilities.weight DESC").
		Order("abilities.channel_id ASC")

	groups = normalizeLookupValues(groups)
	if len(groups) > 0 {
		query = query.Where("abilities."+commonGroupCol+" IN ?", groups)
	}

	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, r := range rows {
		if _, ok := result[r.Model]; ok {
			continue
		}
		result[r.Model] = r.ChannelType
	}
	return result, nil
}

func SearchModels(keyword string, vendor string, offset int, limit int, statusFilter int) ([]*Model, int64, error) {
	var models []*Model
	db := DB.Model(&Model{})
	if statusFilter >= 0 {
		db = db.Where("status = ?", statusFilter)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("model_name LIKE ? OR description LIKE ? OR tags LIKE ?", like, like, like)
	}
	if vendor != "" {
		if vid, err := strconv.Atoi(vendor); err == nil {
			db = db.Where("models.vendor_id = ?", vid)
		} else {
			db = db.Joins("JOIN vendors ON vendors.id = models.vendor_id").Where("vendors.name LIKE ?", "%"+vendor+"%")
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := db.Order("models.id DESC").Offset(offset).Limit(limit).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	return models, total, nil
}

// GetVendorNameFromModel 从模型名称获取厂商名称（用于价格链条日志）
func GetVendorNameFromModel(modelName string) string {
	var m Model
	if err := DB.Where("model_name = ?", modelName).First(&m).Error; err != nil || m.VendorID == 0 {
		return ""
	}
	v, err := GetVendorByID(m.VendorID)
	if err != nil {
		return ""
	}
	return v.Name
}

// GetVendorIdFromModel 从模型名称获取厂商 ID（用于日志 other 与账单导出）
func GetVendorIdFromModel(modelName string) *int64 {
	var m Model
	if err := DB.Where("model_name = ?", modelName).First(&m).Error; err != nil || m.VendorID == 0 {
		return nil
	}
	id := int64(m.VendorID)
	return &id
}
