// Package messagecaptain broadcasts runtime messages to registered members.
// Producers publish facts; each member decides whether it can handle them.
package messagecaptain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const AudioTerminal = "audio.output.terminal"

type Message struct {
	ID         string          `json:"id"`
	Sequence   uint64          `json:"sequence"`
	Kind       string          `json:"kind"`
	RoomID     int64           `json:"room_id"`
	RunID      string          `json:"run_id"`
	TaskID     string          `json:"task_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}
type Handler func(context.Context, Message) error
type member struct {
	id     uint64
	name   string
	handle Handler
}
type Captain struct {
	mu           sync.RWMutex
	members      []member
	boot         string
	sequence     atomic.Uint64
	registration atomic.Uint64
}

func New() *Captain {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return &Captain{boot: hex.EncodeToString(raw[:])}
}
func (c *Captain) Register(name string, handle Handler) func() {
	if handle == nil {
		return func() {}
	}
	id := c.registration.Add(1)
	c.mu.Lock()
	c.members = append(c.members, member{id, name, handle})
	c.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			for i, m := range c.members {
				if m.id == id {
					c.members = append(c.members[:i], c.members[i+1:]...)
					break
				}
			}
		})
	}
}
func (c *Captain) Publish(ctx context.Context, message Message) []error {
	message.Sequence = c.sequence.Add(1)
	message.ID = fmt.Sprintf("%s:%d", c.boot, message.Sequence)
	if message.OccurredAt.IsZero() {
		message.OccurredAt = time.Now().UTC()
	}
	c.mu.RLock()
	members := append([]member(nil), c.members...)
	c.mu.RUnlock()
	var failures []error
	for _, m := range members {
		copy := message
		copy.Payload = append(json.RawMessage(nil), message.Payload...)
		if err := deliver(ctx, m, copy); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", m.name, err))
		}
	}
	return failures
}
func deliver(ctx context.Context, m member, message Message) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("message handler panic: %v", value)
		}
	}()
	return m.handle(ctx, message)
}
