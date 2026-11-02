package httpapi

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPausedRecordingSaveNeedsNoAnalysisServices(t *testing.T) {
	for _, mode := range []string{"original", "stream_clone"} {
		for _, modelName := range []string{"qwen-audio-3.1-tts-flash", "seed-icl-2.0"} {
			t.Run(mode+"/"+modelName, func(t *testing.T) {
				contextFields := map[string]any{
					"content_mode": "user_audio", "recording_voice_mode": mode,
					"recording_script": map[string]any{"tabs": []any{map[string]any{"id": "first", "text": "本页原始话术====第二段"}}},
					"reading_rules":    "原读法", "plan_business": map[string]any{"biz_domain": "local-life"},
					"derived_safety": map[string]any{"source_sha256": "untrusted"},
					"atomic_facts":   "过期事实", "domain_card": map[string]any{"source_sha256": "untrusted"},
				}
				input := model.CreateLiveAgentPlanVersionInput{VoiceIdentity: model.LiveAgentVoiceIdentity{VoiceID: "original-voice", Model: modelName, Rate: 1.13}, GenerationContext: contextFields}
				originalVoice := input.VoiceIdentity
				originalScript, _ := json.Marshal(contextFields["recording_script"])
				// No store, gateway, or model configuration exists. Recording saves
				// must not consult any of them for the paused analysis work.
				if err := (&Server{}).capturePlanSaveWithoutAnalysis(context.Background(), 14, 4, &input); err != nil {
					t.Fatal(err)
				}
				if !planSaveAnalysisPaused(input.GenerationContext) || !reflect.DeepEqual(input.VoiceIdentity, originalVoice) {
					t.Fatal("pause marker or voice changed")
				}
				for _, key := range []string{"derived_safety", "atomic_facts", "domain_card"} {
					if _, exists := input.GenerationContext[key]; exists {
						t.Fatalf("untrusted old analysis retained: %s", key)
					}
				}
				currentScript, _ := json.Marshal(input.GenerationContext["recording_script"])
				if string(currentScript) != string(originalScript) || input.GenerationContext["reading_rules"] != "原读法" {
					t.Fatal("recording text or reading rules modified")
				}
			})
		}
	}
}

func TestPausedRecordingVoiceRepublishAndWorkspace(t *testing.T) {
	for _, mode := range []string{"user_audio", "ai_pregenerated"} {
		t.Run(mode, func(t *testing.T) {
			input := model.CreateLiveAgentPlanVersionInput{GenerationContext: map[string]any{"content_mode": mode, planSaveAnalysisPausedField: true}}
			if err := (&Server{}).refreshVersionDerivedSafety(context.Background(), 14, 4, 7, &input); err != nil {
				t.Fatal("voice-only update requires paused analysis", err)
			}
			v := model.LiveAgentPlanVersion{GenerationContext: input.GenerationContext}
			if checks := (&Server{}).liveAgentPlanWorkspaceChecks(context.Background(), 14, 4, &v); len(checks) != 0 {
				t.Fatalf("expected static version remains usable: %+v", checks)
			}
		})
	}
	legacy := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": "user_audio"}}
	if checks := (&Server{}).liveAgentPlanWorkspaceChecks(context.Background(), 14, 4, &legacy); len(checks) != 1 || checks[0].Code != "saved_snapshot_missing" {
		t.Fatal("unrelated legacy diagnostics were suppressed")
	}
}

func TestPlanSaveCacheOnlyIsRequestLocal(t *testing.T) {
	base := context.Background()
	if planSaveCacheOnly(base) {
		t.Fatal("all workflows unexpectedly disabled")
	}
	child := context.WithValue(base, planSaveCacheOnlyKey{}, true)
	if !planSaveCacheOnly(child) || planSaveCacheOnly(base) {
		t.Fatal("cache-only restriction leaked outside Save request")
	}
}

// Protect the production Save wiring, rather than merely testing a helper that
// could be bypassed by the handler. First-paragraph prewarm remains attached.
func TestPlanSaveDoesNotStartPausedAnalysis(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "live_agent_plan_versions.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "liveAgentPlanVersionCreate" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("production Save handler missing")
	}
	calls := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				calls[selector.Sel.Name] = true
			}
		}
		return true
	})
	for _, forbidden := range []string{"prepareDerivedSafety", "buildAtomicFacts", "buildPlanDomainCard", "captureRealtimeProductFocus"} {
		if calls[forbidden] {
			t.Fatalf("Save still runs old analysis outside cache-only path: %s", forbidden)
		}
	}
	for _, required := range []string{"capturePlanSaveWithoutAnalysis", "CreateLiveAgentPlanVersion", "warmPlanFirstParagraph"} {
		if !calls[required] {
			t.Fatalf("essential Save operation detached: %s", required)
		}
	}
}
