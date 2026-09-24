package douyin

import (
	"errors"
	"testing"

	"livecompanion/core/internal/model"
)

func TestBrowserPoolAssignmentCapacityAndRelease(t *testing.T) {
	pool := NewBrowserPool(2, 2, "", true)

	assigned := make([]int, 0, 4)
	for roomID := int64(1); roomID <= 4; roomID++ {
		index, err := pool.assign(model.Room{ID: roomID, TenantID: 7})
		if err != nil {
			t.Fatalf("assign room %d: %v", roomID, err)
		}
		assigned = append(assigned, index)
	}
	if _, err := pool.assign(model.Room{ID: 5, TenantID: 7}); !errors.Is(err, ErrCollectorCapacity) {
		t.Fatalf("expected capacity error, got %v", err)
	}

	pool.release(1, assigned[0])
	if _, err := pool.assign(model.Room{ID: 5, TenantID: 7}); err != nil {
		t.Fatalf("assignment after release failed: %v", err)
	}
}

func TestStableWorkerIndexIsDeterministic(t *testing.T) {
	first := stableWorkerIndex(17, 99, 5)
	for i := 0; i < 100; i++ {
		if got := stableWorkerIndex(17, 99, 5); got != first {
			t.Fatalf("worker index changed: first=%d got=%d", first, got)
		}
	}
	if first < 0 || first >= 5 {
		t.Fatalf("worker index out of range: %d", first)
	}
}
