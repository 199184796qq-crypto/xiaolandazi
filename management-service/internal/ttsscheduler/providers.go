package ttsscheduler

import "strings"

// The Aliyun models share an account allowance. Another vendor never inherits
// that account's environment key, voice mappings, quota or FIFO position.
func AccountProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "aliyun", "aliyun_qwen", "aliyun_qwen_clone", "qwen_audio", "qwen", "dashscope", "cosyvoice", "aliyun_cosyvoice":
		return "aliyun"
	case "volcengine", "doubao", "volcano":
		return "volcengine"
	default:
		return ""
	}
}

func CompatibleProvider(account, request string) bool {
	a, b := AccountProvider(account), AccountProvider(request)
	// Empty legacy jobs were Aliyun before the provider field was introduced.
	if request == "" {
		b = "aliyun"
	}
	return a != "" && a == b
}
