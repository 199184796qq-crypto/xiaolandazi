package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func unitOfRunes(n int) volcengineScriptUnit {
	return volcengineScriptUnit{Text: strings.Repeat("鸭", n)}
}

// 预取余量必须随下一段字数增长，短段仍用 40 秒下限。
func TestScriptFirstSupplyLeadScalesWithLength(t *testing.T) {
	cases := []struct {
		runes int
		want  int64
	}{
		{200, 40000},  // 200*26+2000+15000=22200 → 低于下限
		{400, 40000},  // 400*26+17000=27400 → 低于下限
		{940, 41440},  // 940*26+2000+15000
		{2000, 69000}, // 2000*26+17000=69000
	}
	for _, tc := range cases {
		units := []volcengineScriptUnit{unitOfRunes(tc.runes)}
		got := scriptFirstSupplyLead(units, 0, ttsgateway.ProviderQwen+"-model", true)
		if got != tc.want {
			t.Fatalf("%d 字余量 %d，至少应为 %d", tc.runes, got, tc.want)
		}
	}
}

func TestSilenceWaitsForDrainAndOnlyFallsBackAfterTwentySeconds(t *testing.T) {
	for _, ms := range []int64{15000, 14999, 2000, 1} {
		for _, waiting := range []bool{false, true} {
			pad, fallback := scriptFirstSilenceDue(coreclient.ProgramRefreshSnapshot{BufferedMainlineMS: ms, WaitingForContent: waiting}, 10)
			if pad || fallback {
				t.Fatalf("premature replay/padding with %dms remaining", ms)
			}
		}
	}
	cursor := volcengineScriptCursor{}
	accepted := map[string]bool{}
	for block := 0; block < 10; block++ {
		pad, fallback := scriptFirstSilenceDue(coreclient.ProgramRefreshSnapshot{WaitingForContent: true}, cursor.SilenceBlocks)
		if !pad || fallback {
			t.Fatal("silence budget not honored", block)
		}
		if err := deliverScriptFirstSilence(t.Context(), 17, &cursor, "https://example.invalid/silence.wav", func() error { return nil }, func(_ context.Context, track coreclient.ProgramRefreshTrack) error {
			if accepted[track.ID] || track.DurationMS != 2000 || track.Text != "" || !track.UntimedTranscript || len(track.Timeline) != 0 {
				t.Fatal("duplicate/false-caption silence", track)
			}
			accepted[track.ID] = true
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	pad, fallback := scriptFirstSilenceDue(coreclient.ProgramRefreshSnapshot{WaitingForContent: true}, cursor.SilenceBlocks)
	if pad || !fallback || len(accepted) != 10 {
		t.Fatal("must permit fallback only after 10 drained blocks")
	}
}

func TestSilenceLostAckAndRestartReuseIdenticalRequest(t *testing.T) {
	cursor := volcengineScriptCursor{SilenceAssetID: 9}
	var persisted []byte
	save := func() error { var e error; persisted, e = json.Marshal(cursor); return e }
	var first coreclient.ProgramRefreshTrack
	lost := errors.New("accepted; response lost")
	err := deliverScriptFirstSilence(t.Context(), 12, &cursor, "https://example.invalid/first", save, func(_ context.Context, track coreclient.ProgramRefreshTrack) error { first = track; return lost })
	if !errors.Is(err, lost) || cursor.SilenceBlocks != 0 || cursor.PendingSilence == nil {
		t.Fatal(cursor, err)
	}
	var restored volcengineScriptCursor
	if err = json.Unmarshal(persisted, &restored); err != nil {
		t.Fatal(err)
	}
	cursor = restored
	err = deliverScriptFirstSilence(t.Context(), 12, &cursor, "https://example.invalid/refreshed", save, func(_ context.Context, track coreclient.ProgramRefreshTrack) error {
		a, _ := json.Marshal(first)
		b, _ := json.Marshal(track)
		if string(a) != string(b) {
			t.Fatal("append body changed on recovery")
		}
		return nil
	})
	if err != nil || cursor.SilenceBlocks != 1 || cursor.PendingSilence != nil || cursor.Sequence != 1 || cursor.SilenceAssetID != 9 {
		t.Fatal(cursor, err)
	}
}

func TestSilenceDoesNotDeliverBeforeCheckpointOrAfterStop(t *testing.T) {
	cursor := volcengineScriptCursor{}
	calls := 0
	deliver := func(context.Context, coreclient.ProgramRefreshTrack) error { calls++; return nil }
	err := deliverScriptFirstSilence(t.Context(), 1, &cursor, "url", func() error { return errors.New("disk full") }, deliver)
	if err == nil || calls != 0 || cursor.SilenceBlocks != 0 {
		t.Fatal("delivered without durable checkpoint")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = deliverScriptFirstSilence(ctx, 1, &cursor, "url", func() error { return nil }, deliver)
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("delivered after stop", err)
	}
}

func TestSilenceRetriesFailedCheckpointBeforeDelivering(t *testing.T) {
	cursor := volcengineScriptCursor{}
	fail := true
	durable := false
	calls := 0
	save := func() error {
		if fail {
			return errors.New("disk full")
		}
		durable = true
		return nil
	}
	deliver := func(context.Context, coreclient.ProgramRefreshTrack) error {
		if !durable {
			t.Fatal("silence was sent before its checkpoint became durable")
		}
		calls++
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := deliverScriptFirstSilence(t.Context(), 1, &cursor, "same-url", save, deliver); err == nil || calls != 0 || cursor.SilenceBlocks != 0 {
			t.Fatal("failed checkpoint retried unsafely", err)
		}
	}
	fail = false
	if err := deliverScriptFirstSilence(t.Context(), 1, &cursor, "same-url", save, deliver); err != nil || calls != 1 || cursor.Sequence != 1 || cursor.SilenceBlocks != 1 {
		t.Fatal(cursor, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	other := volcengineScriptCursor{}
	if err := deliverScriptFirstSilence(ctx, 1, &other, "url", func() error { cancel(); return nil }, deliver); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal("sent after cancellation during checkpoint", err)
	}
}

// 复用段是现成的，不能因为字数把余量抬高。
func TestScriptFirstSupplyLeadReuseUsesFloor(t *testing.T) {
	units := []volcengineScriptUnit{unitOfRunes(2000)}
	if got := scriptFirstSupplyLead(units, 0, "", false); got != volcengineSupplyThresholdMS {
		t.Fatalf("复用段余量应为 %d，实际 %d", volcengineSupplyThresholdMS, got)
	}
	if got := scriptFirstSupplyLead(units, 5, "", true); got != volcengineSupplyThresholdMS {
		t.Fatalf("越界索引应回落到下限，实际 %d", got)
	}
}

// 火山的每字耗时更低，同样是 940 字，余量应小于阿里。
func TestScriptFirstSupplyLeadVolcengineFaster(t *testing.T) {
	units := []volcengineScriptUnit{unitOfRunes(940)}
	qwen := scriptFirstSupplyLead(units, 0, "qwen-audio-3.1-tts-flash", true)
	volc := scriptFirstSupplyLead(units, 0, ttsgateway.VolcengineICL2, true)
	if volc >= qwen {
		t.Fatalf("火山余量 %d 应小于阿里 %d", volc, qwen)
	}
}

// 静音垫片：2 秒一块，最多 20 秒，超了才允许替补。
func TestSilencePadBudget(t *testing.T) {
	if scriptFirstSilenceBlockMS*scriptFirstSilenceMaxBlock != 20000 {
		t.Fatalf("静音上限应为 20 秒，实际 %d 毫秒", scriptFirstSilenceBlockMS*scriptFirstSilenceMaxBlock)
	}
	cursor := volcengineScriptCursor{}
	for i := 0; i < scriptFirstSilenceMaxBlock; i++ {
		if cursor.SilenceBlocks >= scriptFirstSilenceMaxBlock {
			t.Fatalf("第 %d 块时不该再补静音", i+1)
		}
		cursor.SilenceBlocks++
	}
	if cursor.SilenceBlocks < scriptFirstSilenceMaxBlock {
		t.Fatalf("应能补满 %d 块，实际 %d", scriptFirstSilenceMaxBlock, cursor.SilenceBlocks)
	}
}

// 静音块是 2 秒 24kHz 单声道 16 位的合法 WAV。
func TestSilenceWAVIsTwoSeconds(t *testing.T) {
	raw := scriptFirstSilenceWAV()
	if len(raw) != 44+24000*2*2 {
		t.Fatalf("静音 WAV 长度 %d，期望 %d", len(raw), 44+24000*2*2)
	}
	if string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Fatalf("静音 WAV 头不合法")
	}
	for i := 44; i < len(raw); i++ {
		if raw[i] != 0 {
			t.Fatalf("第 %d 字节不是静音", i)
		}
	}
}

func TestMandarinInstructionInvalidatesOnlyQwenParagraphCache(t *testing.T) {
	v := model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "clone", Rate: 1}}
	text := "同一个主播完整朗读这一段话术。"
	raw, _ := json.Marshal([]any{"script-paragraph-v1", ttsgateway.ProviderForModel(v.VoiceIdentity.Model), v.VoiceIdentity.Model, v.VoiceIdentity.VoiceID, v.VoiceIdentity.Rate, text, recordingInstructionForVersion(v, text), "untimed-transcript-v1"})
	sum := sha256.Sum256(raw)
	if scriptFirstParagraphKey(v, text) == hex.EncodeToString(sum[:]) {
		t.Fatal("old accent cache still matches")
	}
	req := withNaturalBreathInstruction(ttsgateway.SynthesizeRequest{Text: text, Instruction: "原有表达要求"})
	if req.Text != text || !strings.Contains(req.Instruction, "原有表达要求") || !strings.Contains(req.Instruction, "请用标准普通话朗读，不要带方言口音") {
		t.Fatal("spoken text/style/Mandarin constraint lost")
	}
	v.VoiceIdentity.Model = ttsgateway.VolcengineICL2
	if scriptFirstParagraphKey(v, text) != volcengineParagraphKey(v.VoiceIdentity.VoiceID, text, v.VoiceIdentity.Rate) {
		t.Fatal("Volcengine cache unnecessarily invalidated")
	}
}
