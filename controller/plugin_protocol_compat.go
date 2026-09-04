package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type taskSubmissionOutcome struct {
	Result    *relay.TaskSubmitResult
	Task      *model.Task
	RelayInfo *relaycommon.RelayInfo
}

func RelayTaskPluginEndpoint(c *gin.Context, fallback gin.HandlerFunc) {
	value, exists := c.Get(pluginruntime.ContextKeyPinnedEndpoint)
	if !exists {
		fallback(c)
		return
	}
	pinned, ok := value.(pluginruntime.PinnedEndpoint)
	if !ok || pinned.Plugin == nil || pinned.Generation == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "Task protocol request failed", "type": "new_api_error", "code": "task_protocol_error"}})
		return
	}
	if pinned.Protocol != "openai_responses" {
		fallback(c)
		return
	}
	serveTaskPluginProtocol(c, pinned, defaultPluginProtocolBridgeDeps())
}

func executeTaskSubmission(c *gin.Context, info *relaycommon.RelayInfo) (*taskSubmissionOutcome, *dto.TaskError) {
	result, taskErr := relay.RelayTaskSubmit(c, info)
	if taskErr != nil {
		return nil, taskErr
	}
	if result == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task submission returned no result"), "task_submit_failed", http.StatusInternalServerError)
	}
	task := model.InitTask(result.Platform, info)
	task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
	task.PrivateData.BillingContext = &model.TaskBillingContext{ModelPrice: info.PriceData.ModelPrice, GroupRatio: info.PriceData.GroupRatioInfo.GroupRatio, ModelRatio: info.PriceData.ModelRatio, OtherRatios: info.PriceData.OtherRatios(), OriginModelName: info.OriginModelName}
	task.Quota, task.Data, task.Action = result.Quota, result.TaskData, info.Action
	if err := task.Insert(); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "task_insert_failed", http.StatusInternalServerError)
	}
	if err := service.SettleBilling(c, info, result.Quota); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "task_billing_settlement_failed", http.StatusInternalServerError)
	}
	service.LogTaskConsumption(c, info)
	return &taskSubmissionOutcome{Result: result, Task: task, RelayInfo: info}, nil
}
