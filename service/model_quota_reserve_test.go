package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCheckModelQuotaReserveRejectsConfiguredModelBelowReserve(t *testing.T) {
	common.RedisEnabled = false
	require.NoError(t, model.DB.Where("id = ?", 91001).Delete(&model.User{}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 91001, Username: "reserve-below", Quota: 99}).Error)
	t.Cleanup(func() {
		model.DB.Where("id = ?", 91001).Delete(&model.User{})
	})

	quotaSetting := operation_setting.GetQuotaSetting()
	oldQuotaSetting := *quotaSetting
	*quotaSetting = operation_setting.QuotaSetting{
		ModelQuotaReserve: map[string]int{"seedance*": 100},
	}
	t.Cleanup(func() { *quotaSetting = oldQuotaSetting })

	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{UserId: 91001, OriginModelName: "seedance-2-0"}

	apiErr := CheckModelQuotaReserve(c, info)

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, "Insufficient minimum usage quota: Current balance: $0.000198, Minimum usage quota: $0.000200", apiErr.Error())
}

func TestCheckModelQuotaReserveForModelSupportsLegacyModelEntryPoints(t *testing.T) {
	common.RedisEnabled = false
	require.NoError(t, model.DB.Where("id = ?", 91003).Delete(&model.User{}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 91003, Username: "reserve-legacy", Quota: 99}).Error)
	t.Cleanup(func() {
		model.DB.Where("id = ?", 91003).Delete(&model.User{})
	})

	quotaSetting := operation_setting.GetQuotaSetting()
	oldQuotaSetting := *quotaSetting
	*quotaSetting = operation_setting.QuotaSetting{
		ModelQuotaReserve: map[string]int{"mj_*": 100},
	}
	t.Cleanup(func() { *quotaSetting = oldQuotaSetting })

	c, _ := gin.CreateTestContext(nil)
	apiErr := CheckModelQuotaReserveForModel(c, 91003, "mj_imagine")

	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
}

func TestCheckModelQuotaReserveAllowsUnconfiguredAndNegativeBalanceUsers(t *testing.T) {
	common.RedisEnabled = false
	require.NoError(t, model.DB.Where("id = ?", 91002).Delete(&model.User{}).Error)
	require.NoError(t, model.DB.Create(&model.User{Id: 91002, Username: "reserve-bypass", Quota: 1}).Error)
	t.Cleanup(func() {
		model.DB.Where("id = ?", 91002).Delete(&model.User{})
	})

	quotaSetting := operation_setting.GetQuotaSetting()
	oldQuotaSetting := *quotaSetting
	*quotaSetting = operation_setting.QuotaSetting{
		ModelQuotaReserve: map[string]int{"seedance*": 100},
	}
	t.Cleanup(func() { *quotaSetting = oldQuotaSetting })

	t.Run("unconfigured model", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		info := &relaycommon.RelayInfo{UserId: 91002, OriginModelName: "gpt-5"}
		require.Nil(t, CheckModelQuotaReserve(c, info))
	})

	t.Run("negative balance allowlist", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{AllowNegativeBalance: true})
		info := &relaycommon.RelayInfo{UserId: 91002, OriginModelName: "seedance-2-0"}
		require.Nil(t, CheckModelQuotaReserve(c, info))
	})
}
