package decisionexecutor

import (
	"context"
	"testing"
	"time"

	"livecompanion/management/internal/ttsgateway"
)

func TestFAQAudioPrefixKeepsBodyIdentityAndVoiceFingerprint(t *testing.T) {
	w, _, _, _, tts, now := newFAQTestWorker()
	body := "按页面已确认的规格选择。"
	expires := now.Add(time.Hour)
	w.faqCache.buckets["prefix"] = []faqAnswerVariant{{Text: body, ExpiresAt: expires}}
	item := &decisionItem{faqCacheTicket: &faqCacheTicket{TenantID: 7, Key: "prefix", Text: body, ExpiresAt: expires}}
	req := ttsgateway.SynthesizeRequest{Text: body, VoiceID: "voice-1"}
	for range 2 {
		if _, err := w.synthesizeFAQAudio(context.Background(), item, req); err != nil {
			t.Fatal(err)
		}
	}
	if tts.calls != 1 || tts.last.Text != body || w.faqCache.buckets["prefix"][0].Text != body {
		t.Fatal("TTS prefix prevented reuse or polluted the stored spoken body", tts.calls, tts.last.Text)
	}
	req.VoiceID = "voice-2"
	if _, err := w.synthesizeFAQAudio(context.Background(), item, req); err != nil || tts.calls != 2 {
		t.Fatal("changed voice reused old synthesized audio", err, tts.calls)
	}
}
