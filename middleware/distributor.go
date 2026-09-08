package middleware

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type ModelRequest struct {
	Model string `json:"model"`
	Group string `json:"group,omitempty"`
}

const channelAffinityBindingContextKey = "channel_affinity_binding"

const channelAffinityRetryContextKey = "channel_affinity_retry"

type channelAffinityRetryState struct {
	ChannelID     int
	ExcludedHashs map[string]struct{}
}

// ClearChannelAffinityBindingContext prevents a failed affinity key from
// being reused by a subsequent retry attempt in the same request.
func ClearChannelAffinityBindingContext(c *gin.Context) {
	if c != nil {
		c.Set(channelAffinityBindingContextKey, service.ChannelAffinityBinding{})
	}
}

// MarkChannelAffinityRetryKey remembers the failed credential so the next
// retry can exhaust other enabled credentials in the same channel first.
func MarkChannelAffinityRetryKey(c *gin.Context, channelID int) {
	if c == nil || channelID <= 0 {
		return
	}
	key := common.GetContextKeyString(c, constant.ContextKeyChannelKey)
	hash, err := service.ChannelAffinityKeyHash(key)
	if err != nil {
		return
	}
	state := channelAffinityRetryState{ChannelID: channelID, ExcludedHashs: map[string]struct{}{}}
	if previous, ok := c.Get(channelAffinityRetryContextKey); ok {
		if value, ok := previous.(channelAffinityRetryState); ok && value.ChannelID == channelID {
			state = value
			if state.ExcludedHashs == nil {
				state.ExcludedHashs = map[string]struct{}{}
			}
		}
	}
	state.ExcludedHashs[hash] = struct{}{}
	c.Set(channelAffinityRetryContextKey, state)
}

// TrySetupContextForAffinityRetry selects an unused enabled credential from
// the failed channel. It returns false when that channel has no replacement.
func TrySetupContextForAffinityRetry(c *gin.Context, modelName string) (*model.Channel, bool) {
	if c == nil {
		return nil, false
	}
	previous, ok := c.Get(channelAffinityRetryContextKey)
	state, ok := previous.(channelAffinityRetryState)
	if !ok || state.ChannelID <= 0 {
		return nil, false
	}
	channel, err := model.CacheGetChannel(state.ChannelID)
	if err != nil || channel == nil {
		return nil, false
	}
	for index, key := range channel.GetKeys() {
		if channel.ChannelInfo.IsMultiKey && channel.ChannelInfo.MultiKeyStatusList != nil {
			status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
			if exists && status != common.ChannelStatusEnabled {
				continue
			}
		}
		hash, hashErr := service.ChannelAffinityKeyHash(key)
		if hashErr != nil {
			continue
		}
		if _, used := state.ExcludedHashs[hash]; used {
			continue
		}
		state.ExcludedHashs[hash] = struct{}{}
		c.Set(channelAffinityRetryContextKey, state)
		c.Set(channelAffinityBindingContextKey, service.ChannelAffinityBinding{ChannelID: channel.Id, KeyIndex: index, KeyHash: hash})
		if setupErr := setupContextForSelectedChannel(c, channel, modelName); setupErr != nil {
			return nil, false
		}
		return channel, true
	}
	return nil, false
}

func resolveChannelAffinityKey(channel *model.Channel, binding service.ChannelAffinityBinding) (string, int, string, bool) {
	if channel == nil || binding.ChannelID != channel.Id {
		return "", 0, "", false
	}
	keys := channel.GetKeys()
	if len(keys) == 0 {
		return "", 0, "", false
	}
	statusEnabled := func(index int) bool {
		if !channel.ChannelInfo.IsMultiKey || channel.ChannelInfo.MultiKeyStatusList == nil {
			return true
		}
		return channel.ChannelInfo.MultiKeyStatusList[index] == common.ChannelStatusEnabled ||
			(channel.ChannelInfo.MultiKeyStatusList[index] == 0)
	}
	keyHash := func(index int) (string, bool) {
		if index < 0 || index >= len(keys) || !statusEnabled(index) {
			return "", false
		}
		hash, err := service.ChannelAffinityKeyHash(keys[index])
		return hash, err == nil
	}
	if binding.KeyHash == "" {
		return "", 0, "", false
	}
	if hash, ok := keyHash(binding.KeyIndex); ok && hash == binding.KeyHash {
		return keys[binding.KeyIndex], binding.KeyIndex, hash, true
	}
	for index := range keys {
		hash, ok := keyHash(index)
		if ok && hash == binding.KeyHash {
			return keys[index], index, hash, true
		}
	}
	return "", 0, "", false
}

func selectReplacementChannelAffinityKey(channel *model.Channel, excludedHash string) (service.ChannelAffinityBinding, bool) {
	if channel == nil {
		return service.ChannelAffinityBinding{}, false
	}
	keys := channel.GetKeys()
	for index, key := range keys {
		if channel.ChannelInfo.IsMultiKey && channel.ChannelInfo.MultiKeyStatusList != nil {
			status, exists := channel.ChannelInfo.MultiKeyStatusList[index]
			if exists && status != common.ChannelStatusEnabled {
				continue
			}
		}
		hash, err := service.ChannelAffinityKeyHash(key)
		if err != nil || hash == excludedHash {
			continue
		}
		return service.ChannelAffinityBinding{ChannelID: channel.Id, KeyIndex: index, KeyHash: hash}, true
	}
	return service.ChannelAffinityBinding{}, false
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		var channel *model.Channel
		constraints := service.GetChannelConstraints(c)
		// Native asset routing still supplies the legacy context key. Bridge it
		// into the same pin used by plugin routing and retry selection.
		if value, exists := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); exists {
			id, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || id <= 0 {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			constraints.AddPin(taskdto.ChannelPin{ChannelId: id, Source: taskdto.PinSourceToken, Rank: taskdto.PinRankToken, RetryMode: taskdto.PinRetrySingleAttempt})
		}
		constraints.AddFilter(taskdto.ChannelFilter{
			Kind:        taskdto.FilterRequestPath,
			RequestPath: c.Request.URL.Path,
		})
		service.AppendTaskPluginIdentityFilter(c, c.GetString("expected_task_plugin_key"))
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
			playgroundRequest := &dto.PlayGroundRequest{}
			if err := common.UnmarshalBodyReusable(c, playgroundRequest); err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidPlayground, map[string]any{"Error": err.Error()}))
				return
			}
			if playgroundRequest.Group != "" {
				usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				if !service.GroupInUserUsableGroups(usingGroup, playgroundRequest.Group) && playgroundRequest.Group != usingGroup {
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorGroupAccessDenied))
					return
				}
				common.SetContextKey(c, constant.ContextKeyUsingGroup, playgroundRequest.Group)
			}
		}
		if service.IsAutoModel(modelRequest.Model) {
			if !service.IsAutoSupportedPath(c.Request.URL.Path) {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorAutoUnsupportedPath))
				return
			}
			autoName := modelRequest.Model
			result, resolveErr := service.ResolveAutoModel(c, autoName, service.ReadAutoRequestBody(c), service.CollectAutoAvailableModels(c))
			if resolveErr != nil {
				status := http.StatusServiceUnavailable
				message := i18n.T(c, i18n.MsgDistributorAutoResolveFailed, map[string]any{"Error": resolveErr.Error()})
				if errors.Is(resolveErr, service.ErrAutoRouterDisabled) {
					status = http.StatusBadRequest
					message = i18n.T(c, i18n.MsgDistributorAutoNotEnabled)
				} else if errors.Is(resolveErr, service.ErrAutoNoAvailable) {
					message = i18n.T(c, i18n.MsgDistributorAutoNoModel)
				}
				abortWithOpenAiMessage(c, status, message)
				return
			}
			service.ApplyAutoResolveTransparency(c, autoName, result)
			modelRequest.Model = result.Model
		}
		if pin, found, overridden := constraints.ResolvedPin(); found {
			for _, lost := range overridden {
				logger.LogWarn(c, fmt.Sprintf(
					"channel pin overridden: winning_source=%s winning_channel_id=%d overridden_source=%s overridden_channel_id=%d",
					pin.Source, pin.ChannelId, lost.Source, lost.ChannelId,
				))
			}
			channel, err = model.CacheGetChannel(pin.ChannelId)
			if err != nil {
				if pin.Source == taskdto.PinSourceOriginTask {
					abortWithOpenAiMessage(c, http.StatusBadRequest, "origin_task_channel_disabled", types.ErrorCode("origin_task_channel_disabled"))
				} else {
					abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				}
				return
			}
			if channel.Status != common.ChannelStatusEnabled {
				if pin.Source == taskdto.PinSourceOriginTask {
					abortWithOpenAiMessage(c, http.StatusBadRequest, "origin_task_channel_disabled", types.ErrorCode("origin_task_channel_disabled"))
				} else {
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorChannelDisabled))
				}
				return
			}
			if ok, kind := model.ChannelSatisfiesFilters(channel, modelRequest.Model, constraints.Filters); !ok {
				if kind == taskdto.FilterTaskPluginIdentity {
					logTaskPluginChannelDecision(c, channel, modelRequest.Model, "channel_rejected", "identity_mismatch")
				}
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": common.GetContextKeyString(c, constant.ContextKeyUsingGroup), "Model": modelRequest.Model}), types.ErrorCode(kind))
				return
			}
		} else {
			// Select a channel for the user
			// check token model mapping
			modelLimitEnable := service.IsTokenModelAccessLimited(c)
			relayMode := c.GetInt("relay_mode")
			// Video fetch requests contain only a task ID; ownership is checked when the
			// task is loaded, so there is no request model to validate here.
			isVideoFetch := c.Request.Method == http.MethodGet && relayMode == relayconstant.RelayModeVideoFetchByID
			if modelLimitEnable && !isVideoFetch {
				allowed := service.IsModelAllowedByToken(c, modelRequest.Model)
				if !allowed {
					if autoOriginal := common.GetContextKeyString(c, constant.ContextKeyAutoOriginalModel); autoOriginal != "" {
						allowed = service.IsAutoModelAllowedByToken(c, autoOriginal, modelRequest.Model)
					}
				}
				if !allowed {
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": modelRequest.Model}))
					return
				}
			}

			if shouldSelectChannel {
				if modelRequest.Model == "" {
					abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorModelNameRequired))
					return
				}
				var selectGroup string
				usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)

				if preferredBinding, found := service.GetPreferredChannelAffinityBinding(c, modelRequest.Model, usingGroup); found {
					if !service.IsChannelAffinityKeyEnabled(c) {
						preferredBinding = service.ChannelAffinityBinding{ChannelID: preferredBinding.ChannelID, Legacy: true}
					}
					preferredChannelID := preferredBinding.ChannelID
					affinityUsable := false
					preferred, err := model.CacheGetChannel(preferredChannelID)
					affinitySatisfied := false
					if err == nil && preferred != nil {
						affinitySatisfied, _ = model.ChannelSatisfiesFilters(preferred, modelRequest.Model, constraints.Filters)
					}
					if affinitySatisfied && preferred.Status == common.ChannelStatusEnabled {
						if preferredBinding.Legacy {
							key, index, keyErr := preferred.GetNextEnabledKey()
							if keyErr != nil {
								preferred = nil
							} else if keyHash, hashErr := service.ChannelAffinityKeyHash(key); hashErr != nil {
								preferred = nil
							} else {
								preferredBinding.Legacy = false
								preferredBinding.KeyIndex = index
								preferredBinding.KeyHash = keyHash
								c.Set(channelAffinityBindingContextKey, preferredBinding)
							}
						}
						if preferred != nil && !preferredBinding.Legacy {
							_, index, keyHash, ok := resolveChannelAffinityKey(preferred, preferredBinding)
							if !ok {
								if replacement, replacementOK := selectReplacementChannelAffinityKey(preferred, preferredBinding.KeyHash); replacementOK {
									preferredBinding = replacement
									c.Set(channelAffinityBindingContextKey, preferredBinding)
								} else {
									preferred = nil
								}
							} else {
								preferredBinding.KeyIndex = index
								preferredBinding.KeyHash = keyHash
								c.Set(channelAffinityBindingContextKey, preferredBinding)
							}
						}
					}
					if affinitySatisfied && preferred != nil && preferred.Status == common.ChannelStatusEnabled {
						if usingGroup == "auto" {
							userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
							autoGroups := service.GetRequestAutoGroups(c, userGroup)
							for _, g := range autoGroups {
								if model.IsChannelEnabledForGroupModel(g, modelRequest.Model, preferred.Id) {
									selectGroup = g
									common.SetContextKey(c, constant.ContextKeyAutoGroup, g)
									channel = preferred
									affinityUsable = true
									service.MarkChannelAffinityUsed(c, g, preferred.Id)
									if !preferredBinding.Legacy {
										service.SetChannelAffinityKeyInfo(c, preferredBinding.KeyIndex, preferredBinding.KeyHash)
									}
									break
								}
							}
						} else if model.IsChannelEnabledForGroupModel(usingGroup, modelRequest.Model, preferred.Id) {
							channel = preferred
							selectGroup = usingGroup
							affinityUsable = true
							service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
							if !preferredBinding.Legacy {
								service.SetChannelAffinityKeyInfo(c, preferredBinding.KeyIndex, preferredBinding.KeyHash)
							}
						}
					}
					if !affinityUsable && !service.ShouldKeepChannelAffinityOnChannelDisabled() {
						service.ClearChannelAffinityHit(c)
						c.Set(channelAffinityBindingContextKey, service.ChannelAffinityBinding{})
						service.ClearCurrentChannelAffinityCache(c)
					} else if !affinityUsable {
						service.ClearChannelAffinityHit(c)
						c.Set(channelAffinityBindingContextKey, service.ChannelAffinityBinding{})
					}
				}

				if channel == nil {
					channel, selectGroup, err = service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
						Ctx:         c,
						ModelName:   modelRequest.Model,
						TokenGroup:  usingGroup,
						RequestPath: c.Request.URL.Path,
						Retry:       common.GetPointer(0),
					})
					if err != nil {
						showGroup := usingGroup
						if usingGroup == "auto" {
							showGroup = fmt.Sprintf("auto(%s)", selectGroup)
						}
						message := i18n.T(c, i18n.MsgDistributorGetChannelFailed, map[string]any{"Group": showGroup, "Model": modelRequest.Model, "Error": err.Error()})
						// 如果错误，但是渠道不为空，说明是数据库一致性问题
						//if channel != nil {
						//	common.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
						//	message = "数据库一致性已被破坏，请联系管理员"
						//}
						abortWithOpenAiMessage(c, http.StatusNotFound, message, types.ErrorCodeModelNotFound)
						return
					}
					if channel == nil {
						c.Set(channelAffinityBindingContextKey, service.ChannelAffinityBinding{})
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, noAvailableChannelMessage(c, usingGroup, modelRequest.Model), types.ErrorCodeModelNotFound)
						return
					}
				}
			}
		}
		if channel != nil {
			if ok, kind := model.ChannelSatisfiesFilters(channel, modelRequest.Model, constraints.Filters); !ok {
				if kind == taskdto.FilterTaskPluginIdentity {
					logTaskPluginChannelDecision(c, channel, modelRequest.Model, "channel_rejected", "identity_mismatch")
				}
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable, noAvailableChannelMessage(c, common.GetContextKeyString(c, constant.ContextKeyUsingGroup), modelRequest.Model), types.ErrorCodeModelNotFound)
				return
			}
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		SetupContextForSelectedChannel(c, channel, modelRequest.Model)
		c.Next()
		if channel != nil && c.Writer != nil && c.Writer.Status() < http.StatusBadRequest {
			service.RecordChannelAffinity(c, channel.Id)
		}
	}
}

// noAvailableChannelMessage explains a 503 for a task-plugin-claimed model.
// A model claimed by a plugin is served only by that plugin's channels, so the
// generic "no channel" text hides the real cause: the claiming plugin has no
// enabled channel, and the operator must disable or override that plugin for
// any other plugin or channel to take the model. Non-plugin requests keep the
// generic message.
func noAvailableChannelMessage(c *gin.Context, group, modelName string) string {
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if exists && ok && pinned.Plugin != nil {
		return i18n.T(c, i18n.MsgDistributorNoAvailableChannelTaskPlugin, map[string]any{"Group": group, "Model": modelName, "Plugin": pinned.Plugin.Meta.Key})
	}
	return i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": group, "Model": modelName})
}

func channelMatchesExpectedTaskPlugin(c *gin.Context, channel *model.Channel, expected string) bool {
	if channel == nil {
		return false
	}
	if c != nil {
		if _, matched := pinnedEndpointCandidateForChannel(c, channel, expected); matched {
			return true
		}
	}
	if channel.Type == constant.ChannelTypeTaskPlugin {
		return expected != "" && channel.GetSetting().TaskPluginKey == expected
	}
	if expected == "" {
		return true
	}

	if c == nil {
		return false
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil || pinned.Plugin.Meta.Key != expected {
		return false
	}
	plugin, ok := pinned.Generation.GetByChannelType(channel.Type)
	return ok && plugin == pinned.Plugin
}

func pinnedEndpointCandidateForChannel(c *gin.Context, channel *model.Channel, expected string) (jsplugin.ProtocolBinding, bool) {
	if c == nil || channel == nil || expected == "" {
		return jsplugin.ProtocolBinding{}, false
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	pinned, ok := value.(jsplugin.PinnedEndpoint)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil {
		return jsplugin.ProtocolBinding{}, false
	}
	candidates := pinned.Candidates
	if len(candidates) == 0 {
		candidates = []jsplugin.ProtocolBinding{{Plugin: pinned.Plugin, Protocol: pinned.Protocol, Operation: pinned.Operation, Model: pinned.Model}}
	}
	expectedOwned := false
	selected := jsplugin.ProtocolBinding{}
	for _, candidate := range candidates {
		if candidate.Plugin == nil {
			continue
		}
		if candidate.Plugin.Meta.Key == expected {
			expectedOwned = true
		}
		if channel.Type == constant.ChannelTypeTaskPlugin {
			if channel.GetSetting().TaskPluginKey == candidate.Plugin.Meta.Key {
				selected = candidate
			}
			continue
		}
		plugin, indexed := pinned.Generation.GetByChannelType(channel.Type)
		if indexed && plugin == candidate.Plugin {
			selected = candidate
		}
	}
	return selected, expectedOwned && selected.Plugin != nil
}

// getModelFromRequest 从请求中读取模型信息
// 根据 Content-Type 自动处理：
// - application/json
// - application/x-www-form-urlencoded
// - multipart/form-data
func getModelFromRequest(c *gin.Context) (*ModelRequest, error) {
	if cached, exists := c.Get(contextKeyTaskPluginEndpointModel); exists {
		if modelRequest, ok := cached.(ModelRequest); ok {
			cachedRequest := modelRequest
			return &cachedRequest, nil
		}
	}
	if strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		modelRequest, err := getModelFromJSONBody(c)
		if err != nil {
			return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
		}
		return modelRequest, nil
	}

	var modelRequest ModelRequest
	err := common.UnmarshalBodyReusable(c, &modelRequest)
	if err != nil {
		return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
	}
	return &modelRequest, nil
}

func getModelFromJSONBody(c *gin.Context) (*ModelRequest, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(requestBody) {
		return nil, errors.New("invalid JSON request body")
	}
	if countTopLevelJSONKey(requestBody, "model") > 1 {
		return nil, errors.New("model must be provided once")
	}

	values := gjson.GetManyBytes(requestBody, "model", "group")
	model, err := getJSONStringValue(values[0], "model")
	if err != nil {
		return nil, err
	}
	group, err := getJSONStringValue(values[1], "group")
	if err != nil {
		return nil, err
	}

	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.Request.Body = io.NopCloser(storage)

	return &ModelRequest{
		Model: model,
		Group: group,
	}, nil
}

func countTopLevelJSONKey(data []byte, target string) int {
	depth := 0
	inString := false
	escaped := false
	stringStart := 0
	expectingKey := false
	count := 0
	for index, current := range data {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if current == '\\' {
				escaped = true
				continue
			}
			if current != '"' {
				continue
			}
			inString = false
			if depth == 1 && expectingKey {
				key := string(data[stringStart:index])
				var decodedKey string
				if common.Unmarshal(data[stringStart-1:index+1], &decodedKey) == nil {
					key = decodedKey
				}
				cursor := index + 1
				for cursor < len(data) && (data[cursor] == ' ' || data[cursor] == '\t' || data[cursor] == '\r' || data[cursor] == '\n') {
					cursor++
				}
				if cursor < len(data) && data[cursor] == ':' && key == target {
					count++
				}
				expectingKey = false
			}
			continue
		}
		switch current {
		case '"':
			inString = true
			stringStart = index + 1
		case '{':
			depth++
			if depth == 1 {
				expectingKey = true
			}
		case '}':
			depth--
		case ',':
			if depth == 1 {
				expectingKey = true
			}
		}
	}
	return count
}

func getJSONStringValue(result gjson.Result, field string) (string, error) {
	if !result.Exists() || result.Type == gjson.Null {
		return "", nil
	}
	if result.Type != gjson.String {
		return "", fmt.Errorf("field %s must be a string", field)
	}
	return result.String(), nil
}

func getModelRequest(c *gin.Context) (*ModelRequest, bool, error) {
	var modelRequest ModelRequest
	shouldSelectChannel := true
	var err error
	if modelName := c.GetString("resolved_task_model"); modelName != "" {
		modelRequest.Model = modelName
	} else if strings.Contains(c.Request.URL.Path, "/mj/") {
		relayMode := relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeMidjourneyTaskFetch ||
			relayMode == relayconstant.RelayModeMidjourneyTaskFetchByCondition ||
			relayMode == relayconstant.RelayModeMidjourneyNotify ||
			relayMode == relayconstant.RelayModeMidjourneyTaskImageSeed {
			shouldSelectChannel = false
		} else {
			midjourneyRequest := taskdto.MidjourneyRequest{}
			err = common.UnmarshalBodyReusable(c, &midjourneyRequest)
			if err != nil {
				return nil, false, errors.New(i18n.T(c, i18n.MsgDistributorInvalidMidjourney, map[string]any{"Error": err.Error()}))
			}
			midjourneyModel, mjErr, success := service.GetMjRequestModel(relayMode, &midjourneyRequest)
			if mjErr != nil {
				return nil, false, fmt.Errorf("%s", mjErr.Description)
			}
			if midjourneyModel == "" {
				if !success {
					return nil, false, fmt.Errorf("%s", i18n.T(c, i18n.MsgDistributorInvalidParseModel))
				} else {
					// task fetch, task fetch by condition, notify
					shouldSelectChannel = false
				}
			}
			modelRequest.Model = midjourneyModel
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/suno/") {
		relayMode := relayconstant.Path2RelaySuno(c.Request.Method, c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeSunoFetch || relayMode == relayconstant.RelayModeSunoFetchByID {
			shouldSelectChannel = false
		} else {
			modelRequest.Model = service.CoverTaskActionToModelName(constant.TaskPlatformSuno, c.Param("action"))
		}
		c.Set("platform", string(constant.TaskPlatformSuno))
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos/") && strings.HasSuffix(c.Request.URL.Path, "/remix") {
		relayMode := relayconstant.RelayModeVideoSubmit
		c.Set("relay_mode", relayMode)
		shouldSelectChannel = false
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos") {
		//curl https://api.openai.com/v1/videos \
		//  -H "Authorization: Bearer $OPENAI_API_KEY" \
		//  -F "model=sora-2" \
		//  -F "prompt=A calico cat playing a piano on stage"
		//	-F input_reference="@image.jpg"
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			relayMode = relayconstant.RelayModeVideoSubmit
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			if req != nil {
				modelRequest.Model = req.Model
			}
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model = getTaskOriginModelName(c)
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/video/generations") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
			modelRequest.Model = getTaskOriginModelName(c)
		}
		if _, ok := c.Get("relay_mode"); !ok {
			c.Set("relay_mode", relayMode)
		}
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") ||
		strings.HasPrefix(c.Request.URL.Path, "/v1beta1/models/") ||
		strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		// Gemini API 路径处理: /v1beta/models/gemini-2.0-flash:generateContent
		// v1beta1 是 Vertex AI 对同一 preview 面的版本名，同样接受
		relayMode := relayconstant.RelayModeGemini
		modelName := extractModelNameFromGeminiPath(c.Request.URL.Path)
		if modelName != "" {
			modelRequest.Model = modelName
		}
		c.Set("relay_mode", relayMode)
	} else if !strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") && !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") {
		//wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01
		modelRequest.Model = c.Query("model")
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
		if modelRequest.Model == "" {
			modelRequest.Model = "text-moderation-stable"
		}
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		if modelRequest.Model == "" {
			modelRequest.Model = c.Param("model")
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
		modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "dall-e")
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		//modelRequest.Model = common.GetStringIfEmpty(c.PostForm("model"), "gpt-image-1")
		contentType := c.ContentType()
		if slices.Contains([]string{gin.MIMEPOSTForm, gin.MIMEMultipartPOSTForm}, contentType) {
			req, err := getModelFromRequest(c)
			if err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		relayMode := relayconstant.RelayModeAudioSpeech
		if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/speech") {

			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "tts-1")
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranslation
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranscription
		}
		c.Set("relay_mode", relayMode)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
		// playground chat completions
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
		modelRequest.Group = req.Group
		common.SetContextKey(c, constant.ContextKeyTokenGroup, modelRequest.Group)
	}

	return &modelRequest, shouldSelectChannel, nil
}

// 修复 #4834: GET /v1/video/generations/:task_id && /v1/video/:task_id 此前不解析 model，
// 当 token 启用「可用模型限制」时，下游 modelLimitEnable 校验会因
// modelRequest.Model 为空而误报 "This token has no access to model"。
// 从已存储的任务记录中回填 OriginModelName 即可让校验走在正确的模型上。
func getTaskOriginModelName(c *gin.Context) string {
	if !service.IsTokenModelAccessLimited(c) {
		return ""
	}

	taskId := c.Param("task_id")
	if taskId == "" {
		return ""
	}

	userId := c.GetInt("id")
	if task, exist, err := model.GetByTaskId(userId, taskId); err == nil && exist && task != nil {
		return task.Properties.OriginModelName
	}
	return ""
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	return setupContextForSelectedChannel(c, channel, modelName)
}

func setupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	c.Set("original_model", modelName) // for retry
	expectedPlugin := c.GetString("expected_task_plugin_key")
	if channel == nil {
		logTaskPluginChannelDecision(c, nil, modelName, "channel_rejected", "nil_channel")
		return types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if expectedPlugin != "" && !channelMatchesExpectedTaskPlugin(c, channel, expectedPlugin) {
		logTaskPluginChannelDecision(c, channel, modelName, "channel_rejected", "identity_mismatch")
		return types.NewError(
			errors.New("selected channel does not match the pinned task plugin"),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if candidate, matched := pinnedEndpointCandidateForChannel(c, channel, expectedPlugin); matched {
		if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
			if pinned, ok := value.(jsplugin.PinnedEndpoint); ok && candidate.Plugin != nil && candidate.Plugin != pinned.Plugin {
				previousPlugin := pinned.Plugin.Meta.Key
				pinned.Plugin = candidate.Plugin
				pinned.Protocol = candidate.Protocol
				pinned.Operation = candidate.Operation
				c.Set(jsplugin.ContextKeyPinnedEndpoint, pinned)
				c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: pinned.Generation, Plugin: candidate.Plugin})
				c.Set("expected_task_plugin_key", candidate.Plugin.Meta.Key)
				c.Set("task_plugin_key", candidate.Plugin.Meta.Key)
				c.Set("platform", candidate.Plugin.Meta.Key)
				logger.LogDebug(
					c,
					"task_plugin subsystem=endpoint event=provider_selected generation=%d previous_plugin=%q plugin=%q model=%q channel_id=%d channel_type=%d",
					pinned.Generation.Number,
					previousPlugin,
					candidate.Plugin.Meta.Key,
					modelName,
					channel.Id,
					channel.Type,
				)
			}
		}
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	if channel.Type == constant.ChannelTypeTaskPlugin {
		c.Set("task_plugin_key", channel.GetSetting().TaskPluginKey)
	}
	logTaskPluginChannelDecision(c, channel, modelName, "channel_selected", "")
	paramOverride := channel.GetParamOverride()
	headerOverride := channel.GetHeaderOverride()
	if mergedParam, applied := service.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
		paramOverride = mergedParam
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)
	if nil != channel.OpenAIOrganization && *channel.OpenAIOrganization != "" {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())

	var key string
	var index int
	if anyBinding, exists := c.Get(channelAffinityBindingContextKey); exists {
		if binding, ok := anyBinding.(service.ChannelAffinityBinding); ok && binding.ChannelID > 0 {
			if binding.ChannelID == channel.Id {
				var resolved bool
				key, index, _, resolved = resolveChannelAffinityKey(channel, binding)
				if !resolved {
					return types.NewError(errors.New("channel affinity key is unavailable"), types.ErrorCodeChannelNoAvailableKey)
				}
			}
		}
	}
	if key == "" {
		var newAPIError *types.NewAPIError
		key, index, newAPIError = channel.GetNextEnabledKey()
		if newAPIError != nil {
			return newAPIError
		}
	}
	if channel.ChannelInfo.IsMultiKey {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		// 必须设置为 false，否则在重试到单个 key 的时候会导致日志显示错误
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	}
	// c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	// Preserve the legacy cost_discount context contract. The value is now the
	// effective channel cost for this request model, not a user billing ratio.
	if costDiscount, ok := channel.GetCostDiscountForModel(modelName); ok {
		common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, costDiscount)
	} else {
		// 重试切换渠道时，清除上一个渠道的 cost_discount，避免残留旧值
		common.SetContextKey(c, constant.ContextKeyChannelCostDiscount, float64(0))
	}

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	// TODO: api_version统一
	switch channel.Type {
	case constant.ChannelTypeAzure, constant.ChannelTypeAzureVideo:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
	}
	return nil
}

// extractModelNameFromGeminiPath 从 Gemini API URL 路径中提取模型名
// 输入格式: /v1beta/models/gemini-2.0-flash:generateContent
// 输出: gemini-2.0-flash
func extractModelNameFromGeminiPath(path string) string {
	// 查找 "/models/" 的位置
	modelsPrefix := "/models/"
	modelsIndex := strings.Index(path, modelsPrefix)
	if modelsIndex == -1 {
		return ""
	}

	// 从 "/models/" 之后开始提取
	startIndex := modelsIndex + len(modelsPrefix)
	if startIndex >= len(path) {
		return ""
	}

	// 查找 ":" 的位置，模型名在 ":" 之前
	colonIndex := strings.Index(path[startIndex:], ":")
	if colonIndex == -1 {
		// 如果没有找到 ":"，返回从 "/models/" 到路径结尾的部分
		return path[startIndex:]
	}

	// 返回模型名部分
	return path[startIndex : startIndex+colonIndex]
}
