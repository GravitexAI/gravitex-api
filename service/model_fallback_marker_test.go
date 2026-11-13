package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/stretchr/testify/require"
)

// TestAttachUpstreamModelFallbackNestsUnderAdminInfo verifies the fallback
// marker is nested under other.admin_info.model_fallback so it is admin-only
// (the log formatter strips admin_info for non-admin viewers), and that all
// three keys survive.
func TestAttachUpstreamModelFallbackNestsUnderAdminInfo(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		UserId:          7,
		OriginModelName: "claude-requested",
		UpstreamModelFallback: &relaycommon.ModelFallbackInfo{
			SentModel:   "claude-requested",
			ActualModel: "claude-swapped",
		},
	}

	other := model.NewLogOther()
	other.SetPublic("model_price", 0.004)
	attachUpstreamModelFallback(relayInfo, other)

	adminInfo, ok := other.Snapshot()["admin_info"].(map[string]interface{})
	require.True(t, ok, "admin_info should be created")
	fb, ok := adminInfo["model_fallback"].(map[string]any)
	require.True(t, ok, "model_fallback should be nested under admin_info")
	require.Equal(t, "claude-requested", fb["requested_model"])
	require.Equal(t, "claude-requested", fb["sent_model"])
	require.Equal(t, "claude-swapped", fb["actual_model"])
	_, hasPriced := fb["priced"]
	require.False(t, hasPriced, "billing is never re-priced, so no priced flag")
}

// TestAttachUpstreamModelFallbackPreservesAdminInfo verifies the marker is
// merged into an existing admin_info instead of replacing it.
func TestAttachUpstreamModelFallbackPreservesAdminInfo(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{
		OriginModelName: "claude-requested",
		UpstreamModelFallback: &relaycommon.ModelFallbackInfo{
			SentModel:   "claude-requested",
			ActualModel: "vendor-unknown-model",
		},
	}
	other := model.NewLogOther()
	other.SetAdmin("admin_username", "root")
	attachUpstreamModelFallback(relayInfo, other)

	adminInfo := other.Snapshot()["admin_info"].(map[string]interface{})
	require.Equal(t, "root", adminInfo["admin_username"], "existing admin_info fields preserved")
	fb := adminInfo["model_fallback"].(map[string]any)
	require.Equal(t, "vendor-unknown-model", fb["actual_model"])
}

// TestAttachUpstreamModelFallbackNoFallbackNoMarker verifies the common case
// (no swap observed) leaves the log untouched.
func TestAttachUpstreamModelFallbackNoFallbackNoMarker(t *testing.T) {
	relayInfo := &relaycommon.RelayInfo{}
	other := model.NewLogOther()
	other.SetPublic("model_price", 0.004)
	attachUpstreamModelFallback(relayInfo, other)

	_, hasAdmin := other.Snapshot()["admin_info"]
	require.False(t, hasAdmin, "no admin_info should be added when there is no fallback")
}

// TestAttachUpstreamModelFallbackRecordsClaudeIterations verifies an official
// Claude server-side fallback (two or more usage.iterations) is marked on the
// admin log with its trigger category and per-attempt usage.
func TestAttachUpstreamModelFallbackRecordsClaudeIterations(t *testing.T) {
	iterations := []dto.ClaudeIterationUsage{
		{Type: "message", Model: "claude-rejected", InputTokens: 535},
		{Type: "fallback_message", Model: "claude-final", InputTokens: 412, OutputTokens: 264},
	}
	relayInfo := &relaycommon.RelayInfo{
		ClaudeFallbackTriggerCategory: "bio",
		ClaudeUsageIterations:         iterations,
	}
	other := model.NewLogOther()
	attachUpstreamModelFallback(relayInfo, other)

	adminInfo := other.Snapshot()["admin_info"].(map[string]interface{})
	marker := adminInfo["claude_fallback_iterations"].(map[string]any)
	require.Equal(t, "bio", marker["trigger_category"])
	require.Equal(t, iterations, marker["iterations"])
}
