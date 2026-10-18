package agentgateway

import (
	"context"
	"strings"
	"testing"
)

func TestTextModelProfilesResolveAliasesAndPreserveOtherRoles(t *testing.T) {
	for _, c := range []struct{ model, id string }{{"qwen3.7-plus", "qwen-plus"}, {"Qwen3.7Plus", "qwen-plus"}, {"qwen3.7-plus-2026-05-26", "qwen-plus"}, {"qwen3.7-flash", "qwen-flash"}, {"deepseek-v4-flash-0731", "deepseek"}, {"unconfigured-vendor", "generic"}} {
		id, hash, _, err := TextPromptContract(c.model)
		if err != nil || id != c.id || len(hash) != 64 {
			t.Fatal(c, id, hash, err)
		}
		req := Request{Stage: "speech_generation", EnableThinking: true, Messages: []Message{{Role: "user", Content: "问题"}}}
		prepared, err := PrepareTextRequest(SpeechModel{Model: c.model}, req)
		if err != nil || prepared.EnableThinking || prepared.TextPromptID != c.id || len(prepared.Messages) != 2 {
			t.Fatal(prepared, err)
		}
		if !strings.Contains(prepared.Messages[0].Content, "允许自然区间和近似表达") {
			t.Fatal("price paraphrases lost")
		}
		if !strings.Contains(prepared.Messages[1].Content, "超过 4 位") || !strings.Contains(prepared.Messages[1].Content, "简称") {
			t.Fatal("nickname prompt not adjacent to task")
		}
		if c.id == "qwen-flash" && (prepared.Temperature == nil || *prepared.Temperature != 0.25) {
			t.Fatal("model sampling configuration not applied")
		}
		again, err := PrepareTextRequest(SpeechModel{Model: c.model}, prepared)
		if err != nil || len(again.Messages) != len(prepared.Messages) {
			t.Fatal("adaptation duplicated")
		}
		req.Stage = "style_analysis"
		unchanged, err := PrepareTextRequest(SpeechModel{Model: c.model}, req)
		if err != nil || len(unchanged.Messages) != 1 || !unchanged.EnableThinking {
			t.Fatal("analysis was changed")
		}
		req.Stage = "speech_review"
		review, err := PrepareTextRequest(SpeechModel{Model: c.model}, req)
		if err != nil || len(review.Messages) != len(req.Messages) || review.TextPromptSHA != "" {
			t.Fatal("writing adaptation mixed into final review", review, err)
		}
	}
}

type roleFixture struct {
	profile SpeechModel
	role    string
}

func (r *roleFixture) ResolveTextRole(context.Context, string) (string, error) { return r.role, nil }
func (r *roleFixture) ResolveSpeechModel(_ context.Context, id string) (*SpeechModel, error) {
	copy := r.profile
	copy.ID = id
	return &copy, nil
}
func TestTextCacheIdentityChangesWithOperatorModelAndPrompt(t *testing.T) {
	resolver := &roleFixture{role: "plus", profile: SpeechModel{Model: "qwen3.7-plus", BaseURL: "https://example.test/v1"}}
	g := New("qwen", "qwen3.8-flash").WithSpeechModels(resolver)
	a, err := g.TextGenerationIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resolver.profile.Model = "deepseek-v4-flash"
	b, _ := g.TextGenerationIdentity(context.Background())
	resolver.profile.SystemPrompt = "用户自己的基础提示词"
	c, _ := g.TextGenerationIdentity(context.Background())
	if a == b || b == c || len(a) != 64 {
		t.Fatal("switched model served stale cache identity")
	}
}
