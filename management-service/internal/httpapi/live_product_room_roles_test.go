package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestNormalizeLiveRoomProductRoles(t *testing.T) {
	roles, err := normalizeLiveRoomProductRoleValues([]string{"main", "profit", "main"})
	if err != nil || len(roles) != 2 || roles[0] != "main" || roles[1] != "profit" {
		t.Fatalf("unexpected normalized roles: %v %v", roles, err)
	}
	if _, err := normalizeLiveRoomProductRoleValues([]string{"ordinary", "main"}); err == nil {
		t.Fatal("ordinary positioning must remain exclusive")
	}
	if _, err := normalizeLiveRoomProductRoleValues([]string{"unknown"}); err == nil {
		t.Fatal("unknown positioning must be rejected")
	}
}

func TestRoomRolesNeverBecomeSpeakableFacts(t *testing.T) {
	manifest := compileAuthorizedGenerationFacts(nil, nil, []model.LiveAgentPlanProductLink{{
		ID: 1, LinkKey: "1号链接", ProductName: "菜籽油", RoomRoles: []string{"main", "profit"}, Status: "active", VersionNo: 1,
	}})
	for _, fact := range manifest {
		if fact.Predicate == "room_role" || fact.Value == "主推" || fact.Value == "利润" {
			t.Fatalf("internal positioning leaked into generation facts: %+v", fact)
		}
	}
}
