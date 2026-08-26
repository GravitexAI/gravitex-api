package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestApplyInitialTaskSubmitResultCompletesLyriaFromCreateResponse(t *testing.T) {
	task := &model.Task{
		Platform:   constant.TaskPlatformLyria,
		SubmitTime: 100,
		Status:     model.TaskStatusNotStart,
		Progress:   "0%",
	}
	result := &relay.TaskSubmitResult{InitialTaskInfo: &relaycommon.TaskInfo{
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Url:      "data:audio/mpeg;base64,SUQz",
	}}

	applyInitialTaskSubmitResult(task, result, 120)

	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, "100%", task.Progress)
	require.Equal(t, int64(100), task.StartTime)
	require.Equal(t, int64(120), task.FinishTime)
	require.Empty(t, task.PrivateData.ResultURL)
}

func TestBuildSubmittedTaskKeepsRawVertexLyriaResponseAndFailureState(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	raw := []byte(`{"id":"i-1","status":"FAILED","errors":[{"code":"bad","message":"prompt"}]}`)
	info := &relaycommon.RelayInfo{
		NativeInteractions: true,
		OriginModelName:    "lyria-3-pro-preview",
		UsingGroup:         "Google",
		PriceData:          types.PriceData{Quota: 40000},
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelType: 41},
		TaskRelayInfo:      &relaycommon.TaskRelayInfo{PublicTaskID: "i-1"},
	}
	result := &relay.TaskSubmitResult{
		UpstreamTaskID: "i-1",
		TaskData:       raw,
		Platform:       constant.TaskPlatformLyria,
		Quota:          40000,
		InitialTaskInfo: &relaycommon.TaskInfo{
			Status: model.TaskStatusFailure, Progress: "100%", Reason: "bad: prompt",
		},
	}

	task := buildSubmittedTask(c, info, result, false)
	require.Equal(t, raw, []byte(task.Data))
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	require.Equal(t, "bad: prompt", task.FailReason)
	require.Equal(t, "i-1", task.TaskID)
	require.Equal(t, 40000, task.Quota)
}

func TestApplyInitialTaskSubmitResultDoesNotChangeOtherPlatforms(t *testing.T) {
	task := &model.Task{Platform: "video", Status: model.TaskStatusNotStart, Progress: "0%"}
	result := &relay.TaskSubmitResult{InitialTaskInfo: &relaycommon.TaskInfo{
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
	}}

	applyInitialTaskSubmitResult(task, result, 120)

	require.Equal(t, model.TaskStatus(model.TaskStatusNotStart), task.Status)
	require.Equal(t, "0%", task.Progress)
}

func TestApplyInitialTaskSubmitResultPersistsLyriaCancellation(t *testing.T) {
	task := &model.Task{Platform: constant.TaskPlatformLyria, SubmitTime: 100, Status: model.TaskStatusNotStart}
	result := &relay.TaskSubmitResult{InitialTaskInfo: &relaycommon.TaskInfo{
		Status: string(model.TaskStatusCancelled), Progress: "100%", Reason: "cancelled: provider cancelled",
	}}

	applyInitialTaskSubmitResult(task, result, 120)

	require.Equal(t, model.TaskStatusCancelled, task.Status)
	require.Equal(t, "cancelled: provider cancelled", task.FailReason)
	require.Equal(t, int64(120), task.FinishTime)
}

func TestShouldPersistLyriaTaskOnlyForExplicitBackgroundRequest(t *testing.T) {
	require.True(t, shouldPersistLyriaTask(true, "lyria-3-pro-preview", true))
	require.True(t, shouldPersistLyriaTask(true, "lyria-3-clip-preview", true))
	require.False(t, shouldPersistLyriaTask(true, "lyria-3-pro-preview", false))
	require.False(t, shouldPersistLyriaTask(true, "lyria-3-clip-preview", false))
}

func TestShouldPersistGoogleLyriaTaskForExplicitBackgroundRequest(t *testing.T) {
	require.True(t, shouldPersistLyriaTask(true, "lyria-3-pro-preview", true))
	require.True(t, shouldPersistLyriaTask(true, "lyria-3-clip-preview", true))
}

func TestLyriaSubmitOutcomeOnlyFailsExactVertexLyriaScope(t *testing.T) {
	failure := &relay.TaskSubmitResult{Platform: constant.TaskPlatformLyria, InitialTaskInfo: &relaycommon.TaskInfo{
		Status: model.TaskStatusFailure,
		Reason: "completed_without_audio: Vertex returned COMPLETED without audio output",
	}}

	require.True(t, isFailedVertexLyriaSubmit(true, 41, "lyria-3-pro-preview", failure))
	require.True(t, isFailedVertexLyriaSubmit(true, 41, "lyria-3-clip-preview", failure))
	require.True(t, isFailedVertexLyriaSubmit(true, 41, "lyria-3-pro-preview", &relay.TaskSubmitResult{
		Platform: constant.TaskPlatformLyria, InitialTaskInfo: &relaycommon.TaskInfo{Status: string(model.TaskStatusCancelled)},
	}))
	require.False(t, isFailedVertexLyriaSubmit(true, 41, "lyria-3-pro", failure))
	require.False(t, isFailedVertexLyriaSubmit(true, 1, "lyria-3-pro-preview", failure))
	require.False(t, isFailedVertexLyriaSubmit(false, 41, "lyria-3-pro-preview", failure))
	require.False(t, isFailedVertexLyriaSubmit(true, 41, "lyria-3-pro-preview", &relay.TaskSubmitResult{
		Platform:        constant.TaskPlatformLyria,
		InitialTaskInfo: &relaycommon.TaskInfo{Status: model.TaskStatusSuccess},
	}))
}

func TestSynchronousLyriaInteractionSkipsTaskPersistenceOnlyForNativeLyria(t *testing.T) {
	require.False(t, shouldPersistSynchronousLyriaTask(true, "lyria-3-pro-preview"))
	require.False(t, shouldPersistSynchronousLyriaTask(true, "lyria-3-clip-preview"))
	require.True(t, shouldPersistSynchronousLyriaTask(false, "lyria-3-pro-preview"))
	require.True(t, shouldPersistSynchronousLyriaTask(true, "gemini-omni-flash-preview"))
}
