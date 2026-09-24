package coordination

import "testing"

func TestParseLeaseValue(t *testing.T) {
	owner, fence, err := parseLeaseValue("core-a|42")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "core-a" || fence != 42 {
		t.Fatalf("owner=%q fence=%d", owner, fence)
	}
}

func TestParseLeaseValueRejectsInvalidFence(t *testing.T) {
	for _, value := range []string{"", "core-a", "core-a|0", "core-a|bad"} {
		if _, _, err := parseLeaseValue(value); err == nil {
			t.Fatalf("expected invalid lease value %q", value)
		}
	}
}

func TestRoomLeaseKeysAreTenantScoped(t *testing.T) {
	lease, fence := roomLeaseKeys(27, 991)
	if lease != "livecompanion:cluster:room-lease:27:991" {
		t.Fatalf("lease key=%q", lease)
	}
	if fence != lease+":fence" {
		t.Fatalf("fence key=%q", fence)
	}
}
