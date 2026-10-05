package httpapi

import "testing"

func TestWakeGreetingOnlyNeverDispatchesCommands(t *testing.T) {
	for _, text := range []string{"小蓝小蓝", "小蓝，小蓝！", "你好小蓝", "小蓝搭子", "小蓝", "小蓝小蓝请"} {
		if !wakeGreetingOnly(text) {
			t.Fatal("wake not recognised", text)
		}
	}
	for _, text := range []string{"", "。", "请", "帮我", "小蓝小蓝帮我音量调整到30%", "小蓝音量30%", "小蓝小蓝看下环境", "小蓝小蓝你知道我是谁吗"} {
		if wakeGreetingOnly(text) {
			t.Fatal("command mistaken for greeting", text)
		}
	}
}
