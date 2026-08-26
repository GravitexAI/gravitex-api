package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// DispatchLyriaAsync pre-creates the local task after the normal pricing and
// pre-charge stages. The provider call is deliberately moved to a detached
// worker so the POST can return its interaction id immediately.
func DispatchLyriaAsync(c *gin.Context, info *relaycommon.RelayInfo, _ []byte) (*relay.TaskSubmitResult, *taskdto.TaskError) {
	if info == nil || info.TaskRelayInfo == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("lyria async task context is missing"), "async_dispatch_failed", http.StatusInternalServerError)
	}
	if info.TaskRelayInfo.PublicTaskID == "" {
		info.TaskRelayInfo.PublicTaskID = model.GenerateTaskID()
	}
	taskID := info.TaskRelayInfo.PublicTaskID
	initialBody, _ := common.Marshal(map[string]any{
		"id": taskID, "object": "interaction", "status": "in_progress",
		"role": "model", "model": info.OriginModelName,
	})
	placeholder := &relay.TaskSubmitResult{
		UpstreamTaskID: taskID,
		TaskData:       initialBody,
		Platform:       constant.TaskPlatformLyria,
		Quota:          info.PriceData.Quota,
	}
	task := buildSubmittedTask(c, info, placeholder, false)
	task.Status = model.TaskStatusInProgress
	task.Progress = "0%"
	task.PrivateData.LocalAsync = true
	task.StartTime = 0
	task.FinishTime = 0
	if err := task.Insert(); err != nil {
		return nil, service.TaskErrorWrapper(err, "async_task_insert_failed", http.StatusInternalServerError)
	}

	workerCtx := c.Copy()
	// The request middleware owns and closes the original BodyStorage when the
	// POST returns. Never copy that storage into the worker. Rebuild a fresh
	// storage from the raw Lyria request captured before middleware conversion.
	var rawRequest []byte
	if raw, ok := c.Get(common.KeyLyriaRawRequestBody); ok {
		rawRequest, _ = raw.([]byte)
	}
	if len(rawRequest) == 0 && c.Request != nil && c.Request.Body != nil {
		rawRequest, _ = io.ReadAll(c.Request.Body)
	}
	// Validation reads the gateway's converted task body, not the public
	// Interactions body (which has `input`, not `prompt`). The provider adapter
	// still reads KeyLyriaRawRequestBody and forwards the original request.
	validationBody, _ := common.Marshal(map[string]any{
		"model":  info.OriginModelName,
		"prompt": "Lyria interaction",
	})
	workerStorage, storageErr := common.CreateBodyStorage(validationBody)
	if storageErr != nil {
		return nil, service.TaskErrorWrapper(storageErr, "async_body_storage_failed", http.StatusInternalServerError)
	}
	workerCtx.Set(common.KeyBodyStorage, workerStorage)
	workerCtx.Set(common.KeyRequestBody, append([]byte(nil), validationBody...))
	if c.Request != nil {
		workerCtx.Request = c.Request.Clone(context.Background())
		workerCtx.Request.ContentLength = int64(len(validationBody))
		workerCtx.Request.Body = io.NopCloser(bytes.NewReader(validationBody))
	}
	workerCtx.Set("native_interactions_async", true)
	workerCtx.Set("native_interactions_worker", true)
	workerCtx.Set("native_interactions_background", false)
	workerInfo := *info
	if info.TaskRelayInfo != nil {
		relayTaskInfo := *info.TaskRelayInfo
		workerInfo.TaskRelayInfo = &relayTaskInfo
	}
	go runLyriaAsyncWorker(workerCtx, &workerInfo, taskID)

	c.JSON(http.StatusOK, map[string]any{
		"id": taskID, "object": "interaction", "status": "in_progress",
		"role": "model", "model": info.OriginModelName,
	})
	return &relay.TaskSubmitResult{
		UpstreamTaskID:  taskID,
		TaskData:        initialBody,
		Platform:        constant.TaskPlatformLyria,
		Quota:           info.PriceData.Quota,
		AsyncDispatched: true,
		InitialTaskInfo: &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, Progress: "0%"},
	}, nil
}

func runLyriaAsyncWorker(c *gin.Context, info *relaycommon.RelayInfo, taskID string) {
	if storage, ok := c.Get(common.KeyBodyStorage); ok {
		if bodyStorage, ok := storage.(common.BodyStorage); ok {
			defer bodyStorage.Close()
		}
	}
	result, taskErr := relay.RelayTaskSubmit(c, info)
	task, exists, err := model.GetByTaskId(info.UserId, taskID)
	if err != nil || !exists {
		common.SysError(fmt.Sprintf("lyria async task %s reload failed: %v", taskID, err))
		if info.Billing != nil {
			info.Billing.Refund(c)
		}
		return
	}
	if taskErr != nil {
		failReason := taskErr.Message
		if taskErr.Error != nil {
			failReason = taskErr.Error.Error()
		}
		if result != nil && result.InitialTaskInfo != nil && result.InitialTaskInfo.Reason != "" {
			// Prefer the parsed provider error over the generic relay wrapper.
			// The raw response is retained below for diagnostics.
			failReason = result.InitialTaskInfo.Reason
		}
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = time.Now().Unix()
		task.FailReason = failReason
		failureResponse := map[string]any{
			"id": taskID, "object": "interaction", "status": "failed",
			"role": "model", "model": info.OriginModelName,
			"error": map[string]any{"code": taskErr.Code, "message": failReason},
		}
		if result != nil {
			if result.UpstreamStatusCode != 0 {
				failureResponse["upstream_status_code"] = result.UpstreamStatusCode
			}
			if len(result.TaskData) > 0 {
				var raw any
				if common.Unmarshal(result.TaskData, &raw) == nil {
					failureResponse["upstream_response"] = raw
				}
			}
		}
		task.Data, _ = common.Marshal(failureResponse)
		if info.Billing != nil {
			info.Billing.Refund(c)
			info.Billing = nil
		}
		_, _ = task.UpdateWithStatus(model.TaskStatusInProgress)
		recordVertexLyriaSubmitFailure(c, info, failReason, taskErr.StatusCode)
		return
	}

	failed := isFailedNativeLyriaSubmit(info.NativeInteractions, info.OriginModelName, result)
	if failed {
		reason := "upstream Lyria request failed"
		if result.InitialTaskInfo != nil && result.InitialTaskInfo.Reason != "" {
			reason = result.InitialTaskInfo.Reason
		}
		if info.Billing != nil {
			info.Billing.Refund(c)
			info.Billing = nil
		}
		recordVertexLyriaSubmitFailure(c, info, reason, result.UpstreamStatusCode)
	} else if info.Billing != nil {
		if err := service.SettleBilling(c, info, result.Quota); err != nil {
			common.SysError(fmt.Sprintf("lyria async task %s settle failed: %v", taskID, err))
		}
		service.LogTaskConsumption(c, info)
		info.Billing = nil
	}

	// The provider may return its own Interaction id during the worker call.
	// The public id remains the local task id returned by POST and used by CLI
	// polling; keep the provider id only in private_data.upstream_task_id.
	info.TaskRelayInfo.PublicTaskID = taskID
	finalTask := buildSubmittedTask(c, info, result, false)
	finalTask.PrivateData.LocalAsync = true
	finalTask.ID = task.ID
	finalTask.CreatedAt = task.CreatedAt
	if finalTask.Status == model.TaskStatusNotStart {
		finalTask.Status = model.TaskStatusInProgress
		finalTask.Progress = "0%"
	}
	if finalTask.Status == model.TaskStatusInProgress && finalTask.Progress == "" {
		finalTask.Progress = "0%"
	}
	if _, err := finalTask.UpdateWithStatus(model.TaskStatusInProgress); err != nil {
		common.SysError(fmt.Sprintf("lyria async task %s update failed: %v", taskID, err))
	}
}
