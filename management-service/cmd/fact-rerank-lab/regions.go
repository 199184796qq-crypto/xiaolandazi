package main

import "strings"

// regionPredicate 是事实条件/例外里的一条地区条件。
type regionPredicate struct {
	Provinces map[string]bool
	Other     bool
	Label     string
	// Except=true 表示这是“例外”条件：命中地区时该事实不适用（如“包邮，除 7 省”）。
	Except bool
}

func regionPredicatesOf(f fact, geo *geoTable) []regionPredicate {
	out := []regionPredicate{}
	addRange := func(predicates []predicate, except bool) {
		for _, p := range predicates {
			field := strings.TrimSpace(p.Field)
			// 只处理“收货地区/地区/区域”这类字段；包装方式、加热方式等不在这里判断。
			if field != "" && !strings.Contains(field, "地区") && !strings.Contains(field, "区域") {
				continue
			}
			item := regionPredicate{Provinces: map[string]bool{}, Except: except}
			values := p.Values
			if len(values) == 0 && p.Raw != "" {
				values = []string{p.Raw}
			}
			for _, value := range values {
				provinces, other := geo.expandRegion(value)
				if other {
					item.Other = true
					if item.Label == "" {
						item.Label = "其它地区"
					}
					continue
				}
				if len(provinces) == 0 {
					continue
				}
				if item.Label == "" {
					if len(p.Values) > 1 {
						item.Label = strings.Join(p.Values, "、")
					} else {
						item.Label = strings.TrimSpace(value)
					}
				}
				for _, province := range provinces {
					item.Provinces[province] = true
				}
			}
			if item.Other || len(item.Provinces) > 0 {
				out = append(out, item)
			}
		}
	}
	addRange(f.conditionPredicates, false)
	addRange(f.exceptionPredicates, true)
	return out
}

// applyRegionConditions 用观众地区过滤掉“条件不适用”的事实，并返回命中的地区分支。
//   - 条件地区是明确省份列表：观众省份在列表里才适用（如“7省需补空运费”）；
//   - 条件是“其它地区”：观众省份不在同主题其它明确分支里才适用；
//   - 例外地区：观众命中例外时，该事实不适用（如“包邮，除 7 省”）。
func (l *lab) applyRegionConditions(feed []candidate, cached *cachedPlan, location []string) ([]candidate, string) {
	if l.geo == nil || len(location) == 0 {
		return feed, ""
	}
	locationSet := map[string]bool{}
	for _, province := range location {
		locationSet[province] = true
	}
	byID := map[int64]fact{}
	for _, f := range cached.facts {
		byID[f.ID] = f
	}
	topicProvinces := l.regionTopicProvinces(cached)
	kept := []candidate{}
	branch := ""
	for _, item := range feed {
		f, ok := byID[item.ID]
		if !ok {
			kept = append(kept, item)
			continue
		}
		applicable, hitBranch := l.regionApplicable(f, locationSet, topicProvinces)
		if !applicable {
			continue
		}
		if hitBranch != "" && branch == "" {
			branch = hitBranch
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		return feed, branch
	}
	return kept, branch
}

// regionTopicProvinces 汇总每个主题下所有“明确省份”分支，供“其它地区”判断排除集合。
func (l *lab) regionTopicProvinces(cached *cachedPlan) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range cached.facts {
		for _, predicate := range regionPredicatesOf(f, l.geo) {
			if predicate.Other || len(predicate.Provinces) == 0 {
				continue
			}
			if out[f.Topic] == nil {
				out[f.Topic] = map[string]bool{}
			}
			for province := range predicate.Provinces {
				out[f.Topic][province] = true
			}
		}
	}
	return out
}

// regionApplicable 判断一条事实的地区条件是否适用于观众地区，并返回命中的分支标签。
func (l *lab) regionApplicable(f fact, location map[string]bool, topicProvinces map[string]map[string]bool) (bool, string) {
	predicates := regionPredicatesOf(f, l.geo)
	if len(predicates) == 0 {
		return true, ""
	}
	hitBranch := ""
	for _, predicate := range predicates {
		if predicate.Other {
			for province := range location {
				if topicProvinces[f.Topic][province] {
					return false, ""
				}
			}
			if hitBranch == "" {
				hitBranch = "其它地区"
			}
			continue
		}
		matched := false
		for province := range location {
			if predicate.Provinces[province] {
				matched = true
				break
			}
		}
		if predicate.Except {
			// 例外命中时该事实不适用（例如“全场包邮，除 7 省”遇到新疆）。
			if matched {
				return false, ""
			}
			continue
		}
		if !matched {
			return false, ""
		}
		if hitBranch == "" {
			hitBranch = predicate.Label
		}
	}
	return true, hitBranch
}
