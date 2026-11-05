package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
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

// validateImageMarkup checks the PNG container without decoding all pixels.
// Orientation, transparency and alignment with the first input remain upstream checks.
func validateImageMarkup(data []byte) error {
	if len(data) > maxImageSize {
		return invalidImageMarkup("markup exceeds the 50 MB file limit")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return invalidImageMarkup("markup must be a PNG image")
	}
	return nil
}

func readImageMarkup(ctx context.Context, raw json.RawMessage) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, invalidImageMarkup("markup must be an image reference object")
	}
	var ref imageRef
	if err := common.Unmarshal(raw, &ref); err != nil {
		return nil, invalidImageMarkup("markup references must be strings")
	}
	_, hasURL := fields["image_url"]
	_, hasID := fields["file_id"]
	if hasURL == hasID || (hasURL && strings.TrimSpace(ref.ImageURL) == "") || (hasID && strings.TrimSpace(ref.FileID) == "") {
		return nil, invalidImageMarkup("markup requires exactly one non-empty image_url or file_id")
	}
	if hasID {
		return nil, invalidImageMarkup("file_id markup references are not supported through this gateway; use image_url or a PNG data URL")
	}
	var data []byte
	if strings.HasPrefix(ref.ImageURL, "data:") {
		header, payload, ok := strings.Cut(ref.ImageURL, ",")
		if !ok || header != "data:image/png;base64" {
			return nil, invalidImageMarkup("markup must use a base64 PNG data URL")
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
	} else {
		parsed, err := url.Parse(ref.ImageURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, invalidImageMarkup("markup image_url must be HTTPS or a PNG data URL")
		}
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.ImageURL, nil)
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
	}
	if err := validateImageMarkup(data); err != nil {
		return nil, err
	}
	return data, nil
}
