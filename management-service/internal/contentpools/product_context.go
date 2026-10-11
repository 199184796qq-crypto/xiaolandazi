package contentpools

import (
	"fmt"
	"strings"
)

// Source roles and product scope are program-owned. Services and secondary
// evidence never become implicit product context.
func PrimaryProductFacts(set Set, products []int64) []Fact {
	rows := map[string]SourceRow{}
	for _, row := range set.Sources.Rows {
		rows[row.ID] = row
	}
	seen := map[string]bool{}
	out := []Fact{}
	for _, pool := range set.Pools {
		for _, fact := range pool.Facts {
			if seen[fact.ID] {
				continue
			}
			applicable := []int64{}
			for _, id := range products {
				if Contains(fact.Products, id) {
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
			if core {
				seen[fact.ID] = true
				fact.Products = applicable
				out = append(out, fact)
			}
		}
	}
	return out
}

// This packet has content only; ordinary link numbers identify the offered
// item, never database references to be maintained by a model.
func ProductContext(set Set) string {
	lines, ids, links := []string{}, []int64{}, map[int64]string{}
	for _, p := range set.Sources.Products {
		ids = append(ids, p.ID)
		links[p.ID] = p.Link + "号链接"
		if p.Name != "" {
			lines = append(lines, fmt.Sprintf("%s：%s", links[p.ID], p.Name))
		}
	}
	for _, f := range PrimaryProductFacts(set, ids) {
		names := []string{}
		for _, id := range f.Products {
			names = append(names, links[id])
		}
		lines = append(lines, strings.Join(names, "、")+"："+f.Text)
	}
	return strings.Join(lines, "\n")
}
