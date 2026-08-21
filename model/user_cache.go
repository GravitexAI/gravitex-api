package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
)

// userCacheSchemaVersion tags the user cache hash format. The atomic quota
// reserve/delta Lua scripts (quota_reserve.go) validate it, so a stale-format
// hash is treated as a miss and re-hydrated from the database.
const userCacheSchemaVersion = 2

type UserBase struct {
	Id          int    `json:"id"`
	Group       string `json:"group"`
	Email       string `json:"email"`
	Quota       int    `json:"quota"`
	Status      int    `json:"status"`
	Username    string `json:"username"`
	Setting     string `json:"setting"`
	CacheSchema int    `json:"cache_schema"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// 供 controller 等上层包在用户状态变更（如禁用、删除、角色变更）后主动清理缓存。
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

func populateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}

	return common.RedisHSetObj(
		getUserCacheKey(user.Id),
		user.ToBaseUser(),
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
}

// updateUserCache refreshes non-quota user cache fields.
// Quota is maintained by atomic quota delta paths and must not be overwritten
// by stale user snapshots from profile/settings updates.
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}
	if err := updateUserGroupCache(user.Id, user.Group); err != nil {
		return err
	}
	if err := updateUserEmailCache(user.Id, user.Email); err != nil {
		return err
	}
	if err := updateUserStatusCache(user.Id, user.Status == common.UserStatusEnabled); err != nil {
		return err
	}
	if err := updateUserNameCache(user.Id, user.Username); err != nil {
		return err
	}
	return updateUserSettingCache(user.Id, user.Setting)
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (userCache *UserBase, err error) {
	var user *User
	var fromDB bool
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && user != nil {
			gopool.Go(func() {
				if err := populateUserCache(*user); err != nil {
					common.SysLog("failed to update user status cache: " + err.Error())
				}
			})
		}
	}()

	// Try getting from Redis first
	userCache, err = cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}

	// If Redis fails, get from DB
	fromDB = true
	user, err = GetUserById(userId, false)
	if err != nil {
		return nil, err // Return nil and error if DB lookup fails
	}

	userCache = &UserBase{
		Id:       user.Id,
		Group:    user.Group,
		Quota:    user.Quota,
		Status:   user.Status,
		Username: user.Username,
		Setting:  user.Setting,
		Email:    user.Email,
	}

	return userCache, nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var userCache UserBase
	// Try getting from Redis first
	err := common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields.
// 通过守卫式 Lua 脚本执行：哈希不存在时直接跳过（下次读取会从数据库水合），
// 不会像裸 HINCRBY 那样创建只含 Quota 字段的残缺哈希。
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := cacheApplyUserQuotaDelta(userId, delta)
	return err
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// syncCreditUserQuotaCache 在授信事务（充值/兑换等）提交后同步把增量补进缓存
// 余额。预扣以缓存值为准（存在期间），授信不能绕过它，否则新到账的额度在
// 缓存过期前不可用；缓存未命中无需处理，下次读取会从已提交的数据库余额水合。
func syncCreditUserQuotaCache(userId int, quota int, operation string) {
	if quota <= 0 {
		return
	}
	if err := cacheIncrUserQuota(userId, int64(quota)); err != nil {
		common.SysLog(fmt.Sprintf("failed to sync %s credit to user quota cache: %s", operation, err.Error()))
	}
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	if !common.RedisEnabled {
		return nil
	}
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Status", fmt.Sprintf("%d", statusInt))
}

func updateUserQuotaCache(userId int, quota int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Quota", fmt.Sprintf("%d", quota))
}

func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Group", group)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserEmailCache(userId int, email string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Email", email)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Setting", setting)
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}
