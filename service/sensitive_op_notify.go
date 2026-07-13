package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// sensitiveOpNotifyTimeout 是敏感操作告警回调 Java 后端的硬超时时间。
// 告警属于旁路通知，不应拖慢主流程，超时后直接放弃。
const sensitiveOpNotifyTimeout = 10 * time.Second

type sensitiveOpAlertRequest struct {
	UserId  int    `json:"user_id"`
	Scene   string `json:"scene"`
	Subject string `json:"subject"`
	Content string `json:"content"`
}

// NotifySensitiveOp 在企业子账号执行敏感操作（新增 API 密钥、修改 IP 白名单等）后，
// best-effort 通知 Java 后端向该企业的告警收件人发送邮件。
//
// 仅当该用户是已开启 sensitiveOpAlertEnabled 的企业子账号时才会真正发出请求；
// 非企业用户、主账号、未开启该开关的企业，直接跳过。
//
// 所有失败路径（未配置 GRAVITEX_API_END、HTTP 错误、非 2xx 响应）均只记录日志，
// 绝不向调用方返回错误，调用方应在独立 goroutine 中调用本函数，
// 确保令牌创建/修改操作永不因告警失败而受影响。
func NotifySensitiveOp(userId int, scene string, subject string, htmlContent string) {
	_, enabled, err := model.GetSubAccountSensitiveOpAlert(userId)
	if err != nil {
		common.SysError("check sensitive op alert setting failed: " + err.Error())
		return
	}
	if !enabled {
		return
	}

	api := strings.TrimRight(strings.TrimSpace(os.Getenv("GRAVITEX_API_END")), "/")
	if api == "" {
		common.SysLog("GRAVITEX_API_END not set, skip sensitive op alert")
		return
	}

	reqBody := sensitiveOpAlertRequest{
		UserId:  userId,
		Scene:   scene,
		Subject: subject,
		Content: htmlContent,
	}
	jsonData, err := common.Marshal(reqBody)
	if err != nil {
		common.SysError("marshal sensitive op alert request failed: " + err.Error())
		return
	}

	req, err := http.NewRequest(http.MethodPost, api+"/api/enterprise/sensitive-op-alert", bytes.NewBuffer(jsonData))
	if err != nil {
		common.SysError("build sensitive op alert request failed: " + err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: sensitiveOpNotifyTimeout}
	resp, err := client.Do(req)
	if err != nil {
		common.SysError("call sensitive op alert api failed: " + err.Error())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysError("read sensitive op alert response failed: " + err.Error())
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		common.SysError(fmt.Sprintf("sensitive op alert http %d: %s", resp.StatusCode, string(body)))
	}
}
