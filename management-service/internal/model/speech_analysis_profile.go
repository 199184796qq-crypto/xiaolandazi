package model

import "time"

type SpeechAnalysisProfile struct {
	ID                    int64     `json:"id"`
	Version               int       `json:"version"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	Provider              string    `json:"provider"`
	Model                 string    `json:"model"`
	SegmentSystemPrompt   string    `json:"segment_system_prompt"`
	SegmentPromptTemplate string    `json:"segment_prompt_template"`
	SummarySystemPrompt   string    `json:"summary_system_prompt"`
	SummaryPromptTemplate string    `json:"summary_prompt_template"`
	Status                string    `json:"status"`
	UpdatedByUserID       *int64    `json:"updated_by_user_id,omitempty"`
	UpdatedByDisplayName  string    `json:"updated_by_display_name"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type SpeechAnalysisProfileInput struct {
	Name                  string `json:"name"`
	Description           string `json:"description"`
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	SegmentSystemPrompt   string `json:"segment_system_prompt"`
	SegmentPromptTemplate string `json:"segment_prompt_template"`
	SummarySystemPrompt   string `json:"summary_system_prompt"`
	SummaryPromptTemplate string `json:"summary_prompt_template"`
	Activate              bool   `json:"activate"`
}
