package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMinimumRemainingQuotaForModelUsesExactAndLongestPrefixRules(t *testing.T) {
	old := quotaSetting
	t.Cleanup(func() { quotaSetting = old })

	quotaSetting = QuotaSetting{
		MinimumRemainingQuota: 500,
		ModelQuotaReserve: map[string]int{
			"seed*":        1000,
			"seedance*":    2000,
			"seedance-2*":  3000,
			"seedance-2-0": 4000,
		},
	}

	require.Equal(t, 4000, GetMinimumRemainingQuota("seedance-2-0"))
	require.Equal(t, 3000, GetMinimumRemainingQuota("seedance-2-0-mini"))
	require.Equal(t, 2000, GetMinimumRemainingQuota("seedance-1-0"))
	require.Equal(t, 500, GetMinimumRemainingQuota("gpt-5"))
}

func TestMinimumRemainingQuotaDefaultsToZeroWhenDisabled(t *testing.T) {
	old := quotaSetting
	t.Cleanup(func() { quotaSetting = old })

	quotaSetting = QuotaSetting{}

	require.Zero(t, GetMinimumRemainingQuota("seedance-2-0"))
}

func TestValidateModelQuotaReserve(t *testing.T) {
	require.NoError(t, ValidateModelQuotaReserve(`{"seedance*":10000,"seedance-2-0":0}`))
	require.Error(t, ValidateModelQuotaReserve(`{"seedance*":-1}`))
	require.Error(t, ValidateModelQuotaReserve(`{"seed*ance":10000}`))
	require.Error(t, ValidateModelQuotaReserve(`[]`))
}
