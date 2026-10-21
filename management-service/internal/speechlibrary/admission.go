package speechlibrary

import (
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/anchorcycle"
)

// Admission records the actual acceptance mode. Skipping review must never be
// represented as a model approval. Both modes bind the frozen scope and prose.
const AdmissionVersion = "accepted-mainline-v1"
const GenerationAccepted = "generation_accepted"
const ReviewPassed = "review_passed"

type Admission struct {
	Version    string `json:"version"`
	Mode       string `json:"mode"`
	ContextSHA string `json:"context_sha"`
	TextSHA    string `json:"text_sha"`
}

func SpokenText(c anchorcycle.Candidate) string {
	if strings.TrimSpace(c.OutputText) != "" {
		return strings.TrimSpace(c.OutputText)
	}
	return strings.TrimSpace(c.Text)
}

func NewAdmission(sc Scope, c anchorcycle.Candidate, mode string) *Admission {
	return &Admission{AdmissionVersion, mode, sc.TextContext, Digest(SpokenText(c))}
}

func (e Entry) ValidateAdmission(sc Scope, mode string) error {
	a := e.Admission
	text := SpokenText(e.Candidate)
	if a == nil || a.Version != AdmissionVersion || a.ContextSHA != sc.TextContext || a.TextSHA != Digest(text) || text == "" || mode != "" && a.Mode != mode {
		return errors.New("话术库采纳凭据与当前内容不一致")
	}
	switch a.Mode {
	case GenerationAccepted:
		return nil
	case ReviewPassed:
		r := e.Candidate.FinalContentAudit
		if r != nil && r.Decision == "通过" && r.Error == "" && r.TextSHA == Digest(text) && r.ContextSHA != "" && r.ScopeSHA != "" {
			return nil
		}
	}
	return errors.New("话术库审核模式或审核回执无效")
}

// Keep the existing JSON column; no business schema migration is necessary.
// Legacy bare candidates remain readable, but have no new admission authority.
func (e Entry) MarshalPayload() ([]byte, error) {
	return json.Marshal(struct {
		Version   string                `json:"version"`
		Candidate anchorcycle.Candidate `json:"candidate"`
		Admission *Admission            `json:"admission"`
	}{AdmissionVersion, e.Candidate, e.Admission})
}

func (e *Entry) UnmarshalPayload(raw []byte) error {
	var p struct {
		Version   string                `json:"version"`
		Candidate anchorcycle.Candidate `json:"candidate"`
		Admission *Admission            `json:"admission"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	e.Admission = nil
	if p.Version != "" {
		if p.Version != AdmissionVersion {
			return errors.New("未知的话术库内容版本")
		}
		e.Candidate, e.Admission = p.Candidate, p.Admission
		return nil
	}
	return json.Unmarshal(raw, &e.Candidate)
}
