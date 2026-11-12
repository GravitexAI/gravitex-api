package common

import "testing"

func TestMessageLogContentTextRendersChinese(t *testing.T) {
	got := NewMessage("API info #{{index}} is missing the URL field", map[string]any{"index": 2}).LogContentText()
	if got != "第2个API信息缺少URL字段" {
		t.Fatalf("got %q", got)
	}
	// alpha 原有中文文案直接作为 key 传入时原样输出
	if got := NewMessage("开始设置两步验证").LogContentText(); got != "开始设置两步验证" {
		t.Fatalf("got %q", got)
	}
	var nilMessage *Message
	if nilMessage.LogContentText() != "" {
		t.Fatal("nil message should render empty")
	}
}
