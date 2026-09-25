package db

import (
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestSalesBusinessHandoffTransitions(t *testing.T) {
	for _, from := range []string{"pending", "accepted", "in_progress", "completed", "invalid"} {
		for _, to := range []string{"pending", "accepted", "in_progress", "completed", "invalid"} {
			want := from == to && from != "invalid" || from == "pending" && to == "accepted" || from == "accepted" && to == "in_progress" || from == "in_progress" && to == "completed"
			if got := validCustomerHandoffTransition(from, to); got != want {
				t.Fatalf("%s -> %s: got %t want %t", from, to, got, want)
			}
		}
	}
}

func TestSalesBusinessHandoverScopeFailsClosed(t *testing.T) {
	for _, scope := range []model.StaffBusinessScope{{Mode: "self", ActorUserID: 99}, {Mode: "unknown"}, {Mode: "groups"}} {
		q, args := salesHandoverScopeSQL(scope, "ss.user_id")
		if q != "1=0" || len(args) != 0 {
			t.Fatalf("scope escaped: %q %#v", q, args)
		}
	}
	q, args := salesHandoverScopeSQL(model.StaffBusinessScope{Mode: "groups", GroupIDs: []int64{7, 8}}, "ss.user_id")
	if !strings.Contains(q, "IN (?,?)") || len(args) != 2 || strings.Contains(q, "employment_status='active'") {
		t.Fatal("manager must retain department boundary including departed employees")
	}
	for _, value := range []string{"; DROP TABLE users", "name-asc; --", "invalid"} {
		if got := salesLeadOrderBy(value); got != "l.created_at DESC, l.id DESC" {
			t.Fatal("untrusted sort entered SQL")
		}
	}
}
