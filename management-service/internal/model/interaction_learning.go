package model

type InteractionLearningReview struct {
	PlanID          int64    `json:"plan_id"`
	Kind            string   `json:"kind"`
	Meaning         string   `json:"meaning"`
	Context         string   `json:"context"`
	ObservedContext string   `json:"observed_context,omitempty"`
	Reason          string   `json:"reason"`
	SuggestedRule   string   `json:"suggested_rule"`
	NeededFacts     []string `json:"needed_facts"`
	Occurrences     int      `json:"occurrences"`
	LastSourceRef   string   `json:"last_source_ref"`
	RawModelOutput  string   `json:"raw_model_output,omitempty"`
	ModelProvider   string   `json:"model_provider,omitempty"`
	ModelName       string   `json:"model_name,omitempty"`
	PromptSHA256    string   `json:"prompt_sha256,omitempty"`
}

type InteractionLearningObservation struct {
	TenantID    int64
	RoomID      int64
	ActorUserID int64
	Question    string
	Reply       string
	SourceType  string
	SourceRef   string
	Review      InteractionLearningReview
}
