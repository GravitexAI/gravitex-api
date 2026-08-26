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

const nativeOmniModel = "gemini-omni-flash-preview"

// NativeInteractions converts the Google Interactions API request into the
// gateway's existing video task request. The route is intentionally isolated
// so existing /v1/video/generations and /v1/videos clients are unchanged.
func NativeInteractions() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost {
			originalPath := c.Request.URL.Path
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
			if isLyriaInteractionModel(modelName) && !shouldUseLyriaNativeAdapter(originalPath, modelName) {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": gin.H{
					"message": "Lyria 3 is supported only on /v1beta/interactions",
					"code":    "invalid_request",
				}})
				return
			}
			if shouldUseLyriaNativeAdapter(originalPath, modelName) {
				c.Set(common.KeyLyriaRawMirror, true)
				c.Set(common.KeyLyriaRawRequestBody, append([]byte(nil), raw...))
				c.Set("native_interactions_original_path", originalPath)
			}
			c.Set("native_interactions", true)
			c.Set("native_interactions_model", modelName)
			background, hasBackground := requestBool(raw, "background")
			if !hasBackground {
				// Lyria 3 does not support background interactions. Keep the
				// historical async default for Omni, but do not inject background
				// into Lyria requests that omitted the field.
				background = !isLyriaInteractionModel(modelName)
			}
			c.Set("native_interactions_background", background)
			// Only an explicit background=true request is locally asynchronous.
			// Synchronous Lyria requests must not create a task row.
			c.Set("native_interactions_async", background && isLyriaInteractionModel(modelName))
			stream, _ := requestBool(raw, "stream")
			c.Set("native_interactions_stream", stream && strings.EqualFold(modelName, nativeOmniModel))
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

func requestBool(raw []byte, key string) (bool, bool) {
	var request map[string]any
	if err := common.Unmarshal(raw, &request); err != nil {
		return false, false
	}
	value, ok := request[key].(bool)
	return value, ok
}

func isLyriaInteractionModel(modelName string) bool {
	return modelName == "lyria-3-pro-preview" || modelName == "lyria-3-clip-preview"
}

func shouldUseLyriaNativeAdapter(path, modelName string) bool {
	return path == "/v1beta/interactions" && isLyriaInteractionModel(modelName)
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
	if modelName == "lyria-3-pro-preview" || modelName == "lyria-3-clip-preview" {
		converted, err := convertLyriaInteractionRequest(request)
		return converted, modelName, err
	}
	prompt := nativeInteractionPrompt(request["input"])
	if strings.TrimSpace(prompt) == "" {
		return nil, "", fmt.Errorf("field input is required")
	}

	metadata := map[string]any{}
	if nativeInteractionHasAudio(request["input"]) {
		return nil, "", fmt.Errorf("audio input is not supported for gemini-omni-flash-preview video tasks")
	}
	images, videos := nativeInteractionMedia(request["input"])
	if len(images) == 1 {
		metadata["image"] = images[0]
	}
	if len(images) > 1 {
		metadata["images"] = images
	}
	if len(videos) > 0 {
		metadata["video"] = videos[0]
	}
	if generationConfig, ok := request["generation_config"].(map[string]any); ok {
		if videoConfig, ok := generationConfig["video_config"].(map[string]any); ok {
			for _, key := range []string{"task", "resolution", "aspect_ratio", "durationSeconds", "duration_seconds"} {
				if value, exists := videoConfig[key]; exists {
					metadata[key] = value
				}
			}
		}
	}
	if background, exists := request["background"]; exists {
		metadata["background"] = background
	}
	stream, _ := request["stream"].(bool)
	if stream && strings.EqualFold(modelName, nativeOmniModel) {
		metadata["stream"] = true
	}
	for _, key := range []string{"previous_interaction_id"} {
		if value, exists := request[key]; exists {
			metadata[key] = value
		}
	}
	// Native synchronous requests use the existing background task path. Native
	// SSE is different: the Omni adaptor sends the request directly to the
	// upstream stream and completes the task from the final GET response.
	if stream && strings.EqualFold(modelName, nativeOmniModel) {
		metadata["background"] = false
	} else if background, ok := request["background"].(bool); !ok || !background {
		metadata["background"] = true
	}
	if responseFormat := nativeInteractionResponseFormat(request["response_format"]); responseFormat != nil {
		if aspect, ok := responseFormat["aspect_ratio"].(string); ok {
			metadata["aspectRatio"] = aspect
		}
		if duration, ok := responseFormat["duration"].(string); ok {
			if seconds, err := strconv.Atoi(strings.TrimSuffix(duration, "s")); err == nil && seconds > 0 {
				metadata["durationSeconds"] = seconds
			}
		}
		if delivery, ok := responseFormat["delivery"].(string); ok && (delivery == "inline" || delivery == "uri") {
			metadata["delivery"] = delivery
		}
		if gcsURI, ok := responseFormat["gcs_uri"].(string); ok && strings.TrimSpace(gcsURI) != "" {
			metadata["gcs_uri"] = strings.TrimSpace(gcsURI)
		}
	}

	converted := map[string]any{"model": modelName, "prompt": prompt, "metadata": metadata}
	data, err := common.Marshal(converted)
	return data, modelName, err
}

func convertLyriaInteractionRequest(request map[string]any) ([]byte, error) {
	input, exists := request["input"]
	prompt := nativeInteractionPrompt(input)
	if strings.TrimSpace(prompt) == "" {
		// Internal task plumbing requires a prompt, but the original request is
		// sent to Vertex verbatim. Leave provider-side parameter validation to
		// Vertex instead of rejecting the public request here.
		prompt = "Lyria interaction"
	}
	metadata := map[string]any{}
	if exists {
		metadata["input"] = input
	}
	if value, ok := request["response_format"]; ok {
		metadata["response_format"] = value
	}
	if value, ok := request["previous_interaction_id"]; ok {
		metadata["previous_interaction_id"] = value
	}
	if value, exists := request["background"]; exists {
		metadata["background"] = value
	}
	if value, exists := request["store"]; exists {
		metadata["store"] = value
	}
	return common.Marshal(map[string]any{
		"model":    request["model"],
		"prompt":   prompt,
		"metadata": metadata,
	})
}

func nativeInteractionResponseFormat(value any) map[string]any {
	switch format := value.(type) {
	case map[string]any:
		return format
	case []any:
		for _, item := range format {
			candidate, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if contentType, _ := candidate["type"].(string); contentType == "video" {
				return candidate
			}
		}
	}
	return nil
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

func nativeInteractionMedia(input any) ([]string, []string) {
	items, ok := input.([]any)
	if !ok {
		return nil, nil
	}
	var images, videos []string
	for _, item := range items {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := content["type"].(string)
		value, _ := content["data"].(string)
		if value == "" {
			value, _ = content["uri"].(string)
		}
		if value == "" {
			continue
		}
		if kind == "image" {
			mimeType, _ := content["mime_type"].(string)
			if !strings.HasPrefix(value, "data:") && !strings.Contains(value, "://") {
				if mimeType == "" {
					mimeType = "image/png"
				}
				value = "data:" + mimeType + ";base64," + value
			}
			images = append(images, value)
		} else if kind == "video" {
			mimeType, _ := content["mime_type"].(string)
			if !strings.HasPrefix(value, "data:") && !strings.Contains(value, "://") {
				if mimeType == "" {
					mimeType = "video/mp4"
				}
				value = "data:" + mimeType + ";base64," + value
			}
			videos = append(videos, value)
		}
	}
	return images, videos
}

func nativeInteractionHasAudio(input any) bool {
	items, ok := input.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		content, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := content["type"].(string)
		if strings.EqualFold(kind, "audio") {
			return true
		}
	}
	return false
}
