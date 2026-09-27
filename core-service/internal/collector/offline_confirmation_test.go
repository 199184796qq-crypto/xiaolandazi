package collector

import "testing"

func TestOfflineConfirmationRequiresTwoConsecutiveNoActivityFailures(t *testing.T) {
	suspected, confirmed := nextOfflineConfirmation(false, false)
	if !suspected || confirmed {
		t.Fatalf("first offline: suspected=%v confirmed=%v", suspected, confirmed)
	}

	suspected, confirmed = nextOfflineConfirmation(suspected, false)
	if suspected || !confirmed {
		t.Fatalf("second offline: suspected=%v confirmed=%v", suspected, confirmed)
	}
}

func TestOfflineConfirmationClearsAfterRealActivity(t *testing.T) {
	suspected, confirmed := nextOfflineConfirmation(false, false)
	if !suspected || confirmed {
		t.Fatalf("first offline: suspected=%v confirmed=%v", suspected, confirmed)
	}

	suspected, confirmed = nextOfflineConfirmation(suspected, true)
	if !suspected || confirmed {
		t.Fatalf("offline after recovered activity should restart confirmation: suspected=%v confirmed=%v", suspected, confirmed)
	}

	suspected, confirmed = nextOfflineConfirmation(false, true)
	if !suspected || confirmed {
		t.Fatalf("activity followed by later timeout should be first suspicion: suspected=%v confirmed=%v", suspected, confirmed)
	}
}
