package seedancegateway

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeQueryResponseWrappedSuccess(t *testing.T) {
	respBody := []byte(`{
		"code": "success",
		"message": "",
		"data": {
			"status": "SUCCESS",
			"result_url": "https://example.com/result.mp4",
			"data": {
				"code": "success",
				"data": {
					"id": "cgt-20260708094649-mxfjc",
					"model": "doubao-seedance-2.0",
					"status": "succeeded",
					"content": {
						"video_url": "https://example.com/result.mp4"
					},
					"duration": 4,
					"resolution": "480p",
					"usage": {
						"completion_tokens": 40594,
						"total_tokens": 40594
					}
				},
				"message": ""
			}
		}
	}`)

	normalized, changed, err := normalizeQueryResponse(respBody)
	require.NoError(t, err)
	require.True(t, changed)

	var task responseTask
	require.NoError(t, common.Unmarshal(normalized, &task))
	assert.Equal(t, "succeeded", task.Status)
	assert.Equal(t, "https://example.com/result.mp4", task.Content.VideoURL)
	assert.Equal(t, 4, task.Duration)
	assert.Equal(t, "480p", task.Resolution)
	assert.Equal(t, 40594, task.Usage.CompletionTokens)
}

func TestConvertToOpenAIVideoUsesNormalizedData(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:    "task_demo",
		CreatedAt: 1783475210,
		UpdatedAt: 1783475364,
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		Properties: model.Properties{
			OriginModelName: "doubao-seedance-2.0",
		},
		Data: []byte(`{
			"id":"cgt-20260708094649-mxfjc",
			"model":"doubao-seedance-2.0",
			"status":"succeeded",
			"content":{"video_url":"https://example.com/result.mp4"},
			"duration":4,
			"resolution":"480p",
			"usage":{"completion_tokens":40594,"total_tokens":40594}
		}`),
	}

	payload, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(payload, &video))
	assert.Equal(t, dto.VideoStatusCompleted, video.Status)
	assert.Equal(t, "https://example.com/result.mp4", video.VideoURL)
	assert.Equal(t, "4", video.Seconds)
}
