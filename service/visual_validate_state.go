package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// VisualValidateState is the payload embedded into the CallbackURL `state` parameter
// for BytePlus liveness verification. It binds a verification session to a specific
// gateway user/channel/group_name so the callback page can be invoked anonymously
// (it carries its own authority via the HMAC signature).
type VisualValidateState struct {
	UserId     int    `json:"u"`
	ChannelId  int    `json:"c"`
	GroupName  string `json:"n"`
	GroupDesc  string `json:"d,omitempty"`
	BytedToken string `json:"b"`
	IssuedAt   int64  `json:"i"`
	ExpireAt   int64  `json:"e"`
}

// VisualValidateStateTTL is the validity window of a signed state token.
// BytePlus's BytedToken itself only lasts 120 seconds, but we keep the wrapping
// state alive for 15 minutes to allow for slow user verification + retries.
const VisualValidateStateTTL = 15 * time.Minute

// SignVisualValidateState serializes and HMAC-signs the state. The result is
// safe to embed in a URL query parameter (URL-safe base64 + dot separator).
func SignVisualValidateState(s *VisualValidateState) (string, error) {
	if s == nil {
		return "", fmt.Errorf("nil state")
	}
	if s.IssuedAt == 0 {
		s.IssuedAt = time.Now().Unix()
	}
	if s.ExpireAt == 0 {
		s.ExpireAt = s.IssuedAt + int64(VisualValidateStateTTL.Seconds())
	}
	payload, err := common.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("marshal state: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(common.CryptoSecret))
	mac.Write([]byte(body))
	sig := hex.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

// VerifyVisualValidateState verifies signature + expiry and returns the decoded state.
func VerifyVisualValidateState(token string) (*VisualValidateState, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid state format")
	}
	body, sig := parts[0], parts[1]
	mac := hmac.New(sha256.New, []byte(common.CryptoSecret))
	mac.Write([]byte(body))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return nil, fmt.Errorf("state signature mismatch")
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("decode state body: %w", err)
	}
	var s VisualValidateState
	if err := common.Unmarshal(payload, &s); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	if time.Now().Unix() > s.ExpireAt {
		return nil, fmt.Errorf("state expired")
	}
	return &s, nil
}
