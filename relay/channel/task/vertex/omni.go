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
const omniTaskIDPrefix = "omni:"

func isVertexOmniModel(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), vertexOmniModelName)
}

func markOmniTaskID(interactionID string) string {
	return omniTaskIDPrefix + interactionID
}

func unmarkOmniTaskID(taskID string) string {
	return strings.TrimPrefix(taskID, omniTaskIDPrefix)
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
	return taskgemini.BuildOmniRequestBody(req)
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
