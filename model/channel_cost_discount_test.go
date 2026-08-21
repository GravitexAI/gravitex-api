package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelGetCostDiscountForModelPrefersModelOverride(t *testing.T) {
	channel := &Channel{
		CostDiscount:  float64PtrForTest(0.952),
		OtherSettings: `{"model_cost_discount":{"seedance-2-0-fast":0.75}}`,
	}

	discount, ok := channel.GetCostDiscountForModel("seedance-2-0-fast")

	require.True(t, ok)
	require.Equal(t, 0.75, discount)
}

func TestChannelGetCostDiscountForModelWithoutConfiguration(t *testing.T) {
	channel := &Channel{OtherSettings: `{}`}

	discount, ok := channel.GetCostDiscountForModel("seedance-2-0-fast")

	require.False(t, ok)
	require.Zero(t, discount)
}

func TestChannelGetCostDiscountForModelFallsBackToChannelDiscount(t *testing.T) {
	channel := &Channel{
		CostDiscount:  float64PtrForTest(0.8),
		OtherSettings: `{"model_cost_discount":{"seedance-2-0-fast":0.75}}`,
	}

	discount, ok := channel.GetCostDiscountForModel("seedance-2-0")

	require.True(t, ok)
	require.Equal(t, 0.8, discount)
}

func TestChannelGetCostDiscountForModelRejectsInvalidOverride(t *testing.T) {
	channel := &Channel{
		CostDiscount:  float64PtrForTest(0.8),
		OtherSettings: `{"model_cost_discount":{"seedance-2-0-fast":1.2}}`,
	}

	discount, ok := channel.GetCostDiscountForModel("seedance-2-0-fast")

	require.True(t, ok)
	require.Equal(t, 0.8, discount)
}

func float64PtrForTest(value float64) *float64 {
	return &value
}
