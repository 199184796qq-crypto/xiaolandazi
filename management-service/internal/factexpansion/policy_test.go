package factexpansion

import "testing"

func TestCompileUsesUserAuthorizedLevelsWithoutRelaxingLockedBoundaries(t *testing.T) {
	for _, test := range []struct {
		freedom int
		level   string
	}{{0, "conservative"}, {40, "balanced"}, {70, "open"}, {95, "edge_compliant"}} {
		policy := Compile(test.freedom)
		if policy.Level != test.level || policy.Freedom != test.freedom || !policy.UserAuthorized || !policy.BoundaryRewriteFirst {
			t.Fatalf("freedom %d: %+v", test.freedom, policy)
		}
		if len(policy.AlwaysLocked) < 3 {
			t.Fatalf("freedom %d lost locked boundaries: %+v", test.freedom, policy)
		}
	}
}
