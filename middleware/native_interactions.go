package middleware

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// NativeInteractions converts the Google Interactions API request into the
// gateway's existing video task request. The route is intentionally isolated
// so existing /v1/video/generations and /v1/videos clients are unchanged.
func NativeInteractions() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost {
			body, err := common.GetBodyStorage(c)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "code": "invalid_request"}})
				return
			}
			raw, err := body.Bytes()
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "code": "invalid_request"}})
				return
			}

			converted, modelName, err := convertNativeInteractionRequest(raw)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "code": "invalid_request"}})
				return
			}
			c.Set("native_interactions", true)
			c.Set("native_interactions_model", modelName)
			convertedStorage, err := common.CreateBodyStorage(converted)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "code": "invalid_request"}})
				return
			}
			// The distributor reads the reusable BodyStorage cache, not only
			// Request.Body, so both must point at the converted request.
			_ = body.Close()
			c.Set(common.KeyBodyStorage, convertedStorage)
			c.Request.Body = io.NopCloser(convertedStorage)
		}

		// Reuse the existing task distributor and controller. GET needs the
		// internal task path so it selects the existing video fetch mode.
		if c.Request.Method == http.MethodPost {
			c.Request.URL.Path = "/v1/video/generations"
		} else if c.Request.Method == http.MethodGet {
			c.Request.URL.Path = "/v1/video/generations/" + c.Param("interaction_id")
			c.Set("task_id", c.Param("interaction_id"))
		}
		c.Next()
	}
}

func convertNativeInteractionRequest(raw []byte) ([]byte, string, error) {
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return nil, "", fmt.Errorf("invalid JSON request body")
	}
	modelName, _ := request["model"].(string)
	if strings.TrimSpace(modelName) == "" {
		return nil, "", fmt.Errorf("field model is required")
	}
	prompt := nativeInteractionPrompt(request["input"])
	if strings.TrimSpace(prompt) == "" {
		return nil, "", fmt.Errorf("field input is required")
	}

	metadata := map[string]any{}
	if image, mimeType := nativeInteractionImage(request["input"]); image != "" {
		metadata["image"] = "data:" + mimeType + ";base64," + image
	}
	if generationConfig, ok := request["generation_config"].(map[string]any); ok {
		if videoConfig, ok := generationConfig["video_config"].(map[string]any); ok {
			for _, key := range []string{"resolution", "aspect_ratio", "durationSeconds", "duration_seconds"} {
				if value, exists := videoConfig[key]; exists {
					metadata[key] = value
				}
			}
		}
	}
	if responseFormat, ok := request["response_format"].(map[string]any); ok {
		if aspect, ok := responseFormat["aspect_ratio"].(string); ok {
			metadata["aspectRatio"] = aspect
		}
		if duration, ok := responseFormat["duration"].(string); ok {
			if seconds, err := strconv.Atoi(strings.TrimSuffix(duration, "s")); err == nil && seconds > 0 {
				metadata["durationSeconds"] = seconds
			}
		}
	}

	converted := map[string]any{"model": modelName, "prompt": prompt, "metadata": metadata}
	data, err := common.Marshal(converted)
	return data, modelName, err
}

func nativeInteractionPrompt(input any) string {
	if text, ok := input.(string); ok {
		return text
	}
	items, ok := input.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, item := range items {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := content["text"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func nativeInteractionImage(input any) (string, string) {
	items, ok := input.([]any)
	if !ok {
		return "", ""
	}
	for _, item := range items {
		content, ok := item.(map[string]any)
		if !ok || content["type"] != "image" {
			continue
		}
		data, _ := content["data"].(string)
		mimeType, _ := content["mime_type"].(string)
		if data != "" {
			if mimeType == "" {
				mimeType = "image/png"
			}
			return data, mimeType
		}
	}
	return "", ""
}
