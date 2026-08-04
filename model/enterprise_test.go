package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedEnterprise(t *testing.T, entId int64, settings string) {
	t.Helper()
	require.NoError(t, DB.Create(&EnterpriseInfo{
		Id:                 entId,
		EnterpriseName:     "T",
		EnterpriseSettings: settings,
		Status:             1,
		DelFlag:            0,
	}).Error)
}

func seedEnterpriseUser(t *testing.T, entId, userId int64, userType int) {
	t.Helper()
	require.NoError(t, DB.Create(&EnterpriseUser{
		EnterpriseId: entId,
		UserId:       userId,
		UserType:     userType,
		Status:       1,
		DelFlag:      0,
	}).Error)
}

func TestParseOwnerApikeyRestriction(t *testing.T) {
	assert.False(t, parseOwnerApikeyRestriction(""))
	assert.False(t, parseOwnerApikeyRestriction(`{}`))
	assert.False(t, parseOwnerApikeyRestriction(`not json`))
	assert.False(t, parseOwnerApikeyRestriction(`{"ownerApikeyRestrictionEnabled": false}`))
	assert.True(t, parseOwnerApikeyRestriction(`{"ownerApikeyRestrictionEnabled": true}`))
	// Java 后续可能加别的键，未知键必须被忽略而不影响解析
	assert.True(t, parseOwnerApikeyRestriction(`{"foo": 1, "ownerApikeyRestrictionEnabled": true}`))
}

func TestIsEnterpriseApikeyRestrictedOwner(t *testing.T) {
	truncateTables(t)

	seedEnterprise(t, 1, `{"ownerApikeyRestrictionEnabled": true}`)
	seedEnterpriseUser(t, 1, 100, 1) // 受限企业的主账号

	seedEnterpriseUser(t, 1, 101, 2) // 受限企业的子账号

	seedEnterprise(t, 2, `{"ownerApikeyRestrictionEnabled": false}`)
	seedEnterpriseUser(t, 2, 200, 1) // 未开限制的主账号

	seedEnterprise(t, 3, ``)
	seedEnterpriseUser(t, 3, 300, 1) // 空配置的主账号

	cases := []struct {
		name   string
		userId int
		want   bool
	}{
		{"restricted owner", 100, true},
		{"sub account", 101, false},
		{"non-restricted owner", 200, false},
		{"empty settings owner", 300, false},
		{"non-enterprise user", 999, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsEnterpriseApikeyRestrictedOwner(tc.userId)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
