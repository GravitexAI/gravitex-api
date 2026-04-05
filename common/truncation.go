package common

import (
	"fmt"
	"strings"
)

// TruncateBase64Content 截断JSON字符串中的base64内容，保留其他信息
// 支持多种格式：
// 1. Gemini 的 inlineData 格式
// 2. 传统的 data:image/ 格式
// 3. 纯 base64 数据
func TruncateBase64Content(content string) string {
	const maxBase64Length = 800

	// 处理 Gemini 的 inlineData 格式
	content = truncateInlineDataBase64(content, maxBase64Length)

	// 处理传统的 data:image/ 格式
	content = truncateDataImageBase64(content, maxBase64Length)

	// 处理没有前缀的纯 base64 数据
	content = truncateRawBase64Content(content)

	return content
}

// truncateInlineDataBase64 处理 Gemini 的 inlineData 格式中的 base64 数据
// 同时处理 data:video/mp4;base64, 格式的视频数据
func truncateInlineDataBase64(content string, maxLength int) string {
	const inlineDataPattern = `"inlineData":`
	const dataPattern = `"data":`
	const dataVideoPattern = `data:video/`
	const dataImagePattern = `data:image/`

	var result strings.Builder
	startIndex := 0

	for {
		// 查找 inlineData 字段
		inlineDataIndex := strings.Index(content[startIndex:], inlineDataPattern)
		if inlineDataIndex == -1 {
			// 没有更多 inlineData，添加剩余部分
			result.WriteString(content[startIndex:])
			break
		}
		inlineDataIndex += startIndex

		// 添加 inlineData 前的内容
		result.WriteString(content[startIndex:inlineDataIndex])

		// 查找 data 字段
		dataIndex := strings.Index(content[inlineDataIndex:], dataPattern)
		if dataIndex == -1 {
			// 没找到 data 字段，保持原样
			result.WriteString(content[inlineDataIndex:])
			break
		}
		dataIndex += inlineDataIndex

		// 查找 data 值的开始位置（冒号后的引号）
		colonIndex := strings.Index(content[dataIndex:], ":")
		if colonIndex == -1 {
			result.WriteString(content[inlineDataIndex:])
			break
		}
		colonIndex += dataIndex

		// 查找引号开始位置
		quoteStartIndex := strings.Index(content[colonIndex:], "\"")
		if quoteStartIndex == -1 {
			result.WriteString(content[inlineDataIndex:])
			break
		}
		quoteStartIndex += colonIndex + 1

		// 查找引号结束位置
		quoteEndIndex := strings.Index(content[quoteStartIndex:], "\"")
		if quoteEndIndex == -1 {
			result.WriteString(content[inlineDataIndex:])
			break
		}
		quoteEndIndex += quoteStartIndex

		// 获取引号内的内容
		quotedContent := content[quoteStartIndex:quoteEndIndex]

		// 检查是否是 data:video/ 或 data:image/ 格式
		if strings.HasPrefix(quotedContent, dataVideoPattern) || strings.HasPrefix(quotedContent, dataImagePattern) {
			// 处理 data:video/mp4;base64, 或 data:image/ 格式
			truncateDataUrlBase64(content, inlineDataIndex, quoteStartIndex, quoteEndIndex, maxLength, &result)
			startIndex = quoteEndIndex + 1
		} else {
			// 计算 base64 数据长度
			base64DataLength := quoteEndIndex - quoteStartIndex

			// 如果 base64 数据长度超过指定长度，则截断
			if base64DataLength > maxLength {
				// 保留前缀和部分 base64 数据
				result.WriteString(content[inlineDataIndex:quoteStartIndex])
				result.WriteString(content[quoteStartIndex : quoteStartIndex+maxLength])
				result.WriteString("...[base64数据已截断，长度:")
				result.WriteString(fmt.Sprintf("%d", base64DataLength))
				result.WriteString("]\"")
				startIndex = quoteEndIndex + 1
			} else {
				// 短数据保持原样
				result.WriteString(content[inlineDataIndex : quoteEndIndex+1])
				startIndex = quoteEndIndex + 1
			}
		}
	}

	return result.String()
}

// truncateDataUrlBase64 处理 data:video/ 或 data:image/ 格式的 base64 数据
func truncateDataUrlBase64(content string, inlineDataIndex, quoteStartIndex, quoteEndIndex, maxLength int, result *strings.Builder) {
	quotedContent := content[quoteStartIndex:quoteEndIndex]

	// 查找 base64 数据的开始位置
	base64Marker := ";base64,"
	base64StartIndex := strings.Index(quotedContent, base64Marker)

	if base64StartIndex == -1 {
		// 没找到 base64 标记，保持原样
		result.WriteString(content[inlineDataIndex : quoteEndIndex+1])
		return
	}

	base64StartIndex += quoteStartIndex + len(base64Marker)
	base64DataLength := quoteEndIndex - base64StartIndex

	// 如果 base64 数据长度超过指定长度，则截断
	if base64DataLength > maxLength {
		// 保留前缀和部分 base64 数据
		result.WriteString(content[inlineDataIndex:base64StartIndex])
		result.WriteString(content[base64StartIndex : base64StartIndex+maxLength])
		result.WriteString("...[base64数据已截断，长度:")
		result.WriteString(fmt.Sprintf("%d", base64DataLength))
		result.WriteString("]\"")
	} else {
		// 短数据保持原样
		result.WriteString(content[inlineDataIndex : quoteEndIndex+1])
	}
}

// truncateDataImageBase64 处理传统的 data:image/ 和 data:video/ 格式中的 base64 数据
func truncateDataImageBase64(content string, maxLength int) string {
	const base64ImagePrefix = "data:image/"
	const base64VideoPrefix = "data:video/"
	const base64Marker = ";base64,"

	var result strings.Builder
	startIndex := 0

	for {
		// 查找base64前缀（image或video）
		imageIndex := strings.Index(content[startIndex:], base64ImagePrefix)
		videoIndex := strings.Index(content[startIndex:], base64VideoPrefix)

		var base64Index int
		if imageIndex == -1 && videoIndex == -1 {
			break
		} else if imageIndex == -1 {
			base64Index = videoIndex + startIndex
		} else if videoIndex == -1 {
			base64Index = imageIndex + startIndex
		} else {
			// 两个都找到了，选择更早出现的
			if imageIndex < videoIndex {
				base64Index = imageIndex + startIndex
			} else {
				base64Index = videoIndex + startIndex
			}
		}

		// 添加base64前的内容
		result.WriteString(content[startIndex:base64Index])

		// 查找base64标记
		markerIndex := strings.Index(content[base64Index:], base64Marker)
		if markerIndex == -1 {
			// 没找到base64标记，保持原样
			result.WriteString(content[base64Index:])
			break
		}
		markerIndex += base64Index
		base64StartIndex := markerIndex + len(base64Marker)

		// 查找base64数据的结束位置（下一个双引号或字符串末尾）
		base64EndIndex := strings.Index(content[base64StartIndex:], "\"")
		if base64EndIndex == -1 {
			// 没找到结束引号，base64数据一直到字符串末尾
			base64EndIndex = len(content)
		} else {
			base64EndIndex += base64StartIndex
		}

		// 计算base64数据长度
		base64DataLength := base64EndIndex - base64StartIndex

		// 如果base64数据长度超过指定长度，则截断
		if base64DataLength > maxLength {
			// 保留前缀和部分base64数据
			result.WriteString(content[base64Index:base64StartIndex])
			result.WriteString(content[base64StartIndex : base64StartIndex+maxLength])
			result.WriteString("...[base64数据已截断，长度:")
			result.WriteString(fmt.Sprintf("%d", base64DataLength))
			result.WriteString("]")
			// 补回 JSON 字符串值的结束引号，并跳过原内容中的该引号，避免与后面的 content[startIndex:] 重复
			if base64EndIndex < len(content) {
				result.WriteString("\"")
				startIndex = base64EndIndex + 1
			} else {
				startIndex = base64EndIndex
			}
		} else {
			// 短数据保持原样
			if base64EndIndex < len(content) {
				result.WriteString(content[base64Index : base64EndIndex+1])
				startIndex = base64EndIndex + 1
			} else {
				result.WriteString(content[base64Index:])
				startIndex = base64EndIndex
			}
		}
	}

	// 添加剩余内容
	result.WriteString(content[startIndex:])
	return result.String()
}

// truncateRawBase64Content 处理没有前缀的纯base64数据
func truncateRawBase64Content(content string) string {
	const maxBase64Length = 1000
	const minBase64Length = 2000 // 只有超过这个长度的才认为是需要截断的base64数据

	var result strings.Builder
	startIndex := 0

	for {
		// 查找可能的base64数据开始位置（以双引号开始的长字符串）
		quoteIndex := strings.Index(content[startIndex:], "\"")
		if quoteIndex == -1 {
			// 没有更多引号，添加剩余部分
			result.WriteString(content[startIndex:])
			break
		}
		quoteIndex += startIndex

		// 添加引号前的内容
		result.WriteString(content[startIndex : quoteIndex+1])

		// 查找下一个引号
		nextQuoteIndex := strings.Index(content[quoteIndex+1:], "\"")
		if nextQuoteIndex == -1 {
			// 没找到结束引号，保持原样
			result.WriteString(content[quoteIndex+1:])
			break
		}
		nextQuoteIndex += quoteIndex + 1

		// 获取引号内的内容
		quotedContent := content[quoteIndex+1 : nextQuoteIndex]

		// 检查是否是base64数据（长度足够且包含base64字符）
		if len(quotedContent) > minBase64Length && isBase64String(quotedContent) {
			// 这是base64数据，需要截断
			if len(quotedContent) > maxBase64Length {
				result.WriteString(quotedContent[:maxBase64Length])
				result.WriteString("...[base64数据已截断，长度:")
				result.WriteString(fmt.Sprintf("%d", len(quotedContent)))
				result.WriteString("]")
			} else {
				result.WriteString(quotedContent)
			}
			result.WriteString("\"") // 补回结束引号
		} else {
			// 不是base64数据，保持原样（含结束引号，避免破坏 JSON 键值对）
			result.WriteString(content[quoteIndex+1 : nextQuoteIndex+1])
		}

		startIndex = nextQuoteIndex + 1
	}

	return result.String()
}

// isBase64String 检查字符串是否可能是base64数据
func isBase64String(s string) bool {
	if len(s) == 0 {
		return false
	}

	// 检查是否只包含base64字符
	base64Chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="
	base64CharCount := 0

	for _, char := range s {
		if strings.ContainsRune(base64Chars, char) {
			base64CharCount++
		}
	}

	// 如果超过80%的字符是base64字符，且长度足够，则认为是base64
	return float64(base64CharCount)/float64(len(s)) > 0.8
}

// TruncateJsonValues 截断JSON字符串中每个属性值，如果值超过100个字符则截断（http/https开头的URL除外）
// 递归处理嵌套对象和数组，避免 base64 等长字符串打爆日志
// 解析失败时按总长度截断兜底
func TruncateJsonValues(jsonStr string) string {
	const maxValueLength = 100
	const maxJsonSizeForFastPath = 5000 // 小于5KB的JSON，如果不需要截断则直接返回

	// 快速路径：如果JSON很小，先检查是否有需要截断的长字符串
	if len(jsonStr) < maxJsonSizeForFastPath {
		if !hasLongStringValueFast(jsonStr, maxValueLength) {
			return jsonStr
		}
	}

	// 尝试解析JSON，逐字段截断，确保所有字段（包括 base64 后面的字段）都能完整打印
	var jsonData interface{}
	if err := Unmarshal([]byte(jsonStr), &jsonData); err != nil {
		// 解析失败时兜底：按总长度截断
		const maxLogLength = 2000
		if len(jsonStr) > maxLogLength {
			return jsonStr[:maxLogLength] + fmt.Sprintf("...[JSON解析失败，总长度: %d]", len(jsonStr))
		}
		return jsonStr
	}

	// 递归处理JSON数据，逐字段截断
	truncatedData := truncateJsonValue(jsonData, maxValueLength)

	// 重新序列化为JSON字符串（使用 common.Marshal 符合项目规范）
	jsonBytes, err := Marshal(truncatedData)
	if err != nil {
		// 序列化失败时兜底
		const maxLogLength = 2000
		if len(jsonStr) > maxLogLength {
			return jsonStr[:maxLogLength] + fmt.Sprintf("...[JSON序列化失败，总长度: %d]", len(jsonStr))
		}
		return jsonStr
	}

	return string(jsonBytes)
}

// hasLongStringValueFast 快速检查JSON字符串中是否有超过指定长度的字符串值（排除URL）
func hasLongStringValueFast(jsonStr string, maxLength int) bool {
	searchPattern := `": "`
	patternLen := len(searchPattern)

	for i := 0; i < len(jsonStr)-patternLen; i++ {
		if jsonStr[i:i+patternLen] == searchPattern {
			start := i + patternLen
			// 检查是否是URL
			if start+8 < len(jsonStr) {
				prefix := jsonStr[start : start+8]
				if strings.HasPrefix(prefix, "http://") || strings.HasPrefix(prefix, "https:/") {
					if quoteIdx := strings.IndexByte(jsonStr[start:], '"'); quoteIdx > 0 {
						i = start + quoteIdx
						continue
					}
				}
			}

			for j := start; j < len(jsonStr) && j < start+maxLength+10; j++ {
				if jsonStr[j] == '"' && (j == start || jsonStr[j-1] != '\\') {
					strLen := j - start
					if strLen > maxLength {
						return true
					}
					i = j
					break
				}
			}
		}
	}

	return false
}

// truncateJsonValue 递归处理JSON值，截断过长的字符串
func truncateJsonValue(value interface{}, maxLength int) interface{} {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			return v
		}
		if len(v) > maxLength {
			return v[:maxLength] + "..."
		}
		return v
	case map[string]interface{}:
		result := make(map[string]interface{})
		for key, val := range v {
			result[key] = truncateJsonValue(val, maxLength)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = truncateJsonValue(val, maxLength)
		}
		return result
	default:
		return value
	}
}
