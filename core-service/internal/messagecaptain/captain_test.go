package messagecaptain

import (
	"context"
	"errors"
	"testing"
)

func TestBroadcastIsolationUnsubscribeAndFailureDoesNotBlockOtherMembers(t *testing.T) {
	bus := New()
	seen := []string{}
	bus.Register("cannot-handle", func(context.Context, Message) error { return nil })
	bus.Register("broken", func(context.Context, Message) error { panic("broken member") })
	bus.Register("first", func(_ context.Context, m Message) error {
		m.Payload[0] = 'x'
		seen = append(seen, m.ID)
		return errors.New("delivery problem")
	})
	remove := bus.Register("second", func(_ context.Context, m Message) error {
		if string(m.Payload) != "{}" {
			t.Fatal("another member mutated payload")
		}
		seen = append(seen, m.ID)
		return nil
	})
	failures := bus.Publish(context.Background(), Message{Kind: "unknown.to.producer", Payload: []byte("{}")})
	if len(failures) != 2 || len(seen) != 2 || seen[0] == "" || seen[0] != seen[1] {
		t.Fatal(failures, seen)
	}
	remove()
	remove()
	bus.Publish(context.Background(), Message{Payload: []byte("{}")})
	if len(seen) != 3 {
		t.Fatal("unregistered member received message")
	}
}
