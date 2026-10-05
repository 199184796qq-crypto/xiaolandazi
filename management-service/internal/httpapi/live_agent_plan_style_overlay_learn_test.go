package httpapi

import (
	"slices"
	"testing"
)

func TestVerifiedEvidenceQuotesDropsModelHallucinations(t *testing.T) {
	sample := "哥哥姐姐们先看清楚，我刚才说快了，不对，是今天这个节奏。"
	got := verifiedEvidenceQuotes(sample, []string{
		"我刚才说快了", "不存在的主播原话", "我刚才说快了", "不对，是今天这个节奏",
	})
	want := []string{"我刚才说快了", "不对，是今天这个节奏"}
	if !slices.Equal(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}
