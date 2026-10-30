package coreaudio

import (
	"context"
	"errors"
	"livecompanion/core/internal/messagecaptain"
	"testing"
	"time"
)

func TestInteractionAudioReservationPropagatesRejectionAndDuration(t *testing.T) {
	c, _ := newTestClient(t, 100)
	captain := messagecaptain.New()
	c.SetMessageCaptain(captain)
	seen := false
	remove := captain.Register("scheduling-budget", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind != "interaction.v2.reserve-audio" {
			return nil
		}
		seen = true
		if m.RoomID != 29 || len(m.Payload) == 0 {
			t.Fatal(m)
		}
		return errors.New("expired or no audio budget")
	})
	if err := c.reserveInteractionV2(context.Background(), 29, "decision", 7000, time.Now()); !errors.Is(err, errInteractionV2Wait) || !seen {
		t.Fatal(err, seen)
	}
	remove()
	if err := c.reserveInteractionV2(context.Background(), 29, "decision", 7000, time.Now()); err != nil {
		t.Fatal(err)
	}
}
