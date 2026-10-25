package httpapi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/recordingcache"
	"livecompanion/management/internal/ttsgateway"
)

func TestRecordingRefreshNoveltyRejectsOnlyOptionalDuplicateSpeech(t *testing.T) {
	const source = "欢迎朋友，看看套餐。"
	const previous = "套餐介绍，朋友们可以看一看。"
	for _, tc := range []struct {
		name     string
		priority string
		spoken   string
		existing []string
		wantErr  bool
	}{
		{"source_echo", livecost.PriorityOptional, source, nil, true},
		{"source_punctuation_and_whitespace", livecost.PriorityOptional, " 欢迎朋友！\n看看套餐！ ", nil, true},
		{"known_version", livecost.PriorityOptional, previous, []string{previous}, true},
		{"known_version_punctuation_and_whitespace", livecost.PriorityOptional, "套餐介绍。\n朋友们可以看一看！", []string{previous}, true},
		{"later_known_version", livecost.PriorityOptional, previous, []string{"先看套餐介绍。", previous}, true},
		{"novel_expression", livecost.PriorityOptional, "朋友们，先看看套餐介绍。", []string{previous}, false},
		{"empty_previous_does_not_block", livecost.PriorityOptional, previous, []string{"", " \n "}, false},
		{"necessary_original_supply", livecost.PriorityNecessary, source, []string{previous}, false},
		{"necessary_existing_expression", livecost.PriorityNecessary, previous, []string{previous}, false},
		{"unscoped_original_supply", "", source, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := withLiveCostScope(t.Context(), liveCostCallScope{Priority: tc.priority})
			if err := recordingRefreshNoveltyError(ctx, source, tc.spoken, tc.existing...); (err != nil) != tc.wantErr {
				t.Fatalf("duplicate decision err=%v wantErr=%t", err, tc.wantErr)
			}
		})
	}
}

func TestRecordingKnownSpokenVersionsIncludesOnlyCurrentlyValidWholeVersions(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友，看看套餐。"
	memory := []recordingReadyUnit{inventoryFixtureUnit(301, "欢迎朋友。"), inventoryFixtureUnit(302, "看看套餐。")}
	persistent := []recordingReadyUnit{inventoryFixtureUnit(303, "朋友们好。"), inventoryFixtureUnit(304, "一起看看套餐。")}
	valid := inventoryFixtureEntry(t, source, persistent...)
	invalid := inventoryFixtureUnit(305, "失效声音。")
	invalid.Track.AudioURL = ""
	rejected := inventoryFixtureEntry(t, source, inventoryFixtureUnit(306, "咱们家全网最好，绝对第一。"))
	stale := inventoryFixtureEntry(t, "另一句旧原文。", inventoryFixtureUnit(307, "另一句旧原文。"))
	malformed := inventoryFixtureEntry(t, source, inventoryFixtureUnit(308, "朋友们，看看套餐。"))
	malformed.Duration = 0
	got := recordingKnownSpokenVersions(source, [][]recordingReadyUnit{memory, {invalid}}, []recordingcache.Entry{valid, rejected, stale, malformed}, policy)
	want := []string{"欢迎朋友。看看套餐。", "朋友们好。一起看看套餐。"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("known versions=%q want=%q", got, want)
	}
}

// This is an offline composition of the real cost adapter, novelty decision
// and reuse resolver. The returned model text and TTS provider are test stubs;
// it does not exercise rewriteCycleText, model-configuration SQL or audio I/O.
func TestRecordingRefreshDuplicateKeepsReturnedModelCostWithoutTTS(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		name := "memory"
		if persistent {
			name = "persistent"
		}
		t.Run(name, func(t *testing.T) {
			const source = "欢迎朋友，看看套餐。"
			const previous = "朋友们好，一起看看套餐。"
			const returned = "朋友们好！\n一起看看套餐！"
			policy := incrementalPolicy(t)
			unit := inventoryFixtureUnit(401, previous)
			versions := [][]recordingReadyUnit{{unit}}
			var cached []recordingcache.Entry
			if persistent {
				versions = nil
				cached = []recordingcache.Entry{inventoryFixtureEntry(t, source, unit)}
			}
			known := recordingKnownSpokenVersions(source, versions, cached, policy)
			if len(known) != 1 || !sameSpeechText(known[0], returned) || sameSpeechText(source, returned) {
				t.Fatalf("fixture must hit an existing rewrite, never the original: %q", known)
			}
			store := &adapterCostStore{}
			server := adapterTestServer(t, store)
			events := &liveCostCallEvents{}
			ctx := withLiveCostScope(t.Context(), liveCostCallScope{
				TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 480, Key: "duplicate-rewrite",
				Priority: livecost.PriorityOptional, Events: events,
			})
			provider := &adapterTTSProvider{}
			modelCalls := 0
			result, err := resolveRecordingReuse(ctx, recordingReuseRequest{
				Versions: versions, Cached: cached, Refresh: true,
				BuildCache: func(context.Context, recordingcache.Entry) ([]recordingReadyUnit, bool) {
					return []recordingReadyUnit{unit}, true
				},
				Generate: func(c context.Context) ([]recordingReadyUnit, error) {
					request := agentgateway.Request{Stage: "mainline_text_rewrite", Provider: "qwen", Model: "m", MaxTokens: 800,
						Messages: []agentgateway.Message{{Role: "user", Content: source}}}
					if err := server.beginLiveModelCost(c, 1, request.Stage, "", request); err != nil {
						return nil, err
					}
					modelCalls++
					response := agentgateway.Response{Text: returned, Provider: "qwen", Model: "m", InputTokens: 10, OutputTokens: 5}
					server.finishLiveModelCost(c, 1, response, 10, 5, nil)
					if err := recordingRefreshNoveltyError(c, source, response.Text, known...); err != nil {
						return nil, err
					}
					_, err := provider.SynthesizeURL(c, ttsgateway.SynthesizeRequest{Text: response.Text})
					if err != nil {
						return nil, err
					}
					return nil, errors.New("duplicate rewrite must not reach the TTS stub")
				},
			})
			if err != nil || result.Generated || result.GenerationErr == nil || !strings.Contains(result.GenerationErr.Error(), "已有版本重复") || result.CacheHit != persistent || !reflect.DeepEqual(result.Units, []recordingReadyUnit{unit}) {
				t.Fatalf("duplicate was not retained as reuse: result=%+v err=%v", result, err)
			}
			if modelCalls != 1 || provider.calls.Load() != 0 || len(store.requests) != 1 || store.requests[0].Kind != livecost.KindModel || len(store.receipts) != 1 || store.receipts[0].Status != livecost.ReceiptSuccess {
				t.Fatalf("calls/ledger: model=%d tts=%d requests=%+v receipts=%+v", modelCalls, provider.calls.Load(), store.requests, store.receipts)
			}
			if events.ChargedMicros.Load() != 6 || events.Uncertain.Load() || events.AccountingUnavailable.Load() {
				t.Fatalf("returned duplicate model work lost its paid receipt: charged=%d uncertain=%t unavailable=%t", events.ChargedMicros.Load(), events.Uncertain.Load(), events.AccountingUnavailable.Load())
			}
			stats := &recordingSupplyStats{}
			stats.observe(source, true, result, err)
			if stats.Generated != 0 || stats.Updated != 0 || stats.Reused != 1 || stats.RefreshFailed != 1 || stats.Unavailable != 0 {
				t.Fatalf("duplicate was counted as a successful new expression: %+v", stats)
			}
		})
	}
}
