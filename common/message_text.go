package common

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"
)

// alpha：logs.content 由 Java 管理端和客户端直接展示，必须落中文。
// messageTextZhCN 是 web/src/i18n/locales/zh.json 中后端日志会用到的那部分译文
// （key 为 Message 的英文原文），由 bin/gen_message_text_zh.py 从前端译文生成（没有译文的 key 原样输出）。
//
//go:embed message_text.zh-CN.json
var messageTextZhCN []byte

var (
	messageTextsOnce sync.Once
	messageTexts     map[string]string
)

// LogContentText renders the message in Simplified Chinese for the logs.content
// column. A key without a translation (alpha 原有的中文文案直接作为 key 传入)
// renders as itself.
func (m *Message) LogContentText() string {
	if m == nil {
		return ""
	}
	messageTextsOnce.Do(func() {
		_ = Unmarshal(messageTextZhCN, &messageTexts)
	})
	text, ok := messageTexts[m.Key]
	if !ok {
		return m.Error()
	}
	if len(m.Params) == 0 {
		return text
	}
	pairs := make([]string, 0, len(m.Params)*2)
	for name, value := range m.Params {
		pairs = append(pairs, "{{"+name+"}}", fmt.Sprint(value))
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
