package model

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/samber/hot"
	"gorm.io/gorm"
)

const defaultSysUserPlatformID = 1

// GetSysUserPlatformID resolves the platform ownership on every relay request
// (platform isolation check in middleware). Hitting sys_user per request
// saturated MySQL during the 2026-10-24 load test, so successful lookups are
// cached with a TTL. The Java backend owns sys_user, so Go has no direct
// invalidation hook; platform reassignment takes effect after TTL expiry.
const sysUserPlatformCacheNamespace = "new-api:sys_user_platform:v1"

var (
	sysUserPlatformCacheOnce sync.Once
	sysUserPlatformCache     *cachex.HybridCache[int]
)

func sysUserPlatformCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SYS_USER_PLATFORM_CACHE_TTL", 300)
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return time.Duration(ttlSeconds) * time.Second
}

func sysUserPlatformCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SYS_USER_PLATFORM_CACHE_CAP", 10000)
	if capacity <= 0 {
		capacity = 10000
	}
	return capacity
}

func getSysUserPlatformCache() *cachex.HybridCache[int] {
	sysUserPlatformCacheOnce.Do(func() {
		sysUserPlatformCache = cachex.NewHybridCache[int](cachex.HybridCacheConfig[int]{
			Namespace: cachex.Namespace(sysUserPlatformCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[int]{},
			Memory: func() *hot.HotCache[string, int] {
				return hot.NewHotCache[string, int](hot.LRU, sysUserPlatformCacheCapacity()).
					WithTTL(sysUserPlatformCacheTTL()).
					WithJanitor().
					Build()
			},
		})
	})
	return sysUserPlatformCache
}

// GetSysUserPlatformID returns the platform ownership recorded by the Java
// backend. Go users.id and Java sys_user.user_id are the shared identity.
func GetSysUserPlatformID(userID int) (int, error) {
	if userID <= 0 {
		return 0, errors.New("invalid user id")
	}

	cacheKey := strconv.Itoa(userID)
	if platformID, found, err := getSysUserPlatformCache().Get(cacheKey); err == nil && found {
		return platformID, nil
	}

	var row struct {
		PlatformID *int `gorm:"column:platform_id"`
	}
	result := DB.Table("sys_user").Select("platform_id").
		Where("user_id = ? AND del_flag = ?", userID, "0").First(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return 0, fmt.Errorf("sys_user %d not found: %w", userID, result.Error)
		}
		return 0, result.Error
	}
	platformID := defaultSysUserPlatformID
	if row.PlatformID != nil && *row.PlatformID > 0 {
		platformID = *row.PlatformID
	}
	_ = getSysUserPlatformCache().SetWithTTL(cacheKey, platformID, sysUserPlatformCacheTTL())
	return platformID, nil
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
		Where("user_name = ? AND platform_id = ? AND del_flag = ?", username, platformID, "0").
		First(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	return GetUserById(row.UserID, false)
}
