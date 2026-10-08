package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
)

func TestProductScopeModelRepairsExtraFieldsAndNumericLinks(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	badExtra := strings.Replace(validScopeResponse, `"shared":false`, `"shared":false,"comparison":false`, 1)
	badNumber := strings.Replace(validScopeResponse, `"links":["1"]`, `"links":[1]`, 1)
	calls := 0
	inputs := []agentgateway.Message{{Role: "system", Content: "schema"}, {Role: "user", Content: "original immutable inputs"}}
	err := validatedCycleProductScope(context.Background(), scope, facts, tasks, inputs, "repair contract", func(_ context.Context, stage string, messages []agentgateway.Message) (string, error) {
		calls++
		if messages[1].Content != inputs[1].Content {
			t.Fatal("input overwritten")
		}
		if calls > 1 && (stage != "cycle_product_scope_repair" || !strings.Contains(messages[len(messages)-1].Content, "validation_error")) {
			t.Fatal("missing corrective feedback")
		}
		switch calls {
		case 1:
			return badExtra, nil
		case 2:
			return badNumber, nil
		default:
			return validScopeResponse, nil
		}
	})
	if err != nil || calls != 3 || scope.Facts[0].Text != facts[0].Text {
		t.Fatal(err, calls, scope)
	}
}

func TestProductScopeModelRepairFailsClosedAndKeepsOldScope(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	scope.Facts = []anchorcycle.ScopedFact{{Text: "prior immutable fact"}}
	calls := 0
	err := validatedCycleProductScope(context.Background(), scope, facts, tasks, nil, "repair", func(_ context.Context, _ string, _ []agentgateway.Message) (string, error) {
		calls++
		return strings.Replace(validScopeResponse, `"links":["2"]`, `"links":["99"]`, 1), nil
	})
	if err == nil || calls != 3 || len(scope.Facts) != 1 || scope.Facts[0].Text != "prior immutable fact" || !strings.Contains(err.Error(), "未保存或发布") {
		t.Fatal(err, calls, scope)
	}
	calls = 0
	err = validatedCycleProductScope(context.Background(), scope, facts, tasks, nil, "repair", func(_ context.Context, _ string, _ []agentgateway.Message) (string, error) {
		calls++
		return "", errors.New("provider unavailable")
	})
	if err == nil || calls != 1 {
		t.Fatal("transport errors retried blindly", err, calls)
	}
}
