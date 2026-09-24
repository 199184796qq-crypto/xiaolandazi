package model

import "time"

const (
	LivePolicyLayerL1 = "L1"
	LivePolicyLayerL2 = "L2"
	LivePolicyLayerL3 = "L3"

	LivePolicyModeIntent   = "intent"
	LivePolicyModeVerbatim = "verbatim"

	LivePolicyOverrideAdd     = "add"
	LivePolicyOverrideReplace = "replace"
	LivePolicyOverrideDisable = "disable"
)

type LivePolicyIndustry struct {
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	ParentCode string    `json:"parent_code,omitempty"`
	Status     string    `json:"status"`
	SortOrder  int       `json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type LivePolicyScope struct {
	ID               int64     `json:"id"`
	Layer            string    `json:"layer"`
	ScopeKey         string    `json:"scope_key"`
	IndustryCode     string    `json:"industry_code,omitempty"`
	TenantID         *int64    `json:"tenant_id,omitempty"`
	RoomID           *int64    `json:"room_id,omitempty"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	CurrentVersionID *int64    `json:"current_version_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type LivePolicyRule struct {
	Key           string         `json:"key"`
	Title         string         `json:"title,omitempty"`
	Text          string         `json:"text"`
	ExecutionMode string         `json:"execution_mode"`
	FixedText     string         `json:"fixed_text,omitempty"`
	Enabled       bool           `json:"enabled"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type LivePolicyOverride struct {
	Key           string         `json:"key"`
	Operation     string         `json:"operation"`
	Title         string         `json:"title,omitempty"`
	Text          string         `json:"text,omitempty"`
	ExecutionMode string         `json:"execution_mode,omitempty"`
	FixedText     string         `json:"fixed_text,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type LivePolicyConflict struct {
	Code    string `json:"code"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

type LivePolicyVersion struct {
	ID                int64                `json:"id"`
	PolicyID          int64                `json:"policy_id"`
	VersionNo         uint64               `json:"version_no"`
	LifecycleStatus   string               `json:"lifecycle_status"`
	SourceText        string               `json:"source_text"`
	Rules             []LivePolicyRule     `json:"rules"`
	Overrides         []LivePolicyOverride `json:"overrides"`
	Conflicts         []LivePolicyConflict `json:"conflicts"`
	Note              string               `json:"note,omitempty"`
	SourceVersionID   *int64               `json:"source_version_id,omitempty"`
	CreatedByUserID   *int64               `json:"created_by_user_id,omitempty"`
	PublishedByUserID *int64               `json:"published_by_user_id,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	PublishedAt       *time.Time           `json:"published_at,omitempty"`
}

type CreateLivePolicyDraftInput struct {
	Layer        string               `json:"layer"`
	IndustryCode string               `json:"industry_code,omitempty"`
	TenantID     int64                `json:"tenant_id,omitempty"`
	RoomID       int64                `json:"room_id,omitempty"`
	Name         string               `json:"name,omitempty"`
	SourceText   string               `json:"source_text"`
	Rules        []LivePolicyRule     `json:"rules,omitempty"`
	Overrides    []LivePolicyOverride `json:"overrides,omitempty"`
	Conflicts    []LivePolicyConflict `json:"conflicts,omitempty"`
	Note         string               `json:"note,omitempty"`
}

type LiveEffectivePolicyRule struct {
	Key             string         `json:"key"`
	Title           string         `json:"title,omitempty"`
	Text            string         `json:"text"`
	ExecutionMode   string         `json:"execution_mode"`
	FixedText       string         `json:"fixed_text,omitempty"`
	SourceLayer     string         `json:"source_layer"`
	SourceVersionID int64          `json:"source_version_id,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type LiveEffectivePolicy struct {
	IndustryCode string                    `json:"industry_code"`
	L1           *LivePolicyVersion        `json:"l1,omitempty"`
	L2           *LivePolicyVersion        `json:"l2,omitempty"`
	L3           *LivePolicyVersion        `json:"l3,omitempty"`
	Rules        []LiveEffectivePolicyRule `json:"rules"`
	Conflicts    []LivePolicyConflict      `json:"conflicts"`
	PromptText   string                    `json:"prompt_text"`
}

type LivePolicyContext struct {
	Scope    LivePolicyScope     `json:"scope"`
	Versions []LivePolicyVersion `json:"versions"`
	Active   *LivePolicyVersion  `json:"active,omitempty"`
	Industry *LivePolicyIndustry `json:"industry,omitempty"`
}

type LiveRoomPolicyContext struct {
	IndustryCode string              `json:"industry_code"`
	Effective    LiveEffectivePolicy `json:"effective"`
	Versions     []LivePolicyVersion `json:"l3_versions"`
	L3Scope      *LivePolicyScope    `json:"l3_scope,omitempty"`
}

type LiveRuntimePolicySnapshot struct {
	SessionID    int64               `json:"session_id"`
	TenantID     int64               `json:"tenant_id"`
	RoomID       int64               `json:"room_id"`
	IndustryCode string              `json:"industry_code"`
	L1VersionID  *int64              `json:"l1_version_id,omitempty"`
	L2VersionID  *int64              `json:"l2_version_id,omitempty"`
	L3VersionID  *int64              `json:"l3_version_id,omitempty"`
	Effective    LiveEffectivePolicy `json:"effective"`
	CreatedAt    time.Time           `json:"created_at"`
}
