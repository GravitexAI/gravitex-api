package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const (
	platformHeader     = "X-Platform-Id"
	platformContextKey = "platform_id"
	defaultPlatformID  = 1
)

// PlatformContext records the platform injected by the trusted IGA origin
// configuration. Invalid or missing values follow the Java backend default.
func PlatformContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		platformID := defaultPlatformID
		if value, err := strconv.Atoi(strings.TrimSpace(c.GetHeader(platformHeader))); err == nil && value > 0 {
			platformID = value
		}
		c.Set(platformContextKey, platformID)
		c.Next()
	}
}

func RequestPlatformID(c *gin.Context) int {
	if value, ok := c.Get(platformContextKey); ok {
		if platformID, ok := value.(int); ok && platformID > 0 {
			return platformID
		}
	}
	return defaultPlatformID
}

// PlatformIsolationEnabled is opt-in for standalone Go deployments. When
// RuoYi auth is enabled it defaults to on, because sys_user is then the
// authoritative user identity source.
func PlatformIsolationEnabled() bool {
	return common.PlatformIsolationEnabled
}

func validateUserPlatform(userID int, requestPlatformID int) error {
	userPlatformID, err := model.GetSysUserPlatformID(userID)
	if err != nil {
		return fmt.Errorf("resolve user platform: %w", err)
	}
	if userPlatformID != requestPlatformID {
		return fmt.Errorf("user platform %d does not match request platform %d", userPlatformID, requestPlatformID)
	}
	return nil
}

func validatePlatformOrAbort(c *gin.Context, userID int, openAIStyle bool) bool {
	if !PlatformIsolationEnabled() {
		return true
	}
	if err := validateUserPlatform(userID, RequestPlatformID(c)); err != nil {
		common.SysLog(fmt.Sprintf("platform authorization failed for user %d: %v", userID, err))
		if openAIStyle {
			abortWithOpenAiMessage(c, http.StatusForbidden, "user is not allowed to access this platform")
		} else {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "user is not allowed to access this platform",
			})
			c.Abort()
		}
		return false
	}
	return true
}
