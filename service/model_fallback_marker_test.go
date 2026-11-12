package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestAttachUpstreamModelFallbackNestsUnderAdminInfo verifies the fallback
// marker is nested under other.admin_info.model_fallback so it is admin-only
// (the log formatter strips admin_info for non-admin viewers), and that all
// four keys survive.
func TestAttachUpstreamModelFallbackNestsUnderAdminInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	relayInfo := &relaycommon.RelayInfo{
		UserId:          7,
		OriginModelName: "claude-requested",
		UpstreamModelFallback: &relaycommon.ModelFallbackInfo{
			SentModel:   "claude-requested",
			ActualModel: "claude-swapped",
			Priced:      true,
		},
	}

	other := model.NewLogOther()
	other.SetPublic("model_price", 0.004)
	attachUpstreamModelFallback(ctx, relayInfo, other)

	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]interface{})
	require.True(t, ok, "admin_info should be created")
	fb, ok := adminInfo["model_fallback"].(map[string]any)
	require.True(t, ok, "model_fallback should be nested under admin_info")
	require.Equal(t, "claude-requested", fb["requested_model"])
	require.Equal(t, "claude-requested", fb["sent_model"])
	require.Equal(t, "claude-swapped", fb["actual_model"])
	require.Equal(t, true, fb["priced"])
}

// TestAttachUpstreamModelFallbackUnpricedVariant verifies the priced=false
// marker (actual model unpriced, manual reconciliation needed).
func TestAttachUpstreamModelFallbackUnpricedVariant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-requested",
		UpstreamModelFallback: &relaycommon.ModelFallbackInfo{
			SentModel:   "claude-requested",
			ActualModel: "vendor-unknown-model",
			Priced:      false,
		},
	}
	other := model.NewLogOther()
	other.SetAdmin("admin_username", "root")
	attachUpstreamModelFallback(ctx, relayInfo, other)

	adminInfo := other.Snapshot()["admin_info"].(map[string]interface{})
	require.Equal(t, "root", adminInfo["admin_username"], "existing admin_info fields preserved")
	fb := adminInfo["model_fallback"].(map[string]any)
	require.Equal(t, false, fb["priced"])
	require.Equal(t, "vendor-unknown-model", fb["actual_model"])
}

// TestAttachUpstreamModelFallbackNoFallbackNoMarker verifies the common case
// (no swap observed) leaves the log untouched.
func TestAttachUpstreamModelFallbackNoFallbackNoMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	relayInfo := &relaycommon.RelayInfo{}
	other := model.NewLogOther()
	other.SetPublic("model_price", 0.004)
	attachUpstreamModelFallback(ctx, relayInfo, other)

	_, hasAdmin := other.Snapshot()["admin_info"]
	require.False(t, hasAdmin, "no admin_info should be added when there is no fallback")
}
