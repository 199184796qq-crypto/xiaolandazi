package interactionruntime

import (
	"testing"
	"time"
)

func TestPaymentOriginalUsesOrderSchedulerWithoutSemanticModel(t *testing.T) {
	for _, raw := range []string{
		"五香味刚付完钱了，麻烦安排发货。",
		"五香味这单刚结完账，支持一下主播。",
	} {
		t.Run(raw, func(t *testing.T) {
			e := newWelcomeEngine()
			s := scope(29)
			add(e, s, "viewer-message", "wang", "chat", raw, base)
			if jobs := e.Batches(base, 10); len(jobs) != 0 {
				t.Fatal("confirmed order was sent to intent/answer model", jobs)
			}
			jobs := behaviorJobs(e, base)
			if len(jobs) != 1 || jobs[0].Kind != "order" || jobs[0].Template != "order_single_v1" || jobs[0].Count != 1 {
				t.Fatal("confirmed order did not use canonical order scheduler", jobs)
			}
			if len(e.rooms[29].SemanticUsers) != 0 {
				t.Fatal("completed order inflated semantic density")
			}
		})
	}
}

func TestDistinctPaymentViewersAggregateAsOrders(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "a", "wang", "chat", "刚付完钱", base)
	add(e, s, "repeat", "wang", "chat", "刚结完账", base.Add(time.Second))
	add(e, s, "b", "zhang", "chat", "结账了", base.Add(2*time.Second))
	if jobs := e.Batches(base.Add(3*time.Second), 10); len(jobs) != 0 {
		t.Fatal("orders entered semantic batch", jobs)
	}
	jobs := behaviorJobs(e, base.Add(3*time.Second))
	if len(jobs) != 1 || jobs[0].Count != 2 || jobs[0].Template != "order_count_v1" {
		t.Fatal("payment originals did not retain distinct-viewer aggregation", jobs)
	}
}
