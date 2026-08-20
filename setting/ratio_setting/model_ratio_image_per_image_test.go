package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按张计费缓存的契约：一份 ImageModelPricePerImage JSON 要按值的形态分流到
// 数字单价缓存与结构化配置缓存，两个 getter 都不再读 OptionMap。
func TestUpdateImageModelPricePerImageSplitsNumericAndStructuredConfig(t *testing.T) {
	require.NoError(t, UpdateImageModelPricePerImageByJSONString(
		`{"seedream-4":0.03,"gpt-image-2":{"inputImageFirst":0.01,"inputImageFromThe2nd":0.005,"outputImage":{"1k":0.04}}}`))

	price, ok := GetImageModelPricePerImage("seedream-4")
	require.True(t, ok)
	assert.Equal(t, 0.03, price)

	// 结构化配置不能混进数字单价缓存，否则会被当成 0 单价计费
	_, ok = GetImageModelPricePerImage("gpt-image-2")
	assert.False(t, ok)

	config, ok := GetImageModelPriceConfig("gpt-image-2")
	require.True(t, ok)
	assert.Equal(t, 0.01, config.InputImageFirst)
	assert.Equal(t, 0.005, config.InputImageFromThe2nd)
	assert.Equal(t, map[string]float64{"1k": 0.04}, config.OutputImage)

	_, ok = GetImageModelPriceConfig("seedream-4")
	assert.False(t, ok)
}

// doubao- 前缀别名双向兼容：配置里写哪一种 key，另一种模型名都要能查到。
func TestGetImageModelPriceMatchesDoubaoAlias(t *testing.T) {
	require.NoError(t, UpdateImageModelPricePerImageByJSONString(
		`{"seedream-4":0.03,"doubao-seedream-5":0.06,"doubao-gpt-image-2":{"outputImage":{"1k":0.04}}}`))

	price, ok := GetImageModelPricePerImage("doubao-seedream-4")
	require.True(t, ok)
	assert.Equal(t, 0.03, price)

	price, ok = GetImageModelPricePerImage("seedream-5")
	require.True(t, ok)
	assert.Equal(t, 0.06, price)

	config, ok := GetImageModelPriceConfig("gpt-image-2")
	require.True(t, ok)
	assert.Equal(t, map[string]float64{"1k": 0.04}, config.OutputImage)
}

// 计费安全：负单价会让配额算出负扣费（等于给用户充值），必须在入口丢弃。
func TestUpdateImageModelPricePerImageDropsNegativePrice(t *testing.T) {
	require.NoError(t, UpdateImageModelPricePerImageByJSONString(`{"seedream-4":-1,"seedream-5":0}`))

	_, ok := GetImageModelPricePerImage("seedream-4")
	assert.False(t, ok)

	// 0 是合法的免费单价，不能跟负值一起丢掉
	price, ok := GetImageModelPricePerImage("seedream-5")
	require.True(t, ok)
	assert.Equal(t, 0.0, price)
}

// 坏值不能把已生效的价格清空，否则一次误保存就会让线上按张计费全部归零。
func TestUpdateImageModelPricePerImageKeepsCacheOnInvalidJSON(t *testing.T) {
	require.NoError(t, UpdateImageModelPricePerImageByJSONString(`{"seedream-4":0.03}`))

	require.Error(t, UpdateImageModelPricePerImageByJSONString(`{"seedream-4":`))

	price, ok := GetImageModelPricePerImage("seedream-4")
	require.True(t, ok)
	assert.Equal(t, 0.03, price)
}

// 配置被清空时缓存要跟着空，否则删掉的价格还会继续计费。
func TestUpdateImageModelPricePerImageClearsCacheOnEmptyValue(t *testing.T) {
	require.NoError(t, UpdateImageModelPricePerImageByJSONString(`{"seedream-4":0.03}`))
	require.NoError(t, UpdateImageModelPricePerImageByJSONString(""))

	_, ok := GetImageModelPricePerImage("seedream-4")
	assert.False(t, ok)
	_, ok = GetImageModelPriceConfig("seedream-4")
	assert.False(t, ok)
}
