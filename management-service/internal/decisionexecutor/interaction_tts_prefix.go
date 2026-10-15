package decisionexecutor

import "strings"

// interactionTTSPrefix 是互动话术递交给 TTS 合成前统一加在开头的前缀。
// 这是一个独立的包装单元：以后要换前缀（换符号、换停顿标记、或去掉），
// 只改这个文件这一处，不用动任何合成/业务调用点。
const interactionTTSPrefix = "****"

// prefixedInteractionText 是互动 TTS 的前缀包装：给话术加固定前缀，重复调用不叠加。
// 主线/开场话术不经过此包装（它们直接合成、无前缀）；这是互动独有的表现层处理，
// 与 cosyvoice 合成逻辑解耦。
func prefixedInteractionText(text string) string {
	if strings.HasPrefix(text, interactionTTSPrefix) {
		return text
	}
	return interactionTTSPrefix + text
}
