package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"sync"
	"testing"
	"time"
)

// Explicit paid historical replay; never writes plans, queues, TTS or playback.
func TestTextContextAngleProviderProbe(t *testing.T) {
	if os.Getenv("TEXT_CONTEXT_ANGLE_PROBE") != "1" {
		t.Skip("explicit provider replay required")
	}
	raw, err := os.ReadFile(os.Getenv("TEXT_CONTEXT_ANGLE_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Cases []struct {
			Name    string               `json:"name"`
			Group   string               `json:"group"`
			Request agentgateway.Request `json:"request"`
			State   *anchorcycle.State   `json:"state"`
			Text    string               `json:"text"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration database unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if err != nil || profile == nil || profile.Model != "qwen3.7-flash" {
		t.Fatal("expected configured Flash model")
	}
	s := &Server{store: store}
	rows := make([]map[string]any, len(input.Cases))
	defer func() {
		data, _ := json.MarshalIndent(map[string]any{"model": profile.Model, "profile": profile.ID, "text_only": true, "passed": !t.Failed(), "rows": rows}, "", "  ")
		if err := os.WriteFile(os.Getenv("TEXT_CONTEXT_ANGLE_OUTPUT"), data, 0600); err != nil {
			t.Error(err)
		}
	}()
	// Independent samples share the same historical context, with three in flight.
	var wg sync.WaitGroup
	slots := make(chan struct{}, 3)
	for i, c := range input.Cases {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-slots }()
			var anglePlan *anchorcycle.AnglePlan
			var planState *anchorcycle.State
			if c.Group == "两阶段角度" {
				state := c.State
				planState = state
				state.Config.TextProfileID = profile.ID
				state.TextModel = profile
				s.ensureCycleAnglePlan(ctx, state, *profile)
				anglePlan = state.CurrentAnglePlan()
				if anglePlan == nil {
					t.Errorf("case=%s angle planning failed", c.Name)
					return
				}
				s.ensureCycleAnglePlan(ctx, state, *profile)
				if state.CurrentAnglePlan() != anglePlan {
					t.Error("unchanged angle plan was regenerated")
					return
				}
				var packet map[string]any
				if json.Unmarshal([]byte(c.Request.Messages[len(c.Request.Messages)-1].Content), &packet) != nil {
					t.Error("invalid historical packet")
					return
				}
				applyCycleAnglePlanPacket(state, packet)
				body, _ := json.Marshal(packet)
				c.Request.Messages[len(c.Request.Messages)-1].Content = string(body)
				c.State = nil
			}
			if c.State != nil {
				state := c.State
				state.Config.TextProfileID = profile.ID
				state.TextModel = profile
				candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: c.Text, RawText: c.Text, OutputText: c.Text}
				cleanCycleOpeningInstruction(state, &candidate, "")
				if err := state.CheckP0Candidate(&candidate); err != nil {
					t.Error(err)
					return
				}
				rows[i] = map[string]any{"name": c.Name, "group": c.Group, "candidate": candidate, "approved": candidate.P0Report.Passed}
				if candidate.P0Report.Passed && state.CheckApprovedText(candidate, candidate.OutputText) != nil {
					t.Error("invalid final receipt")
				}
				t.Logf("case=%s approved=%v edits=%d", c.Name, candidate.P0Report.Passed, len(candidate.OpeningInstructionEdits))
				return
			}
			request := c.Request
			request.Model = profile.Model
			request.EnableThinking = false
			request.Timeout = 45 * time.Second
			// Use the operator's current expression patch, exactly as live requests do.
			if profile.SystemPrompt != "" {
				request.Messages = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, request.Messages...)
			}
			response, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, request)
			rows[i] = map[string]any{"name": c.Name, "group": c.Group, "request": request, "response": response, "angle_plan": anglePlan, "success": callErr == nil}
			if callErr == nil && planState != nil {
				candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, RawText: response.Text, OutputText: response.Text, AnglePlan: anglePlan}
				cleanCycleOpeningInstruction(planState, &candidate, "")
				if err := planState.CheckP0Candidate(&candidate); err != nil {
					t.Error(err)
				} else {
				}
				rows[i]["candidate"], rows[i]["approved"] = candidate, candidate.P0Report != nil && candidate.P0Report.Passed
			}
			if callErr != nil {
				t.Errorf("case=%s provider failed", c.Name)
			}
			t.Logf("case=%s input=%d output=%d latency_ms=%d", c.Name, response.InputTokens, response.OutputTokens, response.LatencyMS)
		}(i)
	}
	wg.Wait()
}
