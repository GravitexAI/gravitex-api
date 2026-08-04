package ali

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBillingResolutionKeyFromParams_4k(t *testing.T) {
	// Veo upstream request body sets parameters.resolution = "4k" (no trailing "p"),
	// this must normalize to "4k" so it matches the "4k" price tier key, not "4kp".
	key := BillingResolutionKeyFromParams(&AliVideoParameters{Resolution: "4k"})
	assert.Equal(t, "4k", key)
}

func TestBillingResolutionKeyFromParams_720P(t *testing.T) {
	key := BillingResolutionKeyFromParams(&AliVideoParameters{Resolution: "720P"})
	assert.Equal(t, "720p", key)
}
