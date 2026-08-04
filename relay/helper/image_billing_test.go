package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
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

func TestCountImageInputs(t *testing.T) {
	request := &dto.ImageRequest{Image: []byte(`["a","b"]`)}
	require.Equal(t, 2, CountImageInputs(request))

	request.Image = []byte(`"a"`)
	require.Equal(t, 1, CountImageInputs(request))
}
