package gemini

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/tidwall/gjson"
)

const omniModelName = relaycommon.GeminiOmniFlashPreview
const omniTaskIDPrefix = "omni:"
const interactionTaskIDPrefix = "interaction:"

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
	return relaycommon.IsGeminiOmniModel(name)
}

func markOmniTaskID(interactionID string) string {
	return omniTaskIDPrefix + interactionID
}

func unmarkOmniTaskID(taskID string) string {
	return strings.TrimPrefix(taskID, omniTaskIDPrefix)
}

func markInteractionTaskID(interactionID string) string {
	return interactionTaskIDPrefix + interactionID
}

func unmarkInteractionTaskID(taskID string) string {
	return strings.TrimPrefix(taskID, interactionTaskIDPrefix)
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
		videoInput = metadataString(metadata, "input_reference")
	}
	if videoInput == "" {
		videoInput = strings.TrimSpace(req.InputReference)
	}
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
	if isOmniImageInput(videoInput) {
		images = appendUniqueOmniInput(images, videoInput)
		videoInput = ""
	}
	task := omniTaskFromMetadata(metadata, images, videoInput)
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
	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		modelName = omniModelName
	}
	body := map[string]interface{}{
		"model": modelName,
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
	if strings.EqualFold(modelName, relaycommon.GeminiOmni11FlashPreview) {
		// The 1.1 model accepts resolution in response_format. Keep the
		// older model's upstream request shape unchanged.
		switch resolution := strings.ToLower(strings.TrimSpace(metadataString(metadata, "resolution"))); resolution {
		case "360p", "720p", "1080p", "4k":
			responseFormat["resolution"] = resolution
		}
	}
	// The Interactions API defines response_format as an array. Keep inline
	// delivery so the gateway does not require an OSS/GCS output location.
	body["response_format"] = []map[string]string{responseFormat}
	if previousID := metadataString(metadata, "previous_interaction_id"); previousID != "" {
		body["previous_interaction_id"] = previousID
	}
	return common.Marshal(body)
}

func isOmniImageInput(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(value, "data:image/") {
		return true
	}
	path := strings.TrimSuffix(strings.Split(value, "?")[0], "/")
	for _, suffix := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func appendUniqueOmniInput(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.TrimSpace(existing) == value {
			return values
		}
	}
	return append(values, value)
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
	usageJSON := gjson.GetBytes(respBody, "usage")
	usageAvailable := false
	if usageJSON.IsObject() {
		for _, key := range []string{"total_input_tokens", "total_output_tokens", "total_tokens", "total_thought_tokens", "input_tokens_by_modality", "output_tokens_by_modality"} {
			if usageJSON.Get(key).Exists() && usageJSON.Get(key).Type != gjson.Null {
				usageAvailable = true
				break
			}
		}
	}
	ti := &relaycommon.TaskInfo{TaskID: interaction.ID, UsageAvailable: usageAvailable, UsageFacts: map[string]any{}}
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
	if ti.TextOutputTokens == 0 && interaction.Usage.TotalThoughtTokens > 0 {
		ti.TextOutputTokens = interaction.Usage.TotalThoughtTokens
	}
	if ti.VideoOutputTokens == 0 && interaction.Usage.TotalOutputTokens > ti.TextOutputTokens {
		ti.VideoOutputTokens = interaction.Usage.TotalOutputTokens - ti.TextOutputTokens
	}
	ti.CompletionTokens = ti.VideoOutputTokens
	ti.TotalTokens = interaction.Usage.TotalTokens
	if ti.TotalTokens == 0 {
		ti.TotalTokens = ti.InputTokens + ti.VideoOutputTokens + ti.TextOutputTokens
	}
	if ti.UsageAvailable {
		ti.ThoughtTokens = interaction.Usage.TotalThoughtTokens
		ti.UsageFacts["input_tokens"] = ti.InputTokens
		ti.UsageFacts["output_tokens"] = ti.CompletionTokens
		ti.UsageFacts["total_tokens"] = ti.TotalTokens
		ti.UsageFacts["text_input_tokens"] = ti.TextInputTokens
		ti.UsageFacts["image_input_tokens"] = ti.ImageInputTokens
		ti.UsageFacts["video_input_tokens"] = ti.VideoInputTokens
		ti.UsageFacts["text_output_tokens"] = ti.TextOutputTokens
		ti.UsageFacts["video_output_tokens"] = ti.VideoOutputTokens
		ti.UsageFacts["thought_tokens"] = interaction.Usage.TotalThoughtTokens
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

// ParseNativeInteractionTaskResult normalizes usage from Gemini's native
// Interactions response without changing the legacy Omni task parser used by
// /v1/video/generations.
func ParseNativeInteractionTaskResult(respBody []byte, modelNames ...string) (*relaycommon.TaskInfo, error) {
	result, err := ParseOmniTaskResult(respBody)
	if err != nil {
		return nil, err
	}
	usage := gjson.GetBytes(respBody, "usage")
	if !usage.IsObject() {
		result.UsageAvailable = false
		return result, nil
	}
	inputModalities, inputBreakdownAvailable := interactionUsageModalities(usage.Get("input_tokens_by_modality"))
	outputModalities, outputBreakdownAvailable := interactionUsageModalities(usage.Get("output_tokens_by_modality"))
	totalInput, hasTotalInput := interactionUsageCount(usage.Get("total_input_tokens"))
	totalOutput, hasTotalOutput := interactionUsageCount(usage.Get("total_output_tokens"))
	totalTokens, hasTotalTokens := interactionUsageCount(usage.Get("total_tokens"))
	thoughtTokens, hasThoughtTokens := interactionUsageCount(usage.Get("total_thought_tokens"))
	if !hasThoughtTokens {
		thoughtTokens, hasThoughtTokens = outputModalities["thought"]
	}
	textInput, hasTextInput := interactionUsageCount(usage.Get("text_input_tokens"))
	imageInput, hasImageInput := interactionUsageCount(usage.Get("image_input_tokens"))
	videoInput, hasVideoInput := interactionUsageCount(usage.Get("video_input_tokens"))
	audioInput, hasAudioInput := interactionUsageCount(usage.Get("audio_input_tokens"))
	textOutput, hasTextOutput := interactionUsageCount(usage.Get("text_output_tokens"))
	imageOutput, hasImageOutput := interactionUsageCount(usage.Get("image_output_tokens"))
	videoOutput, hasVideoOutput := interactionUsageCount(usage.Get("video_output_tokens"))
	audioOutput, hasAudioOutput := interactionUsageCount(usage.Get("audio_output_tokens"))
	if !hasTextInput {
		textInput = inputModalities["text"]
	}
	if !hasImageInput {
		imageInput = inputModalities["image"]
	}
	if !hasVideoInput {
		videoInput = inputModalities["video"]
	}
	if !hasAudioInput {
		audioInput = inputModalities["audio"]
	}
	if !hasTextOutput {
		textOutput = outputModalities["text"]
	}
	if !hasImageOutput {
		imageOutput = outputModalities["image"]
	}
	if !hasVideoOutput {
		videoOutput = outputModalities["video"]
	}
	if !hasAudioOutput {
		audioOutput = outputModalities["audio"]
	}
	inputModalityTotal := textInput + imageInput + videoInput + audioInput
	if !hasTotalInput || (totalInput == 0 && inputModalityTotal > 0) {
		totalInput = inputModalityTotal
		hasTotalInput = true
	}
	if !hasTotalOutput && (hasTextOutput || hasImageOutput || hasVideoOutput || hasAudioOutput || outputBreakdownAvailable) {
		totalOutput = textOutput + imageOutput + videoOutput + audioOutput + thoughtTokens
		hasTotalOutput = true
	}
	outputModality, hasOnlyOutputModality := nativeInteractionOutputModality(respBody)
	hasScalarOutput := textOutput > 0 || imageOutput > 0 || videoOutput > 0 || audioOutput > 0
	canDeriveTextOutput := hasOnlyOutputModality && outputModality == "text" &&
		hasTotalInput && hasTotalTokens && !outputBreakdownAvailable && !hasScalarOutput && (!hasTotalOutput || totalOutput == 0)
	canDeriveImageOutput := hasOnlyOutputModality && outputModality == "image" &&
		hasTotalInput && hasTotalTokens && !outputBreakdownAvailable && !hasScalarOutput && (!hasTotalOutput || totalOutput == 0)
	canDeriveAudioOutput := hasOnlyOutputModality && outputModality == "audio" &&
		hasTotalInput && hasTotalTokens && !outputBreakdownAvailable && !hasScalarOutput && (!hasTotalOutput || totalOutput == 0)
	canDeriveVideoOutput := hasOnlyOutputModality && outputModality == "video" &&
		hasTotalInput && hasTotalTokens && !outputBreakdownAvailable && !hasScalarOutput && (!hasTotalOutput || totalOutput == 0)
	hasInputUsage := hasTotalInput || inputBreakdownAvailable
	hasOutputUsage := hasTotalOutput || outputBreakdownAvailable || hasThoughtTokens || canDeriveTextOutput || canDeriveImageOutput || canDeriveAudioOutput || canDeriveVideoOutput
	result.UsageAvailable = hasInputUsage && hasOutputUsage
	if !result.UsageAvailable {
		return result, nil
	}

	result.TextInputTokens = textInput
	result.ImageInputTokens = imageInput
	result.VideoInputTokens = videoInput
	result.AudioInputTokens = audioInput
	result.InputTokens = totalInput
	if !hasTotalInput {
		result.InputTokens = result.TextInputTokens + result.ImageInputTokens + result.VideoInputTokens + result.AudioInputTokens
	}
	result.TextOutputTokens = textOutput
	result.ImageOutputTokens = imageOutput
	result.VideoOutputTokens = videoOutput
	result.AudioOutputTokens = audioOutput
	result.ThoughtTokens = thoughtTokens
	result.CompletionTokens = totalOutput
	if !hasTotalOutput || (totalOutput == 0 && (hasTextOutput || hasImageOutput || hasVideoOutput || hasAudioOutput || outputBreakdownAvailable || canDeriveTextOutput || canDeriveImageOutput || canDeriveAudioOutput || canDeriveVideoOutput)) {
		result.CompletionTokens = result.TextOutputTokens + result.ImageOutputTokens + result.VideoOutputTokens + result.AudioOutputTokens + thoughtTokens
		if canDeriveTextOutput || canDeriveImageOutput || canDeriveAudioOutput || canDeriveVideoOutput {
			remaining := totalTokens - result.InputTokens - thoughtTokens
			if remaining < 0 {
				result.UsageAvailable = false
				return result, nil
			}
			result.CompletionTokens = remaining + thoughtTokens
			if canDeriveImageOutput {
				result.ImageOutputTokens = remaining
			} else if canDeriveAudioOutput {
				result.AudioOutputTokens = remaining
			} else if canDeriveVideoOutput {
				result.VideoOutputTokens = remaining
			} else {
				result.TextOutputTokens = remaining
			}
		}
	}
	if result.ThoughtTokens > result.CompletionTokens {
		result.ThoughtTokens = result.CompletionTokens
	}
	result.CompletionTokens -= result.ThoughtTokens
	if hasOnlyOutputModality && !outputBreakdownAvailable && !hasScalarOutput && result.CompletionTokens > 0 {
		// Match Gemini GenerateContent accounting: classify aggregate output by
		// the actual returned modality, not the model name, so mixed-capability
		// models receive the same billing and log treatment as their text peers.
		switch outputModality {
		case "text":
			result.TextOutputTokens = result.CompletionTokens
		case "image":
			result.ImageOutputTokens = result.CompletionTokens
		case "audio":
			result.AudioOutputTokens = result.CompletionTokens
		case "video":
			result.VideoOutputTokens = result.CompletionTokens
		}
	}
	result.TotalTokens = totalTokens
	minimumTotalTokens := result.InputTokens + result.CompletionTokens + result.ThoughtTokens
	if !hasTotalTokens || totalTokens < minimumTotalTokens {
		result.TotalTokens = minimumTotalTokens
	}
	if nativeInteractionHasImageOutput(respBody) && result.ImageOutputTokens == 0 {
		result.UsageAvailable = false
		result.UsageFacts = map[string]any{}
		return result, nil
	}
	if nativeInteractionHasAudioOutput(respBody) && result.AudioOutputTokens == 0 {
		result.UsageAvailable = false
		result.UsageFacts = map[string]any{}
		return result, nil
	}
	result.CachedTokens = 0
	if cached, ok := interactionUsageCount(usage.Get("cached_tokens")); ok {
		result.CachedTokens = cached
	}
	if result.CachedTokens > result.InputTokens {
		result.CachedTokens = result.InputTokens
	}
	result.UsageFacts = map[string]any{
		"input_tokens":  result.InputTokens,
		"output_tokens": result.CompletionTokens + result.ThoughtTokens,
		"total_tokens":  result.TotalTokens,
	}
	if hasThoughtTokens {
		result.UsageFacts["thought_tokens"] = result.ThoughtTokens
	}
	if result.TextInputTokens > 0 {
		result.UsageFacts["text_input_tokens"] = result.TextInputTokens
	}
	if result.ImageInputTokens > 0 {
		result.UsageFacts["image_input_tokens"] = result.ImageInputTokens
	}
	if result.VideoInputTokens > 0 {
		result.UsageFacts["video_input_tokens"] = result.VideoInputTokens
	}
	if result.AudioInputTokens > 0 {
		result.UsageFacts["audio_input_tokens"] = result.AudioInputTokens
	}
	if result.TextOutputTokens > 0 {
		result.UsageFacts["text_output_tokens"] = result.TextOutputTokens
	}
	if result.ImageOutputTokens > 0 {
		result.UsageFacts["image_output_tokens"] = result.ImageOutputTokens
	}
	if result.VideoOutputTokens > 0 {
		result.UsageFacts["video_output_tokens"] = result.VideoOutputTokens
	}
	if result.AudioOutputTokens > 0 {
		result.UsageFacts["audio_output_tokens"] = result.AudioOutputTokens
	}
	if result.CachedTokens > 0 {
		result.UsageFacts["cached_tokens"] = result.CachedTokens
	}
	return result, nil
}

func nativeInteractionOutputModality(respBody []byte) (string, bool) {
	var interaction struct {
		OutputText  string `json:"output_text"`
		OutputAudio *struct {
			Type string `json:"type"`
		} `json:"output_audio"`
		Outputs []struct {
			Type string `json:"type"`
		} `json:"outputs"`
		Steps []struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"steps"`
	}
	if common.Unmarshal(respBody, &interaction) != nil {
		return "", false
	}
	modalities := make([]string, 0, len(interaction.Outputs)+len(interaction.Steps))
	if strings.TrimSpace(interaction.OutputText) != "" {
		modalities = append(modalities, "text")
	}
	if interaction.OutputAudio != nil {
		modality := strings.ToLower(strings.TrimSpace(interaction.OutputAudio.Type))
		if modality == "" {
			modality = "audio"
		}
		modalities = append(modalities, modality)
	}
	for _, output := range interaction.Outputs {
		modalities = append(modalities, strings.ToLower(strings.TrimSpace(output.Type)))
	}
	for _, step := range interaction.Steps {
		for _, content := range step.Content {
			modalities = append(modalities, strings.ToLower(strings.TrimSpace(content.Type)))
		}
	}
	if len(modalities) == 0 {
		return "", false
	}
	modality := modalities[0]
	switch modality {
	case "text", "image", "audio", "video":
	default:
		return "", false
	}
	for _, current := range modalities[1:] {
		if current != modality {
			return "", false
		}
	}
	return modality, true
}

func nativeInteractionHasImageOutput(respBody []byte) bool {
	outputs := gjson.GetBytes(respBody, "outputs")
	hasImage := false
	outputs.ForEach(func(_, output gjson.Result) bool {
		if strings.EqualFold(strings.TrimSpace(output.Get("type").String()), "image") {
			hasImage = true
		}
		return true
	})
	gjson.GetBytes(respBody, "steps").ForEach(func(_, step gjson.Result) bool {
		step.Get("content").ForEach(func(_, content gjson.Result) bool {
			if strings.EqualFold(strings.TrimSpace(content.Get("type").String()), "image") {
				hasImage = true
			}
			return true
		})
		return true
	})
	return hasImage
}

func nativeInteractionHasAudioOutput(respBody []byte) bool {
	if gjson.GetBytes(respBody, "output_audio").Exists() {
		return true
	}
	hasAudio := false
	gjson.GetBytes(respBody, "outputs").ForEach(func(_, output gjson.Result) bool {
		if strings.EqualFold(strings.TrimSpace(output.Get("type").String()), "audio") {
			hasAudio = true
		}
		return true
	})
	gjson.GetBytes(respBody, "steps").ForEach(func(_, step gjson.Result) bool {
		step.Get("content").ForEach(func(_, content gjson.Result) bool {
			if strings.EqualFold(strings.TrimSpace(content.Get("type").String()), "audio") {
				hasAudio = true
			}
			return true
		})
		return true
	})
	return hasAudio
}

func interactionUsageCount(value gjson.Result) (int, bool) {
	if !value.Exists() || value.Type == gjson.Null {
		return 0, false
	}
	count, err := strconv.ParseInt(value.Raw, 10, 64)
	if err != nil || count < 0 || count > int64(^uint(0)>>1) {
		return 0, false
	}
	return int(count), true
}

func interactionUsageModalities(value gjson.Result) (map[string]int, bool) {
	modalities := make(map[string]int)
	if !value.IsArray() {
		return modalities, false
	}
	available := false
	value.ForEach(func(_, item gjson.Result) bool {
		count, valid := interactionUsageCount(item.Get("tokens"))
		if !valid {
			return true
		}
		modality := strings.ToLower(strings.TrimSpace(item.Get("modality").String()))
		modalities[modality] += count
		available = true
		return true
	})
	return modalities, available
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
