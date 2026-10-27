package agentgateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
)

type pinnedRoleFixture struct {
	profiles      map[string]SpeechModel
	system        string
	review        string
	operatorCalls int
	poolCalls     int
}

func (f *pinnedRoleFixture) ResolveTextRole(_ context.Context, stage string) (string, error) {
	if UsesInteractionAnswerModel(stage) || UsesReviewModel(stage) {
		return f.review, nil
	}
	return f.system, nil
}

func (f *pinnedRoleFixture) ResolveSpeechModel(_ context.Context, id string) (*SpeechModel, error) {
	f.poolCalls++
	p, ok := f.profiles[id]
	if !ok {
		return nil, fmt.Errorf("missing profile %s", id)
	}
	return &p, nil
}

func (f *pinnedRoleFixture) ResolveOperatorSpeechModel(_ context.Context, id string) (*SpeechModel, error) {
	f.operatorCalls++
	p, ok := f.profiles[id]
	if !ok {
		return nil, fmt.Errorf("missing profile %s", id)
	}
	return &p, nil
}

// Interaction answers are spoken to viewers. When the operator pins them, the
// call must use the operator profile directly and never take the room pool's
// leased 口播 model, otherwise the pool silently overrides the role choice.
func TestInteractionAnswerPinsOperatorRoleOutsidePool(t *testing.T) {
	review := SpeechModel{ID: "review", Model: "deepseek-flash", BaseURL: "https://api.deepseek.com", SystemPrompt: ""}
	pooled := SpeechModel{ID: "pool", Model: "qwen3.7-flash", BaseURL: "https://dashscope.aliyuncs.com"}
	f := &pinnedRoleFixture{
		profiles: map[string]SpeechModel{"review": review, "pool": pooled},
		system:   "pool",
		review:   "review",
	}
	ctx := WithModelResolver(context.Background(), f)
	g := NewFromContext(ctx)

	got, err := g.ModelRoleIdentity(ctx, "interaction_single_direct_answer")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(review.ID+":"+review.Model+":"+review.BaseURL+":"+review.SystemPrompt)))
	if got != want {
		t.Fatalf("interaction identity used pooled model: got %s want %s", got, want)
	}
	if f.operatorCalls != 1 || f.poolCalls != 0 {
		t.Fatalf("interaction answer did not bypass pool: operator=%d pool=%d", f.operatorCalls, f.poolCalls)
	}

	// Non-pinned room work keeps using the leased pool model.
	if _, err = g.ModelRoleIdentity(ctx, "mainline_text"); err != nil {
		t.Fatal(err)
	}
	if f.poolCalls != 1 || f.operatorCalls != 1 {
		t.Fatalf("mainline no longer uses the pool: operator=%d pool=%d", f.operatorCalls, f.poolCalls)
	}
}

func TestInteractionAnswerStagesAreLimited(t *testing.T) {
	// Every interaction speech stage renders spoken words, so it must render
	// them on the operator's review model rather than the room's pool model.
	for _, stage := range []string{
		"interaction_single_direct_answer", "interaction_single_greet", "interaction_single_answer",
		"interaction_no_fact_boundary", "interaction_boundary_line", "interaction_covered_facts",
		"interaction_welcome_greeting", "interaction_welcome_hook", "interaction_welcome_intro",
		"interaction_welcome_highlight", "interaction_welcome_humanize",
		"interaction_p0_sentence_rewrite", "interaction_order_thanks", "interaction_order_fulfillment",
	} {
		if !UsesInteractionAnswerModel(stage) {
			t.Fatalf("%s should be pinned", stage)
		}
	}
	// Mainline and non-speech work keep the room's pool model.
	for _, stage := range []string{"mainline_text", "mainline_text_rewrite", "interaction_welcome_benefit_recognize", ""} {
		if UsesInteractionAnswerModel(stage) {
			t.Fatalf("%s must stay on the room model", stage)
		}
	}
}
