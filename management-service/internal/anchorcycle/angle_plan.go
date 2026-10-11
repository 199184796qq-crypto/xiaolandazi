package anchorcycle

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// A plan is writing guidance, never a spoken paragraph or content approval.
// Directions are permanent plan assets; selection and usage are per-run memory.
type AnglePlan struct {
	GenerationID        string               `json:"generation_id,omitempty"`
	GenerationCount     int                  `json:"generation_count,omitempty"`
	LifetimePlays       map[string]int       `json:"lifetime_plays,omitempty"`
	Cards               []AngleHook          `json:"hooks,omitempty"`
	GeneratedCards      []AngleHook          `json:"generated_hooks,omitempty"`
	DirectionSelections []DirectionSelection `json:"direction_selections,omitempty"`
	Calculations        []HookCalculation    `json:"calculations,omitempty"`
	PlanError           string               `json:"planning_error,omitempty"`
	AudienceConcern     string               `json:"audience_concern"`
	Goal                string               `json:"communication_goal"`
	Action              string               `json:"explanation_action"`
	Hooks               []string             `json:"hook_intents"`
	Raw                 string               `json:"raw_response,omitempty"`
	SourceSHA           string               `json:"source_sha256,omitempty"`
	PromptSHA           string               `json:"prompt_sha256,omitempty"`
	Model               string               `json:"model,omitempty"`
	InputTokens         int64                `json:"input_tokens,omitempty"`
	OutputTokens        int64                `json:"output_tokens,omitempty"`
}

// All relation keys are assigned by the program, never returned by the model.
type DirectionSelection struct {
	HookID     string `json:"hook_id"`
	Kept       bool   `json:"kept"`
	Reason     string `json:"reason"`
	ExchangeID int64  `json:"exchange_id,omitempty"`
}

func ParseAnglePlan(raw string) (*AnglePlan, error) {
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "```") {
		if i := strings.IndexByte(body, '\n'); i >= 0 && strings.HasSuffix(body, "```") {
			body = strings.TrimSpace(body[i+1 : len(body)-3])
		}
	}
	var p AnglePlan
	d := json.NewDecoder(strings.NewReader(body))
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errors.New("角度规划JSON无效")
	}
	p.AudienceConcern, p.Goal, p.Action = strings.TrimSpace(p.AudienceConcern), strings.TrimSpace(p.Goal), strings.TrimSpace(p.Action)
	if p.AudienceConcern == "" || p.Goal == "" || p.Action == "" || len(p.Hooks) == 0 || len(p.Hooks) > 32 {
		return nil, errors.New("角度规划缺少必要结构")
	}
	for i, h := range p.Hooks {
		p.Hooks[i] = strings.TrimSpace(h)
		if p.Hooks[i] == "" {
			return nil, errors.New("角度钩子意图为空")
		}
	}
	p.Raw = raw
	return &p, nil
}

func (s *State) AnglePlanKey() string {
	identity := ""
	if p := s.TextModel; p != nil {
		identity = p.ID + ":" + p.Model + ":" + p.BaseURL + ":" + p.SystemPrompt
	}
	task := s.Task()
	// Scheduling IDs, paragraph length and session mode do not change the content
	// question. Keep one pool for the same angle in all rooms using this plan.
	base := ProductDigest(struct {
		Point, Angle, Concern, Audience string
		Products                        []string
		Facts, Model, Mother, P0, Scope string
	}{task.SellingPoint, task.Angle, task.PainPoint, task.TargetAudience,
		s.HookProducts(), ProductDigest(s.HookFacts()), identity, s.AnglePlanMotherSHA,
		s.P0PolicyMD, s.Config.ComplianceScope})
	if len(s.EffectiveCorrections()) > 0 || task.FactScope != nil && len(task.FactScope.ProductBoundary) > 0 {
		var boundary any
		if task.FactScope != nil {
			boundary = task.FactScope.ProductBoundary
		}
		return ProductDigest(struct {
			Base            string
			Corrections     any
			ProductBoundary any
		}{base, s.EffectiveCorrections(), boundary})
	}
	return base
}
func (s *State) CurrentAnglePlan() *AnglePlan {
	if s.AnglePlanMotherSHA == "" {
		return nil
	}
	return s.AnglePlans[s.AnglePlanKey()]
}
