package collector

import (
	roomstore "livecompanion/core/internal/room"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCollectorUsesOneRoomAuthorityAndNoLocalGrace(t *testing.T) {
	if roomstore.CollectionGrace != 60*time.Second {
		t.Fatal("room grace changed")
	}
	source, err := os.ReadFile("manager.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"SetStatus(", "MarkLive(", "nextOfflineConfirmation(", "offlineConfirmationWindow"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("collector regained authority: %s", forbidden)
		}
	}
	if !strings.Contains(string(source), "ApplyCollectedEvent(") || !strings.Contains(string(source), "ReportCollection(") {
		t.Fatal("evidence/event ordering gate missing")
	}
	metrics, err := os.ReadFile("runtime_updates.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metrics), "TouchLiveMetrics(") || strings.Contains(string(metrics), "MarkLive(") {
		t.Fatal("metrics can revive stopped room")
	}
}

func TestCapturedSessionReopensAfterWorkerReplacementWithoutBlindNewSession(t *testing.T) {
	if !captureSessionChanged("same-platform-id", "same-platform-id", 0, 0, true) {
		t.Fatal("ended cached session not reopened after worker replacement")
	}
	if captureSessionChanged("same-platform-id", "same-platform-id", 0, 0, false) {
		t.Fatal("ordinary retry created another session")
	}
	if !captureSessionChanged("same-platform-id", "same-platform-id", 1, 0, false) {
		t.Fatal("authority end revision ignored")
	}
	if captureSessionChanged("", "same-platform-id", 1, 0, true) {
		t.Fatal("no platform evidence invented a session")
	}
}
