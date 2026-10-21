package anchorcycle

type FinalContentAudit struct {
	Decision       string               `json:"decision"`
	Raw            string               `json:"raw_response,omitempty"`
	Error          string               `json:"error,omitempty"`
	TextSHA        string               `json:"text_sha256"`
	ScopeSHA       string               `json:"scope_sha256"`
	ContextSHA     string               `json:"context_sha256"`
	Model          string               `json:"model,omitempty"`
	ProfileID      string               `json:"profile_id,omitempty"`
	PromptSHA      string               `json:"prompt_sha256,omitempty"`
	ExchangeID     int64                `json:"exchange_id,omitempty"`
	InputTokens    int64                `json:"input_tokens,omitempty"`
	OutputTokens   int64                `json:"output_tokens,omitempty"`
	LatencyMS      int64                `json:"latency_ms,omitempty"`
	UpstreamReview string               `json:"upstream_review,omitempty"`
	Reason         string               `json:"reason,omitempty"`
	Quote          string               `json:"quote,omitempty"`
	Attempts       []FinalReviewAttempt `json:"attempts,omitempty"`
}

type FinalReviewAttempt struct {
	ExchangeID   int64  `json:"exchange_id,omitempty"`
	Raw          string `json:"raw_response,omitempty"`
	Error        string `json:"error,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	LatencyMS    int64  `json:"latency_ms,omitempty"`
}

func (s *State) FinalContentSourceSHA() string {
	return ProductDigest(struct {
		Task        Task
		Facts       []HookFact
		P0, Scope   string
		Corrections []map[string]any
	}{s.Task(), s.HookFacts(), s.P0PolicyMD, s.Config.ComplianceScope, s.EffectiveCorrections()})
}

func (s *State) FinalContentCandidateApproved(c Candidate) bool {
	if s.Config.SkipReview || !s.FinalContentReviewRequired {
		return true
	}
	a := c.FinalContentAudit
	return a != nil && a.Error == "" && a.Decision == "通过" && a.ContextSHA != "" && a.ContextSHA == s.FinalContentReviewContextSHA && a.TextSHA == ProductDigest(spokenCandidate(c)) && a.ScopeSHA == s.FinalContentSourceSHA() && s.CheckApprovedText(c, spokenCandidate(c)) == nil && s.ExpressionCandidateApproved(c)
}
