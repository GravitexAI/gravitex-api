package controller

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// orphanTaskDumpFile 兜底文件，与 model 的 failed_logs.jsonl 同一思路：
// 上游任务已建好但本地行没落库时，把重建这条任务所需的字段完整落盘，供人工补录。
const orphanTaskDumpFile = "failed_tasks.jsonl"

// handleTaskPersistFailure 处理「上游任务已创建、本地 tasks 行入库失败」这一状态不一致：
// 先落盘留痕，再取消上游任务，避免用户付了钱却拿到一个平台查不到、也不会计费的 task id。
// 调用方负责改写响应（500）并让预扣费走 Billing.Refund 退回。
func handleTaskPersistFailure(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, cause error) {
	logger.LogError(c, fmt.Sprintf("[TaskSubmit] persist task failed, task=%s platform=%s channel=%d user=%d: %v",
		task.TaskID, task.Platform, task.ChannelId, task.UserId, cause))
	dumpOrphanTask(task, cause)
	cancelOrphanUpstreamTask(c, info, task)
}

// dumpOrphanTask 把无法入库的任务追加写到本地 JSONL。字段按「手工补录一行 tasks 记录」
// 所需来挑选，不能直接 Marshal(task) —— private_data 带 json:"-"，那样会丢掉计费上下文。
func dumpOrphanTask(task *model.Task, cause error) {
	defer func() { _ = recover() }()
	f, err := os.OpenFile(orphanTaskDumpFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		common.SysError("[TaskSubmit] open orphan task dump file failed: " + err.Error())
		return
	}
	defer f.Close()

	record := map[string]any{
		"dumped_at":             time.Now().Unix(),
		"cause":                 cause.Error(),
		"task_id":               task.TaskID,
		"platform":              task.Platform,
		"user_id":               task.UserId,
		"group":                 task.Group,
		"channel_id":            task.ChannelId,
		"token_id":              task.TokenId,
		"token_name":            task.TokenName,
		"quota":                 task.Quota,
		"action":                task.Action,
		"status":                task.Status,
		"progress":              task.Progress,
		"submit_time":           task.SubmitTime,
		"properties":            task.Properties,
		"private_data":          task.PrivateData,
		"data":                  string(task.Data),
		"upstream_request_body": string(task.UpstreamRequestBody),
	}
	line, err := common.Marshal(record)
	if err != nil {
		common.SysError("[TaskSubmit] marshal orphan task failed: " + err.Error())
		return
	}
	_, _ = f.Write(append(line, '\n'))
}

// cancelOrphanUpstreamTask 取消上游任务。取消用的是本次提交实际使用的 baseURL/key，
// 而不是重新从渠道读一次，避免多 key 渠道取消到别的 key 上。平台不支持取消时只留日志，
// 落盘记录已经保证这条任务不会彻底失去痕迹。
func cancelOrphanUpstreamTask(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task) {
	upstreamTaskID := task.GetUpstreamTaskID()
	if upstreamTaskID == "" {
		return
	}
	cancelAdaptor, ok := relay.GetTaskAdaptor(task.Platform).(TaskCancelAdaptor)
	if !ok {
		logger.LogWarn(c, fmt.Sprintf("[TaskSubmit] upstream task %s left running: platform %s does not support cancel", upstreamTaskID, task.Platform))
		return
	}

	proxy := ""
	if channel, err := model.CacheGetChannel(info.ChannelId); err == nil && channel != nil {
		proxy = channel.GetSetting().Proxy
	}
	resp, err := cancelAdaptor.CancelTask(info.ChannelBaseUrl, info.ApiKey, upstreamTaskID, proxy)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("[TaskSubmit] cancel upstream task %s failed: %v", upstreamTaskID, err))
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	logger.LogInfo(c, fmt.Sprintf("[TaskSubmit] cancelled upstream task %s after local persist failure, upstream status %d", upstreamTaskID, resp.StatusCode))
}
