package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
)

// Same primary-source filter as the interaction core. Secondary data and
// service/benefit sources cannot become ordinary product-use context.
func contentProductBoundary(set contentpools.Set, products []int64, links map[int64]string) []anchorcycle.HookFact {
	out := []anchorcycle.HookFact{}
	for _, f := range contentpools.PrimaryProductFacts(set, products) {
		scope := []string{}
		for _, id := range f.Products {
			scope = append(scope, links[id])
		}
		out = append(out, anchorcycle.HookFact{ID: f.ID, Data: f.Text, Products: scope, Benefits: []string{}})
	}
	return out
}
