package service

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestValidUsageAcceptsImageTokenUsage(t *testing.T) {
	require.True(t, ValidUsage(&dto.Usage{OutputTokens: 4096, TotalTokens: 4096}))
}
