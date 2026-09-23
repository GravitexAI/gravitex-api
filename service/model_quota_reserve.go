package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

// CheckModelQuotaReserve applies the configured wallet reserve as a request
// gate independently of the model's billing mode or funding source.
func CheckModelQuotaReserve(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo == nil {
		return nil
	}
	return CheckModelQuotaReserveForModel(c, relayInfo.UserId, relayInfo.GetBillingModelName())
}

// CheckModelQuotaReserveForModel supports legacy relay entry points that know
// the effective model name but do not use the unified billing session.
func CheckModelQuotaReserveForModel(c *gin.Context, userID int, modelName string) *types.NewAPIError {
	if IsNegativeBalanceAllowed(c) {
		return nil
	}

	minimumRemainingQuota := operation_setting.GetMinimumRemainingQuota(modelName)
	if minimumRemainingQuota <= 0 {
		return nil
	}

	userQuota, err := model.GetUserQuota(userID, false)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if userQuota >= minimumRemainingQuota {
		return nil
	}

	return types.NewErrorWithStatusCode(
		fmt.Errorf("%w: Current balance: %s, Minimum usage quota: %s", ErrInsufficientQuotaReserve, formatQuotaForEnglishMessage(userQuota), formatQuotaForEnglishMessage(minimumRemainingQuota)),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func formatQuotaForEnglishMessage(quota int) string {
	return strings.ReplaceAll(logger.FormatQuota(quota), "＄", "$")
}
