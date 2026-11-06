package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// Go → Java 内部调用签名请求头（与 Java 侧校验约定一致，修改需两端同步）。
const (
	InternalSignTimestampHeader = "X-Gx-Timestamp"
	InternalSignSignatureHeader = "X-Gx-Signature"
)

// internalSignMissingSecretOnce 保证"密钥未配置"告警只打印一次，避免刷日志。
var internalSignMissingSecretOnce sync.Once

// SignInternalRequest 为 Go → Java 的内部调用加签。
//
// 签名约定：
//   - X-Gx-Timestamp：当前 Unix 秒级时间戳（十进制字符串）
//   - X-Gx-Signature：hex(HMAC-SHA256(key=RUOYI_JWT_SECRET, msg=timestamp + "." + body))，小写
//
// body 必须是实际发送的请求体原始字节（无请求体时传 nil）。
// 密钥为空时不加签名、只告警一次，不影响请求发送。
func SignInternalRequest(req *http.Request, body []byte) {
	if req == nil {
		return
	}
	// 与 RuoYi JWT 验签一致，直接使用原始密钥字节（不做 trim）
	secret := common.RuoYiJWTSecret
	if strings.TrimSpace(secret) == "" {
		internalSignMissingSecretOnce.Do(func() {
			common.SysError("RUOYI_JWT_SECRET is empty, Go→Java internal requests are sent without signature")
		})
		return
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req.Header.Set(InternalSignTimestampHeader, timestamp)
	req.Header.Set(InternalSignSignatureHeader, computeInternalSignature(secret, timestamp, body))
}

// computeInternalSignature 计算 hex(HMAC-SHA256(secret, timestamp + "." + body))。
func computeInternalSignature(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
