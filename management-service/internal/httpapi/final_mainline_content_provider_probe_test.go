package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"testing"
	"time"
)

func TestFinalMainlineContentCrossCategoryProvider(t *testing.T) {
	if os.Getenv("FINAL_MAINLINE_CONTENT_PROBE") != "1" {
		t.Skip("explicit paid final text regression")
	}
	raw, e := os.ReadFile(os.Getenv("FINAL_MAINLINE_CONTENT_INPUT"))
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name, Identity, Facts, Text string
		WantPass                    bool
		Corrections                 []map[string]any
	}
	if e = json.Unmarshal(raw, &cases); e != nil || len(cases) == 0 || len(cases) > 64 {
		t.Fatal("invalid explicit fixtures", e)
	}
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	id, e := store.ResolveTextRole(ctx, "mainline_final_content_review")
	if e != nil {
		t.Fatal(e)
	}
	profile, e := store.ResolveSpeechModel(ctx, id)
	if e != nil || profile == nil {
		t.Fatal("auditor unavailable")
	}
	p0, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	mother, _, e := loadReplaceablePromptAsset("MAINLINE_FINAL_CONTENT_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/mainline-final-content-review.md", "最终正文审核母版")
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{store: store}
	rows := []map[string]any{}
	defer func() {
		b, e := json.MarshalIndent(map[string]any{"rows": rows, "model": profile.Model, "mother": mother, "p0": p0, "passed": !t.Failed(), "business_assets_changed": false, "tts": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("FINAL_MAINLINE_CONTENT_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	for _, f := range cases {
		task := anchorcycle.Task{Key: "offline-current-case", SellingPoint: "围绕当前商品和要求说明", Angle: "按本次确认资料与要求说明", AllowedLinksJSON: `["1"]`, FactScope: &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "program-fact", Products: []string{"1"}, Data: f.Facts}}, ProductBoundary: []anchorcycle.HookFact{{ID: "program-identity", Products: []string{"1"}, Data: f.Identity}}, Corrections: f.Corrections}}
		state, e := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{Rolling: true, MaxChars: 220, BodySegments: 3})
		if e != nil {
			t.Fatal(e)
		}
		state.TenantID, state.PlanID, state.Surface, state.Phase = 14, 0, factPreviewSurface, "body"
		state.ReviewModel = profile
		state.Facts = f.Identity + "\n" + f.Facts
		if e = state.InstallP0(p0, source); e != nil {
			t.Fatal(e)
		}
		c := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: f.Text, OutputText: f.Text}
		started := time.Now()
		s.auditFinalMainlineContent(ctx, &state, &c)
		accepted := state.FinalContentCandidateApproved(c)
		messages, _ := finalMainlineContentMessages(&state, c, mother)
		rows = append(rows, map[string]any{"case": f, "accepted": accepted, "matches_expected": accepted == f.WantPass, "p0": c.P0Report, "audit": c.FinalContentAudit, "messages": messages, "full_ms": time.Since(started).Milliseconds()})
		if accepted != f.WantPass {
			t.Errorf("case=%s expected=%v actual=%v raw=%s", f.Name, f.WantPass, accepted, c.FinalContentAudit.Raw)
		} else {
			t.Logf("%s selected=%v", f.Name, accepted)
		}
	}
}
