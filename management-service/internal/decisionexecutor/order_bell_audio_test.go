package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type orderBellArchiveStore struct {
	*v2Store
	history []model.GeneratedSpeechHistoryInput
	assets  []model.SessionSpeechAsset
}

func claimOrderBellOriginal(core *fakeCore, e interactionplan.Envelope, original string) {
	claimV2(core, e)
	var claim map[string]any
	if err := json.Unmarshal([]byte(core.claimRaw), &claim); err != nil {
		panic(err)
	}
	claim["item"].(map[string]any)["sample_questions"] = []string{original}
	raw, _ := json.Marshal(claim)
	core.claimRaw = string(raw)
}

func (s *orderBellArchiveStore) RecordGeneratedSpeechHistory(_ context.Context, input model.GeneratedSpeechHistoryInput) error {
	s.history = append(s.history, input)
	return nil
}
func (s *orderBellArchiveStore) SaveSessionSpeechAsset(_ context.Context, asset model.SessionSpeechAsset) error {
	s.assets = append(s.assets, asset)
	return nil
}
func (s *orderBellArchiveStore) MarkSessionSpeechPlayed(context.Context, int64, int64, int64, string, time.Time) error {
	return nil
}

func TestOrderBellRealWorkerComposesOnlyAfterTTSAndRetainsOneCombinedAudio(t *testing.T) {
	w, store, core, agent, tts, session, e := v2Fixture(t, 93001)
	archive := &orderBellArchiveStore{v2Store: store}
	w.store = archive
	e.Intent.Type = "下单意向"
	e.Body = "收到，感谢支持。"
	claimOrderBellOriginal(core, e, "已拍五香")
	calls := 0
	w.orderAudioComposer = func(_ context.Context, source string) (ttsgateway.OrderBellAudio, error) {
		calls++
		if tts.calls != 1 || source != "http://audio.local/reply.wav" || strings.Contains(tts.last.Text, "叮咚") {
			t.Fatal("bell must follow one human-speech synthesis", tts.calls, tts.last.Text, source)
		}
		return ttsgateway.OrderBellAudio{AudioURL: "http://audio.local/bell-and-speech.wav", BellDurationMS: 1416, GapDurationMS: 100, DurationMS: 2516}, nil
	}
	core.dispatchStatus = 409
	core.dispatchRaw = `{"error":"当前35秒内没有安全句末切点，继续排队"}`
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	core.dispatchStatus = 0
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || tts.calls != 1 || agent.calls != 0 || core.dispatch["audio_url"] != "http://audio.local/bell-and-speech.wav" || len(archive.history) != 1 {
		t.Fatal("combined audio was lost, duplicated or regenerated", calls, tts.calls, agent.calls, core.dispatch)
	}
	if len(archive.assets) != 1 || archive.assets[0].AudioURL != core.dispatch["audio_url"] {
		t.Fatal("archive and live output must use the same combined audio", archive.assets)
	}
	if strings.Contains(tts.last.Text, "叮咚") || !strings.HasSuffix(tts.last.Text, e.Body) {
		t.Fatal("bell changed spoken text", tts.last.Text)
	}
}

func TestOrderBellMissingStillDispatchesHumanSpeechWithoutAnotherTTS(t *testing.T) {
	w, _, core, agent, tts, session, e := v2Fixture(t, 93002)
	e.Intent.Type = "下单意向"
	claimOrderBellOriginal(core, e, "已拍五香")
	w.orderAudioComposer = func(context.Context, string) (ttsgateway.OrderBellAudio, error) {
		return ttsgateway.OrderBellAudio{}, ttsgateway.ErrOrderBellMissing
	}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if core.dispatch["audio_url"] != "http://audio.local/reply.wav" || tts.calls != 1 || agent.calls != 0 {
		t.Fatal(core.dispatch, tts.calls, agent.calls)
	}
}

func TestOrderBellNoCompositionForOrdinaryRepliesOrFailedTTS(t *testing.T) {
	for i, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary_reply", true: "tts_failed"}[fail], func(t *testing.T) {
			w, _, core, _, tts, session, e := v2Fixture(t, int64(93003+i))
			if fail {
				e.Intent.Type = "下单意向"
				tts.err = errors.New("fixture TTS failed")
			}
			claimV2(core, e)
			if fail {
				claimOrderBellOriginal(core, e, "已拍五香")
			}
			w.orderAudioComposer = func(context.Context, string) (ttsgateway.OrderBellAudio, error) {
				t.Fatal("a bell cannot play without completed order speech")
				return ttsgateway.OrderBellAudio{}, nil
			}
			err := w.processRoom(context.Background(), session)
			if fail && (err == nil || core.dispatches != 0) {
				t.Fatal(err, core.dispatches)
			}
			if !fail && (err != nil || core.dispatches != 1) {
				t.Fatal(err, core.dispatches)
			}
		})
	}
}

func TestOrderBellConfirmedSingleAndCountBehaviorsUseRealAudio(t *testing.T) {
	for i, template := range []string{"order_single_v1", "order_count_v1"} {
		t.Run(template, func(t *testing.T) {
			w, _, core, agent, tts, session, e := v2Fixture(t, int64(93010+i))
			e.Template, e.EventCount = template, 1+i*3
			e.Intent.Type = "闲聊"
			e.Body, _ = interactionplan.BehaviorBody("order", template, e.EventCount)
			raw, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "confirmed-order", "mission_kind": "conversion_signal", "nicknames": []string{"小王"}, "purpose_knowledge": e, "expires_at": e.ExpiresAt}})
			core.claimRaw = string(raw)
			calls := 0
			w.orderAudioComposer = func(context.Context, string) (ttsgateway.OrderBellAudio, error) {
				calls++
				return ttsgateway.OrderBellAudio{AudioURL: "http://audio.local/confirmed.wav"}, nil
			}
			if err := w.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || tts.calls != 1 || agent.calls != 0 || core.dispatch["audio_url"] != "http://audio.local/confirmed.wav" || strings.Contains(tts.last.Text, "叮咚") {
				t.Fatal(calls, tts.calls, agent.calls, core.dispatch, tts.last.Text)
			}
		})
	}
}

func TestOrderBellExistingGapDecisionSkipsOnlyBellNotSpeech(t *testing.T) {
	w, _, core, _, tts, session, e := v2Fixture(t, 93020)
	e.Intent.Type = "下单意向"
	claimOrderBellOriginal(core, e, "已拍五香")
	calls := 0
	w.orderAudioComposer = func(context.Context, string) (ttsgateway.OrderBellAudio, error) {
		calls++
		return ttsgateway.OrderBellAudio{AudioURL: "http://audio.local/bell.wav"}, nil
	}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	core.claimRaw = strings.Replace(core.claimRaw, `"d-1"`, `"d-2"`, 1)
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || tts.calls != 2 || core.dispatch["audio_url"] != "http://audio.local/reply.wav" {
		t.Fatal("existing ring gap was bypassed or speech suppressed", calls, tts.calls, core.dispatch)
	}
}

func TestOrderBellWorkerVerifiesOriginalAcrossModelLabels(t *testing.T) {
	cases := []struct {
		label, original string
		ring            bool
	}{
		{"下单意向", "已拍五香", true},
		{"催单", "五香味刚付完钱了，麻烦安排发货。", true},
		{"闲聊", "五香味这单刚结完账，支持一下主播。", true},
		{"提问", "我拍了五香，多久发", true},
		{"下单意向", "我想买五香", false},
		{"下单意向", "我还没付款", false},
		{"下单意向", "", false},
		{"催单", "什么时候发货", false},
		{"下单意向", "已拍?", false},
		{"投诉", "我拍了但有破损", false},
	}
	for i, c := range cases {
		t.Run(c.label+"/"+c.original, func(t *testing.T) {
			w, _, core, agent, tts, session, e := v2Fixture(t, int64(93100+i))
			e.Intent.Type, e.Intent.Meaning = c.label, "观众已经付款下单"
			claimOrderBellOriginal(core, e, c.original)
			calls := 0
			w.orderAudioComposer = func(context.Context, string) (ttsgateway.OrderBellAudio, error) {
				calls++
				return ttsgateway.OrderBellAudio{AudioURL: "http://audio.local/confirmed.wav"}, nil
			}
			if err := w.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			if (calls == 1) != c.ring || calls > 1 || tts.calls != 1 || agent.calls != 0 || core.dispatches != 1 {
				t.Fatal("original confirmation or single TTS path changed", calls, c.ring, tts.calls, agent.calls, core.dispatches)
			}
		})
	}
}

func TestOrderBellGeneratedSummaryCannotProveOriginalOrder(t *testing.T) {
	w, _, _, _, _, _, _ := v2Fixture(t, 93200)
	item := decisionItem{ID: "summary-is-not-original", Summary: "用户已下单", Title: "已付款"}
	w.markOrderBell(&item, "下单意向")
	if item.OrderBell {
		t.Fatal("generated summary or title must not replace missing viewer original")
	}
}
