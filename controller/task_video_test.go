package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestGeminiOmniInputTokensFallsBackToAggregateUsage(t *testing.T) {
	aggregate, textInput := geminiOmniInputTokens(&relaycommon.TaskInfo{InputTokens: 23})
	require.Equal(t, 23, aggregate)
	require.Equal(t, 23, textInput)
}

func TestGeminiOmniInputTokensKeepsImageAndVideoInputSeparate(t *testing.T) {
	aggregate, textInput := geminiOmniInputTokens(&relaycommon.TaskInfo{
		InputTokens:      1200,
		ImageInputTokens: 700,
		VideoInputTokens: 400,
	})
	require.Equal(t, 1200, aggregate)
	require.Equal(t, 100, textInput)
}

func TestVideoTaskUseTimeSecondsFallsBackToSubmitTimeForOmni(t *testing.T) {
	task := &model.Task{
		SubmitTime: 100,
		FinishTime: 163,
		StartTime:  0,
		CreatedAt:  90,
	}

	require.Equal(t, 63, videoTaskUseTimeSeconds(task, "gemini-omni-flash-preview"))
	require.Zero(t, videoTaskUseTimeSeconds(task, "another-video-model"))
}
