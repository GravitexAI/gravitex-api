package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestTryReserveUserQuotaWithMinimumRemainingKeepsConfiguredReserve(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	common.RedisEnabled = false

	user := createReserveTestUser(t, 100)
	reserved, err := TryReserveUserQuotaWithMinimumRemaining(user.Id, 60, 40)
	require.NoError(t, err)
	require.True(t, reserved)
	require.Equal(t, 40, getUserQuotaFromDB(t, user.Id))

	reserved, err = TryReserveUserQuotaWithMinimumRemaining(user.Id, 1, 40)
	require.NoError(t, err)
	require.False(t, reserved)
	require.Equal(t, 40, getUserQuotaFromDB(t, user.Id))
}
