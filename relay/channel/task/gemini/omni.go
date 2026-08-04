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

type omniInteractionStep struct {
	Type    string        `json:"type"`
	Content []omniContent `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type omniInteraction struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Steps []omniInteractionStep `json:"steps"`
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
	videoInput := metadataString(metadata, "video")
	if videoInput == "" {
		videoInput = strings.TrimSpace(req.InputReference)
	}
	task := omniTaskFromMetadata(metadata, req.Images, videoInput)
	content := []omniContent{{Type: "text", Text: req.Prompt}}
	images := req.Images
	if len(images) == 0 {
		images = omniStringSlice(metadata["images"])
	}
	if len(images) == 0 {
		if image := metadataString(metadata, "image"); image != "" {
			images = []string{image}
		}
	}
	for _, image := range images {
		parsed, err := ParseImageInput(image)
		if err != nil {
			return nil, fmt.Errorf("image conversion failed: %w", err)
		}
		if parsed != nil {
			content = append(content, omniContent{Type: "image", Data: parsed.BytesBase64Encoded, MimeType: parsed.MimeType})
		}
	}
	if videoInput != "" {
		parsed, err := parseOmniVideoInput(videoInput)
		if err != nil {
			return nil, fmt.Errorf("video conversion failed: %w", err)
		}
		if parsed != nil {
			content = append(content, *parsed)
		}
	}
	duration := sanitizeDurationSecondsFromMetadata(metadata)
	aspect := sanitizeAspectRatioFromMetadata(metadata)
	background := true
	if value, ok := metadata["background"]; ok {
		if parsed, ok := value.(bool); ok {
			background = parsed
		}
	}
	stream := false
	if value, ok := metadata["stream"]; ok {
		if parsed, ok := value.(bool); ok {
			stream = parsed
		}
	}
	if stream {
		// The gateway translates upstream task snapshots to SSE. Keep the
		// upstream call asynchronous so the client receives progress events
		// while the normal task poller performs completion billing.
		background = true
	}
	body := map[string]interface{}{
		"model": omniModelName,
		"input": content,
		"generation_config": map[string]interface{}{
			"video_config": map[string]string{"task": task},
		},
		"background": background,
	}
	if stream {
		// Native Interactions SSE is a foreground upstream request. The gateway
		// still persists the completed interaction as a normal video task after
		// the stream ends so existing billing remains unchanged.
		body["stream"] = true
		body["background"] = false
	}
	responseFormat := map[string]string{
		"type":     "video",
		"duration": fmt.Sprintf("%ds", duration),
		"delivery": "inline",
	}
	if delivery := strings.ToLower(metadataString(metadata, "delivery")); delivery == "uri" {
		responseFormat["delivery"] = "uri"
		if gcsURI := metadataString(metadata, "gcs_uri"); gcsURI != "" {
			responseFormat["gcs_uri"] = gcsURI
		}
	}
	// Omni edit tasks reject response_format.aspect_ratio. Keep the
	// requested ratio for text/image/reference generation only.
	if task != "edit" {
		responseFormat["aspect_ratio"] = aspect
	}
	// The Interactions API defines response_format as an array. Keep inline
	// delivery so the gateway does not require an OSS/GCS output location.
	body["response_format"] = []map[string]string{responseFormat}
	if previousID := metadataString(metadata, "previous_interaction_id"); previousID != "" {
		body["previous_interaction_id"] = previousID
	}
	return common.Marshal(body)
}

// BuildOmniRequestBody is shared by the Gemini Developer API and Vertex
// channel adapters because both expose the same Interactions payload.
func BuildOmniRequestBody(req relaycommon.TaskSubmitReq) ([]byte, error) {
	return buildOmniRequestBody(req)
}

func omniTaskFromMetadata(metadata map[string]interface{}, images []string, video string) string {
	if task := strings.ToLower(strings.TrimSpace(metadataString(metadata, "task"))); task != "" {
		switch task {
		case "text_to_video", "image_to_video", "reference_to_video", "edit":
			return task
		}
	}
	if video != "" {
		return "edit"
	}
	if len(images) > 1 || len(omniStringSlice(metadata["images"])) > 1 {
		return "reference_to_video"
	}
	if len(images) == 1 || metadataString(metadata, "image") != "" {
		return "image_to_video"
	}
	return "text_to_video"
}

func metadataString(metadata map[string]interface{}, key string) string {
	if value, ok := metadata[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func omniStringSlice(value interface{}) []string {
	var values []string
	switch items := value.(type) {
	case []string:
		values = items
	case []interface{}:
		for _, item := range items {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				values = append(values, strings.TrimSpace(value))
			}
		}
	}
	return values
}

func parseOmniVideoInput(value string) (*omniContent, error) {
	return parseOmniMediaInput(value, "video", "video/mp4")
}

func parseOmniMediaInput(value, contentType, defaultMimeType string) (*omniContent, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if strings.HasPrefix(value, "data:") {
		parsed := parseDataURI(value)
		if parsed == nil {
			return nil, fmt.Errorf("invalid %s data URI", contentType)
		}
		return &omniContent{Type: contentType, Data: parsed.BytesBase64Encoded, MimeType: defaultOmniMimeTypeWithFallback(parsed.MimeType, defaultMimeType)}, nil
	}
	if strings.Contains(value, "://") {
		return &omniContent{Type: contentType, URI: value, MimeType: defaultMimeType}, nil
	}
	return &omniContent{Type: contentType, Data: value, MimeType: defaultMimeType}, nil
}

func defaultOmniMimeTypeWithFallback(mime, fallback string) string {
	if strings.TrimSpace(mime) == "" {
		return fallback
	}
	return mime
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
			for _, step := range interaction.Steps {
				if step.Error != nil && step.Error.Message != "" {
					ti.Reason = step.Error.Message
					break
				}
			}
		}
		if ti.Reason == "" {
			ti.Reason = "Omni interaction failed with status: " + interaction.Status
		}
	default:
		ti.Status = model.TaskStatusInProgress
		ti.Progress = "50%"
	}
	for _, modality := range interaction.Usage.InputByModality {
		switch strings.ToLower(modality.Modality) {
		case "text":
			ti.TextInputTokens += modality.Tokens
		case "image":
			ti.ImageInputTokens += modality.Tokens
		case "video":
			ti.VideoInputTokens += modality.Tokens
		case "audio":
			// Omni currently does not support audio input. Keep it in the
			// aggregate only if an upstream response ever reports it.
		default:
			continue
		}
		ti.InputTokens += modality.Tokens
	}
	if ti.InputTokens == 0 {
		ti.InputTokens = interaction.Usage.TotalInputTokens
	}
	if ti.TextInputTokens == 0 && ti.InputTokens > ti.ImageInputTokens+ti.VideoInputTokens {
		// Preserve compatibility with responses that only return total input
		// tokens while still retaining the explicit image/video dimensions.
		ti.TextInputTokens = ti.InputTokens - ti.ImageInputTokens - ti.VideoInputTokens
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
