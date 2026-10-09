package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpurpose"
	"livecompanion/management/internal/semantic"
)

// Explicit operational opt-in. Saves scoped purpose knowledge/model exchanges,
// never creates a live session, interaction queue, plan version or voice task.
func TestInterruptPhase1ProviderProbe(t *testing.T) {
	if os.Getenv("INTERRUPT_PHASE1_PROBE") != "1" {
		t.Skip("explicit paid operational probe required")
	}
	cfg := config.Load()
	store, err := appdb.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store, purposeInternalToken: cfg.CoreToken, purposeSemantic: semantic.NewService(semantic.NewFromEnv(), store)}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []map[string]any{}
	defer func() {
		if path := os.Getenv("INTERRUPT_PHASE1_RESULT_PATH"); path != "" {
			raw, _ := json.MarshalIndent(map[string]any{"outputs": outputs, "passed": !t.Failed()}, "", "  ")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	for _, tc := range []struct {
		room, plan int64
		text       string
	}{{15, 2, "发哪个快递"}, {15, 2, "生油还是熟油"}, {26, 4, "一只多少钱，贵不贵"}} {
		input, _ := json.Marshal(map[string]any{"tenant_id": 14, "room_id": tc.room, "plan_id": tc.plan, "text": tc.text})
		var first map[string]json.RawMessage
		for attempt := 0; attempt < 2; attempt++ {
			request := httptest.NewRequest(http.MethodPost, "/internal/v1/interaction-purpose/resolve", bytes.NewReader(input)).WithContext(ctx)
			request.Header.Set("X-Core-Token", cfg.CoreToken)
			response := httptest.NewRecorder()
			began := time.Now()
			s.resolveInteractionPurpose(response, request)
			outputs = append(outputs, map[string]any{"stage": "purpose", "room": tc.room, "question": tc.text, "attempt": attempt, "status": response.Code, "elapsed_ms": time.Since(began).Milliseconds(), "response": json.RawMessage(response.Body.Bytes())})
			if response.Code != 200 {
				t.Fatalf("purpose %s status=%d body=%s", tc.text, response.Code, response.Body.String())
			}
			var parsed map[string]json.RawMessage
			json.Unmarshal(response.Body.Bytes(), &parsed)
			t.Logf("purpose question=%s attempt=%d source=%s elapsed_ms=%d", tc.text, attempt, parsed["source"], time.Since(began).Milliseconds())
			if attempt == 0 {
				first = parsed
			} else if string(parsed["source"]) != `"exact"` || string(parsed["topic"]) != string(first["topic"]) {
				t.Fatal("real exact reuse failed", response.Body.String())
			}
		}
		var knowledge interactionpurpose.Result
		json.Unmarshal(first["knowledge"], &knowledge)
		if !knowledge.Question || !knowledge.Relevant {
			t.Fatal("real product question misclassified", tc.text)
		}
		if tc.room == 26 && os.Getenv("INTERRUPT_PHASE1_CONTENT_PROBE") == "1" {
			published, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, 26)
			if err != nil {
				t.Fatal(err)
			}
			ref := asSupportMap(published.GenerationContext["derived_safety"])
			hash, _ := ref["source_sha256"].(string)
			asset, err := store.GetLiveDerivedSafety(ctx, 14, 4, hash)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := s.prepareContentStrategy(ctx, 14, 4, asset.SafeFacts, settings.SellingPointProfileID, true)
			if err != nil {
				t.Fatal(err)
			}
			outputs = append(outputs, map[string]any{"stage": "content_protocol", "strategy": snapshot})
			t.Logf("content protocol version=%d rows=%d canonical conditions validated", snapshot.Version, len(snapshot.Rows))
			if os.Getenv("INTERRUPT_PHASE1_SPEECH_PROBE") != "1" {
				continue
			}
			active := contentstrategy.ActiveBenefitFacts(snapshot.Facts, time.Now())
			guidance, selection, err := snapshot.RenderSelection(contentstrategy.SelectionRequest{Mode: "互动", AllowedFacts: asset.SafeFacts, Question: tc.text, PurposeID: knowledge.PurposeID, ActiveBenefit: len(active) > 0, ActiveFactIDs: active})
			if err != nil {
				t.Fatal(err)
			}
			if guidance == "" {
				guidance, selection, err = snapshot.RenderSelection(contentstrategy.SelectionRequest{Mode: "互动", AllowedFacts: asset.SafeFacts, Question: tc.text})
				if err != nil {
					t.Fatal(err)
				}
			}
			result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "speech_generation", agentgateway.ResponseText, []agentgateway.Message{{Role: "system", Content: guidance}, {Role: "user", Content: "完整已发布事实：\n" + asset.SafeFacts + "\n观众原话：" + tc.text + "\n请直接回答，约60至100字，遵守完整事实中的禁说及改写。只输出可播正文。"}}, 650, false)
			outputs = append(outputs, map[string]any{"stage": "selected_speech", "strategy": snapshot, "selection": selection, "text": result.Text, "model": result.Model})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("selected_speech model=%s text=%s", result.Model, result.Text)
		}
	}
}
