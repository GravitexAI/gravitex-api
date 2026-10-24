package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	defaultProxyProbeURL = "https://www.gstatic.com/generate_204"
	proxyProbeTimeout    = 10 * time.Second
)

// ProbeProxy validates rawProxyURL with the save-time rules and then sends one GET
// through it to PROXY_TEST_URL (default gstatic generate_204). It returns the round
// trip latency. A fresh client is used so arbitrary test URLs never enter the shared
// proxy client cache. Error messages never contain the proxy credentials.
func ProbeProxy(ctx context.Context, rawProxyURL string) (time.Duration, error) {
	parsedURL, err := common.ParseProxyURLStrict(rawProxyURL)
	if err != nil {
		return 0, fmt.Errorf("invalid proxy URL: %w", err)
	}
	if parsedURL == nil {
		return 0, fmt.Errorf("proxy URL is empty")
	}
	client, err := newProxyHTTPClient(parsedURL)
	if err != nil {
		return 0, fmt.Errorf("invalid proxy URL: %w", err)
	}
	defer client.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(ctx, proxyProbeTimeout)
	defer cancel()
	probeURL := common.GetEnvOrDefaultString("PROXY_TEST_URL", defaultProxyProbeURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return 0, fmt.Errorf("invalid PROXY_TEST_URL: %w", err)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		message := err.Error()
		if parsedURL.User != nil {
			message = strings.ReplaceAll(message, parsedURL.User.String(), "***")
		}
		if ctx.Err() == context.DeadlineExceeded {
			return 0, fmt.Errorf("timed out after %s", proxyProbeTimeout)
		}
		return 0, fmt.Errorf("%s", message)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	latency := time.Since(start)
	if resp.StatusCode >= 400 {
		return latency, fmt.Errorf("probe URL returned HTTP %d", resp.StatusCode)
	}
	return latency, nil
}
