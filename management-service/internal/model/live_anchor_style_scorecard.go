package model

import "time"

// LiveAnchorStyleScorecardAsset is the model-extracted, host-specific ruler
// created from the same sample used to compile the reusable style prompt.
type LiveAnchorStyleScorecardAsset struct {
	ID                     int64      `json:"id"`
	TenantID               int64      `json:"tenant_id"`
	PlanID                 int64      `json:"plan_id"`
	MetaPromptAssetID      int64      `json:"meta_prompt_asset_id"`
	SampleSHA256           string     `json:"sample_sha256"`
	SampleText             string     `json:"-"`
	ExtractionPromptMD     string     `json:"extraction_prompt_md"`
	PromptSource           string     `json:"prompt_source"`
	OriginalScorecardJSON  string     `json:"original_scorecard_json"`
	ConfirmedScorecardJSON string     `json:"confirmed_scorecard_json,omitempty"`
	RequestSystem          string     `json:"request_system"`
	RequestUser            string     `json:"request_user"`
	RawResponse            string     `json:"raw_response"`
	Provider               string     `json:"provider"`
	Model                  string     `json:"model"`
	LatencyMS              int64      `json:"latency_ms"`
	InputTokens            int64      `json:"input_tokens"`
	OutputTokens           int64      `json:"output_tokens"`
	Status                 string     `json:"status"`
	CalibrationError       string     `json:"calibration_error,omitempty"`
	Active                 bool       `json:"active"`
	CreatedByUserID        *int64     `json:"created_by_user_id,omitempty"`
	ConfirmedByUserID      *int64     `json:"confirmed_by_user_id,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	ConfirmedAt            *time.Time `json:"confirmed_at,omitempty"`
}

func (a LiveAnchorStyleScorecardAsset) EffectiveScorecardJSON() string {
	if a.ConfirmedScorecardJSON != "" {
		return a.ConfirmedScorecardJSON
	}
	return a.OriginalScorecardJSON
}

type SaveLiveAnchorStyleScorecardInput struct {
	MetaPromptAssetID     int64
	SampleSHA256          string
	SampleText            string
	ExtractionPromptMD    string
	PromptSource          string
	OriginalScorecardJSON string
	RequestSystem         string
	RequestUser           string
	RawResponse           string
	Provider              string
	Model                 string
	LatencyMS             int64
	InputTokens           int64
	OutputTokens          int64
	Status                string
	CalibrationError      string
}
