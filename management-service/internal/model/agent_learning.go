package model

import "time"

const (
	AgentMemoryTypeSemantic = "semantic"
	AgentMemoryTypeFact     = "fact"
	AgentMemoryTypeWording  = "wording"
	AgentMemoryTypeStyle    = "style"

	// Backward-compatible alias for early development code.
	AgentMemoryTypeExpression = AgentMemoryTypeWording

	AgentLearningStatusEditing = "editing"
	AgentLearningStatusAdopted = "adopted"
	AgentLearningStatusClosed  = "closed"

	AgentMemoryStatusActive   = "active"
	AgentMemoryStatusInactive = "inactive"

	AgentMemoryVersionStatusActive     = "active"
	AgentMemoryVersionStatusSuperseded = "superseded"
	AgentMemoryVersionStatusRolledBack = "rolled_back"
)

type AgentLearningSession struct {
	ID                  int64      `json:"id"`
	TenantID            int64      `json:"tenant_id"`
	RoomID              int64      `json:"room_id"`
	SourceType          string     `json:"source_type"`
	SourceRef           string     `json:"source_ref,omitempty"`
	Question            string     `json:"question,omitempty"`
	OriginalReply       string     `json:"original_reply,omitempty"`
	Target              string     `json:"target,omitempty"`
	Status              string     `json:"status"`
	MemoryType          string     `json:"memory_type,omitempty"`
	AdoptedMemoryItemID *int64     `json:"adopted_memory_item_id,omitempty"`
	CreatedByUserID     int64      `json:"created_by_user_id"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	AdoptedAt           *time.Time `json:"adopted_at,omitempty"`
}

type AgentLearningEvidence struct {
	ID        int64     `json:"id"`
	SessionID int64     `json:"session_id"`
	TurnNo    uint64    `json:"turn_no"`
	Feedback  string    `json:"feedback"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentLearningResult struct {
	ID                  int64          `json:"id"`
	SessionID           int64          `json:"session_id"`
	EvidenceID          int64          `json:"evidence_id"`
	TurnNo              uint64         `json:"turn_no"`
	MemoryType          string         `json:"memory_type"`
	Target              string         `json:"target"`
	MemoryKey           string         `json:"memory_key"`
	MatchedMemoryItemID *int64         `json:"matched_memory_item_id,omitempty"`
	ResultText          string         `json:"result_text"`
	Structured          map[string]any `json:"structured,omitempty"`
	ModelProvider       string         `json:"model_provider,omitempty"`
	ModelName           string         `json:"model_name,omitempty"`
	LatencyMS           int64          `json:"latency_ms,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
}

type AgentLearningTimelineItem struct {
	Evidence AgentLearningEvidence `json:"evidence"`
	Result   AgentLearningResult   `json:"result"`
}

type AgentMemoryEvidenceStat struct {
	ID                      int64      `json:"id"`
	TenantID                int64      `json:"tenant_id"`
	RoomID                  int64      `json:"room_id"`
	MemoryType              string     `json:"memory_type"`
	MemoryKey               string     `json:"memory_key"`
	ValueSignature          string     `json:"value_signature"`
	ValueText               string     `json:"value_text"`
	OccurrenceCount         uint64     `json:"occurrence_count"`
	ConsecutiveCount        uint64     `json:"consecutive_count"`
	ExplicitCorrectionCount uint64     `json:"explicit_correction_count"`
	AdoptedCount            uint64     `json:"adopted_count"`
	LastSessionID           int64      `json:"last_session_id"`
	LastResultID            int64      `json:"last_result_id"`
	FirstSeenAt             time.Time  `json:"first_seen_at"`
	LastSeenAt              time.Time  `json:"last_seen_at"`
	LastAdoptedAt           *time.Time `json:"last_adopted_at,omitempty"`
}

type AgentLearningSessionDetail struct {
	Session  AgentLearningSession        `json:"session"`
	Timeline []AgentLearningTimelineItem `json:"timeline"`
	Latest   *AgentLearningResult        `json:"latest,omitempty"`
}

type AgentMemoryItem struct {
	ID               int64               `json:"id"`
	TenantID         int64               `json:"tenant_id"`
	RoomID           int64               `json:"room_id"`
	MemoryType       string              `json:"memory_type"`
	MemoryKey        string              `json:"memory_key"`
	Target           string              `json:"target"`
	Status           string              `json:"status"`
	CurrentVersionID *int64              `json:"current_version_id,omitempty"`
	CreatedByUserID  int64               `json:"created_by_user_id"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
	CurrentVersion   *AgentMemoryVersion `json:"current_version,omitempty"`
}

type AgentMemoryVersion struct {
	ID              int64          `json:"id"`
	MemoryItemID    int64          `json:"memory_item_id"`
	VersionNo       uint64         `json:"version_no"`
	Status          string         `json:"status"`
	ContentText     string         `json:"content_text"`
	Structured      map[string]any `json:"structured,omitempty"`
	SourceSessionID int64          `json:"source_session_id"`
	SourceResultID  int64          `json:"source_result_id"`
	CreatedByUserID int64          `json:"created_by_user_id"`
	CreatedAt       time.Time      `json:"created_at"`
}

type CreateAgentLearningSessionInput struct {
	SourceType    string `json:"source_type"`
	SourceRef     string `json:"source_ref,omitempty"`
	Question      string `json:"question,omitempty"`
	OriginalReply string `json:"original_reply,omitempty"`
	Target        string `json:"target,omitempty"`
}

type CreateAgentLearningTurnInput struct {
	Feedback string `json:"feedback"`
}

type AgentLearningTurnOutput struct {
	Session AgentLearningSession `json:"session"`
	Result  AgentLearningResult  `json:"result"`
}

type AdoptAgentLearningOutput struct {
	Session AgentLearningSession `json:"session"`
	Memory  AgentMemoryItem      `json:"memory"`
	Version AgentMemoryVersion   `json:"version"`
}
