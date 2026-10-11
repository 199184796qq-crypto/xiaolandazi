package anchorcycle

type FinalContentAudit struct {
	Decision     string `json:"decision"`
	Raw          string `json:"raw_response,omitempty"`
	Error        string `json:"error,omitempty"`
	TextSHA      string `json:"text_sha256"`
	ScopeSHA     string `json:"scope_sha256"`
	ContextSHA   string `json:"context_sha256"`
	Model        string `json:"model,omitempty"`
	ProfileID    string `json:"profile_id,omitempty"`
	PromptSHA    string `json:"prompt_sha256,omitempty"`
	ExchangeID   int64  `json:"exchange_id,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
}

func (s *State) FinalContentSourceSHA() string {
	return ProductDigest(struct {
		Task        Task
		Facts       []HookFact
		Corrections []map[string]any
		P0, Scope   string
	}{s.Task(), s.HookFacts(), s.EffectiveCorrections(), s.P0PolicyMD, s.Config.ComplianceScope})
}

func (s *State) FinalContentCandidateApproved(c Candidate) bool {
	if !s.FinalContentReviewRequired {
		return true
	}
	a := c.FinalContentAudit
	return a != nil && a.Error == "" && a.Decision == "通过" && a.ContextSHA != "" && a.ContextSHA == s.FinalContentReviewContextSHA && a.TextSHA == ProductDigest(spokenCandidate(c)) && a.ScopeSHA == s.FinalContentSourceSHA() && s.CheckApprovedText(c, spokenCandidate(c)) == nil && s.LectureCandidateApproved(c)
}
