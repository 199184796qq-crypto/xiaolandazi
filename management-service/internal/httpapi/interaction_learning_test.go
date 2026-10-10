package httpapi

import (
	"livecompanion/management/internal/auth"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSimulationDeletionRequiresAuthentication(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, id := range []string{"", "1"} {
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/live/rooms/8/interaction-learning?plan_id=4", nil)
		r.SetPathValue("roomID", "8")
		if id != "" {
			r.SetPathValue("sessionID", id)
		}
		w := httptest.NewRecorder()
		s.interactionSimulationDelete(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("deletion allowed without authentication: %d", w.Code)
		}
	}
}

func TestSimulationCannotSilentlyDiscardUnknownOrIncompleteFields(t *testing.T) {
	good := `{"items":[{"question":"不抽可以吗","context":"食品包装","kind":"missing_fact","meaning":"能否选择非真空","reason":"可选规则缺失","suggested_rule":"","needed_facts":["是否可不抽真空"]}]}`
	items, err := decodeInteractionSimulation(good)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	for _, raw := range []string{`{"items":null}`, `{"items":[{"question":"不抽","context":"包装","kind":"unsupported","meaning":"真空","reason":"简称"}]}`, good + ` {}`, `{"items":[],"other":1}`} {
		if _, err := decodeInteractionSimulation(raw); err == nil {
			t.Fatalf("bad simulation accepted: %s", raw)
		}
	}
}

func TestSimulationSuggestionsAreForUserJudgment(t *testing.T) {
	for _, raw := range []string{`{"items":[]}`, `{"items":[{"question":"不抽","context":"包装","kind":"semantic","meaning":"真空","reason":"简称","suggested_rule":"","needed_facts":[]}]}`, `{"items":[{"question":"几天到","context":"物流","kind":"missing_fact","meaning":"到货时效","reason":"模型建议","needed_facts":[]}]}`} {
		if _, err := decodeInteractionSimulation(raw); err != nil {
			t.Fatalf("model suggestion was vetoed before user judgment: %s: %v", raw, err)
		}
	}
}
