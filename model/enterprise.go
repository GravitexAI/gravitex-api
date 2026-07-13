package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// EnterpriseUser 映射 Java 维护的 t_enterprise_user 表（Go 只读）。
// DDL 由 Java 后端负责，Go 不得在生产环境 AutoMigrate 此表。
type EnterpriseUser struct {
	Id           int64  `json:"id" gorm:"column:id;primaryKey"`
	EnterpriseId int64  `json:"enterprise_id" gorm:"column:enterprise_id"`
	UserId       int64  `json:"user_id" gorm:"column:user_id;index"`
	UserType     int    `json:"user_type" gorm:"column:user_type"` // 1=企业管理员(主账号) 2=企业用户(子账号)
	ParentId     *int64 `json:"parent_id" gorm:"column:parent_id"`
	Status       int    `json:"status" gorm:"column:status"`
	DelFlag      int    `json:"del_flag" gorm:"column:del_flag"` // 0=未删除 1=已删除
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
