package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

// TestByteplusAssetInfo_ErrorFieldShape guards against the regression where
// ByteplusAssetInfo.Error was previously typed as `string` even though the
// upstream returns an `object` ({"Code":"...","Message":"..."}) on Failed
// assets. The old type made common.Unmarshal fail, which in turn made
// ListAssets / GetAsset silently skip the entire status refresh — leaving
// Failed assets stuck in "pending" indefinitely.
func TestByteplusAssetInfo_ErrorFieldShape(t *testing.T) {
	cases := []struct {
		name       string
		payload    string
		wantErrSub string // empty means Error should be empty after unmarshal
	}{
		{
			name:       "failed with object error",
			payload:    `{"Id":"a-1","Status":"Failed","Error":{"Code":"InternalServiceError","Message":"multiple faces detected"}}`,
			wantErrSub: "multiple faces detected",
		},
		{
			name:       "active without error field",
			payload:    `{"Id":"a-2","Status":"Active","URL":"https://x"}`,
			wantErrSub: "",
		},
		{
			name:       "processing with explicit null error",
			payload:    `{"Id":"a-3","Status":"Processing","Error":null}`,
			wantErrSub: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var info ByteplusAssetInfo
			if err := common.Unmarshal([]byte(tc.payload), &info); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			got := string(info.Error)
			if tc.wantErrSub == "" {
				if got != "" && got != "null" {
					t.Fatalf("expected empty Error, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantErrSub) {
				t.Fatalf("expected Error to contain %q, got %q", tc.wantErrSub, got)
			}
		})
	}
}

// TestScopeRequestToChannelProject guards the invariant that the channel's
// configured project always wins over anything the caller supplied. An asset
// created under the wrong project is invisible to the channel's API key, and
// video generation then fails with "The specified asset ... is not found".
func TestScopeRequestToChannelProject(t *testing.T) {
	cases := []struct {
		name     string
		cfg      ByteplusAssetConfig
		body     map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name:     "injects project when caller omits it",
			cfg:      ByteplusAssetConfig{ProjectName: "gravitex"},
			body:     map[string]interface{}{"GroupId": "group-1", "URL": "https://cdn/a.png"},
			expected: map[string]interface{}{"GroupId": "group-1", "URL": "https://cdn/a.png", "ProjectName": "gravitex"},
		},
		{
			name:     "overrides client-supplied project",
			cfg:      ByteplusAssetConfig{ProjectName: "gravitex"},
			body:     map[string]interface{}{"Id": "asset-1", "ProjectName": "default"},
			expected: map[string]interface{}{"Id": "asset-1", "ProjectName": "gravitex"},
		},
		{
			name:     "leaves body untouched when channel has no project configured",
			cfg:      ByteplusAssetConfig{},
			body:     map[string]interface{}{"Id": "asset-1", "ProjectName": "client-project"},
			expected: map[string]interface{}{"Id": "asset-1", "ProjectName": "client-project"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := make(map[string]interface{}, len(tc.body))
			for k, v := range tc.body {
				original[k] = v
			}

			got := scopeRequestToChannelProject(tc.cfg, tc.body)

			assert.Equal(t, tc.expected, got)
			assert.Equal(t, original, tc.body, "caller's body must not be mutated")
		})
	}
}
