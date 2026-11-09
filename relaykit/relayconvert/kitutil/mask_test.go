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
