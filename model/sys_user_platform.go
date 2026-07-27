package model

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const defaultSysUserPlatformID = 1

// GetSysUserPlatformID returns the platform ownership recorded by the Java
// backend. Go users.id and Java sys_user.user_id are the shared identity.
func GetSysUserPlatformID(userID int) (int, error) {
	if userID <= 0 {
		return 0, errors.New("invalid user id")
	}

	var row struct {
		PlatformID *int `gorm:"column:platform_id"`
	}
	result := DB.Table("sys_user").Select("platform_id").
		Where("user_id = ?", userID).First(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("sys_user %d not found: %w", userID, result.Error)
		}
		return 0, result.Error
	}
	if row.PlatformID == nil {
		return defaultSysUserPlatformID, nil
	}
	if *row.PlatformID <= 0 {
		return defaultSysUserPlatformID, nil
	}
	return *row.PlatformID, nil
}

// GetUserByUsernameAndPlatform resolves a local Go user through the Java
// sys_user identity, so duplicate usernames on different platforms cannot be
// resolved to the wrong account.
func GetUserByUsernameAndPlatform(username string, platformID int) (*User, error) {
	if username == "" || platformID <= 0 {
		return nil, errors.New("username or platform id is invalid")
	}

	var row struct {
		UserID int `gorm:"column:user_id"`
	}
	result := DB.Table("sys_user").Select("user_id").
		Where("user_name = ? AND platform_id = ?", username, platformID).
		First(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	return GetUserById(row.UserID, false)
}
