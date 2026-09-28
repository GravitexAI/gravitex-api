package ali

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestTaskAdaptorBuildRequestURLSupportsAliBasePath(t *testing.T) {
	for _, test := range []struct {
		name string
		base string
		want string
	}{
		{name: "root base", base: "https://api.gravitex.ai", want: "https://api.gravitex.ai/api/v1/services/aigc/video-generation/video-synthesis"},
		{name: "ali prefixed base", base: "https://api.gravitex.ai/ali", want: "https://api.gravitex.ai/ali/api/v1/services/aigc/video-generation/video-synthesis"},
	} {
		t.Run(test.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: test.base}})
			got, err := adaptor.BuildRequestURL(nil)
			assert.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestProcessAliOtherRatiosSupportsHappyHorseVersionedModel(t *testing.T) {
	ratios, err := ProcessAliOtherRatios(&AliVideoRequest{
		Model: "happyhorse-1.1-t2v-2026-01-01",
		Parameters: &AliVideoParameters{
			Resolution: "1080P",
		},
	})

	assert.NoError(t, err)
	assert.InDelta(t, 4.0/3.0, ratios["resolution-1080P"], 0.000001)
}

func TestAliVideoParametersPreserveExplicitFalseValues(t *testing.T) {
	watermark := false
	promptExtend := false
	payload := AliVideoRequest{
		Model: "happyhorse-1.1-t2v",
		Parameters: &AliVideoParameters{
			Watermark:    &watermark,
			PromptExtend: &promptExtend,
		},
	}
	body, err := common.Marshal(payload)
	assert.NoError(t, err)
	assert.Contains(t, string(body), `"watermark":false`)
	assert.Contains(t, string(body), `"prompt_extend":false`)
	assert.False(t, strings.Contains(string(body), `"watermark":true`))
}

func TestHappyHorseTextToVideoSetsTaskAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/videos", bytes.NewBufferString(`{"model":"happyhorse-1.1-t2v","prompt":"a horse running"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	assert.Nil(t, taskErr)
	assert.Equal(t, constant.TaskActionTextGenerate, info.Action)
}

func TestBillingResolutionKeyFromParams_4k(t *testing.T) {
	// Veo upstream request body sets parameters.resolution = "4k" (no trailing "p"),
	// this must normalize to "4k" so it matches the "4k" price tier key, not "4kp".
	key := BillingResolutionKeyFromParams(&AliVideoParameters{Resolution: "4k"})
	assert.Equal(t, "4k", key)
}

func TestBillingResolutionKeyFromParams_720P(t *testing.T) {
	key := BillingResolutionKeyFromParams(&AliVideoParameters{Resolution: "720P"})
	assert.Equal(t, "720p", key)
}
