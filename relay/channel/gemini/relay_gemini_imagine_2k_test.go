package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 实测：上游模型在 TEXT+IMAGE 双模态下忽略 imageSize（返回 1K），
// 纯 IMAGE 模态才产出 2K。请求 2K 时必须切换模态。
func TestConvertImagineImageRequestSwitchesToImageOnlyModalityFor2K(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	req, err := convertImagineImageRequest(ctx, &relaycommon.RelayInfo{}, dto.ImageRequest{
		Model:   "gemini-nano-banana-2.1",
		Prompt:  "draw a sheep",
		Size:    "16:9",
		Quality: "2K",
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"IMAGE"}, req.GenerationConfig.ResponseModalities)
	var imageConfig map[string]string
	require.NoError(t, common.Unmarshal(req.GenerationConfig.ImageConfig, &imageConfig))
	assert.Equal(t, "16:9", imageConfig["aspectRatio"])
	assert.Equal(t, "2K", imageConfig["imageSize"])
}

func TestConvertImagineImageRequestKeepsTextModalityWithout2K(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	req, err := convertImagineImageRequest(ctx, &relaycommon.RelayInfo{}, dto.ImageRequest{
		Model:  "gemini-nano-banana-2.1",
		Prompt: "draw a sheep",
		Size:   "16:9",
	})
	require.NoError(t, err)

	// 未请求 2K：保持 TEXT+IMAGE（revised_prompt 仍可用），默认 1K 输出。
	assert.Equal(t, []string{"TEXT", "IMAGE"}, req.GenerationConfig.ResponseModalities)

	qualityReq, err := convertImagineImageRequest(ctx, &relaycommon.RelayInfo{}, dto.ImageRequest{
		Model:   "gemini-nano-banana-2.1",
		Prompt:  "draw a sheep",
		Quality: "standard",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"TEXT", "IMAGE"}, qualityReq.GenerationConfig.ResponseModalities)
}
