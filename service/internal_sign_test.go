package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignInternalRequest_SignsTimestampDotBody(t *testing.T) {
	original := common.RuoYiJWTSecret
	common.RuoYiJWTSecret = "test-secret"
	t.Cleanup(func() { common.RuoYiJWTSecret = original })

	body := []byte(`{"user_id":1}`)
	req, err := http.NewRequest(http.MethodPost, "http://java.local/api/x", nil)
	require.NoError(t, err)

	before := time.Now().Unix()
	SignInternalRequest(req, body)
	after := time.Now().Unix()

	ts := req.Header.Get(InternalSignTimestampHeader)
	require.NotEmpty(t, ts)
	tsVal, err := strconv.ParseInt(ts, 10, 64)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, tsVal, before)
	assert.LessOrEqual(t, tsVal, after)

	// 独立按约定重新计算，确保与 Java 侧算法一致
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(ts + "." + string(body)))
	assert.Equal(t, hex.EncodeToString(mac.Sum(nil)), req.Header.Get(InternalSignSignatureHeader))
}

func TestComputeInternalSignature_KnownVector(t *testing.T) {
	// 固定向量（openssl dgst -sha256 -hmac secret 计算），Java 侧可用同一向量做对拍
	assert.Equal(t, "b8569b78799ff9e3cbff0fc2d63a33a2b57f3282abd07c37ae5e8e7d79a5f163",
		computeInternalSignature("secret", "1700000000", []byte("{}")))
	// 空 body 时消息为 "timestamp."
	assert.Equal(t, "4bc5f74d868b97888288889c5d9d65df02526f94c1592a79fdf4fe8b26e311e5",
		computeInternalSignature("secret", "1700000000", nil))
}

func TestSignInternalRequest_EmptySecretSkipsSignature(t *testing.T) {
	original := common.RuoYiJWTSecret
	common.RuoYiJWTSecret = ""
	t.Cleanup(func() { common.RuoYiJWTSecret = original })

	req, err := http.NewRequest(http.MethodPost, "http://java.local/api/x", nil)
	require.NoError(t, err)
	SignInternalRequest(req, []byte("{}"))

	assert.Empty(t, req.Header.Get(InternalSignTimestampHeader))
	assert.Empty(t, req.Header.Get(InternalSignSignatureHeader))
}
