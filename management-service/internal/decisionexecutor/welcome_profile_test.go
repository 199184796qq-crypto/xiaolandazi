package decisionexecutor

import "testing"

func TestWelcomeProfileNeverRepeatsInARow(t *testing.T) {
	worker := &Worker{}
	last := ""
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		profile := worker.pickWelcomeProfile(7)
		if profile.Name == last {
			t.Fatalf("same welcome shape twice in a row: %s", profile.Name)
		}
		last = profile.Name
		seen[profile.Name] = true
	}
	for _, profile := range welcomeProfiles {
		if !seen[profile.Name] {
			t.Fatalf("shape never picked: %s", profile.Name)
		}
	}
}

func TestWelcomeProfileParts(t *testing.T) {
	short := welcomeProfile{Name: "极短", Parts: []string{"greeting"}}
	if !profileNeeds(short, "greeting") || profileNeeds(short, "intro") || profileNeeds(short, "hook") {
		t.Fatalf("short shape parts wrong: %+v", short)
	}
	full := welcomeProfile{Name: "丰富", Parts: []string{"greeting", "intro", "hook", "highlight"}}
	for _, part := range []string{"greeting", "intro", "hook", "highlight"} {
		if !profileNeeds(full, part) {
			t.Fatalf("rich shape missing %s", part)
		}
	}
}
