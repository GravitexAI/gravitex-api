package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type TaskPollingAdaptor struct{ channel.TaskAdaptor }

func (a *TaskPollingAdaptor) FetchTask(base, key string, body map[string]any, proxy string) (*http.Response, error) {
	return fetchTaskForPolling(a.TaskAdaptor, base, key, nil, body, proxy)
}
func (a *TaskPollingAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	return parseSubmittedTaskResult(a.TaskAdaptor, body)
}

func GetTaskPollingAdaptor(platform constant.TaskPlatform) *TaskPollingAdaptor {
	a := GetTaskAdaptor(platform)
	if a == nil {
		return nil
	}
	return &TaskPollingAdaptor{TaskAdaptor: a}
}

func ResolveTaskPluginForPlatform(generation *pluginruntime.RoutingGeneration, platform constant.TaskPlatform) (*pluginruntime.LoadedPlugin, bool) {
	if generation == nil {
		return nil, false
	}
	return generation.Get(string(platform))
}

// ApplyOriginTaskAffinity preserves the official name used by the plugin protocol.
func ApplyOriginTaskAffinity(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info == nil {
		return nil
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	if tasks, ok := common.GetContextKeyType[[]*model.Task](c, constant.ContextKeyOriginTasks); ok {
		for _, task := range tasks {
			if task != nil {
				info.OriginTasks = append(info.OriginTasks, relaycommon.OriginTaskRef{TaskID: task.TaskID, UpstreamTaskID: task.GetUpstreamTaskID(), Action: task.Action, Status: string(task.Status), Data: append([]byte(nil), task.Data...)})
			}
		}
	}
	pin, found, _ := service.GetChannelConstraints(c).ResolvedPin()
	if !found || pin.RetryMode != dto.PinRetrySameChannel {
		return nil
	}
	ch, err := model.CacheGetChannel(pin.ChannelId)
	if err != nil || ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "origin_task_channel_disabled", http.StatusBadRequest)
	}
	info.LockedChannel = ch
	return nil
}
