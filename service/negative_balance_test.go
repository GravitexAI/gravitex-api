package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsNegativeBalanceAllowed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		setup    func(c *gin.Context)
		expected bool
	}{
		{
			name:     "context has no setting — safe default false",
			setup:    func(c *gin.Context) {},
			expected: false,
		},
		{
			name: "context value wrong type — safe default false",
			setup: func(c *gin.Context) {
				c.Set(string(constant.ContextKeyUserSetting), "not a UserSetting")
			},
			expected: false,
		},
		{
			name: "AllowNegativeBalance=false",
			setup: func(c *gin.Context) {
				c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{AllowNegativeBalance: false})
			},
			expected: false,
		},
		{
			name: "AllowNegativeBalance=true — allow",
			setup: func(c *gin.Context) {
				c.Set(string(constant.ContextKeyUserSetting), dto.UserSetting{AllowNegativeBalance: true})
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			require.NotNil(t, c)
			tt.setup(c)
			assert.Equal(t, tt.expected, IsNegativeBalanceAllowed(c))
		})
	}
}
