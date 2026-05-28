package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
