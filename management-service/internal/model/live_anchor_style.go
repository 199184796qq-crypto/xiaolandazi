package model

import "time"

// LiveAnchorStyle is a reusable presenter persona. Product facts and
// interaction policy remain attached to the live-agent plan; this object only
// stores how the presenter speaks.
type LiveAnchorStyle struct {
	ID              int64                           `json:"id"`
	TenantID        int64                           `json:"tenant_id"`
	Name            string                          `json:"name"`
	Description     string                          `json:"description,omitempty"`
	Status          string                          `json:"status"`
	Profile         LiveAgentPlanAnchorStyleProfile `json:"profile"`
	PluginSettings  []LiveAnchorStylePluginSetting  `json:"plugin_settings"`
	SampleCount     int                             `json:"sample_count"`
	TrainingCount   int                             `json:"training_count"`
	BoundPlanCount  int                             `json:"bound_plan_count"`
	CreatedByUserID *int64                          `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64                          `json:"updated_by_user_id,omitempty"`
	CreatedAt       time.Time                       `json:"created_at"`
	UpdatedAt       time.Time                       `json:"updated_at"`
}

type LiveAnchorStyleSample struct {
	ID              int64                           `json:"id"`
	TenantID        int64                           `json:"tenant_id,omitempty"`
	StyleID         int64                           `json:"style_id"`
	Title           string                          `json:"title"`
	SourceType      string                          `json:"source_type"`
	OriginalName    string                          `json:"original_name,omitempty"`
	RawText         string                          `json:"raw_text"`
	ReadableText    string                          `json:"readable_text"`
	AnalysisStatus  string                          `json:"analysis_status"`
	Analysis        LiveAgentPlanAnchorStyleProfile `json:"analysis"`
	Provider        string                          `json:"provider,omitempty"`
	Model           string                          `json:"model,omitempty"`
	LatencyMS       int64                           `json:"latency_ms,omitempty"`
	Progress        int                             `json:"progress"`
	Stage           string                          `json:"stage,omitempty"`
	ErrorMessage    string                          `json:"error_message,omitempty"`
	AnalyzedAt      *time.Time                      `json:"analyzed_at,omitempty"`
	CreatedByUserID *int64                          `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time                       `json:"created_at"`
	UpdatedAt       time.Time                       `json:"updated_at"`
}

type LiveAnchorStyleTraining struct {
	ID               int64          `json:"id"`
	StyleID          int64          `json:"style_id"`
	RequestText      string         `json:"request_text"`
	TargetChars      int            `json:"target_chars"`
	Heat             int            `json:"heat"`
	ExpansionFreedom int            `json:"expansion_freedom"`
	SelectedFacts    []string       `json:"selected_facts"`
	GeneratedText    string         `json:"generated_text"`
	Score            map[string]any `json:"score,omitempty"`
	Status           string         `json:"status"`
	CreatedByUserID  *int64         `json:"created_by_user_id,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type LiveAnchorStylePluginSetting struct {
	ID            int64          `json:"id,omitempty"`
	StyleID       int64          `json:"style_id,omitempty"`
	PluginID      string         `json:"plugin_id"`
	PluginVersion string         `json:"plugin_version"`
	Enabled       bool           `json:"enabled"`
	Parameters    map[string]any `json:"parameters,omitempty"`
	UpdatedAt     *time.Time     `json:"updated_at,omitempty"`
}

type CreateLiveAnchorStyleInput struct {
	TenantID    int64  `json:"tenant_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type UpdateLiveAnchorStyleInput struct {
	TenantID    int64                            `json:"tenant_id,omitempty"`
	Name        string                           `json:"name"`
	Description string                           `json:"description,omitempty"`
	Profile     *LiveAgentPlanAnchorStyleProfile `json:"profile,omitempty"`
}

type CreateLiveAnchorStyleSampleInput struct {
	TenantID     int64  `json:"tenant_id,omitempty"`
	Title        string `json:"title"`
	SourceType   string `json:"source_type,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	RawText      string `json:"raw_text"`
	ReadableText string `json:"readable_text,omitempty"`
}

type LiveAnchorStyleTrainingInput struct {
	TenantID         int64          `json:"tenant_id,omitempty"`
	RequestText      string         `json:"request_text"`
	TargetChars      int            `json:"target_chars"`
	Heat             int            `json:"heat"`
	ExpansionFreedom int            `json:"expansion_freedom"`
	SelectedFacts    []string       `json:"selected_facts,omitempty"`
	GeneratedText    string         `json:"generated_text,omitempty"`
	Score            map[string]any `json:"score,omitempty"`
}
