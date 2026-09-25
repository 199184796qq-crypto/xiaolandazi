package workinbox

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"sync"
	"testing"
	"time"
)

type fakeSource struct {
	mu            sync.Mutex
	calls         map[string]int
	revisions     map[string]uint64
	fail          string
	revisionCalls int
	unavailable   bool
}

func (f *fakeSource) InboxRevisions(context.Context) (map[string]uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revisionCalls++
	if f.unavailable {
		return nil, errors.New("offline")
	}
	m := map[string]uint64{}
	for k, v := range f.revisions {
		m[k] = v
	}
	return m, nil
}
func (f *fakeSource) InboxTopic(ctx context.Context, sc model.InboxScope, topic string) ([]model.InboxGroup, error) {
	f.mu.Lock()
	f.calls[topic]++
	bad := f.fail == topic
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Millisecond):
	}
	if bad {
		return nil, errors.New("query failed")
	}
	return []model.InboxGroup{{Key: topic, Topic: topic, Count: 2, To: "/work/inbox"}}, nil
}
func fakeInbox() *fakeSource {
	return &fakeSource{calls: map[string]int{}, revisions: map[string]uint64{"access": 0, "finance": 0, "inventory": 0, "logistics": 0, "sales": 0, "support": 0}}
}
func TestInboxCoalescedCaching(t *testing.T) {
	ctx := context.Background()
	src := fakeInbox()
	service := New(src, nil, "test")
	service.refresh(ctx)
	sc := model.InboxScope{Actor: model.Actor{UserID: 900, Role: "platform_admin"}, RequireDistinctReviewer: true}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := service.Snapshot(ctx, sc)
			if err != nil || v.Stale || v.Total != 8 {
				t.Errorf("snapshot %#v %v", v, err)
			}
		}()
	}
	wg.Wait()
	for _, topic := range []string{"finance", "inventory", "logistics", "support"} {
		if src.calls[topic] != 1 {
			t.Fatalf("%s rebuilt %d times", topic, src.calls[topic])
		}
	}
	for i := 0; i < 100; i++ {
		service.Version()
	}
	if src.revisionCalls != 1 {
		t.Fatal("heartbeat hit database")
	}
	src.mu.Lock()
	src.revisions["logistics"]++
	src.mu.Unlock()
	service.refresh(ctx)
	_, _ = service.Snapshot(ctx, sc)
	if src.calls["logistics"] != 2 || src.calls["finance"] != 1 {
		t.Fatal("unrelated topic rebuilt", src.calls)
	}
	sc.Actor.UserID = 901
	_, _ = service.Snapshot(ctx, sc)
	if src.calls["finance"] != 2 {
		t.Fatal("self-review counts shared across people")
	}
	if src.calls["inventory"] != 1 || src.calls["logistics"] != 2 {
		t.Fatal("identical warehouse permissions did not share cache", src.calls)
	}
}
func TestInboxFailureDoesNotMeanZero(t *testing.T) {
	ctx := context.Background()
	src := fakeInbox()
	src.fail = "inventory"
	s := New(src, nil, "test")
	s.refresh(ctx)
	sc := model.InboxScope{Actor: model.Actor{UserID: 900, Role: "platform_admin"}}
	v, err := s.Snapshot(ctx, sc)
	if err != nil || !v.Stale || len(v.Unavailable) != 1 || v.Unavailable[0] != "inventory" {
		t.Fatalf("lost error: %#v %v", v, err)
	}
	if Summary(v, "", "") == Summary(model.InboxSnapshot{}, "", "") {
		t.Fatal("unknown shown as empty")
	}
	src.mu.Lock()
	src.unavailable = true
	src.mu.Unlock()
	s.refresh(ctx)
	v, _ = s.Snapshot(ctx, sc)
	if !v.Stale || len(v.Unavailable) != 4 {
		t.Fatal("revision outage served trusted cached counts")
	}
}
func TestInboxExternalScopeIsNotInternal(t *testing.T) {
	ctx := context.Background()
	src := fakeInbox()
	s := New(src, nil, "test")
	s.refresh(ctx)
	tenant := int64(10)
	sc := model.InboxScope{Actor: model.Actor{UserID: 1, Role: "customer", TenantID: &tenant}, Access: model.StaffAccessContext{IsSuperAdmin: true, Permissions: []string{"*"}}}
	_, err := s.Snapshot(ctx, sc)
	if err == nil {
		t.Fatal("terminal customer inbox must be unavailable")
	}
	if len(src.calls) != 0 {
		t.Fatal("terminal customer triggered inbox database work", src.calls)
	}
	if _, err = s.Snapshot(ctx, model.InboxScope{}); err == nil {
		t.Fatal("anonymous accepted")
	}
}
