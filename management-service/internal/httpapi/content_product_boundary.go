package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
)

// Only saved primary product sources qualify. Service/offer sources and lecture
// evidence are never expanded into this common context. No model owns the binding.
func contentProductBoundary(set contentpools.Set, products []int64, links map[int64]string) []anchorcycle.HookFact {
	rows := map[string]contentpools.SourceRow{}
	for _, row := range set.Sources.Rows {
		rows[row.ID] = row
	}
	out := []anchorcycle.HookFact{}
	seen := map[string]bool{}
	for _, pool := range set.Pools {
		for _, fact := range pool.Facts {
			if seen[fact.ID] {
				continue
			}
			applicable := []int64{}
			for _, id := range products {
				if contentpools.Contains(fact.Products, id) {
					applicable = append(applicable, id)
				}
			}
			if len(applicable) == 0 {
				continue
			}
			core := false
			for _, id := range fact.Sources {
				row := rows[id]
				if row.Area == "products" {
					core = true
				}
				if row.Area == "knowledge" {
					switch row.Fields["分类"] {
					case "product", "usage", "craft", "ingredients", "flavor", "brand", "material", "function", "appearance":
						core = true
					}
				}
			}
			if !core {
				continue
			}
			seen[fact.ID] = true
			scope := []string{}
			for _, id := range applicable {
				scope = append(scope, links[id])
			}
			out = append(out, anchorcycle.HookFact{ID: fact.ID, Data: fact.Text, Products: scope, Benefits: []string{}})
		}
	}
	return out
}
