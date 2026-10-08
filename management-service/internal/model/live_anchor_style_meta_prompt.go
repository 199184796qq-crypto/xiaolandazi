package model

import "time"

// LiveAnchorStyleMetaPromptAsset is the durable, fact-free result of one
// anchor-sample compilation. OriginalMetaPromptMD is immutable source output;
// ConfirmedMetaPromptMD is the operator-approved working copy used later with
// any fact package.
type LiveAnchorStyleMetaPromptAsset struct {
	ID                    int64      `json:"id"`
	TenantID              int64      `json:"tenant_id"`
	PlanID                int64      `json:"plan_id"`
	Scene                 string     `json:"scene"`
	SampleSHA256          string     `json:"sample_sha256"`
	OriginalMetaPromptMD  string     `json:"original_meta_prompt_md"`
	ConfirmedMetaPromptMD string     `json:"confirmed_meta_prompt_md,omitempty"`
	MonitorJSON           string     `json:"-"`
	AnalysisPromptSource  string     `json:"analysis_prompt_source"`
	RequestSystem         string     `json:"request_system"`
	RequestUser           string     `json:"request_user"`
	RawResponse           string     `json:"raw_response"`
	Provider              string     `json:"provider"`
	Model                 string     `json:"model"`
	LatencyMS             int64      `json:"latency_ms"`
	InputTokens           int64      `json:"input_tokens"`
	OutputTokens          int64      `json:"output_tokens"`
	Status                string     `json:"status"`
	Active                bool       `json:"active"`
	CreatedByUserID       *int64     `json:"created_by_user_id,omitempty"`
	ConfirmedByUserID     *int64     `json:"confirmed_by_user_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	ConfirmedAt           *time.Time `json:"confirmed_at,omitempty"`
}

func (a LiveAnchorStyleMetaPromptAsset) EffectiveMetaPromptMD() string {
	if a.ConfirmedMetaPromptMD != "" {
		return a.ConfirmedMetaPromptMD
	}
	return a.OriginalMetaPromptMD
}

type SaveLiveAnchorStyleMetaPromptInput struct {
	Scene                string
	SampleSHA256         string
	OriginalMetaPromptMD string
	MonitorJSON          string
	AnalysisPromptSource string
	RequestSystem        string
	RequestUser          string
	RawResponse          string
	Provider             string
	Model                string
	LatencyMS            int64
	InputTokens          int64
	OutputTokens         int64
}
