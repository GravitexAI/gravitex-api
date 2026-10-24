package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupSysUserPlatformDB 用独立 SQLite 建一张 Java 侧的 sys_user 表并接管全局 DB。
func setupSysUserPlatformDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(
		"CREATE TABLE sys_user (user_id INTEGER PRIMARY KEY, platform_id INTEGER, del_flag TEXT)",
	).Error)
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
}

// 首次解析走 DB，之后命中缓存：删掉 DB 行后再调用仍返回缓存值，
// 证明平台校验不再每请求打库（2026-10-24 压测事故根因）。
func TestGetSysUserPlatformIDCachesResolvedPlatform(t *testing.T) {
	setupSysUserPlatformDB(t)
	const userID = 910001
	require.NoError(t, DB.Exec(
		"INSERT INTO sys_user (user_id, platform_id, del_flag) VALUES (?, ?, '0')", userID, 2,
	).Error)

	platformID, err := GetSysUserPlatformID(userID)
	require.NoError(t, err)
	assert.Equal(t, 2, platformID)

	require.NoError(t, DB.Exec("DELETE FROM sys_user WHERE user_id = ?", userID).Error)

	platformID, err = GetSysUserPlatformID(userID)
	require.NoError(t, err)
	assert.Equal(t, 2, platformID, "second lookup must be served from cache")
}

func TestGetSysUserPlatformIDFallsBackToDefaultPlatform(t *testing.T) {
	setupSysUserPlatformDB(t)
	// platform_id 为 NULL 和 0 都回落到默认平台 1
	for _, inserted := range []int{910002, 910003} {
		require.NoError(t, DB.Exec(
			"INSERT INTO sys_user (user_id, platform_id, del_flag) VALUES (?, ?, '0')", inserted, inserted-910002,
		).Error)
		platformID, err := GetSysUserPlatformID(inserted)
		require.NoError(t, err)
		assert.Equal(t, 1, platformID)
	}
}

func TestGetSysUserPlatformIDRejectsMissingAndInvalidUser(t *testing.T) {
	setupSysUserPlatformDB(t)

	_, err := GetSysUserPlatformID(0)
	require.Error(t, err)

	_, err = GetSysUserPlatformID(910004)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
