package xai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func convertXaiImageRequest(t *testing.T, relayMode int, path string, body string) (string, *relaycommon.RelayInfo) {
	t.Helper()
	var req dto.ImageRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	info := &relaycommon.RelayInfo{RelayMode: relayMode, RequestURLPath: path}

	converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, req)
	require.NoError(t, err)
	out, err := common.Marshal(converted)
	require.NoError(t, err)
	return string(out), info
}

func TestXaiImageRequestPassesGrokImagineParams(t *testing.T) {
	out, info := convertXaiImageRequest(t, relayconstant.RelayModeImagesGenerations, "/v1/images/generations", `{
		"model":"grok-imagine-image-2.0","prompt":"cat","n":2,
		"aspect_ratio":"9:16","resolution":"2k","quality":"medium",
		"response_format":"b64_json","size":"1024x1024"
	}`)

	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"cat","n":2,
		"aspect_ratio":"9:16","resolution":"2k","quality":"medium",
		"response_format":"b64_json"
	}`, out)
	assert.Equal(t, "/v1/images/generations", info.RequestURLPath)
}

func TestXaiImageGenerationsWithReferenceImageRoutesToEdits(t *testing.T) {
	out, info := convertXaiImageRequest(t, relayconstant.RelayModeImagesGenerations, "/v1/images/generations", `{
		"model":"grok-imagine-image-2.0","prompt":"sketch",
		"image":{"url":"https://example.com/a.png","type":"image_url"}
	}`)

	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"sketch",
		"image":{"url":"https://example.com/a.png","type":"image_url"}
	}`, out)
	assert.Equal(t, "/v1/images/edits", info.RequestURLPath)
}

func TestXaiImageEditsKeepsMultipleImages(t *testing.T) {
	out, info := convertXaiImageRequest(t, relayconstant.RelayModeImagesEdits, "/v1/images/edits", `{
		"model":"grok-imagine-image-2.0","prompt":"merge","aspect_ratio":"1:1",
		"images":[{"url":"https://example.com/a.png","type":"image_url"},{"url":"data:image/png;base64,AAAA","type":"image_url"}]
	}`)

	assert.JSONEq(t, `{
		"model":"grok-imagine-image-2.0","prompt":"merge","aspect_ratio":"1:1",
		"images":[{"url":"https://example.com/a.png","type":"image_url"},{"url":"data:image/png;base64,AAAA","type":"image_url"}]
	}`, out)
	assert.Equal(t, "/v1/images/edits", info.RequestURLPath)
}
