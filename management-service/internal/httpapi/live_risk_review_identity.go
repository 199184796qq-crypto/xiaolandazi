package httpapi

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
)

// Numeric/evidence qualifiers are never collapsed into a generic attribute.
var reviewQuantified = regexp.MustCompile(`(?:含量|指标|检测|比例|占比|每百|每100|[<>≤≥])[\s\S]{0,12}[0-9]|[0-9.]+\s*(?:%|％|mg|毫克|g/|克/|ppm)`)
var reviewBatch = regexp.MustCompile(`批次|批号|年份|生产日期|保质期|[0-9]{2,4}年(?:新|产|收)`)

type reviewProduct struct{ key, name, line string }

func (d *derivedInput) reviewProducts() ([]reviewProduct, string) {
	products := []reviewProduct{}
	batches := []string{}
	for _, line := range strings.Split(d.Facts, "\n") {
		if reviewBatch.MatchString(line) && !strings.HasPrefix(line, "- [商品卡] ") && !strings.HasPrefix(line, "- [有效福利] ") {
			batches = append(batches, model.NormalizeLiveRiskReviewText(line))
		}
		if !strings.HasPrefix(line, "- [商品卡] ") {
			continue
		}
		parts := derivedFactSeparators.Split(strings.TrimPrefix(line, "- [商品卡] "), -1)
		values := map[string]string{}
		for _, part := range parts {
			if k, v, ok := strings.Cut(part, "="); ok {
				values[strings.TrimSpace(k)] = strings.TrimSpace(v)
				if reviewBatch.MatchString(k) {
					batches = append(batches, model.NormalizeLiveRiskReviewText(part))
				}
			}
		}
		if values["链接"] == "" || values["商品"] == "" {
			continue
		}
		identity, _ := json.Marshal([]string{values["链接"], values["商品"], values["规格"]})
		products = append(products, reviewProduct{key: derivedHash(string(identity)), name: values["链接"] + " · " + values["商品"], line: line})
	}
	sort.Strings(batches)
	return products, derivedHash(strings.Join(batches, "\n"))
}

// The rule mother opts in to atomic grouping. Ambiguous multi-product sources
// stay separate rather than borrowing proof from another product.
func (d *derivedInput) assignReviewScopes(p0, p1, p2 string) error {
	products, batch := d.reviewProducts()
	candidates := map[string][]reviewProduct{}
	for _, product := range products {
		report, err := contentguard.Annotate(product.line, p0, p1, p2)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, hit := range report.Hits {
			if !seen[hit.RuleID] {
				candidates[hit.RuleID] = append(candidates[hit.RuleID], product)
				seen[hit.RuleID] = true
			}
		}
	}
	for i := range d.Fields {
		f := &d.Fields[i]
		f.reviewScopes = map[string]string{}
		f.reviewSubjects = map[string]string{}
		for _, hit := range f.Hits {
			if hit.Severity != "P1" {
				continue
			}
			chosen := []reviewProduct{}
			for _, product := range products {
				link, name, _ := strings.Cut(product.name, " · ")
				// Parts of a product card inherit the line's explicit link.
				sourceLine := ""
				for _, line := range d.factLines {
					if strings.HasPrefix(f.ID, "facts/"+strconv.Itoa(line.index)+"/") {
						sourceLine = d.lines[line.index]
						break
					}
				}
				if strings.Contains(f.Text, link) || strings.Contains(f.Text, name) || strings.Contains(sourceLine, "链接="+link+"；") {
					chosen = append(chosen, product)
				}
			}
			if len(chosen) == 0 && len(products) == 1 {
				chosen = products
			}
			if len(chosen) == 0 && len(candidates[hit.RuleID]) == 1 {
				chosen = candidates[hit.RuleID]
			}
			if len(chosen) == 1 {
				f.reviewScopes[hit.RuleID] = "atomic/v1/" + chosen[0].key + "/" + batch
				f.reviewSubjects[hit.RuleID] = chosen[0].name
			} else if len(products) == 0 {
				// No concrete product has been registered; confirmation is plan-scoped,
				// invalidated as soon as a product or batch is introduced.
				f.reviewScopes[hit.RuleID] = "atomic/v1/unassigned/" + batch
				f.reviewSubjects[hit.RuleID] = "当前方案（尚未登记具体商品）"
			}
			if scope := f.reviewScopes[hit.RuleID]; scope != "" && (hit.ReviewIdentity != "atomic_attribute" || reviewQuantified.MatchString(f.Text)) {
				f.reviewScopes[hit.RuleID] = "exact/" + scope + "/" + derivedHash(model.NormalizeLiveRiskReviewText(f.Text))
			}
		}
	}
	return nil
}
