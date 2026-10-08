package httpapi

import (
	"context"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
)

// buildPlanP0Allowances turns completed fact-review decisions into a frozen,
// plan-local runtime exception list. It never changes the project policy. A
// normal user confirmation can release only contextual rules; hard rules need
// an approved backend appeal before they enter this list.
func buildPlanP0Allowances(reviews []model.LiveRiskReview, policy *contentguard.Policy, scope string) []anchorcycle.P0Allowance {
	seen := map[string]bool{}
	result := []anchorcycle.P0Allowance{}
	for _, review := range reviews {
		if review.Status != "confirmed" {
			continue
		}
		sources := review.Sources
		if len(sources) == 0 {
			sources = []model.LiveRiskReviewSource{{SourceKey: review.SourceKey, Text: review.Text}}
		}
		for _, source := range sources {
			if strings.TrimSpace(source.Text) == "" {
				continue
			}
			for _, hit := range policy.Scan(source.Text, scope).Hits {
				approved := hit.ReviewMode == "semantic" && review.Severity != "P0"
				decision := "用户已知晓并确认"
				if review.Severity == "P0" {
					approved = review.RuleID == hit.RuleID && ((hit.ReviewMode == "semantic" && (review.SemanticDecision == "pass" || review.SemanticDecision == "evidence_required")) || review.AppealStatus == "approved")
				}
				if review.AppealStatus == "approved" {
					decision = "后台复核通过"
				}
				if !approved {
					continue
				}
				matchKey := policy.MatchKey(hit.Text)
				key := hit.RuleID + "\x00" + matchKey
				if matchKey == "" || seen[key] {
					continue
				}
				seen[key] = true
				result = append(result, anchorcycle.P0Allowance{RuleID: hit.RuleID, MatchedText: hit.Text, MatchKey: matchKey, Decision: decision})
			}
		}
	}
	return result
}

func (s *Server) installPlanP0Allowances(ctx context.Context, state *anchorcycle.State) error {
	if state.PlanID == 0 || state.P0PolicyMD == "" {
		return nil
	}
	policy, err := contentguard.Compile(state.P0PolicyMD)
	if err != nil {
		return err
	}
	reviews, err := s.store.ListLiveRiskReviews(ctx, state.TenantID, state.PlanID)
	if err != nil {
		return err
	}
	current := reviews[:0]
	for _, review := range reviews {
		// P1/P2 confirmations were produced by their own rule mothers but may
		// cover a contextual P0 term in the same confirmed fact. P0 decisions
		// themselves must still belong to the exact runtime P0 policy snapshot.
		if review.Severity != "P0" || review.RuleSHA256 == policy.SHA256 {
			current = append(current, review)
		}
	}
	state.P0Allowances = buildPlanP0Allowances(current, policy, state.Config.ComplianceScope)
	return nil
}
