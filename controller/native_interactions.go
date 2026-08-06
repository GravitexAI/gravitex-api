package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
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
	if !c.GetBool("native_interactions_background") && !c.GetBool("native_interactions_stream") {
		body, err = waitNativeInteraction(c, body, false)
		if err != nil {
			c.JSON(http.StatusGatewayTimeout, types.OpenAIError{Message: err.Error(), Type: "timeout"})
			return
		}
	}
	c.Data(capture.status, "application/json", body)
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
			last, err = nativeInteractionResponse(capture.body.Bytes(), c.GetString("native_interactions_model"))
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
// Interaction response. The underlying fetch also updates task state and
// performs the existing completion billing.
func NativeInteractionsFetch(c *gin.Context) {
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
	body, err := nativeInteractionResponse(capture.body.Bytes(), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.OpenAIError{Message: err.Error(), Type: "server_error"})
		return
	}
	c.Data(capture.status, "application/json", body)
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

func nativeInteractionStatus(status any) string {
	value, _ := status.(string)
	switch value {
	case "succeeded", "completed":
		return "completed"
	case "failed", "cancelled":
		return value
	default:
		return "in_progress"
	}
}
