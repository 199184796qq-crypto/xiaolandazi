package model

import "time"

const LiveAnchorSampleLimit = 10000

// All samples count, including pending and failed ones. A single analyzed result
// must never masquerade as the overall result of a multi-sample presenter.
func CanAutoPromoteAnchorSample(samples []LiveAnchorStyleSample, sampleID int64) bool {
	return len(samples) == 1 && samples[0].ID == sampleID && samples[0].AnalysisStatus == "analyzed"
}

func TruncateAnchorSample(text string) (string, bool) {
	chars := []rune(text)
	if len(chars) > LiveAnchorSampleLimit {
		return string(chars[:LiveAnchorSampleLimit]), true
	}
	return text, false
}

type LiveAnchorAnalysisVersion struct {
	ID         int64                           `json:"id"`
	VersionNo  int64                           `json:"version_no"`
	Kind       string                          `json:"kind"`
	SampleIDs  []int64                         `json:"sample_ids"`
	Text       string                          `json:"text"`
	CreatedAt  time.Time                       `json:"created_at"`
	Profile    LiveAgentPlanAnchorStyleProfile `json:"-"`
	SourceText string                          `json:"-"`
}

type LiveAnchorAnalysisState struct {
	Items           []LiveAnchorAnalysisVersion `json:"items"`
	ActiveVersionID int64                       `json:"active_version_id"`
}
