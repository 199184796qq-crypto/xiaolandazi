package anchorcycle

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// A plan is writing guidance, never a spoken paragraph or content approval.
// It is disposable per-run memory, tied to facts, task, model and mother version.
type AnglePlan struct {
	AudienceConcern string   `json:"audience_concern"`
	Goal            string   `json:"communication_goal"`
	Action          string   `json:"explanation_action"`
	Hooks           []string `json:"hook_intents"`
	Raw             string   `json:"raw_response,omitempty"`
	SourceSHA       string   `json:"source_sha256,omitempty"`
	PromptSHA       string   `json:"prompt_sha256,omitempty"`
	Model           string   `json:"model,omitempty"`
	InputTokens     int64    `json:"input_tokens,omitempty"`
	OutputTokens    int64    `json:"output_tokens,omitempty"`
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
	return ProductDigest(struct {
		Task                            Task
		Phase, Facts, Model, Mother, P0 string
	}{s.Task(), s.Phase, s.ProductFacts(s.Task()), identity, s.AnglePlanMotherSHA, s.P0PolicyMD})
}
func (s *State) CurrentAnglePlan() *AnglePlan {
	if s.AnglePlanMotherSHA == "" {
		return nil
	}
	return s.AnglePlans[s.AnglePlanKey()]
}
