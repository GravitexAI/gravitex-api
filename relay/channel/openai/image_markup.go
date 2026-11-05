package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

func invalidImageMarkup(message string) error {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// readImageMarkup resolves a JSON markup reference into file bytes for the
// multipart upstream request. Image format, dimensions and transparency are
// left to the upstream; only what is needed to fetch the bytes safely is checked.
func readImageMarkup(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var ref imageRef
	if err := common.Unmarshal(raw, &ref); err != nil {
		return nil, invalidImageMarkup("markup must be an object with image_url or file_id")
	}
	imageURL := strings.TrimSpace(ref.ImageURL)
	if imageURL == "" {
		if ref.FileID != "" {
			return nil, invalidImageMarkup("file_id markup references are not supported through this gateway; use image_url or a base64 data URL")
		}
		return nil, invalidImageMarkup("markup requires image_url")
	}

	var data []byte
	if strings.HasPrefix(imageURL, "data:") {
		header, payload, ok := strings.Cut(imageURL, ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, invalidImageMarkup("markup data URL must be base64 encoded")
		}
		if len(payload) > base64.StdEncoding.EncodedLen(maxImageSize) {
			return nil, invalidImageMarkup("markup exceeds the 50 MB file limit")
		}
		var err error
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(payload)
		}
		if err != nil {
			return nil, invalidImageMarkup("invalid markup base64")
		}
		return data, nil
	}

	parsed, err := url.Parse(imageURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, invalidImageMarkup("markup image_url must be HTTPS or a base64 data URL")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, invalidImageMarkup("invalid markup URL")
	}
	client := *service.GetSSRFProtectedHTTPClient()
	checkRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("markup redirects must use HTTPS")
		}
		if checkRedirect != nil {
			return checkRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("too many markup redirects")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, invalidImageMarkup("failed to download markup image")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, invalidImageMarkup("failed to download markup image: non-200 status")
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return nil, invalidImageMarkup("failed to read markup image")
	}
	if len(data) > maxImageSize {
		return nil, invalidImageMarkup("markup exceeds the 50 MB file limit")
	}
	return data, nil
}
