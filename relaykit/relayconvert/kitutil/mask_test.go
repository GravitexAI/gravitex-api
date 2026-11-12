package kitutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskSensitiveInfo(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "masks url host and path", input: "dial https://api.openai.com/v1/responses failed", expected: "dial https://***.com/***/*** failed"},
		{name: "masks plain domain", input: "upstream api.openai.com refused", expected: "upstream ***.***.com refused"},
		{name: "masks plain domain with country code tld", input: "host foo.example.co.uk", expected: "host ***.***.co.uk"},
		{name: "masks ip address", input: "connect 10.0.0.12:443", expected: "connect ***.***.***.***:443"},
		{name: "keeps parameter path readable", input: `"param":"reasoning.effort"`, expected: `"param":"reasoning.effort"`},
		{name: "keeps nested parameter path readable", input: "Invalid value for text.format.schema", expected: "Invalid value for text.format.schema"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, MaskSensitiveInfo(tt.input))
		})
	}
}

// 官方 #275 新增的凭据与 IPv6 遮挡在 alpha「全部域名遮挡」策略下同样生效。
func TestMaskSensitiveInfoMasksCredentialsAndIPv6(t *testing.T) {
	assert.Equal(t, `invalid api_key:***`, MaskSensitiveInfo(`invalid api_key:AIzaSyAAAaUooTUni8AdaOkSRMda30n_Q4vrV70`))
	assert.Equal(t, `dial tcp [***]:443: i/o timeout`, MaskSensitiveInfo(`dial tcp [2a01:4f8::1]:443: i/o timeout`))
	assert.Equal(t, `Post "https://***.com/***/***?client_secret=***&grant_type=***": timeout`,
		MaskSensitiveInfo(`Post "https://aip.baidubce.com/oauth/token?grant_type=client_credentials&client_secret=s": timeout`, "https://custom.example"))
}
