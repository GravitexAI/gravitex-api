package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestBuildOutputTierMatrix(t *testing.T) {
	matrix := BuildOutputTierMatrix([]int{1024, 1536, 2048})

	require.Equal(t, "pixelLessEqual236W", matrix[0][0])
	require.Equal(t, "pixelLessEqual236W", matrix[2][0])
	require.Equal(t, "pixelMoreThan236W", matrix[1][2])
	require.Equal(t, "pixelMoreThan236W", matrix[2][2])
}

func TestImagePerImageCost(t *testing.T) {
	config := types.ImagePerImagePricing{
		InputImageFirst:      0,
		InputImageFromThe2nd: 0.003,
		OutputImage: map[string]float64{
			"pixelLessEqual236W": 0.045,
			"pixelMoreThan236W":  0.09,
		},
	}

	cost := ImagePerImageCost(config, &types.ImageBillingUsage{
		InputImageCount:      3,
		SuccessfulImageCount: 1,
		OutputWidth:          2048,
		OutputHeight:         2048,
		OutputSizeTier:       "pixelMoreThan236W",
	})

	require.InDelta(t, 0.096, cost, 0.0000001)
}

func TestImageOutputUnitPriceSupportsCurrentTierKeyNames(t *testing.T) {
	config := types.ImagePerImagePricing{
		OutputImage: map[string]float64{
			"pixelLessEqual261W": 0.045,
			"pixelMoreThan261W":  0.09,
		},
	}

	require.InDelta(t, 0.045, ImageOutputUnitPrice(config, "pixelLessEqual236W"), 0.0000001)
	require.InDelta(t, 0.09, ImageOutputUnitPrice(config, "pixelMoreThan236W"), 0.0000001)
}

func TestCountImageInputs(t *testing.T) {
	request := &dto.ImageRequest{Image: []byte(`["a","b"]`)}
	require.Equal(t, 2, CountImageInputs(request))

	request.Image = []byte(`"a"`)
	require.Equal(t, 1, CountImageInputs(request))
}

func TestSettleImagePerImageUsagePricesEachOutputTier(t *testing.T) {
	config := types.ImagePerImagePricing{
		InputImageFirst:      0,
		InputImageFromThe2nd: 0.003,
		OutputImage: map[string]float64{
			"pixelLessEqual236W": 0.045,
			"pixelMoreThan236W":  0.09,
		},
	}
	request := &dto.ImageRequest{Image: []byte(`"input"`)}
	usage := &dto.Usage{
		GeneratedImages: 6,
		InputImages:     1,
		OutputImageSizes: []string{
			"1056x1088",
			"1056x1088",
			"2048x2048",
			"2048x2048",
			"2048x2048",
			"2048x2048",
		},
	}

	cost, billingUsage, err := SettleImagePerImageUsage(config, request, usage)

	require.NoError(t, err)
	require.InDelta(t, 0.45, cost, 0.0000001)
	require.False(t, billingUsage.LayerDecomposition)
	require.InDelta(t, 1, billingUsage.OutputPriceMultiplier, 0.0000001)
	require.Equal(t, []types.ImageOutputTierUsage{
		{Tier: "pixelLessEqual236W", Count: 2, UnitPrice: 0.045, Subtotal: 0.09},
		{Tier: "pixelMoreThan236W", Count: 4, UnitPrice: 0.09, Subtotal: 0.36},
	}, billingUsage.OutputTiers)
}

func TestSettleImagePerImageUsageHalvesEveryOutputTierForLayerDecomposition(t *testing.T) {
	config := types.ImagePerImagePricing{
		InputImageFirst:      0,
		InputImageFromThe2nd: 0.003,
		OutputImage: map[string]float64{
			"pixelLessEqual236W": 0.045,
			"pixelMoreThan236W":  0.09,
		},
	}
	var request dto.ImageRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"dola-seedream-5-0-pro-260628",
		"prompt":"hello",
		"image":["input-1","input-2","input-3"],
		"layer_decomposition":true
	}`), &request))
	usage := &dto.Usage{
		GeneratedImages: 6,
		InputImages:     3,
		OutputImageSizes: []string{
			"1056x1088",
			"1056x1088",
			"2048x2048",
			"2048x2048",
			"2048x2048",
			"2048x2048",
		},
	}

	cost, billingUsage, err := SettleImagePerImageUsage(config, &request, usage)

	require.NoError(t, err)
	// Input: 2 paid images * $0.003. Output: 2 * $0.0225 + 4 * $0.045.
	require.InDelta(t, 0.231, cost, 0.0000001)
	require.True(t, billingUsage.LayerDecomposition)
	require.InDelta(t, 0.5, billingUsage.OutputPriceMultiplier, 0.0000001)
	require.Equal(t, []types.ImageOutputTierUsage{
		{Tier: "pixelLessEqual236W", Count: 2, UnitPrice: 0.0225, Subtotal: 0.045},
		{Tier: "pixelMoreThan236W", Count: 4, UnitPrice: 0.045, Subtotal: 0.18},
	}, billingUsage.OutputTiers)

	request.Size = "1056x1088"
	estimatedCost, estimatedUsage := EstimateImagePerImageCost(config, &request)
	require.InDelta(t, 0.0285, estimatedCost, 0.0000001)
	require.True(t, estimatedUsage.LayerDecomposition)
	require.InDelta(t, 0.0225, ImageOutputUnitPriceForUsage(config, estimatedUsage), 0.0000001)
}
