package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func getGeminiVideoURL(channel *model.Channel, task *model.Task, key string) (string, error) {
	if channel == nil || task == nil || key == "" {
		return "", fmt.Errorf("missing Gemini video context")
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.GetChannelBaseURL(channel.Type)
	}
	return fmt.Sprintf("%s/v1beta/%s:download?alt=media", base, task.GetUpstreamTaskID()), nil
}

func getVertexVideoURL(channel *model.Channel, task *model.Task) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("missing Vertex video context")
	}
	return task.PrivateData.ResultURL, nil
}
