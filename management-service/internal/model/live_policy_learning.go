package model

import "time"

const (
	LivePolicyLearningStatusPending  = "pending"
	LivePolicyLearningStatusAdopted  = "adopted"
	LivePolicyLearningStatusRejected = "rejected"
)

type LivePolicyLearningHistoryItem struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type LivePolicyLearningCandidate struct {
	ID                   int64                           `json:"id"`
	EvidenceType         string                          `json:"evidence_type"`
	SourceRef            string                          `json:"source_ref,omitempty"`
	SourceLayer          string                          `json:"source_layer"`
	IndustryCode         string                          `json:"industry_code,omitempty"`
	TenantID             *int64                          `json:"tenant_id,omitempty"`
	RoomID               *int64                          `json:"room_id,omitempty"`
	Question             string                          `json:"question"`
	ObservedReply        string                          `json:"observed_reply,omitempty"`
	FinalReply           string                          `json:"final_reply"`
	Feedback             string                          `json:"feedback,omitempty"`
	History              []LivePolicyLearningHistoryItem `json:"history"`
	RecommendedLayer     string                          `json:"recommended_layer"`
	RecommendationReason string                          `json:"recommendation_reason"`
	AbsorbRecommended    bool                            `json:"absorb_recommended"`
	Confidence           int                             `json:"confidence"`
	RuleTitle            string                          `json:"rule_title"`
	RuleText             string                          `json:"rule_text"`
	ExecutionMode        string                          `json:"execution_mode"`
	Status               string                          `json:"status"`
	ModelProvider        string                          `json:"model_provider,omitempty"`
	Model                string                          `json:"model,omitempty"`
	LatencyMS            int64                           `json:"latency_ms,omitempty"`
	LearningMeta         map[string]any                  `json:"learning_meta,omitempty"`
	AdoptedVersionID     *int64                          `json:"adopted_version_id,omitempty"`
	CreatedByUserID      int64                           `json:"created_by_user_id"`
	ReviewedByUserID     *int64                          `json:"reviewed_by_user_id,omitempty"`
	ReviewNote           string                          `json:"review_note,omitempty"`
	CreatedAt            time.Time                       `json:"created_at"`
	UpdatedAt            time.Time                       `json:"updated_at"`
	ReviewedAt           *time.Time                      `json:"reviewed_at,omitempty"`
}

type CreateLivePolicyLearningCandidateInput struct {
	SourceLayer      string                          `json:"source_layer"`
	IndustryCode     string                          `json:"industry_code,omitempty"`
	RoomID           int64                           `json:"room_id,omitempty"`
	Question         string                          `json:"question"`
	ObservedReply    string                          `json:"observed_reply,omitempty"`
	FinalReply       string                          `json:"final_reply"`
	Feedback         string                          `json:"feedback,omitempty"`
	History          []LivePolicyLearningHistoryItem `json:"history,omitempty"`
	EvidenceType     string                          `json:"evidence_type,omitempty"`
	SourceRef        string                          `json:"source_ref,omitempty"`
	LearningProvider string                          `json:"learning_provider,omitempty"`
	LearningModel    string                          `json:"learning_model,omitempty"`
}

type CreateLivePolicyLearningEvidenceInput struct {
	EvidenceType     string                          `json:"evidence_type,omitempty"`
	SourceRef        string                          `json:"source_ref,omitempty"`
	SourceLayer      string                          `json:"source_layer,omitempty"`
	IndustryCode     string                          `json:"industry_code,omitempty"`
	RoomID           int64                           `json:"room_id,omitempty"`
	Question         string                          `json:"question"`
	ObservedReply    string                          `json:"observed_reply,omitempty"`
	CorrectedReply   string                          `json:"corrected_reply,omitempty"`
	Feedback         string                          `json:"feedback,omitempty"`
	History          []LivePolicyLearningHistoryItem `json:"history,omitempty"`
	LearningProvider string                          `json:"learning_provider,omitempty"`
	LearningModel    string                          `json:"learning_model,omitempty"`
}

type AdoptLivePolicyLearningCandidateInput struct {
	TargetLayer  string `json:"target_layer,omitempty"`
	IndustryCode string `json:"industry_code,omitempty"`
	RoomID       int64  `json:"room_id,omitempty"`
	ReviewNote   string `json:"review_note,omitempty"`
}

type RejectLivePolicyLearningCandidateInput struct {
	ReviewNote string `json:"review_note,omitempty"`
}
