package gemini

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

const omniModelName = "gemini-omni-flash-preview"
const omniTaskIDPrefix = "omni:"

type omniContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	URI      string `json:"uri,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

type omniUsageModality struct {
	Modality string `json:"modality"`
	Tokens   int    `json:"tokens"`
}

type omniInteraction struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Steps []struct {
		Type    string        `json:"type"`
		Content []omniContent `json:"content"`
	} `json:"steps"`
	Usage struct {
		TotalInputTokens   int                 `json:"total_input_tokens"`
		TotalOutputTokens  int                 `json:"total_output_tokens"`
		TotalTokens        int                 `json:"total_tokens"`
		TotalThoughtTokens int                 `json:"total_thought_tokens"`
		InputByModality    []omniUsageModality `json:"input_tokens_by_modality"`
		OutputByModality   []omniUsageModality `json:"output_tokens_by_modality"`
	} `json:"usage"`
}

func isOmniModel(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), omniModelName)
}

func markOmniTaskID(interactionID string) string {
	return omniTaskIDPrefix + interactionID
}

func unmarkOmniTaskID(taskID string) string {
	return strings.TrimPrefix(taskID, omniTaskIDPrefix)
}

func buildOmniRequestURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1beta/interactions"
}

func buildOmniRequestBody(req relaycommon.TaskSubmitReq) ([]byte, error) {
	metadata := req.Metadata
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	task := "text_to_video"
	if image, ok := metadata["image"].(string); ok && strings.TrimSpace(image) != "" {
		task = "image_to_video"
	}
	content := []omniContent{{Type: "text", Text: req.Prompt}}
	if image, ok := metadata["image"].(string); ok && strings.TrimSpace(image) != "" {
		parsed, err := ParseImageInput(image)
		if err != nil {
			return nil, fmt.Errorf("image conversion failed: %w", err)
		}
		if parsed != nil {
			content = append(content, omniContent{Type: "image", Data: parsed.BytesBase64Encoded, MimeType: parsed.MimeType})
		}
	}
	duration := sanitizeDurationSecondsFromMetadata(metadata)
	aspect := sanitizeAspectRatioFromMetadata(metadata)
	body := map[string]interface{}{
		"model": omniModelName,
		"input": content,
		"generation_config": map[string]interface{}{
			"video_config": map[string]string{"task": task},
		},
		"response_format": map[string]string{
			"type":         "video",
			"aspect_ratio": aspect,
			"duration":     fmt.Sprintf("%ds", duration),
			"delivery":     "inline",
		},
		"background": true,
	}
	return common.Marshal(body)
}

func ParseOmniTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var interaction omniInteraction
	if err := common.Unmarshal(respBody, &interaction); err != nil {
		return nil, fmt.Errorf("unmarshal Omni interaction failed: %w", err)
	}
	ti := &relaycommon.TaskInfo{TaskID: interaction.ID}
	switch strings.ToLower(interaction.Status) {
	case "completed":
		ti.Status = model.TaskStatusSuccess
		ti.Progress = "100%"
	case "failed", "cancelled":
		ti.Status = model.TaskStatusFailure
		ti.Progress = "100%"
		ti.Reason = interaction.Error.Message
		if ti.Reason == "" {
			ti.Reason = "Omni interaction failed with status: " + interaction.Status
		}
	default:
		ti.Status = model.TaskStatusInProgress
		ti.Progress = "50%"
	}
	for _, modality := range interaction.Usage.InputByModality {
		if strings.EqualFold(modality.Modality, "text") || strings.EqualFold(modality.Modality, "image") || strings.EqualFold(modality.Modality, "video") || strings.EqualFold(modality.Modality, "audio") {
			ti.InputTokens += modality.Tokens
		}
	}
	if ti.InputTokens == 0 {
		ti.InputTokens = interaction.Usage.TotalInputTokens
	}
	for _, modality := range interaction.Usage.OutputByModality {
		switch strings.ToLower(modality.Modality) {
		case "video":
			ti.VideoOutputTokens += modality.Tokens
		case "text", "thought":
			ti.TextOutputTokens += modality.Tokens
		}
	}
	if ti.VideoOutputTokens == 0 && interaction.Usage.TotalOutputTokens > ti.TextOutputTokens {
		ti.VideoOutputTokens = interaction.Usage.TotalOutputTokens - ti.TextOutputTokens
	}
	if ti.TextOutputTokens == 0 && interaction.Usage.TotalThoughtTokens > 0 {
		ti.TextOutputTokens = interaction.Usage.TotalThoughtTokens
	}
	ti.CompletionTokens = ti.VideoOutputTokens
	ti.TotalTokens = interaction.Usage.TotalTokens
	if ti.TotalTokens == 0 {
		ti.TotalTokens = ti.InputTokens + ti.VideoOutputTokens + ti.TextOutputTokens
	}
	if ti.Status != model.TaskStatusSuccess {
		return ti, nil
	}
	for _, step := range interaction.Steps {
		if step.Type != "model_output" {
			continue
		}
		for _, item := range step.Content {
			if item.Type != "video" {
				continue
			}
			if item.URI != "" {
				ti.RemoteUrl = item.URI
				return ti, nil
			}
			if item.Data != "" {
				ti.Url = "data:" + defaultOmniMimeType(item.MimeType) + ";base64," + item.Data
				return ti, nil
			}
		}
	}
	return ti, nil
}

func defaultOmniMimeType(mime string) string {
	if strings.TrimSpace(mime) == "" {
		return "video/mp4"
	}
	return mime
}

func OmniVideoURLFromTaskData(data []byte) string {
	var interaction omniInteraction
	if common.Unmarshal(data, &interaction) != nil {
		return ""
	}
	for _, step := range interaction.Steps {
		for _, item := range step.Content {
			if item.Type != "video" {
				continue
			}
			if item.URI != "" {
				return item.URI
			}
			if item.Data != "" {
				return "data:" + defaultOmniMimeType(item.MimeType) + ";base64," + item.Data
			}
		}
	}
	return ""
}

func omniInteractionURL(baseURL, id string) string {
	return buildOmniRequestURL(baseURL) + "/" + id
}

func isOmniDataURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:video/")
}
