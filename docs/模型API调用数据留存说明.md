# 模型 API 调用数据留存说明

通过平台调用模型时，平台不会保存用户提交的模型输入内容，也不会保存模型返回的输出内容。平台仅记录模型调用所需的用量信息，用于额度扣减、计费、统计和对账。

## 记录的信息

记录的信息包括：

- 输入 Token 数量
- 输出 Token 数量
- 调用次数
- 消耗额度

## 不记录的内容

平台不保存以下内容：

- 提交给模型的完整输入内容
- 模型返回的完整输出内容
- 用户的完整对话记录
- 提示词正文
- 模型生成结果正文
- 上传文件的正文内容

因此，平台保留的是模型调用的用量统计信息，不保留用户与模型交互的具体内容。

## 数据处理方式

用户发起模型调用后，平台仅在完成请求转发、结果返回和用量统计所必需的范围内处理相关数据。模型输入会被发送至所选择的模型服务，用于生成对应结果；模型输出会被返回至发起调用的用户。

请求处理完成后，平台不将模型输入和输出作为长期业务数据保存，也不会基于这些内容建立用户内容档案。

处理流程示例：

```go
func handleModelRequest(input []byte) (result []byte, err error) {
	defer cleanupTemporaryData(input)

	result, usage, err := callModel(input)
	if err != nil {
		return nil, err
	}

	// 仅保存调用用量，不保存 input 或 result 的正文内容。
	if err := recordUsage(usage); err != nil {
		return nil, err
	}

	return result, nil
}
```

其中，`usage` 仅包含输入 Token、输出 Token、调用次数和消耗额度等统计信息；`input` 和 `result` 仅用于完成本次调用及返回结果。

## 用量信息的使用目的

平台记录的用量信息仅用于以下目的：

- 计算输入和输出 Token；
- 扣减账户额度或计算调用费用；
- 生成用量统计和账单数据；
- 处理异常扣费、退款和对账；
- 支持必要的服务运行和用量核对。

用量信息用于统计和计费，不用于还原用户的完整对话内容。

## 数据访问范围

模型输入和输出不作为平台长期保存的数据提供查询、导出或展示功能。用量统计仅在完成计费、统计和服务管理所需的范围内使用，并按照平台的权限管理要求进行访问控制。

## 数据保存期限

模型输入和输出不作为长期业务数据保存。用量统计信息会根据额度管理、计费、对账和运营统计的需要保留；具体保存期限以平台实际的业务配置和适用要求为准。

## 第三方模型服务

模型调用可能经过相应的模型服务商或其他外部服务。平台仅将完成模型调用所必需的数据发送给对应服务，外部服务对数据的处理、存储和删除期限由其自身的隐私政策、服务条款和配置决定。

如对数据处理范围有更高要求，应在调用前确认所选模型服务的相关政策，并避免提交不必要的敏感信息。

## 说明范围

本说明适用于平台自身对模型 API 调用数据的处理和留存范围，不延伸解释模型服务商、网络服务商、对象存储、日志采集、备份系统或其他外部服务的独立数据处理行为。

如模型服务商或其他外部服务存在独立的数据处理和留存规则，则以其公布的隐私政策和服务条款为准。

## 实现代码节选

以下代码用于说明请求处理完成后的临时数据清理方式，以及用量信息的记录范围。

### 请求结束后的临时数据清理

```go
func CleanupBodyStorage(c *gin.Context) {
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			bs.Close()
		}
		c.Set(KeyBodyStorage, nil)
	}
}
```

```go
func BodyStorageCleanup() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			common.CleanupBodyStorage(c)
			service.CleanupFileSources(c)
		}()
		c.Next()
	}
}
```

### 用量信息记录字段

```go
type Log struct {
	Quota            int `json:"quota"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	UseTime          int `json:"use_time"`
}
```

### 多媒体内容处理

对于请求中的 Base64 多媒体内容，系统会进行截断处理，避免将完整的大体积内容作为任务数据长期保存：

```go
func TruncateBase64Content(content string) string {
	const maxBase64Length = 800

	content = truncateInlineDataBase64(content, maxBase64Length)
	content = truncateDataImageBase64(content, maxBase64Length)
	content = truncateRawBase64Content(content)
	return content
}
```

### 异步任务说明

异步视频、图片、音频等任务需要依靠任务状态、轮询、结算和结果查询完成服务流程，因此可能保留任务所需的必要参数、状态、结果信息和计费信息。此类任务不适用于“调用完成后完全不产生任何任务记录”的表述。
