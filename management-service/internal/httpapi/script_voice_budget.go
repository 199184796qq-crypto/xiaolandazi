package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// 每小时合成字符预算：防止首小时一次性攒池把额度花光，
// 也防止客户改完话术、旧缓存全废之后又被大量重合成。
//
// 预算只算"新合成"的字符数（含原文和改写），播放复用池子不计数。
const (
	// scriptVoiceHourlyCharsDefault 每个方案每小时最多新合成多少汉字（原文 + 改写共用）。
	scriptVoiceHourlyCharsDefault = 14000
	// scriptVoiceBudgetWindow 预算窗口长度。
	scriptVoiceBudgetWindow = time.Hour
)

type scriptVoiceBudgetState struct {
	WindowStart string                         `json:"window_start"`
	Chars       int                            `json:"chars"`
	Purchases   map[string]scriptVoicePurchase `json:"purchases,omitempty"`
}

type scriptVoicePurchase struct {
	At    time.Time `json:"at"`
	Chars int       `json:"chars"`
}

var errScriptVoiceBudget = errors.New("本方案本小时新合成预算不足，继续使用已有声音")

type scriptBudgetAdmissionError struct{ error }

func (e scriptBudgetAdmissionError) Unwrap() error { return e.error }

// Reserve immediately before a new provider submission, under the same lock
// as persistence. Ambiguous outcomes remain charged; definite non-submission
// releases the reservation. Cache/archive restoration never reaches this gate.
func reserveScriptVoiceSynthesis(planID int64, key string, chars int) (func(bool), error) {
	scriptVoiceBudgetMu.Lock()
	defer scriptVoiceBudgetMu.Unlock()
	state, err := readScriptVoiceBudget(planID)
	if err != nil {
		return nil, err
	}
	if _, exists := state.Purchases[key]; exists {
		return func(bool) {}, nil
	}
	if state.Chars+chars > scriptVoiceHourlyLimit() {
		return nil, errScriptVoiceBudget
	}
	if state.Purchases == nil {
		state.Purchases = map[string]scriptVoicePurchase{}
	}
	state.Purchases[key] = scriptVoicePurchase{At: time.Now(), Chars: chars}
	state.Chars += chars
	if err := writeVolcengineJSON(scriptVoiceBudgetPath(planID), state); err != nil {
		return nil, err
	}
	return func(notSent bool) {
		if !notSent {
			return
		}
		scriptVoiceBudgetMu.Lock()
		defer scriptVoiceBudgetMu.Unlock()
		latest, err := readScriptVoiceBudget(planID)
		if err != nil {
			return
		}
		if entry, ok := latest.Purchases[key]; ok {
			latest.Chars -= entry.Chars
			delete(latest.Purchases, key)
			_ = writeVolcengineJSON(scriptVoiceBudgetPath(planID), latest)
		}
	}, nil
}

func readScriptVoiceBudget(planID int64) (scriptVoiceBudgetState, error) {
	var state scriptVoiceBudgetState
	raw, err := os.ReadFile(scriptVoiceBudgetPath(planID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &state); err != nil {
			return state, err
		}
	}
	now := time.Now()
	// Preserve the old aggregate until its recorded hour expires; do not reset
	// paid usage during the file-format migration.
	start, parseErr := time.Parse(time.RFC3339, state.WindowStart)
	legacy := state.Chars
	for _, item := range state.Purchases {
		legacy -= item.Chars
	}
	if parseErr != nil || now.Sub(start) >= time.Hour {
		legacy = 0
		state.WindowStart = now.Format(time.RFC3339)
	}
	state.Chars = max(0, legacy)
	for key, item := range state.Purchases {
		if now.Sub(item.At) >= time.Hour {
			delete(state.Purchases, key)
		} else {
			state.Chars += item.Chars
		}
	}
	return state, nil
}

var scriptVoiceBudgetMu sync.Mutex

func scriptVoiceHourlyLimit() int {
	if raw := os.Getenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return scriptVoiceHourlyCharsDefault
}

func scriptVoiceBudgetPath(planID int64) string {
	return filepath.Join(recordingCacheMediaRoot(), "script-voice-budget", strconv.FormatInt(planID, 10)+".json")
}

func loadScriptVoiceBudget(planID int64) scriptVoiceBudgetState {
	state, err := readScriptVoiceBudget(planID)
	if err != nil {
		return scriptVoiceBudgetState{Chars: scriptVoiceHourlyLimit()}
	}
	return state
}

// scriptVoiceBudgetLeft 返回当前窗口还能新合成多少字符。
func scriptVoiceBudgetLeft(planID int64) int {
	scriptVoiceBudgetMu.Lock()
	defer scriptVoiceBudgetMu.Unlock()
	limit := scriptVoiceHourlyLimit()
	used := loadScriptVoiceBudget(planID).Chars
	if used >= limit {
		return 0
	}
	return limit - used
}

// allowScriptVoiceSynthesis 判断这次合成是否在预算内，超了就拒绝。
//
// 拒绝只影响"生成新版本"，已有版本继续照常播放，不会断音。
func allowScriptVoiceSynthesis(planID int64, chars int) bool {
	if chars <= 0 {
		return true
	}
	left := scriptVoiceBudgetLeft(planID)
	if chars > left {
		log.Printf("script voice budget exhausted plan=%d need=%d left=%d", planID, chars, left)
		return false
	}
	return true
}

// recordScriptVoiceSynthesis 记一次新合成消耗。
func recordScriptVoiceSynthesis(planID int64, chars int) {
	if chars <= 0 {
		return
	}
	scriptVoiceBudgetMu.Lock()
	defer scriptVoiceBudgetMu.Unlock()
	state := loadScriptVoiceBudget(planID)
	state.Chars += chars
	if state.WindowStart == "" {
		state.WindowStart = time.Now().Format(time.RFC3339)
	}
	dir := filepath.Dir(scriptVoiceBudgetPath(planID))
	if err := os.MkdirAll(dir, 0o755); err == nil {
		if raw, err := json.Marshal(state); err == nil {
			_ = os.WriteFile(scriptVoiceBudgetPath(planID), raw, 0o644)
		}
	}
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
