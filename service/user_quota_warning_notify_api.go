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
	"github.com/golang-jwt/jwt/v5"
)

type quotaWarningNotifyRequest struct {
	UserID                int    `json:"user_id"`
	NotifyType            string `json:"notify_type,omitempty"`
	RemainingQuota        int64  `json:"remaining_quota"`
	QuotaWarningThreshold int64  `json:"quota_warning_threshold"`
}

type quotaWarningNotifyResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func SendQuotaWarningNotifyToAPIEnd(userID int, notifyType string, remainingQuota int64, quotaWarningThreshold int64) error {
	api := strings.TrimRight(strings.TrimSpace(os.Getenv("GRAVITEX_API_END")), "/")
	if api == "" {
		return fmt.Errorf("GRAVITEX_API_END not set")
	}

	reqBody := quotaWarningNotifyRequest{
		UserID:                userID,
		NotifyType:            notifyType,
		RemainingQuota:        remainingQuota,
		QuotaWarningThreshold: quotaWarningThreshold,
	}

	jsonData, err := common.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal quota warning notify request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, api+"/api/user/quota-warning-notify/send", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("create quota warning notify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token, err := buildRuoYiBearerToken(userID); err != nil {
		return err
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := GetHttpClient()
	if client.Timeout == 0 {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call quota warning notify api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read quota warning notify response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("quota warning notify http %d: %s", resp.StatusCode, string(body))
	}

	if len(body) == 0 {
		return nil
	}

	var result quotaWarningNotifyResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse quota warning notify response: %w", err)
	}
	if result.Code != 200 {
		return fmt.Errorf("quota warning notify failed: %s", result.Msg)
	}

	return nil
}

func buildRuoYiBearerToken(userID int) (string, error) {
	if !common.RuoYiAuthEnabled {
		return "", nil
	}
	secret := strings.TrimSpace(common.RuoYiJWTSecret)
	if secret == "" {
		return "", fmt.Errorf("RUOYI_JWT_SECRET is empty")
	}

	username, err := model.GetUsernameById(userID, false)
	if err != nil {
		return "", fmt.Errorf("load username for user %d: %w", userID, err)
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"loginId":  fmt.Sprintf("sys_user:%d", userID),
		"userId":   userID,
		"userName": username,
		"username": username,
		"iat":      now.Unix(),
		"nbf":      now.Unix(),
		"exp":      now.Add(30 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign ruoyi jwt token: %w", err)
	}
	return tokenString, nil
}
