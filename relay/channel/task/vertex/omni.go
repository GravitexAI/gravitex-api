package vertex

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	taskgemini "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

const vertexOmniModelName = "gemini-omni-flash-preview"

func isVertexOmniModel(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), vertexOmniModelName)
}

func taskModelFromBody(body map[string]any) string {
	modelName, _ := body["model"].(string)
	return modelName
}

func buildVertexOmniURL(baseURL, key string) (string, error) {
	if isAPIKey(key) {
		baseURL = strings.TrimRight(baseURL, "/")
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com"
		}
		return baseURL + "/v1beta/interactions", nil
	}
	var credentials vertexcore.Credentials
	if err := common.Unmarshal([]byte(key), &credentials); err != nil {
		return "", fmt.Errorf("failed to decode credentials: %w", err)
	}
	return vertexcore.BuildAPIBaseURL(baseURL, "v1beta1", credentials.ProjectID, "global") + "/interactions", nil
}

func buildVertexOmniBody(req relaycommon.TaskSubmitReq) ([]byte, error) {
	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	task := "text_to_video"
	contents := []map[string]interface{}{{"type": "text", "text": req.Prompt}}
	if image, ok := metadata["image"].(string); ok && strings.TrimSpace(image) != "" {
		task = "image_to_video"
		parsed, err := taskgemini.ParseImageInput(image)
		if err != nil {
			return nil, fmt.Errorf("image conversion failed: %w", err)
		}
		if parsed != nil {
			contents = append(contents, map[string]interface{}{
				"type": "image", "data": parsed.BytesBase64Encoded, "mime_type": parsed.MimeType,
			})
		}
	}
	duration := vertexSanitizeDurationSeconds(metadata)
	body := map[string]interface{}{
		"model":             vertexOmniModelName,
		"input":             contents,
		"generation_config": map[string]interface{}{"video_config": map[string]string{"task": task}},
		"response_format": map[string]string{
			"type": "video", "aspect_ratio": vertexSanitizeAspectRatio(metadata),
			"duration": fmt.Sprintf("%ds", duration), "delivery": "inline",
		},
		"background": true,
	}
	return common.Marshal(body)
}

func parseVertexOmniTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	// The response shape is identical to the Gemini Developer API. Reuse the
	// protocol parser by keeping the implementation in the Gemini task package.
	return taskgemini.ParseOmniTaskResult(body)
}

func fetchVertexOmniTask(baseURL, key, id, proxy string) (*http.Response, error) {
	url, err := buildVertexOmniURL(baseURL, key)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, url+"/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if isAPIKey(key) {
		req.Header.Set("x-goog-api-key", key)
	} else {
		var credentials vertexcore.Credentials
		if err := common.Unmarshal([]byte(key), &credentials); err != nil {
			return nil, err
		}
		token, err := vertexcore.AcquireAccessToken(credentials, proxy)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("x-goog-user-project", credentials.ProjectID)
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
