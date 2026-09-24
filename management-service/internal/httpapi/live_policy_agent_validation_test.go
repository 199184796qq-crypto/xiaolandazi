package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestAdminPolicyModelRulesComplete(t *testing.T) {
	if adminPolicyModelRulesComplete(nil) {
		t.Fatal("nil rules must be incomplete")
	}
	if adminPolicyModelRulesComplete([]model.LivePolicyRule{{
		Title: "事实真实性", ExecutionMode: model.LivePolicyModeIntent,
	}}) {
		t.Fatal("title-only rule must be incomplete")
	}
	if !adminPolicyModelRulesComplete([]model.LivePolicyRule{{
		Title:         "事实真实性",
		Text:          "不得编造事实",
		ExecutionMode: model.LivePolicyModeIntent,
	}}) {
		t.Fatal("full intent rule should be complete")
	}
	if adminPolicyModelRulesComplete([]model.LivePolicyRule{{
		Title:         "固定原话",
		Text:          "按原话执行",
		ExecutionMode: model.LivePolicyModeVerbatim,
	}}) {
		t.Fatal("verbatim without fixed_text must be incomplete")
	}
}
