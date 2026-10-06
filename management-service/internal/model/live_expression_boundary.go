package model

// Expression choices are user-approved language boundaries, never evidence of
// product properties and never exemptions from platform/law/L1/L2 constraints.
type LiveExpressionBoundaryRule struct {
	ID           string `json:"id"`
	Enabled      bool   `json:"enabled"`
	Decision     string `json:"decision"`
	SourceQuote  string `json:"source_quote"`
	UserIntent   string `json:"user_intent"`
	Purpose      string `json:"purpose"`
	Guidance     string `json:"guidance"`
	Example      string `json:"example,omitempty"`
}

type LiveExpressionBoundaryProfile struct {
	TenantID int64                        `json:"tenant_id"`
	PlanID   int64                        `json:"plan_id"`
	Revision int64                        `json:"revision"`
	Items    []LiveExpressionBoundaryRule  `json:"items"`
}
