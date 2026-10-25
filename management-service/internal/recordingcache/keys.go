package recordingcache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ScriptHash 是整份话术的指纹：标签正文按顺序拼接后取 sha256。
// 任何一段话术改动，hash 就变，旧缓存自然失效。
func ScriptHash(texts []string) string {
	sum := sha256.New()
	for index, text := range texts {
		if index > 0 {
			sum.Write([]byte("\x1e"))
		}
		sum.Write([]byte(strings.TrimSpace(text)))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// VoiceKey 是声音指纹：音色、模型、语速、情感开关任何一项变化，
// 已合成音频都不能再用，所以要一起参与计算。
// ContentRevisionKey ties the cache to facts/rules instead of the whole script.
func ContentRevisionKey(revision string) string {
	return VoiceKey("recording-v2", revision)
}

func VoiceKey(parts ...string) string {
	sum := sha256.New()
	for index, part := range parts {
		if index > 0 {
			sum.Write([]byte("\x1e"))
		}
		sum.Write([]byte(strings.TrimSpace(part)))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// SentenceKey 是单句指纹，用于音频文件名。
func SentenceKey(spoken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(spoken)))
	return hex.EncodeToString(sum[:])[:32]
}
