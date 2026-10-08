package model

import "time"

// LiveSellingPointWheelAsset stores one versioned selling-point/angle pool for
// a live-agent plan together with the exact prompt and fact-package fingerprint
// that produced it.
type LiveSellingPointWheelAsset struct {
	ID                 int64      `json:"id"`
	TenantID           int64      `json:"tenant_id"`
	PlanID             int64      `json:"plan_id"`
	ExtractionPromptMD string     `json:"extraction_prompt_md"`
	PromptSource       string     `json:"prompt_source"`
	FactPackageSHA256  string     `json:"fact_package_sha256"`
	OriginalWheelJSON  string     `json:"original_wheel_json"`
	ConfirmedWheelJSON string     `json:"confirmed_wheel_json,omitempty"`
	RequestSystem      string     `json:"request_system"`
	RequestUser        string     `json:"request_user"`
	RawResponse        string     `json:"raw_response"`
	Provider           string     `json:"provider"`
	Model              string     `json:"model"`
	LatencyMS          int64      `json:"latency_ms"`
	InputTokens        int64      `json:"input_tokens"`
	OutputTokens       int64      `json:"output_tokens"`
	Status             string     `json:"status"`
	Active             bool       `json:"active"`
	CreatedByUserID    *int64     `json:"created_by_user_id,omitempty"`
	ConfirmedByUserID  *int64     `json:"confirmed_by_user_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	ConfirmedAt        *time.Time `json:"confirmed_at,omitempty"`
}

func (a LiveSellingPointWheelAsset) EffectiveWheelJSON() string {
	if a.ConfirmedWheelJSON != "" {
		return a.ConfirmedWheelJSON
	}
	return a.OriginalWheelJSON
}

type SaveLiveSellingPointWheelInput struct {
	Status             string
	ExtractionPromptMD string
	PromptSource       string
	FactPackageSHA256  string
	OriginalWheelJSON  string
	RequestSystem      string
	RequestUser        string
	RawResponse        string
	Provider           string
	Model              string
	LatencyMS          int64
	InputTokens        int64
	OutputTokens       int64
}
