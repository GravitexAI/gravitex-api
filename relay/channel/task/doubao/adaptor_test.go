package doubao

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeRawMirrorBillingFields_TextOnly(t *testing.T) {
	raw := []byte(`{"model":"seedance-2-0","content":[{"type":"text","text":"a cat"}],"resolution":"720p"}`)
	hasVideo, resolution, err := probeRawMirrorBillingFields(raw)
	require.NoError(t, err)
	assert.False(t, hasVideo)
	assert.Equal(t, "720p", resolution)
}

func TestProbeRawMirrorBillingFields_WithVideoInput(t *testing.T) {
	raw := []byte(`{
		"model":"seedance-2-0",
		"content":[
			{"type":"text","text":"dance like this"},
			{"type":"video_url","video_url":{"url":"https://example.com/ref.mp4"},"role":"reference_video"}
		]
	}`)
	hasVideo, resolution, err := probeRawMirrorBillingFields(raw)
	require.NoError(t, err)
	assert.True(t, hasVideo)
	assert.Equal(t, "", resolution)
}

func TestProbeRawMirrorBillingFields_UnknownTopLevelFieldsPreserved(t *testing.T) {
	// execution_expires_after/service_tier/safety_identifier are official fields
	// TaskSubmitReq would silently drop; probing must not choke on them.
	raw := []byte(`{
		"model":"seedance-2-0",
		"content":[{"type":"text","text":"x"}],
		"execution_expires_after": 172800,
		"service_tier": "default",
		"safety_identifier": "user-hash-abc"
	}`)
	hasVideo, resolution, err := probeRawMirrorBillingFields(raw)
	require.NoError(t, err)
	assert.False(t, hasVideo)
	assert.Equal(t, "", resolution)
}

func TestExtractAssetVirtualIdsFromRaw(t *testing.T) {
	raw := []byte(`{
		"content":[
			{"type":"text","text":"x"},
			{"type":"image_url","image_url":{"url":"asset://asset-img-1"},"role":"reference_image"},
			{"type":"video_url","video_url":{"url":"asset://asset-vid-1"},"role":"reference_video"},
			{"type":"audio_url","audio_url":{"url":"https://example.com/plain.mp3"},"role":"reference_audio"}
		]
	}`)
	ids := extractAssetVirtualIdsFromRaw(raw)
	assert.ElementsMatch(t, []string{"asset-img-1", "asset-vid-1"}, ids)
}

func TestExtractAssetVirtualIdsFromRaw_NoAssets(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"plain text only"}]}`)
	ids := extractAssetVirtualIdsFromRaw(raw)
	assert.Empty(t, ids)
}
