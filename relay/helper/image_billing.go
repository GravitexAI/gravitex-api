package helper

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/types"
)

const (
	seedreamOutputPixelThreshold int64 = 2_610_000
	// Keep the existing configuration keys stable. Their threshold semantics
	// follow the current upstream 2.61M-pixel boundary.
	seedreamLessPixelTier        = "pixelLessEqual236W"
	seedreamMorePixelTier        = "pixelMoreThan236W"
	seedreamLessPixelTierCurrent = "pixelLessEqual261W"
	seedreamMorePixelTierCurrent = "pixelMoreThan261W"
)

// seedreamOutputDimensions is the one-dimensional catalog used to build the
// width x height tier matrix. Custom upstream sizes fall back to the same
// pixel-tier rule when they are not present in this catalog.
var seedreamOutputDimensions = []int{1024, 1536, 2048}

func CountImageInputs(request *dto.ImageRequest) int {
	if request == nil {
		return 0
	}
	raw := request.Image
	if len(raw) == 0 {
		raw = request.Images
	}
	if len(raw) == 0 {
		return 0
	}

	var images []string
	if err := common.Unmarshal(raw, &images); err == nil {
		return len(images)
	}
	var image string
	if err := common.Unmarshal(raw, &image); err == nil && image != "" {
		return 1
	}
	return 0
}

func ParseImageSize(size string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(size), "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func resolvePresetImageSize(size string) (int, int, bool) {
	switch strings.ToUpper(strings.TrimSpace(size)) {
	case "1K":
		return 1024, 1024, true
	case "2K":
		return 2048, 2048, true
	default:
		return 0, 0, false
	}
}

func BuildOutputTierMatrix(dimensions []int) [][]string {
	matrix := make([][]string, len(dimensions))
	for widthIndex, width := range dimensions {
		matrix[widthIndex] = make([]string, len(dimensions))
		for heightIndex, height := range dimensions {
			matrix[widthIndex][heightIndex] = outputPixelTier(width, height)
		}
	}
	return matrix
}

func outputPixelTier(width, height int) string {
	if int64(width)*int64(height) <= seedreamOutputPixelThreshold {
		return seedreamLessPixelTier
	}
	return seedreamMorePixelTier
}

func outputTierForSize(width, height int) string {
	widthIndex := -1
	heightIndex := -1
	for index, dimension := range seedreamOutputDimensions {
		if dimension == width {
			widthIndex = index
		}
		if dimension == height {
			heightIndex = index
		}
	}
	if widthIndex >= 0 && heightIndex >= 0 {
		return BuildOutputTierMatrix(seedreamOutputDimensions)[widthIndex][heightIndex]
	}
	return outputPixelTier(width, height)
}

func resolveOutputDimensions(requestSize, responseSize string) (int, int, bool) {
	if width, height, ok := ParseImageSize(responseSize); ok {
		return width, height, true
	}
	if width, height, ok := ParseImageSize(requestSize); ok {
		return width, height, true
	}
	if width, height, ok := resolvePresetImageSize(responseSize); ok {
		return width, height, true
	}
	return resolvePresetImageSize(requestSize)
}

func EstimateImagePerImageCost(config types.ImagePerImagePricing, request *dto.ImageRequest) (float64, *types.ImageBillingUsage) {
	inputCount := CountImageInputs(request)
	outputWidth, outputHeight, ok := resolveOutputDimensions(requestSize(request), "")
	if !ok {
		outputWidth, outputHeight = 2048, 2048
	}
	usage := &types.ImageBillingUsage{
		InputImageCount:      inputCount,
		SuccessfulImageCount: 1,
		OutputWidth:          outputWidth,
		OutputHeight:         outputHeight,
		OutputPixels:         int64(outputWidth) * int64(outputHeight),
		OutputSizeTier:       outputTierForSize(outputWidth, outputHeight),
	}
	return ImagePerImageCost(config, usage), usage
}

func ImagePerImageCost(config types.ImagePerImagePricing, usage *types.ImageBillingUsage) float64 {
	if usage == nil {
		return 0
	}
	cost := 0.0
	if usage.InputImageCount > 0 {
		cost += config.InputImageFirst
		if usage.InputImageCount > 1 {
			cost += float64(usage.InputImageCount-1) * config.InputImageFromThe2nd
		}
	}
	if usage.SuccessfulImageCount > 0 {
		tier := usage.OutputSizeTier
		if tier == "" && usage.OutputWidth > 0 && usage.OutputHeight > 0 {
			tier = outputTierForSize(usage.OutputWidth, usage.OutputHeight)
		}
		cost += float64(usage.SuccessfulImageCount) * ImageOutputUnitPrice(config, tier)
	}
	return cost
}

// ImageOutputUnitPrice keeps the persisted 236W configuration keys compatible
// while accepting the newer 261W key names during migration.
func ImageOutputUnitPrice(config types.ImagePerImagePricing, tier string) float64 {
	if price, ok := config.OutputImage[tier]; ok {
		return price
	}
	switch tier {
	case seedreamLessPixelTier:
		return config.OutputImage[seedreamLessPixelTierCurrent]
	case seedreamMorePixelTier:
		return config.OutputImage[seedreamMorePixelTierCurrent]
	default:
		return 0
	}
}

func SettleImagePerImageUsage(config types.ImagePerImagePricing, request *dto.ImageRequest, usage *dto.Usage) (float64, *types.ImageBillingUsage, error) {
	if usage == nil {
		return 0, nil, fmt.Errorf("image usage is nil")
	}
	inputCount := usage.InputImages
	if inputCount <= 0 {
		inputCount = CountImageInputs(request)
	}
	outputCount := usage.GeneratedImages
	if outputCount < 0 {
		outputCount = 0
	}
	width, height, ok := resolveOutputDimensions(requestSize(request), firstOutputSize(usage.OutputImageSizes))
	if !ok {
		return 0, nil, fmt.Errorf("cannot resolve output image size")
	}
	billingUsage := &types.ImageBillingUsage{
		InputImageCount:      inputCount,
		SuccessfulImageCount: outputCount,
		OutputWidth:          width,
		OutputHeight:         height,
		OutputPixels:         int64(width) * int64(height),
		OutputSizeTier:       outputTierForSize(width, height),
	}
	return ImagePerImageCost(config, billingUsage), billingUsage, nil
}

func requestSize(request *dto.ImageRequest) string {
	if request == nil {
		return ""
	}
	return request.Size
}

func firstOutputSize(sizes []string) string {
	if len(sizes) == 0 {
		return ""
	}
	return sizes[0]
}
