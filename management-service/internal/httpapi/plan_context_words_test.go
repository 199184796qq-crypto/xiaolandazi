package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestContextWordRoutesRequireActor(t *testing.T) {
	for _, method := range []string{"GET", "PUT", "POST"} {
		r := httptest.NewRequest(method, "/api/v1/live-agent-plans/4/context-words", nil)
		w := httptest.NewRecorder()
		s := &Server{}
		if method == "POST" {
			s.organizeContextWords(w, r)
		} else {
			s.planContextWords(w, r)
		}
		if w.Code != 401 {
			t.Fatal(method, w.Code)
		}
	}
}
func TestContextWordSuggestionsHaveNoModelIDsAndNeedConfirmation(t *testing.T) {
	items, q, e := parseContextWordSuggestions("```json\n{\"items\":[{\"id\":\"invented\",\"triggers\":[\"切\"],\"meaning\":\"切块\",\"when\":\"讨论商品处理\"}],\"questions\":[\"砍指什么？\"]}\n```")
	if e != nil || len(items) != 1 || items[0].ID != "" || !items[0].Enabled || items[0].When != "讨论商品处理" || len(q) != 1 {
		t.Fatal(items, q, e)
	}
	if _, _, e = parseContextWordSuggestions(`{"items":[{"triggers":["切"]}]}`); e == nil {
		t.Fatal("missing meaning accepted")
	}
	if _, _, e = parseContextWordSuggestions(`{"items":[]}`); e == nil {
		t.Fatal("empty response accepted")
	}
}
