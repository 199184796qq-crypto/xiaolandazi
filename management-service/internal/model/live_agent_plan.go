package model

import "time"

type LiveAgentPlan struct {
	ID          int64               `json:"id"`
	TenantID    int64               `json:"tenant_id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Status      string              `json:"status"`
	RoomCount   int                 `json:"room_count"`
	TermCount   int                 `json:"term_count"`
	RoomIDs     []int64             `json:"room_ids,omitempty"`
	Terms       []LiveAgentPlanTerm `json:"terms,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type CreateLiveAgentPlanInput struct {
	TenantID    int64  `json:"tenant_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type BindLiveAgentPlanRoomInput struct {
	TenantID int64 `json:"tenant_id,omitempty"`
	RoomID   int64 `json:"room_id"`
}

type LiveAgentPlanTermVariant struct {
	ID                int64     `json:"id"`
	VariantText       string    `json:"variant_text"`
	Source            string    `json:"source"`
	ConfirmationCount int       `json:"confirmation_count"`
	LastConfirmedAt   time.Time `json:"last_confirmed_at"`
}

type LiveAgentPlanTerm struct {
	ID            int64                      `json:"id"`
	PlanID        int64                      `json:"plan_id"`
	CanonicalText string                     `json:"canonical_text"`
	TermType      string                     `json:"term_type"`
	Note          string                     `json:"note,omitempty"`
	Status        string                     `json:"status"`
	Variants      []LiveAgentPlanTermVariant `json:"variants"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

type UpsertLiveAgentPlanTermInput struct {
	TenantID      int64  `json:"tenant_id,omitempty"`
	CanonicalText string `json:"canonical_text"`
	ObservedText  string `json:"observed_text,omitempty"`
	TermType      string `json:"term_type,omitempty"`
	Note          string `json:"note,omitempty"`
	Source        string `json:"source,omitempty"`
}

type NormalizeLiveAgentPlanTextInput struct {
	TenantID int64  `json:"tenant_id,omitempty"`
	Text     string `json:"text"`
}

type LiveAgentPlanCorrectionHit struct {
	ObservedText  string `json:"observed_text"`
	CanonicalText string `json:"canonical_text"`
	TermID        int64  `json:"term_id"`
}

type NormalizeLiveAgentPlanTextOutput struct {
	OriginalText   string                       `json:"original_text"`
	NormalizedText string                       `json:"normalized_text"`
	Applied        []LiveAgentPlanCorrectionHit `json:"applied"`
	HotTerms       []string                     `json:"hot_terms"`
}
