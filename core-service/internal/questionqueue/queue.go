package questionqueue

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultTTL        = 2 * time.Minute
	DefaultClaimLease = 45 * time.Second
)

type Status string

const (
	StatusPending Status = "PENDING"
	StatusClaimed Status = "CLAIMED"
)

type Item struct {
	ID          string     `json:"id"`
	RoomID      int64      `json:"room_id"`
	Question    string     `json:"question"`
	Topic       string     `json:"topic,omitempty"`
	UserID      string     `json:"user_id,omitempty"`
	Count       int        `json:"count"`
	Status      Status     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSeenAt  time.Time  `json:"last_seen_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	AvailableAt time.Time  `json:"available_at"`
	ClaimedAt   *time.Time `json:"claimed_at,omitempty"`
	ClaimUntil  *time.Time `json:"claim_until,omitempty"`
}

type Queue struct {
	mu         sync.Mutex
	rooms      map[int64][]*Item
	now        func() time.Time
	ttl        time.Duration
	claimLease time.Duration
	seq        atomic.Uint64
}

func New() *Queue {
	return NewWithClock(time.Now, DefaultTTL, DefaultClaimLease)
}

func NewWithClock(now func() time.Time, ttl, claimLease time.Duration) *Queue {
	if now == nil {
		now = time.Now
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if claimLease <= 0 {
		claimLease = DefaultClaimLease
	}
	return &Queue{
		rooms:      make(map[int64][]*Item),
		now:        now,
		ttl:        ttl,
		claimLease: claimLease,
	}
}

func (q *Queue) Enqueue(roomID int64, question, topic, userID string) (Item, bool) {
	question = strings.TrimSpace(question)
	topic = strings.ToUpper(strings.TrimSpace(topic))
	userID = strings.TrimSpace(userID)
	if roomID <= 0 || question == "" {
		return Item{}, false
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.now().UTC()
	q.pruneLocked(roomID, now)
	key := normalizeQuestion(question)
	for _, item := range q.rooms[roomID] {
		if normalizeQuestion(item.Question) != key {
			continue
		}
		if topic != "" && item.Topic != "" && item.Topic != topic {
			continue
		}
		item.Count++
		item.LastSeenAt = now
		item.ExpiresAt = now.Add(q.ttl)
		if topic != "" {
			item.Topic = topic
		}
		if userID != "" {
			item.UserID = userID
		}
		return clone(*item), true
	}

	id := fmt.Sprintf("q-%d-%d-%06d", roomID, now.UnixMilli(), q.seq.Add(1))
	item := &Item{
		ID:          id,
		RoomID:      roomID,
		Question:    question,
		Topic:       topic,
		UserID:      userID,
		Count:       1,
		Status:      StatusPending,
		CreatedAt:   now,
		LastSeenAt:  now,
		ExpiresAt:   now.Add(q.ttl),
		AvailableAt: now,
	}
	q.rooms[roomID] = append(q.rooms[roomID], item)
	return clone(*item), false
}

func (q *Queue) List(roomID int64) []Item {
	if roomID <= 0 {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	q.pruneLocked(roomID, now)
	items := q.rooms[roomID]
	out := make([]Item, 0, len(items))
	for _, item := range items {
		out = append(out, clone(*item))
	}
	return out
}

func (q *Queue) ClaimNext(roomID int64) (Item, bool) {
	if roomID <= 0 {
		return Item{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	q.pruneLocked(roomID, now)
	for _, item := range q.rooms[roomID] {
		if item.Status != StatusPending || item.AvailableAt.After(now) {
			continue
		}
		until := now.Add(q.claimLease)
		item.Status = StatusClaimed
		item.ClaimedAt = timePtr(now)
		item.ClaimUntil = timePtr(until)
		return clone(*item), true
	}
	return Item{}, false
}

func (q *Queue) Complete(roomID int64, id string) bool {
	return q.remove(roomID, id)
}

func (q *Queue) Drop(roomID int64, id string) bool {
	return q.remove(roomID, id)
}

func (q *Queue) Release(roomID int64, id string, retryAfter time.Duration) (Item, bool) {
	if roomID <= 0 || strings.TrimSpace(id) == "" {
		return Item{}, false
	}
	if retryAfter < 0 {
		retryAfter = 0
	}
	if retryAfter > time.Minute {
		retryAfter = time.Minute
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	q.pruneLocked(roomID, now)
	for _, item := range q.rooms[roomID] {
		if item.ID != id {
			continue
		}
		if !now.Before(item.ExpiresAt) {
			return Item{}, false
		}
		item.Status = StatusPending
		item.ClaimedAt = nil
		item.ClaimUntil = nil
		item.AvailableAt = now.Add(retryAfter)
		return clone(*item), true
	}
	return Item{}, false
}

func (q *Queue) ClearRoom(roomID int64) {
	if roomID <= 0 {
		return
	}
	q.mu.Lock()
	delete(q.rooms, roomID)
	q.mu.Unlock()
}

func (q *Queue) DropByUser(roomID int64, userID string) int {
	userID = strings.TrimSpace(userID)
	if roomID <= 0 || userID == "" {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	q.pruneLocked(roomID, now)
	items := q.rooms[roomID]
	if len(items) == 0 {
		return 0
	}
	kept := items[:0]
	dropped := 0
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.UserID), userID) {
			dropped++
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		delete(q.rooms, roomID)
	} else {
		q.rooms[roomID] = kept
	}
	return dropped
}

func (q *Queue) remove(roomID int64, id string) bool {
	if roomID <= 0 || strings.TrimSpace(id) == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().UTC()
	q.pruneLocked(roomID, now)
	items := q.rooms[roomID]
	for i, item := range items {
		if item.ID != id {
			continue
		}
		items = append(items[:i], items[i+1:]...)
		if len(items) == 0 {
			delete(q.rooms, roomID)
		} else {
			q.rooms[roomID] = items
		}
		return true
	}
	return false
}

func (q *Queue) pruneLocked(roomID int64, now time.Time) {
	items := q.rooms[roomID]
	if len(items) == 0 {
		return
	}
	kept := items[:0]
	for _, item := range items {
		if !now.Before(item.ExpiresAt) {
			continue
		}
		if item.Status == StatusClaimed && item.ClaimUntil != nil && !now.Before(*item.ClaimUntil) {
			item.Status = StatusPending
			item.ClaimedAt = nil
			item.ClaimUntil = nil
			item.AvailableAt = now
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		delete(q.rooms, roomID)
		return
	}
	q.rooms[roomID] = kept
}

func normalizeQuestion(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), ""))
}

func clone(item Item) Item {
	if item.ClaimedAt != nil {
		value := *item.ClaimedAt
		item.ClaimedAt = &value
	}
	if item.ClaimUntil != nil {
		value := *item.ClaimUntil
		item.ClaimUntil = &value
	}
	return item
}

func timePtr(value time.Time) *time.Time {
	return &value
}
