package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/gin-gonic/gin"
)

// IsNegativeBalanceAllowed reports whether the current user is whitelisted
// to keep calling the API when the wallet quota reaches zero or would go
// negative. It reads dto.UserSetting from gin.Context (populated by
// UserBase.WriteContext) and returns false by default when the setting is
// missing or malformed — safe default: no bypass.
func IsNegativeBalanceAllowed(c *gin.Context) bool {
	if c == nil {
		return false
	}
	v, exists := c.Get(string(constant.ContextKeyUserSetting))
	if !exists {
		return false
	}
	setting, ok := v.(dto.UserSetting)
	if !ok {
		return false
	}
	return setting.AllowNegativeBalance
}
