package ttsscheduler

import (
	"errors"
	"strings"
)

const PoolStandard = "standard"
const PoolHighFidelity = "high_fidelity"

var ErrPoolDenied = errors.New("当前直播间未启用方案选择的语音池，请联系后台调整或选择可用方案")

func PoolLabel(pool string) string {
	if pool == PoolHighFidelity {
		return "高仿真"
	}
	if pool == PoolStandard {
		return "标准"
	}
	return ""
}

func PoolForProvider(provider string) string {
	if provider == "" {
		return PoolStandard
	} // Legacy queued work was Aliyun.
	switch AccountProvider(provider) {
	case "aliyun":
		return PoolStandard
	case "volcengine":
		return PoolHighFidelity
	}
	return "" // Do not relabel historical engines as either pool.
}

func ValidateVoicePool(pool, provider, model string) error {
	if pool == "" {
		return nil
	}
	if PoolLabel(pool) == "" {
		return errors.New("请选择标准或高仿真语音池")
	}
	if strings.TrimSpace(provider) == "" || PoolForProvider(provider) != pool || (pool == PoolHighFidelity) != (strings.TrimSpace(model) == "seed-icl-2.0") || strings.TrimSpace(model) == "tencent-premium" {
		return errors.New("所选声音不属于当前语音池，请重新选择该池内的声音")
	}
	return nil
}
