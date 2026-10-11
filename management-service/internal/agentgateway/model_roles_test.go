package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type unifiedRoleFixture struct {
	profiles               map[string]SpeechModel
	system, speech, review string
	err                    error
}

func (r *unifiedRoleFixture) ResolveTextRole(_ context.Context, stage string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if UsesReviewModel(stage) {
		return r.review, nil
	}
	if UsesSpeechModel(stage) {
		return r.speech, nil
	}
	return r.system, nil
}
func (r *unifiedRoleFixture) ResolveSpeechModel(_ context.Context, id string) (*SpeechModel, error) {
	p, ok := r.profiles[id]
	if !ok {
		return nil, errors.New("profile missing")
	}
	return &p, nil
}

func TestUnifiedRolesRouteLegacyAgentsAndPinInFlightRepairs(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		raw, _ := json.Marshal(payload)
		if !strings.Contains(string(raw), "任务协议") || !strings.Contains(string(raw), "本次资料") {
			t.Error("task prompt lost")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": payload["model"]}}}})
	}))
	defer server.Close()
	previous := speechHTTPClient
	speechHTTPClient = server.Client()
	defer func() { speechHTTPClient = previous }()
	r := &unifiedRoleFixture{profiles: map[string]SpeechModel{}, system: "max", speech: "flash", review: "review"}
	for _, id := range []string{"max", "flash", "review"} {
		p := speechFixture()
		p.ID = id
		p.Model = "fixture-" + id
		p.BaseURL = server.URL
		r.profiles[id] = p
	}
	ctx := WithModelResolver(context.Background(), r)
	g := NewFromContext(ctx)
	for _, c := range []struct{ stage, want string }{{"", "max"}, {"style_analysis", "max"}, {"speech_miner_guide", "max"}, {"mainline_angle_plan", "max"}, {"interaction_learning", "max"}, {"speech_generation", "flash"}, {"interaction_agent_answer", "flash"}, {"interaction_agent_plan", "max"}, {"mainline_text", "flash"}, {"interaction_bridge", "flash"}, {"rolling_feedback", "flash"}, {"accepted_macro_feedback", "flash"}, {"rolling_style_feedback", "flash"}, {"mainline_p0_context", "review"}, {"speech_review", "review"}, {"interaction_p0_context", "review"}} {
		req := Request{Stage: c.stage, Provider: "qwen", Model: "old-per-function-model", Messages: []Message{{Role: "system", Content: "任务协议"}, {Role: "user", Content: "本次资料"}}}
		out, err := g.Complete(ctx, req)
		if err != nil || out.Model != "fixture-"+c.want || out.Text != out.Model {
			t.Fatalf("stage=%s out=%+v err=%v", c.stage, out, err)
		}
	}
	// The selected model for a reply/repair is pinned, even after the default moves.
	for _, stage := range []string{"lecture_task_review", "lecture_source_review", "fact_fields_normalize_review", "fact_expression_examples_review", "lecture_detail_normalize_review", "fact_template_influence_review", "speech_constraint_review", "speech_constraint_text_review"} {
		out, err := g.Complete(ctx, Request{Stage: stage, Messages: []Message{{Role: "system", Content: "任务协议"}, {Role: "user", Content: "本次资料"}}})
		if err != nil || out.Model != "fixture-review" {
			t.Fatal(stage, out, err)
		}
	}
	r.system = "flash"
	out, err := g.Complete(ctx, Request{Provider: "anchor:max", Model: "fixture-max", Messages: []Message{{Role: "system", Content: "任务协议"}, {Role: "user", Content: "本次资料"}}})
	if err != nil || out.Model != "fixture-max" {
		t.Fatal("in-flight model changed", out, err)
	}
	r.err = errors.New("configuration unavailable")
	if _, err = g.Complete(ctx, Request{Stage: "style_analysis"}); err == nil {
		t.Fatal("silently fell back to server model")
	}
}

func TestUnifiedModelSelectionsRequireEnabledProfiles(t *testing.T) {
	p := speechFixture()
	input := SpeechModelsInput{Profiles: []SpeechModelInput{{SpeechModel: p}}, DefaultID: p.ID, SystemDefaultID: p.ID}
	if err := ValidateSpeechModels(input); err != nil {
		t.Fatal(err)
	}
	input.SystemDefaultID = "missing"
	if ValidateSpeechModels(input) == nil {
		t.Fatal("missing system profile accepted")
	}
	input.SystemDefaultID = p.ID
	input.DefaultID = ""
	if ValidateSpeechModels(input) == nil {
		t.Fatal("unified mode lost speech role")
	}
	input.DefaultID = p.ID
	input.ReviewDefaultID = "missing"
	if ValidateSpeechModels(input) == nil {
		t.Fatal("missing review profile accepted")
	}
	input.ReviewDefaultID = "disabled"
	disabled := p
	disabled.ID, disabled.Enabled = "disabled", false
	input.Profiles = append(input.Profiles, SpeechModelInput{SpeechModel: disabled})
	if ValidateSpeechModels(input) == nil {
		t.Fatal("disabled review profile accepted")
	}
}
