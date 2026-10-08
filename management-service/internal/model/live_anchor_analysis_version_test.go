package model

import (
	"strings"
	"testing"
)

func TestAutoPromoteOnlySoleAnalyzedSample(t *testing.T) {
	tests := []struct {
		name    string
		samples []LiveAnchorStyleSample
		want    bool
	}{
		{"sole", []LiveAnchorStyleSample{{ID: 7, AnalysisStatus: "analyzed"}}, true},
		{"empty", nil, false},
		{"different sample", []LiveAnchorStyleSample{{ID: 8, AnalysisStatus: "analyzed"}}, false},
		{"not ready", []LiveAnchorStyleSample{{ID: 7, AnalysisStatus: "pending"}}, false},
		{"second pending", []LiveAnchorStyleSample{{ID: 7, AnalysisStatus: "analyzed"}, {ID: 8, AnalysisStatus: "pending"}}, false},
		{"second failed", []LiveAnchorStyleSample{{ID: 7, AnalysisStatus: "analyzed"}, {ID: 8, AnalysisStatus: "failed"}}, false},
		{"two analyzed", []LiveAnchorStyleSample{{ID: 7, AnalysisStatus: "analyzed"}, {ID: 8, AnalysisStatus: "analyzed"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanAutoPromoteAnchorSample(tt.samples, 7); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAnchorSampleRuneLimit(t *testing.T) {
	input := strings.Repeat("好🙂", 5001)
	text, cut := TruncateAnchorSample(input)
	if !cut || len([]rune(text)) != 10000 || !strings.HasPrefix(input, text) {
		t.Fatal("must truncate Unicode at 10000 characters")
	}
}
