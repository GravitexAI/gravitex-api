package helper

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestGetAndValidOpenAIImageRequestMultipartStream verifies multipart image
// edit parsing: the stream field is parsed and validated, and the request body
// stays replayable for the upstream request.
func TestGetAndValidOpenAIImageRequestMultipartStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, streamValue string, withImage bool) (*gin.Context, string) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		require.NoError(t, writer.WriteField("stream", streamValue))
		if withImage {
			part, err := writer.CreateFormFile("image", "input.png")
			require.NoError(t, err)
			_, err = part.Write([]byte("fake image"))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		originalBody := body.String()

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c, originalBody
	}

	t.Run("valid stream value keeps body replayable", func(t *testing.T) {
		c, originalBody := newContext(t, "true", true)

		req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.NoError(t, err)
		require.NotNil(t, req.Stream)
		require.True(t, *req.Stream)
		require.True(t, req.IsStream(c.Request))

		bodyAfterValidation, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, originalBody, string(bodyAfterValidation))

		form, err := common.ParseMultipartFormReusable(c)
		require.NoError(t, err)
		require.Equal(t, "true", url.Values(form.Value).Get("stream"))
		require.Len(t, form.File["image"], 1)
	})

	t.Run("invalid stream value is rejected", func(t *testing.T) {
		c, _ := newContext(t, "notabool", false)

		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid stream value")
	})
}

// TestGetAndValidOpenAIImageRequestNBounds guards the billing invariant that
// the image generation count can never reach quota calculation with a value
// large enough to overflow int64 into a negative charge.
func TestGetAndValidOpenAIImageRequestNBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newJSONContext := func(t *testing.T, body string) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	boundErr := fmt.Sprintf("n must be an integer between 1 and %d", dto.MaxImageN)

	tests := []struct {
		name     string
		body     string
		wantErr  string
		wantN    uint
		wantNilN bool
	}{
		{
			name:    "overflowed uint64 n is rejected",
			body:    `{"model":"gpt-image-1","prompt":"a cat","n":18446744073686646784}`,
			wantErr: boundErr,
		},
		{
			name:    "n above max is rejected",
			body:    fmt.Sprintf(`{"model":"gpt-image-1","prompt":"a cat","n":%d}`, dto.MaxImageN+1),
			wantErr: boundErr,
		},
		{
			name:  "n at max is accepted",
			body:  fmt.Sprintf(`{"model":"gpt-image-1","prompt":"a cat","n":%d}`, dto.MaxImageN),
			wantN: dto.MaxImageN,
		},
		{
			name:  "explicit n is accepted",
			body:  `{"model":"gpt-image-1","prompt":"a cat","n":3}`,
			wantN: 3,
		},
		{
			// n 原样透传给上游（不再被改写成 1），但计费张数仍必须兜底为 1。
			name:  "zero n is passed through and billed as 1",
			body:  `{"model":"gpt-image-1","prompt":"a cat","n":0}`,
			wantN: 0,
		},
		{
			name:     "absent n stays absent and is billed as 1",
			body:     `{"model":"gpt-image-1","prompt":"a cat"}`,
			wantNilN: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newJSONContext(t, tt.body)
			req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantNilN {
				require.Nil(t, req.N, "用户没传 n 时网关不得补默认值")
			} else {
				require.NotNil(t, req.N)
				require.Equal(t, tt.wantN, *req.N)
			}
			// 计费张数与透传值解耦：n 缺失或为 0 时，计费乘数必须兜底为 1。
			wantBillingN := float64(tt.wantN)
			if tt.wantNilN || tt.wantN == 0 {
				wantBillingN = 1
			}
			require.Equal(t, wantBillingN, req.GetTokenCountMeta().BillingRatios["n"])
		})
	}

	t.Run("negative multipart n is rejected", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "gpt-image-1"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		require.NoError(t, writer.WriteField("n", "-22904832"))
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())

		_, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
		require.Error(t, err)
		require.Contains(t, err.Error(), boundErr)
	})
}

// TestGetAndValidOpenAIImageRequestMultipartImageValues 锁定 multipart 图生图的多图约定：
// image 可重复出现，也可写成 image[] / image[N]，多张必须完整保留成数组而不是只取第一张。
func TestGetAndValidOpenAIImageRequestMultipartImageValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func(t *testing.T, fields [][2]string) *gin.Context {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("model", "doubao-seedream-4-0-250828"))
		require.NoError(t, writer.WriteField("prompt", "edit this image"))
		for _, field := range fields {
			require.NoError(t, writer.WriteField(field[0], field[1]))
		}
		require.NoError(t, writer.Close())

		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return c
	}

	tests := []struct {
		name      string
		fields    [][2]string
		wantImage string
		wantCount int
	}{
		{
			name:      "single image stays a string",
			fields:    [][2]string{{"image", "https://example.com/1.png"}},
			wantImage: `"https://example.com/1.png"`,
			wantCount: 1,
		},
		{
			name: "repeated image fields become an array",
			fields: [][2]string{
				{"image", "https://example.com/1.png"},
				{"image", "https://example.com/2.png"},
			},
			wantImage: `["https://example.com/1.png","https://example.com/2.png"]`,
			wantCount: 2,
		},
		{
			name: "image[] fields become an array",
			fields: [][2]string{
				{"image[]", "https://example.com/1.png"},
				{"image[]", "https://example.com/2.png"},
			},
			wantImage: `["https://example.com/1.png","https://example.com/2.png"]`,
			wantCount: 2,
		},
		{
			name: "indexed image[N] fields keep index order",
			fields: [][2]string{
				{"image[1]", "https://example.com/2.png"},
				{"image[0]", "https://example.com/1.png"},
			},
			wantImage: `["https://example.com/1.png","https://example.com/2.png"]`,
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := GetAndValidOpenAIImageRequest(newContext(t, tt.fields), relayconstant.RelayModeImagesEdits)
			require.NoError(t, err)
			require.JSONEq(t, tt.wantImage, string(req.Image))
			// 输入图数量直接决定按张计费里的输入图费用，必须跟着一起对上。
			require.Equal(t, tt.wantCount, CountImageInputs(req))
		})
	}
}

// TestGetAndValidOpenAIImageRequestMultipartExtraParams 锁定 form-data 与 JSON 的参数能力
// 一致：response_format 解析成已知字段，渠道专有参数进 Extra 并保留原始 JSON 类型，
// 已显式解析的字段不得重复出现在 Extra 里（否则会覆盖模型重定向后的 model 等字段）。
func TestGetAndValidOpenAIImageRequestMultipartExtraParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "doubao-seedream-4-0-250828"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	require.NoError(t, writer.WriteField("size", "2048x2048"))
	require.NoError(t, writer.WriteField("response_format", "b64_json"))
	require.NoError(t, writer.WriteField("seed", "42"))
	require.NoError(t, writer.WriteField("sequential_image_generation", "auto"))
	require.NoError(t, writer.WriteField("sequential_image_generation_options", `{"max_images":4}`))
	require.NoError(t, writer.WriteField("disable_safety_checker", "true"))
	require.NoError(t, writer.WriteField("image[]", "https://example.com/1.png"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)
	require.Equal(t, "b64_json", req.ResponseFormat)

	// 表单只能传字符串，但上游要的是原始类型，数字/布尔/对象必须还原。
	require.JSONEq(t, `42`, string(req.Extra["seed"]))
	require.JSONEq(t, `"auto"`, string(req.Extra["sequential_image_generation"]))
	require.JSONEq(t, `{"max_images":4}`, string(req.Extra["sequential_image_generation_options"]))
	require.JSONEq(t, `true`, string(req.Extra["disable_safety_checker"]))

	for _, key := range []string{"model", "prompt", "size", "response_format", "image", "image[]"} {
		require.NotContains(t, req.Extra, key, "已显式解析的字段不应再进 Extra")
	}
}

// TestGetAndValidOpenAIImageRequestMultipartBracketSyntax 锁定 OpenAI SDK 的 multipart
// 序列化约定：SDK 会把嵌套对象/数组展平成 querystring 括号语法，网关必须还原回嵌套结构，
// 否则上游会收到 "sequential_image_generation_options[max_images]" 这种字面量字段名。
func TestGetAndValidOpenAIImageRequestMultipartBracketSyntax(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "doubao-seedream-4-0-250828"))
	require.NoError(t, writer.WriteField("prompt", "edit this image"))
	// openai-python 对 extra_body={"sequential_image_generation_options": {"max_images": 4}}
	// 实际发出的就是下面这个字段名。
	require.NoError(t, writer.WriteField("sequential_image_generation_options[max_images]", "4"))
	require.NoError(t, writer.WriteField("nested[a][b]", "deep"))
	require.NoError(t, writer.WriteField("tags[]", "x"))
	require.NoError(t, writer.WriteField("tags[]", "y"))
	require.NoError(t, writer.WriteField("broken[unclosed", "raw"))
	part, err := writer.CreateFormFile("image[]", "a.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
	require.NoError(t, err)

	require.JSONEq(t, `{"max_images":4}`, string(req.Extra["sequential_image_generation_options"]))
	require.JSONEq(t, `{"a":{"b":"deep"}}`, string(req.Extra["nested"]))
	require.JSONEq(t, `["x","y"]`, string(req.Extra["tags"]))
	// 非法括号语法保持字面量，不能静默丢弃。
	require.JSONEq(t, `"raw"`, string(req.Extra["broken[unclosed"]))
	// image[] 是参考图，不能混进 Extra。
	require.NotContains(t, req.Extra, "image")
}

// TestGetAndValidOpenAIImageRequestKeepsGptImageQuality 锁定 gpt-image 的 quality 原样透传：
// 旧白名单只认 low/medium/high，会把 gpt-image-2.5 专属的 xhigh/max 静默改写成 medium，
// 用户按 max 付费却拿到低档图。取值合法性由上游判断，网关不做二次校验。
func TestGetAndValidOpenAIImageRequestKeepsGptImageQuality(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, quality := range []string{"xhigh", "max", "auto"} {
		t.Run("multipart/"+quality, func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "gpt-image-2.5-flare"))
			require.NoError(t, writer.WriteField("prompt", "edit this image"))
			require.NoError(t, writer.WriteField("quality", quality))
			require.NoError(t, writer.WriteField("image", "https://example.com/1.png"))
			require.NoError(t, writer.Close())

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())

			req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesEdits)
			require.NoError(t, err)
			require.Equal(t, quality, req.Quality)
		})

		t.Run("json/"+quality, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations",
				bytes.NewBufferString(`{"model":"gpt-image-2.5-sunburst","prompt":"a cat","quality":"`+quality+`"}`))
			c.Request.Header.Set("Content-Type", "application/json")

			req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
			require.NoError(t, err)
			require.Equal(t, quality, req.Quality)
		})
	}
}

// TestGetAndValidOpenAIImageRequestKeepsCustomSize 锁定自定义尺寸不被网关拦下：
// gpt-image-2 / 2.5 支持任意 16 倍数分辨率（最高 3840x2160），dall-e 的固定尺寸白名单
// 也已交给上游，网关只在用户没传时补默认值。
func TestGetAndValidOpenAIImageRequestKeepsCustomSize(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct{ model, size string }{
		{"gpt-image-2.5-flare", "2160x3840"},
		{"gpt-image-2.5-sunburst", "1536x864"},
		{"dall-e-2", "1024x1536"},
	}
	for _, tc := range cases {
		t.Run(tc.model+"/"+tc.size, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations",
				bytes.NewBufferString(`{"model":"`+tc.model+`","prompt":"a cat","size":"`+tc.size+`"}`))
			c.Request.Header.Set("Content-Type", "application/json")

			req, err := GetAndValidOpenAIImageRequest(c, relayconstant.RelayModeImagesGenerations)
			require.NoError(t, err)
			require.Equal(t, tc.size, req.Size)
		})
	}
}
