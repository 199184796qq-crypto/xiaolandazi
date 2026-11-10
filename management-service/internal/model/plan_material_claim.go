package model

import (
	"encoding/json"
	"time"
)

// PlanMaterialClaim is a plan-scoped claim extracted from pasted material.
// It is deliberately separate from the formal live-agent facts: importing
// material must not silently change the facts used by a running room.
type PlanMaterialClaim struct {
	MaterialClaimDetails
	ID              int64           `json:"id"`
	TenantID        int64           `json:"tenant_id"`
	PlanID          int64           `json:"plan_id"`
	ClaimKey        string          `json:"claim_key"`
	Topic           string          `json:"topic"`
	Attribute       string          `json:"attribute"`
	Value           string          `json:"value"`
	Content         string          `json:"content"`
	Polarity        string          `json:"polarity"`
	FactType        string          `json:"fact_type"`
	Conditions      json.RawMessage `json:"conditions"`
	SourceQuote     string          `json:"source_quote"`
	SourceSentence  string          `json:"source_sentence"`
	ScopeKind       string          `json:"scope_kind"`
	StoreName       string          `json:"store_name"`
	ProductLinkID   int64           `json:"product_link_id"`
	EmbeddingModel  string          `json:"embedding_model"`
	Status          string          `json:"status"`
	CreatedByUserID *int64          `json:"created_by_user_id,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// PlanMaterialClaimInput is the reviewed candidate returned by the analysis
// model and accepted by the confirm endpoint.
type PlanMaterialClaimInput struct {
	MaterialClaimDetails
	ClaimKey       string          `json:"claim_key"`
	Topic          string          `json:"topic"`
	Attribute      string          `json:"attribute"`
	Value          string          `json:"value"`
	Content        string          `json:"content"`
	Polarity       string          `json:"polarity"`
	FactType       string          `json:"fact_type"`
	Conditions     json.RawMessage `json:"conditions"`
	SourceQuote    string          `json:"source_quote"`
	SourceSentence string          `json:"source_sentence"`
	ScopeKind      string          `json:"scope_kind"`
	StoreName      string          `json:"store_name"`
	ProductLinkID  int64           `json:"product_link_id"`
	ReplacesID     int64           `json:"replaces_id,omitempty"`
}

// MaterialClaimDetails preserves the preview contract without flattening a
// conditional fact, QA or named scope into an unqualified content string.
// It is stored in details_json; legacy rows remain valid with empty details.
type MaterialClaimDetails struct {
	ObjectTargets     []MaterialObjectTarget `json:"object_targets,omitempty"`
	ClientID          string                 `json:"client_id,omitempty"`
	Destination       string                 `json:"destination,omitempty"`
	Kind              string                 `json:"kind,omitempty"`
	ScopeName         string                 `json:"scope_name,omitempty"`
	Exceptions        json.RawMessage        `json:"exceptions,omitempty"`
	Question          string                 `json:"question,omitempty"`
	Answer            string                 `json:"answer,omitempty"`
	Aliases           []string               `json:"aliases,omitempty"`
	RiskFlags         []string               `json:"risk_flags,omitempty"`
	Risks             []map[string]any       `json:"risks,omitempty"`
	P0Hits            []map[string]any       `json:"p0_hits,omitempty"`
	NeedsConfirmation bool                   `json:"needs_confirmation,omitempty"`
	NeedsSplit        bool                   `json:"needs_split,omitempty"`
	AtomicityReason   string                 `json:"atomicity_reason,omitempty"`
	PolarityIssue     string                 `json:"polarity_issue,omitempty"`
	ConditionIssue    string                 `json:"condition_issue,omitempty"`
}

// Targets bind one attribute to several stable plan objects; a global target
// is exclusive. Unknown ownership is represented by an empty target list.
type MaterialObjectTarget struct {
	Kind      string `json:"kind"`
	LinkID    int64  `json:"link_id,omitempty"`
	LinkKey   string `json:"link_key,omitempty"`
	LinkNo    int64  `json:"link_no,omitempty"` // Legacy preview number, resolved before persistence.
	StoreName string `json:"store_name,omitempty"`
}
type MaterialStoreObject struct {
	Name    string  `json:"name"`
	LinkIDs []int64 `json:"link_ids"`
}
type MaterialObjectCatalog struct {
	Revision int64                 `json:"revision"`
	Stores   []MaterialStoreObject `json:"stores"`
}
