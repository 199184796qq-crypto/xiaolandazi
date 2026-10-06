package speechexpander

import (
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

// A planner proposes discourse work, never new business facts or clock state.
type ContentDecision struct {
	Role           string   `json:"role"`
	PrimaryFactID  string   `json:"primary_fact_id"`
	SupportFactIDs []string `json:"support_fact_ids"`
	Reason         string   `json:"reason"`
}

type ContentDecisionReceipt struct {
	Index          int      `json:"index"`
	Role           string   `json:"role"`
	PrimaryFactID  string   `json:"primary_fact_id,omitempty"`
	SupportFactIDs []string `json:"support_fact_ids,omitempty"`
	Source         string   `json:"source"`
	Reason         string   `json:"reason"`
}

// Roles are a menu, not a prescribed timeline. Type/intensity restrict the menu.
func PlanningRoles(strategy ResolvedContentStrategy) map[string]string {
	result := map[string]string{}
	for _, key := range strategy.RoleSequence {
		if role, ok := contentRoleCatalog[key]; ok {
			result[key] = role.goal
		}
	}
	if strategy.ConversionIntensity == 0 {
		delete(result, "action")
	}
	return result
}

// Keep all material families represented within a bounded planning context;
// prefer cooled-down facts inside each family, not database row order.
func PlanningFacts(manifest []model.LiveAgentGenerationFact, cursor ContentScheduleCursor) []model.LiveAgentGenerationFact {
	families := []string{"identity", "content", "scenario", "choice", "evidence", "benefit", "assurance", "fulfillment"}
	groups := map[string][]model.LiveAgentGenerationFact{}
	for _, recent := range []bool{false, true} {
		for _, fact := range usableGenerationFacts(manifest) {
			if (recentDistance(cursor.RecentFactIDs, fact.FactID) > 0) == recent {
				family := contentFactFamily(fact)
				groups[family] = append(groups[family], fact)
			}
		}
	}
	result := []model.LiveAgentGenerationFact{}
	for offset := 0; len(result) < 40; offset++ {
		added := false
		for _, family := range families {
			if offset < len(groups[family]) {
				result = append(result, groups[family][offset])
				added = true
				if len(result) == 40 {
					break
				}
			}
		}
		if !added {
			break
		}
	}
	return result
}

func ApplyContentDecision(step model.LiveSpeechExpansionStep, choice ContentDecision, manifest []model.LiveAgentGenerationFact, strategy ResolvedContentStrategy) (model.LiveSpeechExpansionStep, error) {
	role, ok := contentRoleCatalog[choice.Role]
	if !ok || PlanningRoles(strategy)[choice.Role] == "" {
		return step, errors.New("内容目的不在允许范围内")
	}
	if len(choice.SupportFactIDs) > 1 {
		return step, errors.New("辅助材料超过一项")
	}
	facts := usableGenerationFacts(manifest)
	byID := map[string]model.LiveAgentGenerationFact{}
	for _, fact := range facts {
		byID[fact.FactID] = fact
	}
	primary, found := byID[choice.PrimaryFactID]
	if len(facts) > 0 && !found {
		return step, errors.New("主材料不存在或未授权")
	}
	if len(facts) == 0 && (choice.PrimaryFactID != "" || len(choice.SupportFactIDs) > 0) {
		return step, errors.New("不能凭空选择材料")
	}
	if choice.Role == "action" && !hasActionMaterial(facts) {
		return step, errors.New("缺少行动依据")
	}
	if found && contentRoleFit(choice.Role, contentFactFamily(primary)) == 0 {
		return step, errors.New("材料与内容目的不匹配")
	}
	for _, id := range choice.SupportFactIDs {
		support, ok := byID[id]
		if !found || !ok || !compatibleSupport(primary, support, choice.Role) {
			return step, errors.New("辅助材料未授权、重复或跨商品错配")
		}
	}
	step.ContentRole, step.Stage, step.Goal = role.key, role.key, role.goal
	step.PrimaryFactID, step.SupportFactIDs = "", nil
	step.FactKeys, step.BenefitKeys, step.LinkKeys = nil, nil, nil
	if found {
		addFactRouting(&step, primary, true)
	}
	for _, id := range choice.SupportFactIDs {
		addFactRouting(&step, byID[id], false)
	}
	// Timing, plugins and passive-interruption opportunities remain program-owned.
	return step, nil
}

func ContentReceipt(step model.LiveSpeechExpansionStep, index int, source, reason string) ContentDecisionReceipt {
	return ContentDecisionReceipt{Index: index, Role: step.ContentRole, PrimaryFactID: step.PrimaryFactID,
		SupportFactIDs: append([]string(nil), step.SupportFactIDs...), Source: source, Reason: strings.TrimSpace(reason)}
}
