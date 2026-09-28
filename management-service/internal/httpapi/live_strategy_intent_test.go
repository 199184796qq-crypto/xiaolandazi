package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormalizeLiveStrategyIntentKind(t *testing.T) {
	tests := map[string]string{
		"chat":    "chat",
		"COMMAND": "command",
		"clarify": "clarify",
		"other":   "clarify",
	}
	for input, want := range tests {
		if got := normalizeLiveStrategyIntentKind(input); got != want {
			t.Fatalf("kind %q => %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeLiveStrategyIntent(t *testing.T) {
	valid := []string{
		"chat",
		"product.add", "product.update", "product.disable",
		"benefit.add", "benefit.update", "benefit.disable",
		"fact.add", "fact.update", "fact.disable",
		"script.add", "script.update",
		"plan.bind", "plan.unbind", "plan.switch",
	}
	for _, input := range valid {
		if got := normalizeLiveStrategyIntent(input); got != input {
			t.Fatalf("intent %q => %q", input, got)
		}
	}
	if got := normalizeLiveStrategyIntent("DROP_DATABASE"); got != "unknown" {
		t.Fatalf("unknown intent => %q, want unknown", got)
	}
}

func TestLiveStrategyExecutableActions(t *testing.T) {
	want := []string{
		"add_live_product", "confirm_live_product_update", "confirm_live_product_disable",
		"add_live_benefit", "confirm_live_benefit_update", "confirm_live_benefit_disable",
		"add_live_fact", "confirm_live_fact_update", "confirm_live_fact_disable",
		"add_live_script_reference", "confirm_live_script_reference_update", "confirm_live_script_reference_disable",
		"confirm_live_plan_bind", "confirm_live_plan_unbind", "confirm_live_plan_switch",
	}
	for _, action := range want {
		if _, ok := liveStrategyExecutableActions[action]; !ok {
			t.Fatalf("missing executable action %q", action)
		}
	}
	if _, ok := liveStrategyExecutableActions["clarify_live_strategy_intent"]; ok {
		t.Fatal("clarification action must never be directly executable")
	}
}

func TestLiveStrategyActionProtocolStates(t *testing.T) {
	failed := liveStrategyFailed("stale_confirmation", "stale", nil)
	if failed.State != agentStateFailed || failed.Code != "stale_confirmation" {
		t.Fatalf("unexpected failed response: %#v", failed)
	}
	succeeded := liveStrategySucceeded("ok", "done", map[string]any{"module": "products"})
	if succeeded.State != agentStateSucceeded || succeeded.Code != "ok" {
		t.Fatalf("unexpected succeeded response: %#v", succeeded)
	}
}

func TestReadAgentCompatibleJSONAcceptsImagePayloadAndExtraFields(t *testing.T) {
	body := `{
  "message":"@图片1 添加2号链接",
  "plan_id":2,
  "current_mode":"products",
  "history":[{"role":"user","text":"上一句"}],
  "image_urls":["data:image/webp;base64,QUJD"],
  "client_context":{"image_labels":["图片1"]}
}`
	req := httptest.NewRequest("POST", "/api/v1/live/rooms/15/agent/interpret", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	var input liveStrategyIntentInput
	if err := readAgentCompatibleJSON(recorder, req, &input); err != nil {
		t.Fatalf("compatible agent payload rejected: %v", err)
	}
	if input.Message != "@图片1 添加2号链接" || input.PlanID != 2 || len(input.ImageURLs) != 1 {
		t.Fatalf("unexpected decoded input: %#v", input)
	}
}

func TestReadAgentCompatibleJSONRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"message":"a"} {"message":"b"}`))
	recorder := httptest.NewRecorder()
	var input liveStrategyIntentInput
	if err := readAgentCompatibleJSON(recorder, req, &input); err == nil {
		t.Fatal("multiple JSON values must be rejected")
	}
}
