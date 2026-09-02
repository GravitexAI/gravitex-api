package controller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeChannelJSONPreservesUnmanagedFields(t *testing.T) {
	merged, err := mergeChannelJSON(`{"ownerUserId":"2095030120640552961","vertex_key_type":"json"}`, `{"vertex_key_type":"api_key","new_option":true}`)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(merged), &got))
	require.Equal(t, "2095030120640552961", got["ownerUserId"])
	require.Equal(t, "api_key", got["vertex_key_type"])
	require.Equal(t, true, got["new_option"])
}

func TestMergeChannelJSONKeepsExistingValueWhenIncomingJSONIsInvalid(t *testing.T) {
	merged, err := mergeChannelJSON(`{"ownerUserId":"2095030120640552961"}`, `{`)
	require.NoError(t, err)
	require.JSONEq(t, `{"ownerUserId":"2095030120640552961"}`, merged)
}

func TestMergeChannelJSONDeletesFieldsExplicitlySetToNull(t *testing.T) {
	merged, err := mergeChannelJSON(`{"ownerUserId":"2095030120640552961","remove_me":true}`, `{"remove_me":null}`)
	require.NoError(t, err)
	require.JSONEq(t, `{"ownerUserId":"2095030120640552961"}`, merged)
}
