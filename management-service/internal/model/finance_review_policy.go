package model

const FinanceDistinctReviewerSetting = "finance_require_distinct_reviewer"

// Separation of duties is independent of the actor's existing approval permission.
type FinanceReviewPolicy struct {
	RequireDistinctReviewer bool `json:"require_distinct_reviewer"`
}

func (p FinanceReviewPolicy) BlocksReviewer(reviewer int64, operators ...int64) bool {
	if !p.RequireDistinctReviewer {
		return false
	}
	for _, operator := range operators {
		if operator > 0 && operator == reviewer {
			return true
		}
	}
	return false
}

func (p FinanceReviewPolicy) Mode() string {
	if p.RequireDistinctReviewer {
		return "distinct_required"
	}
	return "permission_only"
}
