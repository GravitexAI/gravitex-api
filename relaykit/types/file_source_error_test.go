package types

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 请求转换阶段的附件加载失败是客户端错误：必须是 400、不重试、不归因到渠道，
// 并且带上内容块在请求里的位置，便于用户自查是哪一条消息的哪个附件有问题。
func TestNewFileSourceErrorIsClientErrorNotAttributedToChannel(t *testing.T) {
	source := NewBase64FileSource("data:image/png;base64", "")
	apiErr := NewFileSourceError(errors.New("failed to decode base64 data: illegal base64 data at input byte 4"),
		"messages[2](user).content[1]", "image_url", source)

	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, ErrorCodeInvalidFileSource, apiErr.GetErrorCode())
	assert.True(t, IsSkipRetryError(apiErr))
	assert.False(t, IsChannelAttributableError(apiErr))

	message := apiErr.Error()
	assert.Contains(t, message, "messages[2](user).content[1]")
	assert.Contains(t, message, "image_url")
	assert.Contains(t, message, "illegal base64 data at input byte 4")
}

// relay handler 会用 NewError(..., ErrorCodeConvertRequestFailed) 再包一层，
// 该包装必须保留内层的 400 与「不归因渠道」语义，否则又会退回成 500 且记到渠道头上。
func TestFileSourceErrorSurvivesConvertRequestWrapping(t *testing.T) {
	inner := NewFileSourceError(errors.New("download failed"), "input[0].content[0]", "input_image",
		NewURLFileSource("https://files.corp.internal/a/b.png"))

	wrapped := NewError(error(inner), ErrorCodeConvertRequestFailed, ErrOptionWithSkipRetry())

	assert.Equal(t, http.StatusBadRequest, wrapped.StatusCode)
	assert.Equal(t, ErrorCodeInvalidFileSource, wrapped.GetErrorCode())
	assert.False(t, IsChannelAttributableError(wrapped))
}

// 普通错误默认仍然归因到渠道，避免这次改动把所有错误都从渠道统计里摘出去。
func TestOrdinaryErrorStaysChannelAttributable(t *testing.T) {
	assert.True(t, IsChannelAttributableError(NewError(errors.New("boom"), ErrorCodeBadResponse)))
}

func TestDescribeFileSourceHidesRawContent(t *testing.T) {
	urlDesc := DescribeFileSource(NewURLFileSource("https://files.corp.internal/secret/path.png?token=abc"))
	assert.NotContains(t, urlDesc, "files.corp.internal")
	assert.NotContains(t, urlDesc, "secret")
	assert.NotContains(t, urlDesc, "abc")
	assert.Contains(t, urlDesc, "***")

	base64Desc := DescribeFileSource(NewBase64FileSource("0123456789012345678901234567890123456789", ""))
	assert.Equal(t, `inline data(len=40) starting with "012345678901234567890123..."`, base64Desc)
}
