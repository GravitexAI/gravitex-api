package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseResolutionFromUpstreamBodyKeepsOtherModelsOnTopLevelResolution(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"new Omni nested resolution", `{"model":"gemini-omni-1.1-flash-preview","response_format":[{"type":"video","resolution":"4k"}]}`, "4k"},
		{"other model nested resolution", `{"model":"seedance-2-0-pro","response_format":[{"type":"video","resolution":"4k"}]}`, ""},
		{"legacy Omni nested resolution", `{"model":"gemini-omni-flash-preview","response_format":[{"type":"video","resolution":"1080p"}]}`, ""},
		{"other model top-level resolution", `{"model":"seedance-2-0-pro","resolution":"720p","response_format":[{"type":"video","resolution":"4k"}]}`, "720p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, parseResolutionFromUpstreamBody([]byte(tc.body)))
		})
	}
}
