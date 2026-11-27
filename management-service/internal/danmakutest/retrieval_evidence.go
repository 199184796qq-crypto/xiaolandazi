package danmakutest

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"

	"livecompanion/management/internal/model"
)

//go:embed retrieval_regions.json
var retrievalRegionJSON []byte

// The preview ships the plan lab's province/city reference and aliases.
var retrievalRegions = func() struct {
	Aliases   map[string][]string
	Provinces map[string]string
} {
	var data struct {
		Aliases   map[string][]string
		Provinces map[string]string
	}
	if err := json.Unmarshal(retrievalRegionJSON, &data); err != nil {
		panic(err)
	}
	return data
}()

func IsRegion(value string) bool {
	return len(regionValues(value)) > 0
}

func regionValues(value string) []string {
	if province := retrievalRegions.Provinces[regionKey(value)]; province != "" {
		return []string{province}
	}
	value = strings.TrimSuffix(strings.TrimSpace(value), "地区")
	return retrievalRegions.Aliases[value]
}

func ViewerRegion(text string) string {
	runes := []rune(text)
	match := ""
	for start := 0; start < len(runes); start++ {
		for end := min(len(runes), start+10); end >= start+2; end-- {
			value := string(runes[start:end])
			if IsRegion(value) {
				if match != "" && !slices.Equal(regionValues(match), regionValues(value)) {
					return ""
				}
				match = value
				start = end - 1
				break
			}
		}
	}
	return match
}

func isOtherRegion(value string) bool {
	return strings.Contains(value, "其它地区") || strings.Contains(value, "其他地区") ||
		strings.Contains(value, "其余地区") || value == "其它" || value == "其他" || value == "其余"
}

// ExplicitEvidenceRegions gives the positive branches used when interpreting
// the other-regions branch of the same object and attribute.
func ExplicitEvidenceRegions(claim model.PlanMaterialClaim) []string {
	var provinces []string
	visitRegionRules(decode(claim.Conditions), func(rule map[string]any) {
		op, _ := rule["op"].(string)
		if op != "in" && op != "eq" && op != "=" && op != "raw" {
			return
		}
		for _, value := range regionRuleValues(rule) {
			provinces = append(provinces, regionValues(value)...)
		}
	})
	return provinces
}

func EvidenceCondition(claim model.PlanMaterialClaim, region string, covered []string) string {
	provinces := regionValues(region)
	if len(provinces) != 1 {
		return conditionStatus(claim, nil)
	}
	claim.Conditions = normalizedRegionRules(claim.Conditions, provinces[0], covered)
	claim.Exceptions = normalizedRegionRules(claim.Exceptions, provinces[0], covered)
	return conditionStatus(claim, map[string]string{"verified_preview_region": "match"})
}

func visitRegionRules(predicate any, visit func(map[string]any)) {
	switch rule := predicate.(type) {
	case []any:
		for _, child := range rule {
			visitRegionRules(child, visit)
		}
	case map[string]any:
		field, _ := rule["field"].(string)
		if field == "region" || field == "地区" || field == "区域" || field == "收货地区" {
			visit(rule)
		}
		for _, key := range []string{"all", "any", "values"} {
			if values, ok := rule[key].([]any); ok {
				visitRegionRules(values, visit)
			}
		}
	}
}

func regionRuleValues(rule map[string]any) []string {
	values := []string{}
	if explicit, ok := rule["values"].([]any); ok {
		for _, value := range explicit {
			if text, ok := value.(string); ok {
				values = append(values, text)
			}
		}
	}
	if len(values) == 0 {
		for _, key := range []string{"value", "raw"} {
			if value, ok := rule[key].(string); ok && value != "" {
				values = append(values, value)
				break
			}
		}
	}
	return values
}

func normalizedRegionRules(raw json.RawMessage, province string, covered []string) json.RawMessage {
	decoded := decode(raw)
	visitRegionRules(decoded, func(rule map[string]any) {
		op, _ := rule["op"].(string)
		names := []string{}
		rawValues := regionRuleValues(rule)
		for _, value := range rawValues {
			if isOtherRegion(value) {
				if len(rawValues) != 1 || len(covered) == 0 {
					return
				}
				op = "not_in"
				names = covered
				break
			}
			resolved := regionValues(value)
			if len(resolved) == 0 {
				resolved = regionValues(ViewerRegion(value))
			}
			if len(resolved) == 0 {
				rule["status"] = "unresolved"
				return
			}
			names = append(names, resolved...)
		}
		if len(names) == 0 {
			return
		}
		if op == "" || op == "raw" {
			// Only region labels by themselves may become a predicate.
			if len(rawValues) != 1 || len(regionValues(rawValues[0])) == 0 {
				return
			}
			op = "in"
			delete(rule, "status")
		}
		matches := slices.Contains(names, province)
		switch op {
		case "in", "eq", "=":
		case "not_in", "ne", "!=":
			matches = !matches
		default:
			return
		}
		value := "no_match"
		if matches {
			value = "match"
		}
		rule["field"], rule["op"], rule["values"] = "verified_preview_region", "eq", []any{value}
	})
	result, err := json.Marshal(decoded)
	if err != nil {
		return raw
	}
	return result
}
