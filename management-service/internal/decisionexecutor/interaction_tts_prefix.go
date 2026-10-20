package decisionexecutor

// prefixedInteractionText 曾用于给互动话术 TTS 前加 "****" 前缀，意图防止 cosyvoice
// 吞掉开头字，但实测无效（用户 2026-10-20 确认），已去掉前缀、改为原样透传。
// 保留这个函数名作为「互动 TTS 无前缀」的语义锚点，调用点无需改动。
func prefixedInteractionText(text string) string {
	return text
}
