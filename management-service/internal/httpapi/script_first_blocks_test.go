package httpapi

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestScriptFirstBlocksPreserveAudioAndSubtitleOrder(t *testing.T) {
	pcm := make([]int16, 165*24000)
	for i := range pcm {
		pcm[i] = int16(4000 * math.Sin(2*math.Pi*180*float64(i)/24000))
	}
	for _, s := range []int{59, 119} {
		clear(pcm[s*24000 : (s+2)*24000])
	}
	timeline := []model.LiveAgentPlanTimelineSegment{{Text: "第一段完整内容。", StartMS: 0, EndMS: 59000, SafeCut: true}, {Text: "中间完整内容。", StartMS: 61000, EndMS: 119000, SafeCut: true}, {Text: "最后完整内容。", StartMS: 121000, EndMS: 165000, SafeCut: true}}
	spans, err := scriptFirstSpans(pcm, timeline)
	if err != nil || len(spans) != 3 {
		t.Fatal(spans, err)
	}
	var joined []byte
	var text string
	var sample int64
	for _, span := range spans {
		if span.Start != sample || span.Start%480 != 0 {
			t.Fatal("gap/unaligned", span)
		}
		cues, e := scriptFirstClipTimeline(timeline, span)
		if e != nil {
			t.Fatal(e)
		}
		text += scriptFirstText(cues)
		raw := scriptFirstPCMToWAV(pcm[span.Start:span.End])
		joined = append(joined, raw[44:]...)
		if int64(binary.LittleEndian.Uint32(raw[40:])) != (span.End-span.Start)*2 {
			t.Fatal("bad WAV length")
		}
		for _, cue := range cues {
			if cue.StartMS < 0 || cue.EndMS > (span.End-span.Start)/24 {
				t.Fatal("subtitle out of child", cue)
			}
		}
		sample = span.End
	}
	if sample != int64(len(pcm)) || !bytes.Equal(joined, int16Bytes(pcm)) {
		t.Fatal("audio lost/repeated/reordered")
	}
	if text != scriptFirstText(timeline) {
		t.Fatal("subtitle lost/reordered", text)
	}
}

func TestScriptFirstDoesNotHardCutSpeechToMeetSixtySeconds(t *testing.T) {
	pcm := make([]int16, 100*24000)
	for i := range pcm {
		pcm[i] = 2000
	}
	// A quiet point inside one continuing subtitle is not a standalone boundary.
	clear(pcm[59*24000 : 61*24000])
	cues := []model.LiveAgentPlanTimelineSegment{{Text: "一整句持续内容。", StartMS: 0, EndMS: 100000}}
	spans, err := scriptFirstSpans(pcm, cues)
	if err != nil || len(spans) != 1 || spans[0].End != int64(len(pcm)) {
		t.Fatal(spans, err)
	}
}

func TestScriptFirstAliWithoutSubtitlesCanSplitAudioWithoutInventingSRT(t *testing.T) {
	pcm := make([]int16, 165*24000)
	for i := range pcm {
		pcm[i] = int16(4000 * math.Sin(2*math.Pi*180*float64(i)/24000))
	}
	for _, s := range []int{59, 119} {
		clear(pcm[s*24000 : (s+2)*24000])
	}
	spans, err := scriptFirstSpans(pcm, nil)
	if err != nil || len(spans) != 3 {
		t.Fatal(spans, err)
	}
	var joined []byte
	for _, span := range spans {
		cues, e := scriptFirstClipTimeline(nil, span)
		if e != nil || len(cues) != 0 {
			t.Fatal("invented subtitles", cues, e)
		}
		joined = append(joined, scriptFirstPCMToWAV(pcm[span.Start:span.End])[44:]...)
	}
	if !bytes.Equal(joined, int16Bytes(pcm)) {
		t.Fatal("untimed audio lost/repeated")
	}
}

func TestScriptFirstTimedParagraphWithoutCompleteChildSentenceStaysWhole(t *testing.T) {
	pcm := make([]int16, 165*24000)
	for i := range pcm {
		pcm[i] = 2000
	}
	clear(pcm[59*24000 : 61*24000])
	clear(pcm[119*24000 : 121*24000])
	cues := []model.LiveAgentPlanTimelineSegment{{Text: "第一小节，", StartMS: 0, EndMS: 59000, SafeCut: true}, {Text: "第二小节，", StartMS: 61000, EndMS: 119000, SafeCut: true}, {Text: "最后收口。", StartMS: 121000, EndMS: 165000, SafeCut: true}}
	spans, err := scriptFirstPlayableSpans(pcm, cues)
	if err != nil || len(spans) != 1 {
		t.Fatal("invalid child archive would stall supply", spans, err)
	}
}

func TestScriptFirstBlockIndexBindsModeParentAndCompleteSampleRange(t *testing.T) {
	audio := volcengineCachedAudio{AssetID: 4, DurationMS: 1000, UntimedTranscript: true}
	index := scriptFirstBlockIndex{Version: 1, Key: "key", ParentAssetID: 4, SourceSHA: strings.Repeat("a", 64), UntimedTranscript: true, Blocks: []scriptFirstCachedBlock{{AssetID: 5, StartSample: 0, EndSample: 24000, DurationMS: 1000}}}
	if !validScriptFirstBlockIndex(index, "key", audio) {
		t.Fatal("valid index rejected")
	}
	for _, mutate := range []func(*scriptFirstBlockIndex){func(x *scriptFirstBlockIndex) { x.ParentAssetID++ }, func(x *scriptFirstBlockIndex) { x.UntimedTranscript = false }, func(x *scriptFirstBlockIndex) {
		x.Blocks = append([]scriptFirstCachedBlock(nil), x.Blocks...)
		x.Blocks[0].StartSample = 480
	}, func(x *scriptFirstBlockIndex) { x.SourceSHA = "unbound" }} {
		bad := index
		mutate(&bad)
		if validScriptFirstBlockIndex(bad, "key", audio) {
			t.Fatal("mismatched index accepted", bad)
		}
	}
}

func TestScriptFirstWAVRejectsNonPCMAndTruncatedButAcceptsOddChunks(t *testing.T) {
	raw := scriptFirstPCMToWAV([]int16{1, -1, 20, -20})
	withJunk := append([]byte(nil), raw[:36]...)
	withJunk = append(withJunk, []byte{'J', 'U', 'N', 'K', 1, 0, 0, 0, 9, 0}...)
	withJunk = append(withJunk, raw[36:]...)
	binary.LittleEndian.PutUint32(withJunk[4:], uint32(len(withJunk)-8))
	path := filepath.Join(t.TempDir(), "audio.wav")
	if err := os.WriteFile(path, withJunk, 0600); err != nil {
		t.Fatal(err)
	}
	if pcm, rate, err := readWAVPCM16Mono(path); err != nil || rate != 24000 || len(pcm) != 4 {
		t.Fatal(pcm, rate, err)
	}
	bad := append([]byte(nil), raw...)
	binary.LittleEndian.PutUint16(bad[20:], 3)
	for _, audio := range [][]byte{bad, raw[:len(raw)-2]} {
		if err := os.WriteFile(path, audio, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readWAVPCM16Mono(path); err == nil {
			t.Fatal("invalid WAV accepted")
		}
	}
}
