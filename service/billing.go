package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	if relayInfo != nil && relayInfo.QuotaClamp != nil {
		return types.NewErrorWithStatusCode(
			relayInfo.QuotaClamp,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("pre-consume quota cannot be negative: %d", preConsumedQuota),
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if apiErr := CheckModelQuotaReserve(c, relayInfo); apiErr != nil {
		return apiErr
	}
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	return settleBilling(ctx, relayInfo, actualQuota, true)
}

// SettleBillingWithFunding 用于没有在 handler 中调用 model.ConsumeUserQuotaSettle 的路径
// （realtime、任务、Lyria 异步、插件协议兼容）：由 BillingSession 按「实际 - 预扣」调整
// 钱包/订阅，与官方结算语义一致。否则预扣与实际的差额不会退补到用户余额。
func SettleBillingWithFunding(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	return settleBilling(ctx, relayInfo, actualQuota, false)
}

func settleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, skipFunding bool) error {
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, common.LogText("charging the difference after pre-consume: %s (actual %s, pre-consumed %s)",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, common.LogText("refunding the difference after pre-consume: %s (actual %s, pre-consumed %s)",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, common.LogText("pre-consumed quota matches the actual quota, no adjustment: %s (per-request billing)",
				logger.FormatQuota(actualQuota),
			))
		}

		// skipFunding=true：用户额度已在 compatible_handler 中通过 model.ConsumeUserQuotaSettle 一次更新
		if err := relayInfo.Billing.Settle(actualQuota, skipFunding); err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		triggerTokenDailySpendAlert(relayInfo, actualQuota)
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 {
		if err := PostConsumeQuota(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, true); err != nil {
			return err
		}
	}
	triggerTokenDailySpendAlert(relayInfo, actualQuota)
	return nil
}

// triggerTokenDailySpendAlert 在计费结算完成后，异步检查该 token 今日消费是否超过其
// 日消费告警阈值。best-effort：加载 token 失败或 threshold<=0 均直接跳过，绝不影响计费主流程。
func triggerTokenDailySpendAlert(relayInfo *relaycommon.RelayInfo, actualQuota int) {
	if actualQuota == 0 || relayInfo.TokenId <= 0 {
		return
	}
	tokenId := relayInfo.TokenId
	userId := relayInfo.UserId
	gopool.Go(func() {
		token, err := model.GetTokenById(tokenId)
		if err != nil {
			common.SysError(fmt.Sprintf("load token for daily spend alert failed, tokenId=%d: %s", tokenId, err.Error()))
			return
		}
		if token.DailySpendThreshold <= 0 {
			return
		}
		CheckAndAlertDailySpend(tokenId, userId, token.Name, token.DailySpendThreshold)
	})
}
