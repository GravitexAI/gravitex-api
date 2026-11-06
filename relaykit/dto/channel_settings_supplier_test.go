package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelOtherSettingsIsSupplierChannel(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`{"ownerUserId":"2095030120640552961"}`, true}, // Java 写入的字符串雪花 ID
		{`{"ownerUserId":12345}`, true},
		{`{"ownerUserId":null}`, false},
		{`{"ownerUserId":""}`, false},
		{`{"vertex_key_type":"json"}`, false},
	}
	for _, tc := range cases {
		var s ChannelOtherSettings
		require.NoError(t, json.Unmarshal([]byte(tc.raw), &s), tc.raw)
		assert.Equal(t, tc.want, s.IsSupplierChannel(), tc.raw)
	}

	// 往返序列化保持 ownerUserId 原样（不能被改成数字或丢失）
	var s ChannelOtherSettings
	require.NoError(t, json.Unmarshal([]byte(`{"ownerUserId":"2095030120640552961"}`), &s))
	out, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"ownerUserId":"2095030120640552961"`)
}
