package gemini

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractGCSMediaPart 覆盖 gs:// Cloud Storage URI 的识别与 MIME 类型解析规则：
// image_url/video_url 命中 gs:// 前缀时才返回 ok=true，且优先使用客户端显式传入的
// mime_type，缺省时按 URI 扩展名猜测；非 gs:// 输入必须原样放行给旧的下载+base64 逻辑。
func TestExtractGCSMediaPart(t *testing.T) {
	cases := []struct {
		name         string
		part         dto.MediaContent
		wantURI      string
		wantMimeType string
		wantOK       bool
	}{
		{
			name: "video_url with gs:// and no explicit mime type guesses from extension",
			part: dto.MediaContent{
				Type:     dto.ContentTypeVideoUrl,
				VideoUrl: &dto.MessageVideoUrl{Url: "gs://my-bucket/videos/clip.mp4"},
			},
			wantURI:      "gs://my-bucket/videos/clip.mp4",
			wantMimeType: "video/mp4",
			wantOK:       true,
		},
		{
			name: "video_url with gs:// and explicit mime type takes precedence over extension",
			part: dto.MediaContent{
				Type:     dto.ContentTypeVideoUrl,
				VideoUrl: &dto.MessageVideoUrl{Url: "gs://my-bucket/videos/clip.mov", MimeType: "video/quicktime"},
			},
			wantURI:      "gs://my-bucket/videos/clip.mov",
			wantMimeType: "video/quicktime",
			wantOK:       true,
		},
		{
			name: "image_url with gs:// and no extension cannot guess mime type",
			part: dto.MediaContent{
				Type:     dto.ContentTypeImageURL,
				ImageUrl: map[string]any{"url": "gs://my-bucket/objects/no-extension"},
			},
			wantURI:      "gs://my-bucket/objects/no-extension",
			wantMimeType: "",
			wantOK:       true,
		},
		{
			name: "file with gs:// audio and explicit mime type",
			part: dto.MediaContent{
				Type: dto.ContentTypeFile,
				File: map[string]any{"file_data": "gs://my-bucket/audio/note.raw", "mime_type": "audio/wav"},
			},
			wantURI:      "gs://my-bucket/audio/note.raw",
			wantMimeType: "audio/wav",
			wantOK:       true,
		},
		{
			name: "file with gs:// and no mime type falls back to file_name extension",
			part: dto.MediaContent{
				Type: dto.ContentTypeFile,
				File: map[string]any{"file_data": "gs://my-bucket/objects/blob-id-123", "file_name": "report.pdf"},
			},
			wantURI:      "gs://my-bucket/objects/blob-id-123",
			wantMimeType: "application/pdf",
			wantOK:       true,
		},
		{
			name: "http(s) video_url is not treated as a GCS reference",
			part: dto.MediaContent{
				Type:     dto.ContentTypeVideoUrl,
				VideoUrl: &dto.MessageVideoUrl{Url: "https://example.com/clip.mp4"},
			},
			wantOK: false,
		},
		{
			name: "text part is not treated as a GCS reference",
			part: dto.MediaContent{
				Type: dto.ContentTypeText,
				Text: "gs://my-bucket/videos/clip.mp4",
			},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uri, mimeType, ok := extractGCSMediaPart(tc.part)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantURI, uri)
				assert.Equal(t, tc.wantMimeType, mimeType)
			}
		})
	}
}

// TestCovertOpenAI2GeminiPassesThroughGCSVideoAsFileData 确认 Vertex AI 渠道上，对话请求里的
// gs:// 视频引用会被转成 Gemini fileData part 直接透传 fileUri，而不是走下载+base64 的 inlineData
// 路径（gs:// 对象既无法用 http(s) 下载，视频体积也不适合内联成 base64）。
func TestCovertOpenAI2GeminiPassesThroughGCSVideoAsFileData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	msg := dto.Message{Role: "user"}
	msg.SetMediaContent([]dto.MediaContent{
		{Type: dto.ContentTypeText, Text: "describe this video"},
		{Type: dto.ContentTypeVideoUrl, VideoUrl: &dto.MessageVideoUrl{Url: "gs://my-bucket/videos/clip.mp4"}},
	})

	textRequest := dto.GeneralOpenAIRequest{
		Model:    "gemini-3-flash-preview",
		Messages: []dto.Message{msg},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
			ChannelType:       constant.ChannelTypeVertexAi,
		},
	}

	result, err := CovertOpenAI2Gemini(c, textRequest, info)
	require.NoError(t, err)

	geminiRequest, ok := result.(*dto.GeminiChatRequest)
	require.True(t, ok, "expected *dto.GeminiChatRequest, got %T", result)
	require.Len(t, geminiRequest.Contents, 1)

	var filePart *dto.GeminiPart
	for i := range geminiRequest.Contents[0].Parts {
		if geminiRequest.Contents[0].Parts[i].FileData != nil {
			filePart = &geminiRequest.Contents[0].Parts[i]
		}
		// 断言完全没有走 inlineData（即没有触发下载/base64 编码）。
		assert.Nil(t, geminiRequest.Contents[0].Parts[i].InlineData)
	}
	require.NotNil(t, filePart, "expected a fileData part for the gs:// video reference")
	assert.Equal(t, "gs://my-bucket/videos/clip.mp4", filePart.FileData.FileUri)
	assert.Equal(t, "video/mp4", filePart.FileData.MimeType)
}

// TestCovertOpenAI2GeminiRejectsGCSOnNonVertexChannel 确认原生 Gemini 渠道（Gemini 开发者版
// API）上传 gs:// 会被平台直接拒绝并给出可操作的错误信息，而不是把裸 gs:// 当 fileUri 发给上游——
// 该 API 不认裸 gs:// URI，必须先经 Files API 单独注册（未实现），直接透传只会拿到上游 400。
func TestCovertOpenAI2GeminiRejectsGCSOnNonVertexChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	msg := dto.Message{Role: "user"}
	msg.SetMediaContent([]dto.MediaContent{
		{Type: dto.ContentTypeVideoUrl, VideoUrl: &dto.MessageVideoUrl{Url: "gs://my-bucket/videos/clip.mp4"}},
	})

	textRequest := dto.GeneralOpenAIRequest{
		Model:    "gemini-3-flash-preview",
		Messages: []dto.Message{msg},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-flash-preview",
			ChannelType:       constant.ChannelTypeGemini,
		},
	}

	_, err := CovertOpenAI2Gemini(c, textRequest, info)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only supported on a Vertex AI channel")
}
