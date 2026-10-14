package speechlibrary

import (
	"fmt"
	"testing"
)

func TestAdaptiveStableDecisionAndPolicy(t *testing.T) {
	p := DefaultPolicy()
	for _, n := range []int{0, 1, 2, 3, 4} {
		hits := 0
		for i := 0; i < 10000; i++ {
			key := fmt.Sprint("request-", i)
			a := ReuseDecision(key, n, p)
			if a != ReuseDecision(key, n, p) {
				t.Fatal("decision changed on retry")
			}
			if a {
				hits++
			}
		}
		want := p.Rate(n) * 100
		if hits < want-180 || hits > want+180 {
			t.Fatalf("n=%d observed=%d target=%d", n, hits, want)
		}
	}
	for _, p := range []Policy{{0, 30, 60, 70, 180}, {3, 80, 60, 70, 180}, {3, 30, 60, 101, 180}, {3, 30, 60, 70, -1}} {
		if p.Validate() == nil {
			t.Fatal("invalid configuration", p)
		}
	}
}
