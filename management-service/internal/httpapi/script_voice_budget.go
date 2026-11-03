package httpapi

import (
	"errors"
	"path/filepath"
	"strconv"
	"unicode/utf8"
)

// Retained for old paid-job diagnostics only. Hourly character limits and the
// XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS setting no longer control any request.
var errScriptVoiceBudget = errors.New("旧版话术合成小时额度错误")

type scriptBudgetAdmissionError struct{ error }

func (e scriptBudgetAdmissionError) Unwrap() error { return e.error }

// Paid-job identity and the request cost ledger retain purchase fences. The
// retired hourly usage files are neither read nor changed when generating.
func reserveScriptVoiceSynthesis(planID int64, key string, chars int) (func(bool), error) {
	return func(bool) {}, nil
}
func scriptVoiceHourlyLimit() int                            { return 0 }
func scriptVoiceBudgetLeft(planID int64) int                 { return 0 }
func allowScriptVoiceSynthesis(planID int64, chars int) bool { return true }
func recordScriptVoiceSynthesis(planID int64, chars int)     {}
func scriptVoiceBudgetPath(planID int64) string {
	return filepath.Join(recordingCacheMediaRoot(), "script-voice-budget", strconv.FormatInt(planID, 10)+".json")
}

// hanCount 只数汉字：预算按汉字算，标点和数字不计。
func hanCount(text string) int {
	count := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			count++
		}
	}
	return count
}

// runeCount 保留：日志里偶尔要看总字符数。
func runeCount(text string) int {
	return utf8.RuneCountInString(text)
}
