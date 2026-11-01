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
	if v.VoiceIdentity.Model == ttsgateway.VolcengineICL2 {
		return volcengineParagraphKey(v.VoiceIdentity.VoiceID, text, v.VoiceIdentity.Rate)
	}
	raw, _ := json.Marshal([]any{"script-paragraph-v1", ttsgateway.ProviderForModel(v.VoiceIdentity.Model), v.VoiceIdentity.Model, v.VoiceIdentity.VoiceID, v.VoiceIdentity.Rate, text, recordingInstructionForVersion(v, text), "untimed-transcript-v1"})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func scriptFirstUnits(v model.LiveAgentPlanVersion, dir string) ([]volcengineScriptUnit, error) {
	texts := recordingStreamTexts(v)
	if err := validateScriptFirstTexts(texts); err != nil {
		return nil, err
	}
	var units []volcengineScriptUnit
	for _, text := range texts {
		for _, part := range splitVolcengineParagraphs(text) {
			key := scriptFirstParagraphKey(v, part)
			units = append(units, volcengineScriptUnit{Text: part, Key: key, Dir: filepath.Join(dir, key)})
		}
	}
	return units, nil
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
