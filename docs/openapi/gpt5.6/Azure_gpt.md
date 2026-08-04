根据 Chrome 中查看的 Azure 官方文档，结论需要修正：

## 结论

Azure OpenAI 当前文档描述的是自动 Prompt Caching，不支持 OpenAI GPT-5.6 原生 API 文档中的“显式缓存创建”机制。

Azure 官方只支持/说明：

```
"prompt_cache_key": "稳定的缓存路由键"
```

以及旧式缓存保留参数：

```
"prompt_cache_retention": "in_memory"
```

或：

```
"prompt_cache_retention": "24h"
```

Azure 文档没有以下字段：

```
"prompt_cache_options": {
  "mode": "explicit",
  "ttl": "30m"
}
```

也没有：

```
"prompt_cache_breakpoint": {
  "mode": "explicit"
}
```

### Azure 返回什么？

Azure 官方文档只定义缓存读取：

```
"prompt_tokens_details": {
  "cached_tokens": 1408
}
```

没有定义：

```
"cache_write_tokens"
```

因此当前 Azure 实际响应：

```
"prompt_tokens_details": {
  "cached_tokens": 23296
}
```

是符合 Azure 官方文档的。

### Azure 如何创建缓存？

Azure 没有单独的“创建缓存 API”。处理方式是：

1. 第一次发送至少 1024 tokens 的请求；
2. Azure 自动处理并可能缓存前缀；
3. 后续请求保持前 1024 tokens 完全一致；
4. 每增加 128 个相同 token，可能增加缓存命中；
5. 通过 `cached_tokens` 判断是否命中。

Azure 文档还明确说明：

```
Prompt caching is enabled by default.
There's no opt-out support.
```

也就是说，不能显式开启、关闭或指定断点。

## 对当前测试数据的解释

Chat Completions：

```
第一次 cached_tokens = 0
后续 cached_tokens = 23296
```

这已经完整证明：

```
第一次请求触发 Azure 自动缓存
后续请求成功读取缓存
```

但 Azure 不返回缓存写入 token，因此：

```
cache_write_tokens = 不存在
cc = 0
```

属于正常现象，不是平台解析失败。

Responses：

```
三次 cached_tokens = 0
```

说明该 Responses 部署本次没有产生缓存命中。可能是具体模型部署、API 版本、请求转换或缓存路由未命中，但不能通过添加 `prompt_cache_breakpoint` 强制解决，因为 Azure 文档没有支持这个参数。

## 对平台计费的影响

提交 `8c84ed536` 的 `cache_write_tokens → cc` 逻辑适用于返回该字段的 OpenAI 原生 GPT-5.6 上游。

对于 Azure 渠道：

```
缓存读取：使用 cached_tokens / cr 计费
缓存写入：Azure 不返回 cache_write_tokens，不能使用 cc 计费
```

不能根据第一次 `cached_tokens=0` 就把所有输入 token 推算为缓存写入，因为 Azure 官方没有提供这一计费口径。

所以最终准确结论是：

```
Azure Chat 缓存行为符合预期
Azure 使用自动缓存，不支持显式缓存创建
Azure 官方 usage 不包含 cache_write_tokens
当前平台无法也不应该从 Azure 响应推算 cc
Responses 未命中需要单独排查路由、部署和 API 版本
```