package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
	"os"
	"strings"
	"testing"
	"time"
)

// Explicit regression replay. Actual models and P0, no TTS, playback or adoption.
func TestSessionContentBoundaryProviderProbe(t *testing.T) {
	if os.Getenv("SESSION_CONTENT_BOUNDARY_PROBE") != "1" {
		t.Skip("explicit paid replay required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	settings, e := store.AnchorGenerationSettings(ctx)
	if e != nil {
		t.Fatal(e)
	}
	speech, e := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if e != nil || speech == nil {
		t.Fatal("speech unavailable")
	}
	server := &Server{store: store}
	set, e := store.GetContentPoolSet(ctx, 14, 4, 11)
	if e != nil {
		t.Fatal(e)
	}
	productPolicy, _, e := loadCycleProductPolicy()
	if e != nil {
		t.Fatal(e)
	}
	tasks, focus, e := contentPoolRuntime(set, productPolicy)
	if e != nil {
		t.Fatal(e)
	}
	var task anchorcycle.Task
	for _, candidate := range tasks {
		if len(candidate.ProductLinks()) == 1 && candidate.ProductLinks()[0] == "1" && strings.Contains(candidate.SellingPoint, "规格") {
			task = candidate
			break
		}
	}
	if task.Key == "" {
		t.Fatal("explicit replay task missing")
	}
	p0, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	rows := []map[string]any{}
	defer func() {
		raw, _ := json.MarshalIndent(map[string]any{"speech_model": speech.Model, "p0_sha": derivedHash(p0), "rows": rows, "tts": false, "played": false, "adopted": false, "passed": !t.Failed()}, "", "  ")
		if os.WriteFile(os.Getenv("SESSION_CONTENT_BOUNDARY_OUTPUT"), raw, 0600) != nil {
			t.Error("archive failed")
		}
	}()
	for _, phase := range []string{"body", "closing"} {
		state, e := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{Rolling: true, MaxChars: 300, BodySegments: 3, TextProfileID: speech.ID, FeedbackID: speech.ID, ComplianceScope: "ordinary_food"})
		if e != nil {
			t.Fatal(e)
		}
		state.Surface = "offline"
		state.TextModel = speech
		state.ProductFocus = focus
		if focus != nil {
			focus.CurrentLink = "1"
		}
		state.BaselineJSON = textTestBaseline()
		state.Facts = set.SafeFacts
		if card, err := store.GetActiveLiveAnchorStyleScorecard(ctx, 14, 4); err == nil {
			state.BaselineJSON = card.EffectiveScorecardJSON()
		}
		if e = state.InstallP0(p0, source); e != nil {
			t.Fatal(e)
		}
		if phase == "closing" {
			state.Phase = "body"
			state.Accepted = []anchorcycle.Segment{{Task: state.Task(), Selected: anchorcycle.Candidate{Text: ""}}}
		}
		state.Phase = phase
		unique := map[string]bool{}
		eligible := 0
		for repeat := 1; repeat <= 3; repeat++ {
			response, messages, err := server.completeCycleText(ctx, &state)
			row := map[string]any{"kind": phase, "repeat": repeat, "request_ok": err == nil, "messages": messages, "response": response}
			rows = append(rows, row)
			if err != nil {
				t.Errorf("%s request failed", phase)
				continue
			}
			c := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, OutputText: response.Text}
			cleanCycleOpeningInstruction(&state, &c, "")
			cleanCycleConsecutiveSpeech(&state, &c)
			row["duplicate_removals"] = c.ExactDuplicateRemovals
			if c.OutputText == "" {
				row["skipped"] = "consecutive_exact_duplicate"
				row["final_text"] = ""
				continue
			}
			if err = state.CheckP0Candidate(&c); err == nil {
				server.reviewCycleP0Context(ctx, &state, &c)
			}
			row["final_text"], row["p0"], row["instruction_edits"] = c.OutputText, c.P0Report, c.OpeningInstructionEdits
			unique[c.OutputText] = true
			for _, wrong := range []string{"炖汤", "炒菜", "这轮收口", "本轮收口"} {
				if strings.Contains(c.OutputText, wrong) {
					t.Errorf("reproduced %s", wrong)
				}
			}
			if c.P0Report == nil || c.P0Report.Error != "" {
				t.Errorf("%s review unavailable", phase)
			} else if !c.P0Report.Passed {
				row["skipped"] = "p0_blocked" // Expected supply behavior, never a bypass.
			} else {
				eligible++
				state.Accepted = append(state.Accepted, anchorcycle.Segment{Task: state.Task(), Selected: c})
			}
		}
		// Raw model repetitions are expected to be removed, not an approval.
		// Every retained paragraph must differ; skipped paragraphs never enter TTS.
		t.Logf("%s retained wordings=%d; eligible=%d", phase, len(unique), eligible)
		if eligible == 0 {
			t.Errorf("%s no eligible supply", phase)
		}
	}
	plan, e := store.GetLiveAgentPlan(ctx, 14, 4)
	if e != nil {
		t.Fatal(e)
	}
	worker := decisionexecutor.New(&planSpeechTestStore{Store: store, tenantID: 14, roomID: 15, plan: plan}, nil, server.speechGateway(), nil)
	recent := []decisionexecutor.InteractionRecentAnswer{}
	for repeat := 1; repeat <= 3; repeat++ {
		question := "你家油确实好吃，我买过好多次了"
		result, err := worker.SimulateInteraction(ctx, 14, 15, question, "", recent)
		rows = append(rows, map[string]any{"kind": "oil_question", "repeat": repeat, "request_ok": err == nil, "result": result})
		if err != nil {
			t.Errorf("oil response failed")
			continue
		}
		for _, wrong := range []string{"新菜籽", "压榨", "2斤试吃", "中通"} {
			if strings.Contains(result.Reply, wrong) {
				t.Errorf("old business source used: %s", wrong)
			}
		}
		if result.AnswerOutline != nil {
			recent = append(recent, decisionexecutor.InteractionRecentAnswer{Question: question, Reply: result.Reply, Purpose: result.AnswerOutline.Purpose, Angle: result.AnswerOutline.Angle})
		}
	}
}
