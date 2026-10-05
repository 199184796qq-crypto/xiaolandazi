package main

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Commands are a deliberately small allowlist, never an LLM/tool dispatch.
// Values are targets or relative steps; only the terminal knows the applied value.
type deviceCommand struct {
	Action    string `json:"action"`
	Operation string `json:"operation"`
	Value     *int   `json:"value,omitempty"`
}

var volumeTarget = regexp.MustCompile(`^(?:请)?(?:把)?(?:音量|声音)(?:调整到|调到|调整为|调成|设置为|设为|设置到|调至|设到|调整|调)?(?:百分之)?([0-9零〇一二两三四五六七八九十百]+)(?:%|百分比)?$`)

func commandValue(action, operation string, value int) deviceCommand {
	return deviceCommand{Action: action, Operation: operation, Value: &value}
}

func compactCommandText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("，,。！!？?、：:", r) {
			return -1
		}
		if r >= '０' && r <= '９' {
			return '0' + r - '０'
		}
		if r == '％' {
			return '%'
		}
		return r
	}, strings.TrimSpace(text))
}

func normalizeCommandText(text string) string {
	text = compactCommandText(text)
	for _, prefix := range []string{"你好小蓝", "小蓝小蓝", "小蓝搭子", "小蓝"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimPrefix(text, prefix)
			break
		}
	}
	for _, prefix := range []string{"请帮我", "麻烦帮我", "麻烦你", "帮我", "请"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimPrefix(text, prefix)
			break
		}
	}
	return text
}

func isWakeGreetingText(raw string) bool {
	text := compactCommandText(raw)
	for _, prefix := range []string{"你好小蓝", "小蓝小蓝", "小蓝搭子", "小蓝"} {
		if strings.HasPrefix(text, prefix) {
			return normalizeCommandText(raw) == ""
		}
	}
	return false // Empty/noise/courtesy alone must not open a follow-up recording.
}

func parseDeviceCommand(raw string) (deviceCommand, error) {
	text := normalizeCommandText(raw)
	switch text {
	case "声音大一点", "音量大一点", "声音大点", "音量调高", "调高音量", "增大音量", "声音调大一点":
		return commandValue("volume", "adjust", 10), nil
	case "声音小一点", "音量小一点", "声音小点", "音量调低", "调低音量", "降低音量", "声音调小一点":
		return commandValue("volume", "adjust", -10), nil
	case "静音", "把声音关掉", "关闭声音", "关闭音量":
		return deviceCommand{Action: "volume", Operation: "mute"}, nil
	case "取消静音", "恢复声音", "打开声音":
		return deviceCommand{Action: "volume", Operation: "unmute"}, nil
	case "当前音量", "查询音量", "音量多少", "音量是多少", "现在音量多少", "现在音量是多少":
		return deviceCommand{Action: "volume", Operation: "query"}, nil
	case "字体大一点", "文字大一点", "放大文字", "放大字体", "字太小", "字体调大":
		return commandValue("font_size", "adjust", 1), nil
	case "字体小一点", "文字小一点", "缩小文字", "缩小字体", "字太大", "字体调小":
		return commandValue("font_size", "adjust", -1), nil
	case "小号字体", "字体调到小号", "字体设置为小号", "文字调到小号":
		return commandValue("font_size", "set", 0), nil
	case "中号字体", "字体调到中号", "字体设置为中号", "文字调到中号":
		return commandValue("font_size", "set", 1), nil
	case "大号字体", "字体调到大号", "字体设置为大号", "文字调到大号":
		return commandValue("font_size", "set", 2), nil
	case "恢复默认字体", "恢复默认文字", "字体恢复默认":
		return deviceCommand{Action: "font_size", Operation: "reset"}, nil
	case "查询字体", "当前字体", "当前字体大小", "字体多大", "字体大小是多少":
		return deviceCommand{Action: "font_size", Operation: "query"}, nil
	case "拍照", "拍张照", "拍张照片", "拍一下环境", "拍一张环境照片", "拍摄环境", "环境拍照", "拍摄环境照片", "看下环境", "看看环境", "看一下环境":
		return deviceCommand{Action: "capture", Operation: "set"}, nil
	case "最大声音", "最大音量", "音量调到最大", "声音调到最大":
		return deviceCommand{}, errors.New("最大音量可能很响，请明确说：确认最大音量")
	case "确认最大音量", "确认将音量调到100%", "确认音量100%", "确认音量百分之一百":
		return commandValue("volume", "set", 100), nil
	}
	if match := volumeTarget.FindStringSubmatch(text); match != nil {
		value, ok := parsePercentNumber(match[1])
		if !ok || value < 0 || value > 100 {
			return deviceCommand{}, errors.New("音量范围是0到100%，请说：音量调整到30%")
		}
		if value == 100 {
			return deviceCommand{}, errors.New("最大音量可能很响，请明确说：确认最大音量")
		}
		return commandValue("volume", "set", value), nil
	}
	if text == "调整声音" || text == "调整音量" || text == "调音量" {
		return deviceCommand{}, errors.New("想调到多少？请说：小蓝，音量调整到30%")
	}
	if text == "" {
		return deviceCommand{}, errors.New("没有听清，请再说一次")
	}
	return deviceCommand{}, errors.New("暂时只支持音量、字体和拍照，请说：音量调整到30%")
}

func parsePercentNumber(text string) (int, bool) {
	if number, err := strconv.Atoi(text); err == nil {
		return number, true
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	if text == "一百" || text == "百" {
		return 100, true
	}
	runes := []rune(text)
	if len(runes) == 1 {
		if runes[0] == '十' {
			return 10, true
		}
		value, ok := digits[runes[0]]
		return value, ok
	}
	// Accept the usual spoken form 三十/十三/三十五; do not guess 三零 or mixed digits.
	parts := strings.Split(text, "十")
	if len(parts) != 2 || len([]rune(parts[0])) > 1 || len([]rune(parts[1])) > 1 {
		return 0, false
	}
	tens := 1
	if parts[0] != "" {
		var ok bool
		tens, ok = digits[[]rune(parts[0])[0]]
		if !ok || tens == 0 {
			return 0, false
		}
	}
	ones := 0
	if parts[1] != "" {
		var ok bool
		ones, ok = digits[[]rune(parts[1])[0]]
		if !ok || ones == 0 {
			return 0, false
		}
	}
	return tens*10 + ones, true
}
