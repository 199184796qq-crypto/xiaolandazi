package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/ttsgateway"
)

// Optional local-only verification against the already paid long-text test.
// It reads WAV/JSON files and runs ffmpeg; no network/model/TTS is involved.
func TestVolcengineExistingLongAudioOffline(t *testing.T) {
	dir := os.Getenv("VOLCENGINE_LONG_OFFLINE_FIXTURE")
	if dir == "" {
		t.Skip("existing offline audio fixture not requested")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Sentences []ttsgateway.SpeechSubtitle `json:"sentences"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "测试文本.txt"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "长文本单次合成.wav")
	duration, err := volcengineWAVDuration(path)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := volcengineParagraphTimeline(strings.TrimPrefix(string(text), "\ufeff"), result.Sentences, duration)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) < 10 {
		t.Fatal("native phrase boundaries lost", len(timeline))
	}
	for _, pct := range []int{90, 110} {
		rendered, e := renderVolcengineTempo(t.Context(), path, pct)
		if e != nil {
			t.Fatal(e)
		}
		newDuration, e := volcengineWAVDuration(rendered)
		if e != nil {
			t.Fatal(e)
		}
		out, e := stretchVolcengineTimeline(timeline, duration, newDuration)
		if e != nil {
			t.Fatal(e)
		}
		if math.Abs(float64(newDuration)-float64(duration)*100/float64(pct)) > 100 {
			t.Fatal("unexpected rendered duration", pct, newDuration)
		}
		if output := os.Getenv("VOLCENGINE_LONG_OFFLINE_OUTPUT"); output != "" {
			if e = os.MkdirAll(output, 0750); e != nil {
				t.Fatal(e)
			}
			name := filepath.Join(output, fmt.Sprintf("tempo-%d.srt", pct))
			if e = os.WriteFile(name, []byte(liveAgentTimelineSRT(out)), 0600); e != nil {
				t.Fatal(e)
			}
			track := coreclient.ProgramRefreshTrack{ID: fmt.Sprintf("native-%d", pct), Text: string(text), DurationMS: int(newDuration), Timeline: recordingStreamTimeline(out), SafePoints: recordingStreamSafePoints(volcengineSemanticSafePoints(out))}
			trackRaw, e := json.MarshalIndent(track, "", "  ")
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(output, fmt.Sprintf("core-track-%d.json", pct)), trackRaw, 0600); e != nil {
				t.Fatal(e)
			}
		}
		t.Logf("tempo=%d source_ms=%d rendered_ms=%d cues=%d semantic_safe_points=%d", pct, duration, newDuration, len(out), len(volcengineSemanticSafePoints(out)))
		if e = os.Remove(rendered); e != nil {
			t.Fatal(e)
		}
	}
}

func TestVolcengineNativeWordTimelineAndTempoSRT(t *testing.T) {
	subs := []ttsgateway.SpeechSubtitle{{Text: "你好，欢迎。", Start: .2, End: 4, Words: []ttsgateway.SpeechWord{
		{Text: "你好，", Start: .2, End: 1.7}, {Text: "欢迎。", Start: 1.9, End: 3.9},
	}}}
	timeline, err := volcengineParagraphTimeline("你好，\n欢迎。", subs, 4000)
	if err != nil || len(timeline) != 2 || timeline[0].StartMS != 200 || timeline[0].EndMS != 1700 || timeline[1].EndMS != 3900 {
		t.Fatal(timeline, err)
	}
	for _, duration := range []int64{4444, 3636} {
		out, e := stretchVolcengineTimeline(timeline, 4000, duration)
		if e != nil {
			t.Fatal(e)
		}
		if out[1].EndMS != int64(math.Round(3900*float64(duration)/4000)) || len(volcengineSemanticSafePoints(out)) != 1 {
			t.Fatal(out)
		}
		if srt := liveAgentTimelineSRT(out); !strings.Contains(srt, formatSRTMillis(out[1].EndMS)) || !strings.Contains(srt, "欢迎。") {
			t.Fatal(srt)
		}
	}
	if timeline[1].EndMS != 3900 {
		t.Fatal("source mutated")
	}
	if _, err = volcengineParagraphTimeline("不同原文", subs, 4000); err == nil {
		t.Fatal("wrong subtitle bound to audio")
	}
	subs[0].Words[1].Start = 1
	if _, err = volcengineParagraphTimeline("你好，欢迎。", subs, 4000); err == nil {
		t.Fatal("overlapping timestamps accepted")
	}
}

func TestVolcengineParagraphEndWithoutPunctuationAndCoreSafePoints(t *testing.T) {
	timeline, err := volcengineParagraphTimeline("欢迎来到直播间", []ttsgateway.SpeechSubtitle{{Text: "欢迎来到直播间", Start: 0, End: 2}}, 2000)
	if err != nil || timeline[0].Text != "欢迎来到直播间。" || len(volcengineSemanticSafePoints(timeline)) != 1 {
		t.Fatal("paragraph ending cannot reach Core", timeline, err)
	}
	for _, point := range volcengineSemanticSafePoints(timeline) {
		if !volcengineSentenceEnding(point.LeftPreview) {
			t.Fatal("Core rejects this explicit safe point", point)
		}
	}
}
func TestVolcengineTimestampFallbackKeepsVendorSentences(t *testing.T) {
	timeline, err := volcengineParagraphTimeline("一段。二段。", []ttsgateway.SpeechSubtitle{{Text: "一段。二段。", Start: 0, End: 2}}, 2000)
	if err != nil || len(timeline) != 1 || timeline[0].EndMS != 2000 {
		t.Fatal(timeline, err)
	}
	if _, err = volcengineParagraphTimeline("原文", nil, 2000); err == nil {
		t.Fatal("invented timing")
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), -1, 3} {
		if _, err = speechTimeMS(v, 2000); err == nil {
			t.Fatal("bad timestamp accepted", v)
		}
	}
}
