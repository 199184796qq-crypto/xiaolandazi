package model

import "testing"

func TestFinanceReviewerSeparationModes(t *testing.T) {
	strict := FinanceReviewPolicy{RequireDistinctReviewer: true}
	if !strict.BlocksReviewer(10, 10, 20) || !strict.BlocksReviewer(20, 10, 20) || strict.BlocksReviewer(30, 10, 20) {
		t.Fatal("strict mode must cover requester and last submitter only")
	}
	permitted := FinanceReviewPolicy{RequireDistinctReviewer: false}
	if permitted.BlocksReviewer(10, 10, 20) || permitted.Mode() != "permission_only" {
		t.Fatal("permission-only mode still blocks same identity")
	}
}
