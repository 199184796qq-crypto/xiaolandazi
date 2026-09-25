package model

import "testing"

func TestWorkInboxProductProfiles(t *testing.T) {
	for _, role := range []string{"", "customer", "unknown"} {
		if WorkInboxAvailable(role) {
			t.Fatalf("inbox unexpectedly available to %q", role)
		}
	}
	for _, role := range []string{"platform_admin", "staff", "sales_staff", "agent_admin"} {
		if !WorkInboxAvailable(role) {
			t.Fatalf("existing work profile removed: %q", role)
		}
	}
}
