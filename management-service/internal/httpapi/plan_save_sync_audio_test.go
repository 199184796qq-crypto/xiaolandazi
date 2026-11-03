package httpapi

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func syncSaveFixture() model.LiveAgentPlanVersion {
	return model.LiveAgentPlanVersion{TenantID: 14, PlanID: 4, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "fixture", Rate: 1.1}, GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": recordingStreamMode, "recording_script": groupedScriptFixture()}}
}

func TestPlanSaveWaitsForExactOpeningAnchor(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	for _, name := range []string{"qwen-audio-3.1-tts-flash", ttsgateway.VolcengineICL2} {
		t.Run(name, func(t *testing.T) {
			v := syncSaveFixture()
			v.VoiceIdentity.Model = name
			started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			go func() {
				finished <- waitPlanFirstParagraph(t.Context(), 14, 4, v, func(ctx context.Context, u volcengineScriptUnit) error {
					// Fixture's opening label is pinned; shortest entry is the anchor.
					if u.Text != strings.Repeat("乙", 400) || u.Key != scriptFirstParagraphKey(v, u.Text) {
						return errors.New("incorrect opening anchor or synthesis identity")
					}
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > 60*time.Second || time.Until(deadline) < 58*time.Second {
						return errors.New("save generation must have a 60-second deadline")
					}
					close(started)
					<-release
					return nil
				})
			}()
			select {
			case <-started:
			case err := <-finished:
				t.Fatal(err)
			case <-time.After(time.Second):
				t.Fatal("preparation did not start")
			}
			select {
			case err := <-finished:
				t.Fatal("returned before audio ready", err)
			default:
			}
			close(release)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlanSaveTimeoutDoesNotAwaitDetachedSupplierOrAcceptLateSuccess(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancel()
	release, drained := make(chan struct{}), make(chan struct{})
	started := time.Now()
	err := waitPlanFirstParagraph(ctx, 14, 4, syncSaveFixture(), func(work context.Context, _ volcengineScriptUnit) error {
		<-work.Done()
		<-release // Simulates an already-paid provider ignoring cancellation.
		close(drained)
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatal("timeout did not end Save", err)
	}
	close(release)
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("late result blocked")
	}
	if planWarmupTimeout != 60*time.Second {
		t.Fatal("wrong production timeout")
	}
}

func TestPlanSaveGenerationErrorsAndCancellationAreNotSuccess(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	v := syncSaveFixture()
	want := errors.New("fixture upstream failure")
	if err := waitPlanFirstParagraph(t.Context(), 14, 4, v, func(context.Context, volcengineScriptUnit) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if err := waitPlanFirstParagraph(t.Context(), 14, 4, v, func(context.Context, volcengineScriptUnit) error { panic("fixture") }); err == nil {
		t.Fatal("panic accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if err := waitPlanFirstParagraph(ctx, 14, 4, v, func(context.Context, volcengineScriptUnit) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal("canceled Save bought audio", err)
	}
	for _, scope := range [][2]int64{{24, 4}, {14, 5}, {0, 4}} {
		if err := waitPlanFirstParagraph(t.Context(), scope[0], scope[1], v, func(context.Context, volcengineScriptUnit) error { t.Error("wrong scope generated audio"); return nil }); err == nil {
			t.Fatal("wrong scope accepted")
		}
	}
	v.GenerationContext["recording_voice_mode"] = "original"
	if err := waitPlanFirstParagraph(t.Context(), 14, 4, v, func(context.Context, volcengineScriptUnit) error {
		t.Error("original recording bought audio")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Check the actual Save route: readiness failure returns before any version
// insert, and the final success event can only follow both preparation and DB.
func TestPlanSaveProductionWaitsBeforePublishing(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "live_agent_plan_versions.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var prepare, persist, success token.Pos
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "liveAgentPlanVersionCreate" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if s, ok := call.Fun.(*ast.SelectorExpr); ok {
					switch s.Sel.Name {
					case "warmPlanFirstParagraph":
						prepare = call.Pos()
					case "CreateLiveAgentPlanVersion":
						persist = call.Pos()
					}
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "reportAnchorAnalysis" && len(call.Args) > 1 {
					if n, ok := call.Args[1].(*ast.BasicLit); ok && n.Value == "100" {
						success = call.Pos()
					}
				}
			}
			return true
		})
	}
	if prepare == 0 || persist <= prepare || success <= persist {
		t.Fatal("Save readiness/publication/success order changed")
	}
}
