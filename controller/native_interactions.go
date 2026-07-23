package controller

import (
	"bytes"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// NativeInteractionsSubmit exposes the Google Interactions response shape
// while delegating task creation, persistence, retry and billing to RelayTask.
func NativeInteractionsSubmit(c *gin.Context) {
	var capture nativeResponseWriter
	capture.ResponseWriter = c.Writer
	c.Writer = &capture
	RelayTask(c)
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
	body, err := nativeInteractionResponse(capture.body.Bytes(), c.GetString("native_interactions_model"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, types.OpenAIError{Message: err.Error(), Type: "server_error"})
		return
	}
	c.Data(capture.status, "application/json", body)
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
