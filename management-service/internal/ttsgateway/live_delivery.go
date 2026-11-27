package ttsgateway

import "strings"

const naturalBreathInstruction = "自然换气起句，尾字说完整，收尾后短停，不要突然切断；请用标准普通话朗读，不要带方言口音。"

// NaturalBreathInstruction adds the common articulation and pause directions.
func NaturalBreathInstruction(style string) string {
	if style = strings.TrimSpace(style); style != "" {
		return style + "；" + naturalBreathInstruction
	}
	return naturalBreathInstruction
}

// LiveDeliveryStyle chooses the speaking style from the text being synthesized.
func LiveDeliveryStyle(text string, emotionEnabled bool) string {
	if !emotionEnabled {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return "像真人直播自然说话，允许停顿和拖音，不要播音腔。"
	case strings.ContainsAny(trimmed, "？?"):
		return "像真人直播随口一问，句尾自然上扬，问完停半拍。"
	case strings.ContainsAny(trimmed, "！!") || deliveryContainsAny(trimmed, "赶紧", "抓紧", "最后", "抢", "限时"):
		return "像真人直播提醒，重点词稍重，语速略快，句尾收住。"
	case deliveryContainsAny(trimmed, "元", "块", "价", "优惠", "券", "套餐"):
		return "像真人直播报价，数字清楚，前后各停半拍，别像播报。"
	case deliveryContainsAny(trimmed, "欢迎", "来了", "进来", "朋友"):
		return "像真人打招呼，亲切放松，尾音别太用力。"
	default:
		return "像真人直播聊天，逗号处停半拍，允许轻轻拖音，不要播音腔。"
	}
}

func deliveryContainsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
