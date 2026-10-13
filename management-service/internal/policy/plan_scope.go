package policy

import (
	"encoding/json"
	"livecompanion/management/internal/model"
	"reflect"
)

func scopedPlanID(metadata map[string]any) int64 {
	switch v := metadata["plan_id"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		if v == float64(int64(v)) {
			return int64(v)
		}
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

// A room's historical business rules are not proof they belong to its newly
// selected plan. Leave the stored version/history intact; select by provenance.
func ForPlan(version *model.LivePolicyVersion, planID int64) *model.LivePolicyVersion {
	if version == nil || planID <= 0 {
		return version
	}
	out := *version
	out.Overrides = []model.LivePolicyOverride{}
	for _, rule := range version.Overrides {
		if scopedPlanID(rule.Metadata) == planID {
			out.Overrides = append(out.Overrides, rule)
		}
	}
	return &out
}

// Only newly confirmed content gets the current program-owned plan binding.
// Re-saving an unchanged legacy rule must not silently adopt it into a new plan.
func BindPlanOverrides(items []model.LivePolicyOverride, previous *model.LivePolicyVersion, planID int64) []model.LivePolicyOverride {
	old := map[string]model.LivePolicyOverride{}
	if previous != nil {
		for _, item := range previous.Overrides {
			old[item.Key] = item
		}
	}
	out := append([]model.LivePolicyOverride{}, items...)
	for i, item := range out {
		metadata := map[string]any{}
		for k, v := range item.Metadata {
			if k != "plan_id" {
				metadata[k] = v
			}
		}
		prior, exists := old[item.Key]
		a, b := item, prior
		a.Metadata = nil
		b.Metadata = nil
		binding := planID
		if exists && reflect.DeepEqual(a, b) {
			binding = scopedPlanID(prior.Metadata)
		}
		if binding > 0 {
			metadata["plan_id"] = binding
		}
		out[i].Metadata = metadata
	}
	return out
}
