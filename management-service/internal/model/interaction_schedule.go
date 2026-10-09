package model

type InteractionGoal struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Action string   `json:"action"`
	Heats  []string `json:"heats"`
	Stages []string `json:"stages"`
	Angles []string `json:"angles"`
}
type InteractionEventRule struct {
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	Priority        string            `json:"priority"`
	Weight          int               `json:"weight"`
	CooldownSeconds int               `json:"cooldown_seconds"`
	TTLSeconds      int               `json:"ttl_seconds"`
	MaxWaitSeconds  int               `json:"max_wait_seconds"`
	BatchThreshold  int               `json:"batch_threshold"`
	Goals           []InteractionGoal `json:"goals"`
}
type InteractionTiming struct {
	Name           string `json:"name"`
	MaxWaitSeconds int    `json:"max_wait_seconds"`
	TTLSeconds     int    `json:"ttl_seconds"`
	BatchThreshold int    `json:"batch_threshold"`
}
type InteractionSchedule struct {
	OpeningObservationSeconds *int                            `json:"opening_observation_seconds,omitempty"`
	TimingOverrides           map[string]InteractionTiming    `json:"timing_overrides"`
	Version                   int64                           `json:"version"`
	PlanID                    int64                           `json:"plan_id"`
	Name                      string                          `json:"name"`
	TemplateVersion           int                             `json:"template_version"`
	TemplateSHA256            string                          `json:"template_sha256"`
	FieldLabels               map[string]string               `json:"field_labels"`
	PriorityLabels            map[string]string               `json:"priority_labels"`
	HeatLabels                map[string]string               `json:"heat_labels"`
	StageLabels               map[string]string               `json:"stage_labels"`
	AnswerLimits              map[string]int                  `json:"answer_limits"`
	MaxRetries                int                             `json:"max_retries"`
	Events                    map[string]InteractionEventRule `json:"events"`
}
