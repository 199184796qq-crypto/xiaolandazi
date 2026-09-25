package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestTerminalCustomerInboxDisabledBeforeStorage(t *testing.T) {
	// Nil dependencies prove the gate does not touch auth storage, Redis or count SQL.
	s := &Server{}
	actor := model.Actor{UserID: 50, Role: "customer"}
	r := httptest.NewRequest("GET", "/api/v1/work/inbox", nil)
	if _, err := s.inboxScopeForActor(r, actor); err == nil {
		t.Fatal("customer inbox scope accepted")
	}
	w := httptest.NewRecorder()
	if !s.tryInboxAgentResponse(w, r, actor, "我的待办有哪些") {
		t.Fatal("missing retired-feature guidance")
	}
	var out systemAgentChatOutput
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Navigate != nil || out.Action != nil || strings.Contains(w.Body.String(), "/work/inbox") {
		t.Fatal("customer was sent back to work inbox")
	}
	if !strings.Contains(out.Reply, "对应业务页面") {
		t.Fatal("customer progress guidance missing")
	}
	for _, capability := range out.Capabilities {
		if strings.Contains(capability, "待办") {
			t.Fatal("customer inbox capability survived")
		}
	}
	w = httptest.NewRecorder()
	if s.tryInboxAgentResponse(w, r, actor, "运维协助待确认是怎么回事") || w.Body.Len() != 0 {
		t.Fatal("ordinary support consultation intercepted by inbox")
	}
}
