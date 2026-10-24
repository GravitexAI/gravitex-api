package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeProxyGoesThroughProxy(t *testing.T) {
	var proxiedURL string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedURL = r.URL.String()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	t.Setenv("PROXY_TEST_URL", "http://probe.invalid/generate_204")

	_, err := ProbeProxy(context.Background(), proxy.URL)
	require.NoError(t, err)
	assert.Equal(t, "http://probe.invalid/generate_204", proxiedURL)
}

func TestProbeProxyReportsFailures(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("PROXY_TEST_URL", "http://probe.invalid/generate_204")

	_, err := ProbeProxy(context.Background(), proxy.URL)
	assert.ErrorContains(t, err, "HTTP 502")

	_, err = ProbeProxy(context.Background(), "ftp://10.0.0.1:21")
	assert.ErrorContains(t, err, "invalid proxy URL")

	_, err = ProbeProxy(context.Background(), "random")
	assert.ErrorContains(t, err, "invalid proxy URL")
}
