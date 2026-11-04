package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const scriptFirstMinRunes = 200
const scriptFirstMaxRunes = 2000

// All audio children remain in the download. Their shared untimed transcript
// is emitted once per playback instance, not once per 60s child.
var scriptFirstUntimedRefPattern = regexp.MustCompile(`^(mainline:raw:[0-9a-f]{64}):b[0-9]+(?::.*)?$`)

func scriptFirstUntimedPlaybackGroup(ref string) string {
	match := scriptFirstUntimedRefPattern.FindStringSubmatch(ref)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

// Persist only acknowledged children. Retrying an ambiguous handoff uses the
// same child ID and Core's existing append deduplication.
func publishScriptFirstBlocks(ctx context.Context, pending *volcenginePendingTrack, blocks []scriptFirstCachedBlock, deliver func(context.Context, int, scriptFirstCachedBlock) error, save func() error) error {
	for pending.NextBlock < len(blocks) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deliver(ctx, pending.NextBlock, blocks[pending.NextBlock]); err != nil {
			return err
		}
		pending.NextBlock++
		if err := save(); err != nil {
			return err
		}
	}
	return nil
}

func usesScriptFirstParagraphs(v model.LiveAgentPlanVersion) bool {
	return recordingVoiceMode(v) == recordingStreamMode && ttsgateway.SupportsScriptParagraphs(v.VoiceIdentity.Model)
}

// Limits apply to independent ==== paragraphs, not the combined text box or
// uploaded original recordings. Never truncate, merge or rewrite user text.
func validateScriptFirstTexts(texts []string) error {
	count := 0
	for tab, text := range texts {
		for i, part := range splitVolcengineParagraphs(text) {
			n := utf8.RuneCountInString(part)
			if n < scriptFirstMinRunes || n > scriptFirstMaxRunes {
				return fmt.Errorf("第%d个文本框第%d段为%d字；AI话术每段须为200到2000字，请用====分隔完整段落", tab+1, i+1, n)
			}
			count++
		}
	}
	if count == 0 {
		return fmt.Errorf("话术优先没有可播放的正文")
	}
	return nil
}

func scriptFirstParagraphKey(v model.LiveAgentPlanVersion, text string) string {
	legacy := scriptFirstParagraphLegacyKey(v, text)
	if stamp, ok := scriptVersionStamp(v); ok {
		return stamp.Key(legacy)
	}
	return legacy
}

func scriptFirstParagraphLegacyKey(v model.LiveAgentPlanVersion, text string) string {
	if v.VoiceIdentity.Model == ttsgateway.VolcengineICL2 {
		return volcengineParagraphKey(v.VoiceIdentity.VoiceID, text, v.VoiceIdentity.Rate)
	}
	// 版本令牌带 mandarin：指令加入普通话约束后，旧缓存（仍会夹带四川话）必须失效重合成，
	// 否则已付费的飘音会一直被复用。改了指令就要同步改这个令牌。
	raw, _ := json.Marshal([]any{"script-paragraph-v1", ttsgateway.ProviderForModel(v.VoiceIdentity.Model), v.VoiceIdentity.Model, v.VoiceIdentity.VoiceID, v.VoiceIdentity.Rate, text, recordingInstructionForVersion(v, text), "untimed-transcript-v2-mandarin"})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func scriptFirstUnits(v model.LiveAgentPlanVersion, dir string) ([]volcengineScriptUnit, error) {
	texts := recordingStreamTexts(v)
	if hasGroupedRecordingScript(v) {
		var units []volcengineScriptUnit
		// 版本池按条目 ID 建键：文案改了，已生成的版本还能继续用。
		pools := scriptFirstEntryPoolIDs(v)
		for i, text := range texts {
			key := scriptFirstParagraphKey(v, text)
			pool := ""
			if i < len(pools) {
				pool = pools[i]
			}
			units = append(units, volcengineScriptUnit{Text: text, Key: key, Pool: pool, Dir: filepath.Join(dir, key)})
		}
		// 首条按方案保存时定下的开场锚点：优先固定标签，其次第一个标签，标签内取最短。
		// 没有锚点（老方案）时退回"全局最短"，保持旧行为。
		first := 0
		if anchor := scriptOpeningUnitIndex(v, units); anchor >= 0 {
			first = anchor
		} else {
			for i := range units {
				if utf8.RuneCountInString(units[i].Text) < utf8.RuneCountInString(units[first].Text) {
					first = i
				}
			}
		}
		if first > 0 {
			unit := units[first]
			copy(units[1:first+1], units[:first])
			units[0] = unit
		}
		return units, nil
	}
	if err := validateScriptFirstTexts(texts); err != nil {
		return nil, err
	}
	var units []volcengineScriptUnit
	for _, text := range texts {
		for _, part := range splitVolcengineParagraphs(text) {
			key := scriptFirstParagraphKey(v, part)
			units = append(units, volcengineScriptUnit{Text: part, Key: key, Pool: scriptFirstParagraphLegacyKey(v, part), Dir: filepath.Join(dir, key)})
		}
	}
	return units, nil
}

func hasGroupedRecordingScript(v model.LiveAgentPlanVersion) bool {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	return ok && script.Version == 2
}

// IDs are per playback instance; source paragraph identity survives child
// blocks. Never select an arbitrary child as an independent random paragraph.
func scriptFirstTrackID(session int64, sequence int, unit, block int) string {
	return fmt.Sprintf("long-%d-%d-u%d-b%d", session, sequence, unit, block)
}
func scriptFirstPlayingUnit(trackID string, session int64) int {
	var sid int64
	var seq, unit, block int
	if n, err := fmt.Sscanf(trackID, "long-%d-%d-u%d-b%d", &sid, &seq, &unit, &block); err == nil && n == 4 && sid == session && unit >= 0 {
		return unit
	}
	return -1
}
func scriptFirstFallback(known map[int]volcengineCachedAudio, current, last int, rng *rand.Rand) (int, bool) {
	var candidates []int
	// Stable input order makes test/replay selection independent of map order.
	maxIndex := -1
	for i := range known {
		if i > maxIndex {
			maxIndex = i
		}
	}
	for i := 0; i <= maxIndex; i++ {
		if a, ok := known[i]; ok && a.AssetID > 0 && i != current && i != last {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		for i := 0; i <= maxIndex; i++ {
			if a, ok := known[i]; ok && a.AssetID > 0 && i != current {
				candidates = append(candidates, i)
			}
		}
	}
	if len(candidates) == 0 {
		for i := 0; i <= maxIndex; i++ {
			if a, ok := known[i]; ok && a.AssetID > 0 {
				candidates = append(candidates, i)
			}
		}
	}
	if len(candidates) == 0 {
		return 0, false
	}
	return candidates[rng.Intn(len(candidates))], true
}

func scriptFirstRetryDelay(attempt int, rng *rand.Rand) time.Duration {
	base := time.Second << min(max(attempt, 0), 3)
	return time.Duration(float64(base) * (.8 + .4*rng.Float64()))
}

func scriptFirstText(timeline []model.LiveAgentPlanTimelineSegment) string {
	var text strings.Builder
	for _, cue := range timeline {
		text.WriteString(cue.Text)
	}
	return text.String()
}

// scriptOpeningUnitIndex 找出方案保存时定下的开场片段在 units 里的下标。
//
// 用文本指纹对回原片段：锚点只存了 ID，跨版本 ID 可能变化，指纹更稳。
func scriptOpeningUnitIndex(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) int {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.OpeningEntryID == "" {
		return -1
	}
	var text string
	for _, tab := range script.Tabs {
		if script.OpeningTabID != "" && tab.TabID != script.OpeningTabID {
			continue
		}
		for _, entry := range tab.Entries {
			if entry.EntryID == script.OpeningEntryID {
				text = strings.TrimSpace(entry.Text)
			}
		}
	}
	if text == "" {
		return -1
	}
	want := scriptFirstParagraphKey(v, text)
	for i, unit := range units {
		if unit.Key == want {
			return i
		}
	}
	return -1
}

// scriptFirstEntryPoolIDs 按 recordingStreamTexts 的顺序返回每个条目的版本池键。
//
// 用 entry_id（前端生成、改文案不变）而不是文本哈希，避免改一个字就把
// 已付费生成的改写版本全部作废。没有 entry_id 的旧数据返回空，退回文本哈希。
func scriptFirstEntryPoolIDs(v model.LiveAgentPlanVersion) []string {
	script, ok := recordingScriptFromContext(v.GenerationContext)
	if !ok || script.Version != 2 {
		return nil
	}
	var out []string
	for tabIndex, tab := range script.Tabs {
		// entry_id 只在标签内唯一（每个标签都从 s1 开始），必须带上标签身份。
		tabKey := strings.TrimSpace(tab.TabID)
		if tabKey == "" {
			tabKey = fmt.Sprintf("t%d", tabIndex)
		}
		if tab.Entries != nil {
			for _, entry := range tab.Entries {
				if strings.TrimSpace(entry.Text) == "" {
					continue
				}
				if strings.TrimSpace(entry.EntryID) != "" {
					out = append(out, "e-"+tabKey+"-"+strings.TrimSpace(entry.EntryID))
				} else {
					out = append(out, "")
				}
			}
			continue
		}
		if strings.TrimSpace(tab.Text) != "" {
			out = append(out, "")
		}
	}
	return out
}
func scriptEntryPoolID(tab recordingScriptTab, index int, entryID string) string {
	tabKey := strings.TrimSpace(tab.TabID)
	if tabKey == "" {
		tabKey = fmt.Sprintf("t%d", index)
	}
	if strings.TrimSpace(entryID) == "" {
		return ""
	}
	return "e-" + tabKey + "-" + strings.TrimSpace(entryID)
}
