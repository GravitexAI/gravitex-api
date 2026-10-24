package controller

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/gin-gonic/gin"
)

// TaskCancelAdaptor is an optional capability implemented only by task
// adaptors that support cancelling/deleting an upstream task (currently only
// doubao/Seedance's Ark endpoint). Defined at the point of use rather than on
// the shared channel.TaskAdaptor interface so other adaptors are unaffected.
type TaskCancelAdaptor interface {
	CancelTask(baseUrl, key, taskID, proxy string) (*http.Response, error)
}

// RelayTaskCancel handles DELETE /api/v3/contents/generations/tasks/:id — the
// Seedance official-mirror cancel/delete endpoint. It has no
// platform-normalized equivalent; see
// docs/byteplus/seedance-2.0-official-api-mirror-design.md §3.4.
func RelayTaskCancel(c *gin.Context) {
	userId := c.GetInt("id")
	taskId := c.Param("id")

	task, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": "InternalError"}})
		return
	}
	if !exist {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "task not found", "code": "NotFound.Id"}})
		return
	}

	ch, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": "InternalError"}})
		return
	}

	adaptor := relay.GetTaskAdaptor(task.Platform)
	cancelAdaptor, ok := adaptor.(TaskCancelAdaptor)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": gin.H{"message": "cancel not supported for this task", "code": "NotImplemented"}})
		return
	}

	proxy := ch.GetSetting().Proxy
	resp, err := cancelAdaptor.CancelTask(ch.GetBaseURL(), ch.Key, task.GetUpstreamTaskID(), proxy)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": gin.H{"message": err.Error(), "code": "UpstreamError"}})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "code": "InternalError"}})
		return
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		switch task.Status {
		case model.TaskStatusQueued, model.TaskStatusNotStart, model.TaskStatusSubmitted:
			preStatus := task.Status
			task.Status = model.TaskStatusCancelled
			if _, updErr := task.UpdateWithStatus(preStatus); updErr != nil {
				common.SysLog(fmt.Sprintf("[SeedanceMirror] cancel: local status update failed task=%s err=%v", taskId, updErr))
			}
		default:
			if delErr := model.DeleteTaskByID(task.ID); delErr != nil {
				common.SysLog(fmt.Sprintf("[SeedanceMirror] cancel: local delete failed task=%s err=%v", taskId, delErr))
			}
		}
	}

	c.Data(resp.StatusCode, "application/json", body)
}
