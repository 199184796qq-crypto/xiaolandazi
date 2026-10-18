package decisionexecutor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"livecompanion/management/internal/model"
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
	if tts.calls != 1 || tts.last.Text != "****"+body || w.faqCache.buckets["prefix"][0].Text != body {
		t.Fatal("TTS prefix prevented reuse or polluted the stored spoken body", tts.calls, tts.last.Text)
	}
	req.VoiceID = "voice-2"
	if _, err := w.synthesizeFAQAudio(context.Background(), item, req); err != nil || tts.calls != 2 {
		t.Fatal("changed voice reused old synthesized audio", err, tts.calls)
	}
}

func TestFAQChangedGreetingKeepsDistinctAnswerIdentity(t *testing.T) {
	w, _, core, agent, _, _ := newFAQTestWorker()
	session := model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
	// Keep addressing selection stable; only the anti-repeat greeting changes.
	for _, id := range []string{"faq-fixture-11", "faq-fixture-11"} {
		claim := claimResponse{Claimed: true, Item: &decisionItem{ID: id, Topic: "FAMILY:价格费用", Title: "商品咨询", MissionKind: "reply_chat", SampleQuestions: []string{"多少钱"}}}
		raw, _ := json.Marshal(claim)
		core.claimRaw = string(raw)
		if err := w.processRoom(context.Background(), session); err != nil {
			t.Fatal(err)
		}
	}
	if agent.calls != 2 || len(w.faqCache.buckets) != 2 {
		t.Fatal("changed presentation constraints reused a different request", agent.calls, len(w.faqCache.buckets))
	}
}
