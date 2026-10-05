package main

import (
	"testing"
)

func TestDeviceCommandAllowlist(t *testing.T) {
	cases := []struct {
		text, action, operation string
		value                   int
		hasValue                bool
	}{
		{"小蓝，音量调整到30%", "volume", "set", 30, true},
		{"小蓝小蓝帮我音量调整到30%", "volume", "set", 30, true},
		{"小蓝，小蓝，请帮我把声音调整到百分之三十", "volume", "set", 30, true},
		{"小蓝小蓝麻烦帮我字体大一点", "font_size", "adjust", 1, true},
		{"小蓝，音量调到百分之三十。", "volume", "set", 30, true},
		{"你好小蓝，把声音设置为百分之十三", "volume", "set", 13, true},
		{"小蓝搭子 音量３０％", "volume", "set", 30, true},
		{"音量调整到0%", "volume", "set", 0, true},
		{"确认最大音量", "volume", "set", 100, true},
		{"声音大一点", "volume", "adjust", 10, true},
		{"降低音量", "volume", "adjust", -10, true},
		{"静音", "volume", "mute", 0, false},
		{"取消静音", "volume", "unmute", 0, false},
		{"音量是多少", "volume", "query", 0, false},
		{"字体大一点", "font_size", "adjust", 1, true},
		{"缩小文字", "font_size", "adjust", -1, true},
		{"小号字体", "font_size", "set", 0, true},
		{"中号字体", "font_size", "set", 1, true},
		{"大号字体", "font_size", "set", 2, true},
		{"恢复默认字体", "font_size", "reset", 0, false},
		{"当前字体大小", "font_size", "query", 0, false},
		{"拍照", "capture", "set", 0, false},
		{"小蓝，看下环境", "capture", "set", 0, false},
		{"看看环境", "capture", "set", 0, false},
		{"看一下环境", "capture", "set", 0, false},
	}
	for _, test := range cases {
		t.Run(test.text, func(t *testing.T) {
			command, err := parseDeviceCommand(test.text)
			if err != nil || command.Action != test.action || command.Operation != test.operation || (command.Value != nil) != test.hasValue || (test.hasValue && *command.Value != test.value) {
				t.Fatalf("command=%+v error=%v", command, err)
			}
		})
	}
	for _, text := range []string{"", "调整声音", "最大声音", "音量100%", "音量101%", "音量-1%", "音量30.5%", "音量百分之三零", "不要把音量调到30%", "停止直播", "重置账号", "声音大一点然后重启", "字体64", "请给另一个租户拍照"} {
		if command, err := parseDeviceCommand(text); err == nil {
			t.Errorf("unsafe/unknown command %q accepted %+v", text, command)
		}
	}
}
