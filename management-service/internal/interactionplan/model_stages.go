package interactionplan

import (
	"time"

	"livecompanion/management/internal/agentgateway"
)

// ModelStages are program-owned diagnostics for the archived response.
type ModelStages struct {
	Intent *ModelStage `json:"intent,omitempty"`
	Answer *ModelStage `json:"answer,omitempty"`
}

type ModelStage struct {
	Thinking  string `json:"thinking,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

// Record uses the completed call, including when a batch shares one intent call
// or generates intent and the spoken response in a single request.
func (m *ModelStages) Record(stage string, response agentgateway.Response, elapsed time.Duration) {
	call := &ModelStage{Thinking: response.ThinkingMode, ElapsedMS: elapsed.Milliseconds()}
	switch stage {
	case "interaction_answerflow_intent", "interaction_answerflow_intent_qa", "interaction_answerflow_batch_intent":
		m.Intent = call
	case "interaction_answerflow_answer", "interaction_purpose_resolution", "interaction_engagement_announcement", "interaction_welcome_writer":
		m.Answer = call
	}
}

func (m ModelStages) Recorded() *ModelStages {
	if m.Intent == nil && m.Answer == nil {
		return nil
	}
	return &m
}
