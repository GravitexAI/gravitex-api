package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// EnterpriseUser 映射 Java 维护的 t_enterprise_user 表（Go 只读）。
// DDL 由 Java 后端负责，Go 不得在生产环境 AutoMigrate 此表。
type EnterpriseUser struct {
	Id            int64  `json:"id" gorm:"column:id;primaryKey"`
	EnterpriseId  int64  `json:"enterprise_id" gorm:"column:enterprise_id"`
	UserId        int64  `json:"user_id" gorm:"column:user_id;index"`
	UserType      int    `json:"user_type" gorm:"column:user_type"` // 1=企业管理员(主账号) 2=企业用户(子账号)
	ParentId      *int64 `json:"parent_id" gorm:"column:parent_id"`
	Status        int    `json:"status" gorm:"column:status"`
	DelFlag       int    `json:"del_flag" gorm:"column:del_flag"` // 0=未删除 1=已删除
	AllowedModels string `json:"allowed_models" gorm:"column:allowed_models"`
}

func (EnterpriseUser) TableName() string { return "t_enterprise_user" }

// EnterpriseInfo 映射 Java 维护的 t_enterprise_info 表（Go 只读）。
type EnterpriseInfo struct {
	Id                 int64  `json:"id" gorm:"column:id;primaryKey"`
	EnterpriseName     string `json:"enterprise_name" gorm:"column:enterprise_name"`
	EnterpriseSettings string `json:"enterprise_settings" gorm:"column:enterprise_settings"`
	Status             int    `json:"status" gorm:"column:status"`
	DelFlag            int    `json:"del_flag" gorm:"column:del_flag"` // 0=未删除 1=已删除
}

func (EnterpriseInfo) TableName() string { return "t_enterprise_info" }

// enterpriseSettingsPartial 只声明 Go 关心的字段；未知键被忽略，Java 可自由扩展。
type enterpriseSettingsPartial struct {
	OwnerApikeyRestrictionEnabled bool `json:"ownerApikeyRestrictionEnabled"`
	SensitiveOpAlertEnabled       bool `json:"sensitiveOpAlertEnabled"`
}

// parseOwnerApikeyRestriction 从 enterprise_settings JSON 字符串中安全读取
// ownerApikeyRestrictionEnabled 标志位。空串或非法 JSON 一律返回 false。
func parseOwnerApikeyRestriction(settings string) bool {
	if settings == "" {
		return false
	}
	var s enterpriseSettingsPartial
	if err := common.UnmarshalJsonStr(settings, &s); err != nil {
		return false
	}
	return s.OwnerApikeyRestrictionEnabled
}

// IsEnterpriseApikeyRestrictedOwner 判断该用户是否为"已开启 apikey 限制的企业主账号"。
// 非企业用户、子账号、未开启限制的企业，一律返回 (false, nil)，
// 从而保证非企业普通用户完全不受影响。
func IsEnterpriseApikeyRestrictedOwner(userId int) (bool, error) {
	var eu EnterpriseUser
	err := DB.Where("user_id = ? AND del_flag = 0", userId).First(&eu).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if eu.UserType != 1 {
		return false, nil
	}
	var ent EnterpriseInfo
	err = DB.Where("id = ? AND del_flag = 0", eu.EnterpriseId).First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return parseOwnerApikeyRestriction(ent.EnterpriseSettings), nil
}

// enterpriseAllowedModels 子账号允许使用的模型范围；仅 Models 扁平列表用于校验，Vendors 仅回显。
type enterpriseAllowedModels struct {
	Models []string `json:"models"`
}

// GetSubAccountAllowedModelSet 返回该用户作为"受限企业子账号"的允许模型集合。
// 仅当用户是 userType=2 的子账号且配置了非空 models 列表时 restricted=true。
// 非企业用户、主账号、未配置或空列表，一律返回 (nil, false, nil)，保证不影响普通用户。
func GetSubAccountAllowedModelSet(userId int) (map[string]bool, bool, error) {
	var eu EnterpriseUser
	err := DB.Where("user_id = ? AND del_flag = 0", userId).First(&eu).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if eu.UserType != 2 {
		return nil, false, nil
	}
	if eu.AllowedModels == "" {
		return nil, false, nil
	}
	var am enterpriseAllowedModels
	if err := common.UnmarshalJsonStr(eu.AllowedModels, &am); err != nil {
		return nil, false, nil
	}
	if len(am.Models) == 0 {
		return nil, false, nil
	}
	set := make(map[string]bool, len(am.Models))
	for _, m := range am.Models {
		m = strings.TrimSpace(m)
		if m != "" {
			set[m] = true
		}
	}
	if len(set) == 0 {
		return nil, false, nil
	}
	return set, true, nil
}

// GetSubAccountSensitiveOpAlert 返回该用户所属企业是否开启了"敏感操作邮件提醒"。
// 仅当用户是 userType=2 的子账号且其企业的 enterprise_settings 配置了
// sensitiveOpAlertEnabled=true 时 enabled=true；同时返回其所属企业 id。
// 非企业用户、主账号、未开启提醒，一律返回 (0, false, nil)，保证不影响其他用户。
// 真实数据库错误才作为 error 返回，其它异常情况一律视为不告警。
func GetSubAccountSensitiveOpAlert(userId int) (enterpriseId int64, enabled bool, err error) {
	var eu EnterpriseUser
	err = DB.Where("user_id = ? AND del_flag = 0", userId).First(&eu).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if eu.UserType != 2 {
		return 0, false, nil
	}
	var ent EnterpriseInfo
	err = DB.Where("id = ? AND del_flag = 0", eu.EnterpriseId).First(&ent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if ent.EnterpriseSettings == "" {
		return eu.EnterpriseId, false, nil
	}
	var s enterpriseSettingsPartial
	if err := common.UnmarshalJsonStr(ent.EnterpriseSettings, &s); err != nil {
		return eu.EnterpriseId, false, nil
	}
	return eu.EnterpriseId, s.SensitiveOpAlertEnabled, nil
}
