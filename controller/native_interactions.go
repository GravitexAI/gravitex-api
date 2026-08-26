package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// NativeInteractionsSubmit exposes the Google Interactions response shape
// while delegating task creation, persistence, retry and billing to RelayTask.
func NativeInteractionsSubmit(c *gin.Context) {
	nativeStream := c.GetBool("native_interactions_stream")
	if nativeStream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		streamWriter := c.Writer
		c.Set("native_interactions_sse_writer", func(event []byte) {
			writeNativeSSEEventToWriter(streamWriter, event)
		})
	}

	var capture nativeResponseWriter
	capture.ResponseWriter = c.Writer
	c.Writer = &capture
	RelayTask(c)
	c.Writer = capture.ResponseWriter

	if nativeStream {
		if capture.status >= http.StatusBadRequest && capture.body.Len() > 0 {
			writeNativeSSEToWriter(c.Writer, capture.body.Bytes())
		}
		return
	}

	if capture.status >= http.StatusBadRequest || len(capture.body.Bytes()) == 0 {
		status := capture.status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		c.Status(status)
		_, _ = c.Writer.Write(capture.body.Bytes())
		return
	}
	body, err := nativeInteractionResponse(capture.body.Bytes(), c.GetString("native_interactions_model"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.OpenAIError{Message: err.Error(), Type: "server_error"})
		return
	}
	body, err = convertVertexLyriaResponse(body, c.GetBool("native_vertex_lyria_response"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.OpenAIError{Message: err.Error(), Type: "server_error"})
		return
	}
	if shouldWaitNativeInteraction(c.GetString("native_interactions_model"), c.GetBool("native_interactions_background"), c.GetBool("native_interactions_stream")) {
		body, err = waitNativeInteraction(c, body, false)
		if err != nil {
			c.JSON(http.StatusGatewayTimeout, types.OpenAIError{Message: err.Error(), Type: "timeout"})
			return
		}
	}
	c.Data(capture.status, "application/json", body)
}

func shouldWaitNativeInteraction(modelName string, background, stream bool) bool {
	if background || stream {
		return false
	}
	// Lyria 3 does not support background interactions. Its Vertex create call
	// returns the completed audio synchronously, so no internal wait/poll loop is
	// needed here; the response is persisted locally by the task submit path.
	return modelName != "lyria-3-pro-preview" && modelName != "lyria-3-clip-preview"
}

// waitNativeInteraction keeps the normal task persistence and completion
// billing path, but exposes it as the synchronous/SSE Interactions contract.
// The upstream request remains background JSON because the gateway's task
// adaptor consumes that response shape; SSE is produced from terminal task
// snapshots instead of bypassing billing with a second upstream client.
func waitNativeInteraction(c *gin.Context, initial []byte, stream bool) ([]byte, error) {
	var interaction map[string]any
	if err := common.Unmarshal(initial, &interaction); err != nil {
		return nil, err
	}
	id, _ := interaction["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("interaction id is missing")
	}

	originalMethod, originalPath, originalURI := c.Request.Method, c.Request.URL.Path, c.Request.RequestURI
	originalRelayMode, hadRelayMode := c.Get("relay_mode")
	c.Request.Method = http.MethodGet
	c.Request.URL.Path = "/v1/video/generations/" + id
	c.Request.RequestURI = c.Request.URL.Path
	c.Params = gin.Params{{Key: "task_id", Value: id}}
	c.Set("task_id", id)
	// The native POST initially selected the video-submit mode. The internal
	// fetch must explicitly use the existing video-fetch handler; Path2RelayMode
	// does not infer video modes from the rewritten path.
	c.Set("relay_mode", relayconstant.RelayModeVideoFetchByID)
	defer func() {
		c.Request.Method, c.Request.URL.Path, c.Request.RequestURI = originalMethod, originalPath, originalURI
		if hadRelayMode {
			c.Set("relay_mode", originalRelayMode)
		} else {
			c.Set("relay_mode", nil)
		}
	}()

	last := initial
	for attempt := 0; attempt < 180; attempt++ {
		var capture nativeResponseWriter
		capture.ResponseWriter = c.Writer
		c.Writer = &capture
		info, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
		if err == nil {
			errTask := relay.RelayTaskFetch(c, info.RelayMode)
			if errTask != nil {
				err = fmt.Errorf("%s: %s", errTask.Code, errTask.Message)
			}
		}
		c.Writer = capture.ResponseWriter
		if err != nil {
			return nil, err
		}
		if len(capture.body.Bytes()) > 0 {
			raw := capture.body.Bytes()
			if augmented := augmentLyriaInteractionResponse(c.GetInt("id"), id, raw); len(augmented) > 0 {
				raw = augmented
			}
			last, err = nativeInteractionResponse(raw, c.GetString("native_interactions_model"))
			if err != nil {
				return nil, err
			}
			if stream {
				writeNativeSSE(c, last)
			}
			var current map[string]any
			_ = common.Unmarshal(last, &current)
			status, _ := current["status"].(string)
			if status == "completed" || status == "failed" || status == "cancelled" {
				return last, nil
			}
		}
		time.Sleep(time.Second)
	}
	return last, fmt.Errorf("interaction did not complete within 180 seconds")
}

func writeNativeSSE(c *gin.Context, value []byte) {
	writeNativeSSEToWriter(c.Writer, value)
}

func writeNativeSSEToWriter(writer http.ResponseWriter, value []byte) {
	_, _ = fmt.Fprintf(writer, "data: %s\n\n", value)
	flushNativeSSEWriter(writer)
}

func writeNativeSSEEventToWriter(writer http.ResponseWriter, event []byte) {
	_, _ = writer.Write(event)
	if len(event) < 2 || string(event[len(event)-2:]) != "\n\n" {
		_, _ = writer.Write([]byte("\n\n"))
	}
	flushNativeSSEWriter(writer)
}

func flushNativeSSEWriter(writer http.ResponseWriter) {
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeNativeSSEValue(c *gin.Context, value any) {
	data, err := common.Marshal(value)
	if err == nil {
		writeNativeSSE(c, data)
	}
}

// NativeInteractionsFetch converts the existing task response to an
// Interaction response. Lyria terminal results are served from task.data;
// other task platforms retain their existing realtime-fetch behavior.
func NativeInteractionsFetch(c *gin.Context) {
	// Locally dispatched Lyria background tasks are authoritative in tasks.data.
	// Do not send their local task id to the provider's GET endpoint; the worker
	// has already performed the provider call and finalized this row.
	if taskID := c.Param("interaction_id"); taskID != "" {
		if task, exists, err := model.GetByTaskId(c.GetInt("id"), taskID); err == nil && exists && task.Platform == constant.TaskPlatformLyria {
			raw := task.Data
			if len(raw) == 0 {
				raw, _ = common.Marshal(map[string]any{"id": taskID, "object": "interaction", "status": nativeInteractionStatus(string(task.Status))})
			}
			// Local async tasks store the provider response verbatim. For a
			// Vertex channel, convert that response before exposing it through
			// the public Google Interactions endpoint. Native Google channels
			// remain unchanged.
			vertexLyria := false
			if channel, channelErr := model.CacheGetChannel(task.ChannelId); channelErr == nil && channel != nil {
				vertexLyria = channel.Type == constant.ChannelTypeVertexAi
			}
			response, responseErr := nativeInteractionResponse(raw, task.Properties.OriginModelName)
			if responseErr == nil && vertexLyria {
				response, responseErr = convertVertexLyriaResponseToGoogle(response)
			}
			if responseErr == nil {
				c.Data(http.StatusOK, "application/json", response)
				return
			}
		}
	}
	var capture nativeResponseWriter
	capture.ResponseWriter = c.Writer
	c.Writer = &capture
	RelayTaskFetch(c)
	c.Writer = capture.ResponseWriter

	if capture.status >= http.StatusBadRequest || len(capture.body.Bytes()) == 0 {
		status := capture.status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		c.Status(status)
		_, _ = c.Writer.Write(capture.body.Bytes())
		return
	}
	if taskID := c.Param("interaction_id"); taskID != "" {
		if augmented := augmentLyriaInteractionResponse(c.GetInt("id"), taskID, capture.body.Bytes()); len(augmented) > 0 {
			capture.body.Reset()
			_, _ = capture.body.Write(augmented)
		}
	}
	body, err := nativeInteractionResponse(capture.body.Bytes(), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.OpenAIError{Message: err.Error(), Type: "server_error"})
		return
	}
	c.Data(capture.status, "application/json", body)
}

func augmentLyriaInteractionResponse(userID int, taskID string, raw []byte) []byte {
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil || !exists || task.Platform != constant.TaskPlatformLyria || len(task.Data) == 0 {
		return nil
	}
	return mergeLyriaTaskSnapshot(raw, task.Data, task)
}

func mergeLyriaTaskSnapshot(raw, providerRaw []byte, task *model.Task) []byte {
	if task == nil || task.Platform != constant.TaskPlatformLyria {
		return nil
	}
	var current map[string]any
	var provider map[string]any
	if common.Unmarshal(raw, &current) != nil || common.Unmarshal(providerRaw, &provider) != nil {
		return nil
	}
	target := current
	if wrapped, ok := current["data"].(map[string]any); ok {
		target = wrapped
	}
	for _, field := range []string{"steps", "outputs", "error", "errors"} {
		if value, ok := provider[field]; ok {
			target[field] = value
		}
	}
	target["id"] = task.TaskID
	target["task_id"] = task.TaskID
	target["status"] = nativeInteractionStatus(task.Status)
	if task.FailReason != "" {
		target["fail_reason"] = task.FailReason
	}
	result, err := common.Marshal(current)
	if err != nil {
		return nil
	}
	return result
}

type nativeResponseWriter struct {
	gin.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *nativeResponseWriter) WriteHeader(statusCode int) {
	if w.status == 0 {
		w.status = statusCode
	}
}

func (w *nativeResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *nativeResponseWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func nativeInteractionResponse(raw []byte, fallbackModel string) ([]byte, error) {
	if fallbackModel == "lyria-3-pro-preview" || fallbackModel == "lyria-3-clip-preview" {
		return append([]byte(nil), raw...), nil
	}
	var envelope map[string]any
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	data := envelope
	if wrapped, ok := envelope["data"].(map[string]any); ok {
		data = wrapped
	}

	interaction := map[string]any{
		"id":     data["id"],
		"object": "interaction",
		"status": nativeInteractionStatus(data["status"]),
	}
	if platform, _ := data["platform"].(string); platform == string(constant.TaskPlatformLyria) {
		if taskID, _ := data["task_id"].(string); taskID != "" {
			interaction["id"] = taskID
		}
	}
	if interaction["id"] == nil {
		interaction["id"] = data["task_id"]
	}
	if fallbackModel != "" {
		interaction["model"] = fallbackModel
	} else if model, ok := data["model"].(string); ok && model != "" {
		interaction["model"] = model
	}
	if url, ok := data["url"].(string); ok && url != "" {
		interaction["steps"] = []any{map[string]any{
			"type": "video",
			"content": []any{map[string]any{
				"type": "video",
				"uri":  url,
			}},
		}}
	}
	// Lyria stores the provider Interaction response in task.data. Preserve its
	// audio/text steps instead of converting the audio data into a video URL.
	if steps, ok := data["steps"]; ok {
		interaction["steps"] = steps
	} else if nested, ok := data["data"].(map[string]any); ok {
		if steps, ok := nested["steps"]; ok {
			interaction["steps"] = steps
		}
	}
	if outputs, ok := data["outputs"]; ok {
		interaction["outputs"] = outputs
	} else if nested, ok := data["data"].(map[string]any); ok {
		if outputs, ok := nested["outputs"]; ok {
			interaction["outputs"] = outputs
		}
	}
	if errors, ok := data["errors"]; ok {
		interaction["errors"] = errors
	}
	// The video converter stores the original Interaction usage under the
	// OpenAI-compatible response metadata. Expose it again at the native
	// Interaction level without synthesizing or changing token counts.
	if usage, ok := data["usage"].(map[string]any); ok {
		interaction["usage"] = usage
	} else if metadata, ok := data["metadata"].(map[string]any); ok {
		if usage, ok := metadata["usage"].(map[string]any); ok {
			interaction["usage"] = usage
		}
	}
	// Keep the upstream failure detail visible to native Interaction callers.
	// Without this, a failed asynchronous task is reduced to status=failed and
	// the caller cannot distinguish an input validation error from an upstream
	// provider failure.
	for _, field := range []string{"error", "fail_reason", "reason"} {
		if value, ok := data[field]; ok && value != nil {
			interaction[field] = value
		}
	}
	return common.Marshal(interaction)
}

// convertVertexLyriaResponse adapts only the Vertex Lyria response returned
// through the public Google Interactions route. The original outputs/steps
// are retained and Google convenience fields are added for clients that read
// output_audio/output_text.
func convertVertexLyriaResponse(raw []byte, vertexLyria bool) ([]byte, error) {
	if !vertexLyria {
		return raw, nil
	}
	return convertVertexLyriaResponseToGoogle(raw)
}

func convertVertexLyriaResponseToGoogle(raw []byte) ([]byte, error) {
	var response map[string]any
	if err := common.Unmarshal(raw, &response); err != nil {
		return nil, err
	}

	var audio map[string]any
	var textParts []string
	collect := func(blocks []any) {
		for _, value := range blocks {
			block, ok := value.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := block["type"].(string)
			switch typ {
			case "audio":
				if audio == nil {
					audio = map[string]any{}
					for _, key := range []string{"data", "mime_type"} {
						if item, exists := block[key]; exists {
							audio[key] = item
						}
					}
				}
			case "text":
				if text, ok := block["text"].(string); ok && text != "" {
					textParts = append(textParts, text)
				}
			}
		}
	}
	if outputs, ok := response["outputs"].([]any); ok {
		collect(outputs)
	}
	if steps, ok := response["steps"].([]any); ok {
		for _, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if content, ok := step["content"].([]any); ok {
				collect(content)
			}
		}
	}
	if audio != nil && response["output_audio"] == nil {
		response["output_audio"] = audio
	}
	if len(textParts) > 0 && response["output_text"] == nil {
		response["output_text"] = strings.Join(textParts, "\n")
	}
	delete(response, "outputs")
	delete(response, "steps")
	return common.Marshal(response)
}

func nativeInteractionStatus(status any) string {
	var value string
	switch typed := status.(type) {
	case string:
		value = typed
	case model.TaskStatus:
		value = string(typed)
	}
	switch strings.ToLower(value) {
	case "succeeded", "completed":
		return "completed"
	case "success":
		return "completed"
	case "failed", "failure":
		return "failed"
	case "cancelled", "canceled":
		return "cancelled"
	default:
		return "in_progress"
	}
}
