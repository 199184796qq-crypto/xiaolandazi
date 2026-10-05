package model

import "time"

type LiveAgentPlan struct {
	ID          int64               `json:"id"`
	TenantID    int64               `json:"tenant_id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Status      string              `json:"status"`
	RoomCount   int                 `json:"room_count"`
	TermCount   int                 `json:"term_count"`
	RoomIDs     []int64             `json:"room_ids,omitempty"`
	Terms       []LiveAgentPlanTerm `json:"terms,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
}

type CreateLiveAgentPlanInput struct {
	TenantID    int64  `json:"tenant_id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type BindLiveAgentPlanRoomInput struct {
	TenantID int64 `json:"tenant_id,omitempty"`
	RoomID   int64 `json:"room_id"`
}

type LiveAgentPlanTermVariant struct {
	ID                int64     `json:"id"`
	VariantText       string    `json:"variant_text"`
	Source            string    `json:"source"`
	ConfirmationCount int       `json:"confirmation_count"`
	LastConfirmedAt   time.Time `json:"last_confirmed_at"`
}

type LiveAgentPlanTerm struct {
	ID            int64                      `json:"id"`
	PlanID        int64                      `json:"plan_id"`
	CanonicalText string                     `json:"canonical_text"`
	TermType      string                     `json:"term_type"`
	Note          string                     `json:"note,omitempty"`
	Status        string                     `json:"status"`
	Variants      []LiveAgentPlanTermVariant `json:"variants"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

type UpsertLiveAgentPlanTermInput struct {
	TenantID      int64  `json:"tenant_id,omitempty"`
	CanonicalText string `json:"canonical_text"`
	ObservedText  string `json:"observed_text,omitempty"`
	TermType      string `json:"term_type,omitempty"`
	Note          string `json:"note,omitempty"`
	Source        string `json:"source,omitempty"`
}

type NormalizeLiveAgentPlanTextInput struct {
	TenantID int64  `json:"tenant_id,omitempty"`
	Text     string `json:"text"`
}

type LiveAgentPlanCorrectionHit struct {
	ObservedText  string `json:"observed_text"`
	CanonicalText string `json:"canonical_text"`
	TermID        int64  `json:"term_id"`
}

type NormalizeLiveAgentPlanTextOutput struct {
	OriginalText   string                       `json:"original_text"`
	NormalizedText string                       `json:"normalized_text"`
	Applied        []LiveAgentPlanCorrectionHit `json:"applied"`
	HotTerms       []string                     `json:"hot_terms"`
}

type LiveAgentPlanFactCandidate struct {
	Category         string `json:"category"`
	Key              string `json:"key"`
	Value            string `json:"value"`
	ForbiddenWording string `json:"forbidden_wording,omitempty"`
	SafeRewrite      string `json:"safe_rewrite,omitempty"`
	Status           string `json:"status"`
	ReviewBucket     string `json:"review_bucket,omitempty"`
	ReviewReason     string `json:"review_reason,omitempty"`
	SourceQuote      string `json:"source_quote,omitempty"`
	Confidence       string `json:"confidence,omitempty"`
	Note             string `json:"note,omitempty"`
}

type LiveAgentPlanProductLinkCandidate struct {
	LinkKey       string                                   `json:"link_key"`
	ProductName   string                                   `json:"product_name,omitempty"`
	Spec          string                                   `json:"spec,omitempty"`
	DailyPrice    string                                   `json:"daily_price,omitempty"`
	ActivityPrice string                                   `json:"activity_price,omitempty"`
	Quantity      string                                   `json:"quantity,omitempty"`
	Gift          string                                   `json:"gift,omitempty"`
	Activity      string                                   `json:"activity,omitempty"`
	Audience      string                                   `json:"audience,omitempty"`
	ReviewBucket  string                                   `json:"review_bucket,omitempty"`
	ReviewReason  string                                   `json:"review_reason,omitempty"`
	SourceQuotes  []string                                 `json:"source_quotes,omitempty"`
	Confidence    string                                   `json:"confidence,omitempty"`
	Attributes    []LiveAgentPlanProductAttributeCandidate `json:"attributes,omitempty"`
}

// LiveAgentPlanProductAttributeCandidate contains category-specific product
// information inferred from source material. Common fields stay on the product
// link; only facts that do not fit the common schema belong here.
type LiveAgentPlanProductAttributeCandidate struct {
	Code            string `json:"code"`
	Label           string `json:"label"`
	Value           string `json:"value"`
	Unit            string `json:"unit,omitempty"`
	DisplayType     string `json:"display_type,omitempty"`
	DisplayPriority int    `json:"display_priority,omitempty"`
	SourceQuote     string `json:"source_quote,omitempty"`
}

type LiveAgentPlanBenefitCandidate struct {
	Key           string   `json:"key,omitempty"`
	LinkKey       string   `json:"link_key,omitempty"`
	ProductName   string   `json:"product_name,omitempty"`
	ActivityPrice string   `json:"activity_price,omitempty"`
	Gift          string   `json:"gift,omitempty"`
	Activity      string   `json:"activity,omitempty"`
	StartsAt      string   `json:"starts_at,omitempty"`
	EndsAt        string   `json:"ends_at,omitempty"`
	ReviewBucket  string   `json:"review_bucket,omitempty"`
	ReviewReason  string   `json:"review_reason,omitempty"`
	SourceQuotes  []string `json:"source_quotes,omitempty"`
}

type LiveAgentPlanProductLink struct {
	ID                 int64                           `json:"id"`
	TenantID           int64                           `json:"tenant_id"`
	PlanID             int64                           `json:"plan_id"`
	LinkKey            string                          `json:"link_key"`
	ProductName        string                          `json:"product_name,omitempty"`
	Spec               string                          `json:"spec,omitempty"`
	DailyPrice         string                          `json:"daily_price,omitempty"`
	Quantity           string                          `json:"quantity,omitempty"`
	Audience           string                          `json:"audience,omitempty"`
	SourceQuote        string                          `json:"source_quote,omitempty"`
	SourceReviewBucket string                          `json:"source_review_bucket,omitempty"`
	SourceReviewReason string                          `json:"source_review_reason,omitempty"`
	SourceType         string                          `json:"source_type"`
	SourceRef          string                          `json:"source_ref,omitempty"`
	Status             string                          `json:"status"`
	VersionNo          int64                           `json:"version_no"`
	CreatedByUserID    *int64                          `json:"created_by_user_id,omitempty"`
	UpdatedByUserID    *int64                          `json:"updated_by_user_id,omitempty"`
	CreatedAt          time.Time                       `json:"created_at"`
	UpdatedAt          time.Time                       `json:"updated_at"`
	Attributes         []LiveAgentPlanProductAttribute `json:"attributes"`
}

type LiveAgentPlanProductAttribute struct {
	ID              int64     `json:"id"`
	TenantID        int64     `json:"tenant_id"`
	PlanID          int64     `json:"plan_id"`
	ProductLinkID   int64     `json:"product_link_id"`
	Code            string    `json:"code"`
	Label           string    `json:"label"`
	Value           string    `json:"value"`
	Unit            string    `json:"unit,omitempty"`
	DisplayType     string    `json:"display_type"`
	DisplayPriority int       `json:"display_priority"`
	SourceQuote     string    `json:"source_quote,omitempty"`
	SourceType      string    `json:"source_type"`
	SourceRef       string    `json:"source_ref,omitempty"`
	Status          string    `json:"status"`
	VersionNo       int64     `json:"version_no"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateLiveAgentPlanProductAttributeInput struct {
	TenantID        int64  `json:"tenant_id,omitempty"`
	Code            string `json:"code"`
	Label           string `json:"label"`
	Value           string `json:"value"`
	Unit            string `json:"unit,omitempty"`
	DisplayType     string `json:"display_type,omitempty"`
	DisplayPriority int    `json:"display_priority,omitempty"`
	SourceQuote     string `json:"source_quote,omitempty"`
}

type UpdateLiveAgentPlanProductAttributeInput struct {
	TenantID          int64  `json:"tenant_id,omitempty"`
	ExpectedVersionNo int64  `json:"expected_version_no,omitempty"`
	Code              string `json:"code"`
	Label             string `json:"label"`
	Value             string `json:"value"`
	Unit              string `json:"unit,omitempty"`
	DisplayType       string `json:"display_type,omitempty"`
	DisplayPriority   int    `json:"display_priority,omitempty"`
}

type AdoptLiveAgentPlanProductLinksInput struct {
	TenantID  int64                               `json:"tenant_id,omitempty"`
	SourceRef string                              `json:"source_ref,omitempty"`
	Links     []LiveAgentPlanProductLinkCandidate `json:"links"`
}

type UpdateLiveAgentPlanProductLinkInput struct {
	TenantID          int64  `json:"tenant_id,omitempty"`
	ExpectedVersionNo int64  `json:"expected_version_no,omitempty"`
	LinkKey           string `json:"link_key"`
	ProductName       string `json:"product_name"`
	Spec              string `json:"spec,omitempty"`
	DailyPrice        string `json:"daily_price,omitempty"`
	Quantity          string `json:"quantity,omitempty"`
	Audience          string `json:"audience,omitempty"`
}

type LiveAgentPlanProductLinkAdoptionResult struct {
	Candidate LiveAgentPlanProductLinkCandidate `json:"candidate"`
	Status    string                            `json:"status"`
	Message   string                            `json:"message,omitempty"`
	Saved     *LiveAgentPlanProductLink         `json:"saved,omitempty"`
	Existing  *LiveAgentPlanProductLink         `json:"existing,omitempty"`
}

type AdoptLiveAgentPlanProductLinksOutput struct {
	Results   []LiveAgentPlanProductLinkAdoptionResult `json:"results"`
	Adopted   int                                      `json:"adopted"`
	Skipped   int                                      `json:"skipped"`
	Blocked   int                                      `json:"blocked"`
	Conflicts int                                      `json:"conflicts"`
}

type LiveAgentPlanAnalysisCompleteness struct {
	DetectedLinkKeys []string `json:"detected_link_keys"`
	CoveredLinkKeys  []string `json:"covered_link_keys"`
	MissingLinkKeys  []string `json:"missing_link_keys"`
	LinkCoveragePct  int      `json:"link_coverage_pct"`
}

type LiveAgentPlanAnchorStyleDimension struct {
	Key            string   `json:"key"`
	Group          string   `json:"group"`
	Label          string   `json:"label"`
	Level          string   `json:"level,omitempty"`
	Rule           string   `json:"rule,omitempty"`
	EvidenceQuotes []string `json:"evidence_quotes,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	PromotionLevel string   `json:"promotion_level,omitempty"`
}

type LiveAgentPlanAnchorStyleProfile struct {
	Delivery          *LiveAnchorDeliverySpec             `json:"delivery_spec,omitempty"`
	Summary           string                              `json:"summary,omitempty"`
	Dimensions        []LiveAgentPlanAnchorStyleDimension `json:"dimensions"`
	ReusableRules     []string                            `json:"reusable_rules"`
	CandidatePatterns []string                            `json:"candidate_patterns"`
	ExcludedFromStyle []string                            `json:"excluded_from_style"`
}

// Versioned, vendor-neutral output contract consumed by every generation path.
type LiveAnchorDeliverySpec struct {
	Version              string                   `json:"version"`
	Instructions         []string                 `json:"instructions"`
	Habits               []LiveAnchorLiteralHabit `json:"literal_habits"`
	Rulebook             string                   `json:"rulebook,omitempty"`
	SampleChars          int                      `json:"sample_chars"`
	SentenceCount        int                      `json:"sentence_count"`
	AverageSentenceChars int                      `json:"average_sentence_chars"`
	SourceSHA256         string                   `json:"source_sha256,omitempty"`
}

type LiveAnchorLiteralHabit struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Position string `json:"position"`
	When     string `json:"when"`
	Avoid    string `json:"avoid,omitempty"`
	Count    int    `json:"count"`
}

const LiveAnchorStyleOverlayVersion = "anchor-style-overlay/v1"

// LiveAnchorStyleOverlayRule is a vendor-neutral, code-validated rendering of
// one user-authored persona addition. The source sentence is never executed as
// a prompt; generation paths consume only this bounded rule.
type LiveAnchorStyleOverlayRule struct {
	Version                   string   `json:"version"`
	Category                  string   `json:"category"`
	Label                     string   `json:"label"`
	Application               string   `json:"application"`
	Strength                  int      `json:"strength"`
	MainlineInstruction       string   `json:"mainline_instruction"`
	InteractionInstruction    string   `json:"interaction_instruction"`
	SeriousInstruction        string   `json:"serious_instruction"`
	MainlineMinPer1000Chars   int      `json:"mainline_min_per_1000_chars,omitempty"`
	MainlineMaxPer1000Chars   int      `json:"mainline_max_per_1000_chars,omitempty"`
	InteractionMaxOccurrences int      `json:"interaction_max_occurrences,omitempty"`
	MicroActions              []string `json:"micro_actions,omitempty"`
	Avoid                     []string `json:"avoid"`
	Confidence                int      `json:"confidence"`
}

type LiveAnchorStyleOverlayItem struct {
	ID                   string                        `json:"id"`
	SourceText           string                        `json:"source_text"`
	ExplanationText      string                        `json:"explanation_text,omitempty"`
	Enabled              bool                          `json:"enabled"`
	Rule                 LiveAnchorStyleOverlayRule    `json:"rule"`
	InterpretationSource string                        `json:"interpretation_source,omitempty"`
	LearningBasis        string                        `json:"learning_basis,omitempty"`
	EvidenceQuotes       []string                      `json:"evidence_quotes,omitempty"`
	Plugin               *LiveAnchorStylePluginBinding `json:"plugin,omitempty"`
}

type LiveAnchorStylePluginBinding struct {
	InstanceID    string         `json:"instance_id"`
	PluginID      string         `json:"plugin_id"`
	PluginVersion string         `json:"plugin_version"`
	Parameters    map[string]any `json:"parameters,omitempty"`
}

type LiveAgentPlanStyleOverlayProfile struct {
	TenantID        int64                        `json:"tenant_id"`
	PlanID          int64                        `json:"plan_id"`
	Items           []LiveAnchorStyleOverlayItem `json:"items"`
	Revision        int64                        `json:"revision"`
	UpdatedByUserID *int64                       `json:"updated_by_user_id,omitempty"`
	UpdatedAt       *time.Time                   `json:"updated_at,omitempty"`
}

type LiveAgentPlanRhythmNode struct {
	Order           int      `json:"order"`
	Title           string   `json:"title"`
	Goal            string   `json:"goal,omitempty"`
	FactKeys        []string `json:"fact_keys,omitempty"`
	MustCover       []string `json:"must_cover,omitempty"`
	Avoid           []string `json:"avoid,omitempty"`
	ExecutionMode   string   `json:"execution_mode"`
	FixedText       string   `json:"fixed_text,omitempty"`
	DurationSeconds int      `json:"duration_seconds,omitempty"`
	Transition      string   `json:"transition,omitempty"`
}

type LiveAgentPlanScriptReference struct {
	ID              int64     `json:"id"`
	TenantID        int64     `json:"tenant_id"`
	PlanID          int64     `json:"plan_id"`
	ReferenceKey    string    `json:"reference_key"`
	Title           string    `json:"title"`
	ContentText     string    `json:"content_text"`
	Goal            string    `json:"goal,omitempty"`
	Transition      string    `json:"transition,omitempty"`
	ExecutionMode   string    `json:"execution_mode"`
	SourceQuote     string    `json:"source_quote,omitempty"`
	SourceType      string    `json:"source_type"`
	SourceRef       string    `json:"source_ref,omitempty"`
	Status          string    `json:"status"`
	VersionNo       int64     `json:"version_no"`
	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateLiveAgentPlanScriptReferenceInput struct {
	TenantID      int64  `json:"tenant_id,omitempty"`
	ReferenceKey  string `json:"reference_key"`
	Title         string `json:"title"`
	ContentText   string `json:"content_text"`
	Goal          string `json:"goal,omitempty"`
	Transition    string `json:"transition,omitempty"`
	ExecutionMode string `json:"execution_mode,omitempty"`
	SourceQuote   string `json:"source_quote,omitempty"`
	SourceType    string `json:"source_type,omitempty"`
	SourceRef     string `json:"source_ref,omitempty"`
}

type UpdateLiveAgentPlanScriptReferenceInput struct {
	TenantID          int64  `json:"tenant_id,omitempty"`
	ExpectedVersionNo int64  `json:"expected_version_no,omitempty"`
	ReferenceKey      string `json:"reference_key"`
	Title             string `json:"title"`
	ContentText       string `json:"content_text"`
	Goal              string `json:"goal,omitempty"`
	Transition        string `json:"transition,omitempty"`
	ExecutionMode     string `json:"execution_mode,omitempty"`
}

type LiveAgentPlanScriptAnalysis struct {
	Summary      string                              `json:"summary,omitempty"`
	ProductLinks []LiveAgentPlanProductLinkCandidate `json:"product_links"`
	Facts        []LiveAgentPlanFactCandidate        `json:"facts"`
	RhythmNodes  []LiveAgentPlanRhythmNode           `json:"rhythm_nodes"`
	AnchorStyle  LiveAgentPlanAnchorStyleProfile     `json:"anchor_style"`
	Completeness LiveAgentPlanAnalysisCompleteness   `json:"completeness"`
}

type LiveAgentPlanFact struct {
	ID                 int64     `json:"id"`
	TenantID           int64     `json:"tenant_id"`
	PlanID             int64     `json:"plan_id"`
	Category           string    `json:"category"`
	Key                string    `json:"key"`
	Value              string    `json:"value"`
	ForbiddenWording   string    `json:"forbidden_wording,omitempty"`
	SafeRewrite        string    `json:"safe_rewrite,omitempty"`
	SourceQuote        string    `json:"source_quote,omitempty"`
	SourceReviewBucket string    `json:"source_review_bucket,omitempty"`
	SourceReviewReason string    `json:"source_review_reason,omitempty"`
	SourceType         string    `json:"source_type"`
	SourceRef          string    `json:"source_ref,omitempty"`
	Status             string    `json:"status"`
	VersionNo          int64     `json:"version_no"`
	CreatedByUserID    *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID    *int64    `json:"updated_by_user_id,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type LiveAgentPlanBenefit struct {
	ID                 int64      `json:"id"`
	TenantID           int64      `json:"tenant_id"`
	PlanID             int64      `json:"plan_id"`
	Key                string     `json:"key"`
	LinkKey            string     `json:"link_key,omitempty"`
	ProductName        string     `json:"product_name,omitempty"`
	ActivityPrice      string     `json:"activity_price,omitempty"`
	Gift               string     `json:"gift,omitempty"`
	Activity           string     `json:"activity,omitempty"`
	StartsAt           *time.Time `json:"starts_at,omitempty"`
	EndsAt             *time.Time `json:"ends_at,omitempty"`
	SourceQuote        string     `json:"source_quote,omitempty"`
	SourceReviewBucket string     `json:"source_review_bucket,omitempty"`
	SourceReviewReason string     `json:"source_review_reason,omitempty"`
	SourceType         string     `json:"source_type"`
	SourceRef          string     `json:"source_ref,omitempty"`
	Status             string     `json:"status"`
	VersionNo          int64      `json:"version_no"`
	CreatedByUserID    *int64     `json:"created_by_user_id,omitempty"`
	UpdatedByUserID    *int64     `json:"updated_by_user_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type AdoptLiveAgentPlanBenefitsInput struct {
	TenantID  int64                           `json:"tenant_id,omitempty"`
	SourceRef string                          `json:"source_ref,omitempty"`
	Benefits  []LiveAgentPlanBenefitCandidate `json:"benefits"`
}

type UpdateLiveAgentPlanBenefitInput struct {
	TenantID          int64  `json:"tenant_id,omitempty"`
	ExpectedVersionNo int64  `json:"expected_version_no,omitempty"`
	Key               string `json:"key"`
	LinkKey           string `json:"link_key,omitempty"`
	ProductName       string `json:"product_name,omitempty"`
	ActivityPrice     string `json:"activity_price,omitempty"`
	Gift              string `json:"gift,omitempty"`
	Activity          string `json:"activity,omitempty"`
	StartsAt          string `json:"starts_at,omitempty"`
	EndsAt            string `json:"ends_at,omitempty"`
}

type LiveAgentPlanBenefitAdoptionResult struct {
	Candidate LiveAgentPlanBenefitCandidate `json:"candidate"`
	Status    string                        `json:"status"`
	Message   string                        `json:"message,omitempty"`
	Saved     *LiveAgentPlanBenefit         `json:"saved,omitempty"`
	Existing  *LiveAgentPlanBenefit         `json:"existing,omitempty"`
}

type AdoptLiveAgentPlanBenefitsOutput struct {
	Results   []LiveAgentPlanBenefitAdoptionResult `json:"results"`
	Adopted   int                                  `json:"adopted"`
	Drafted   int                                  `json:"drafted"`
	Skipped   int                                  `json:"skipped"`
	Blocked   int                                  `json:"blocked"`
	Conflicts int                                  `json:"conflicts"`
}

type AdoptLiveAgentPlanFactsInput struct {
	TenantID  int64                        `json:"tenant_id,omitempty"`
	SourceRef string                       `json:"source_ref,omitempty"`
	Facts     []LiveAgentPlanFactCandidate `json:"facts"`
}

type UpdateLiveAgentPlanFactInput struct {
	TenantID          int64  `json:"tenant_id,omitempty"`
	ExpectedVersionNo int64  `json:"expected_version_no,omitempty"`
	Category          string `json:"category"`
	Key               string `json:"key"`
	Value             string `json:"value"`
	ForbiddenWording  string `json:"forbidden_wording,omitempty"`
	SafeRewrite       string `json:"safe_rewrite,omitempty"`
}

type LiveAgentPlanFactAdoptionResult struct {
	Candidate LiveAgentPlanFactCandidate `json:"candidate"`
	Status    string                     `json:"status"`
	Message   string                     `json:"message,omitempty"`
	Saved     *LiveAgentPlanFact         `json:"saved,omitempty"`
	Existing  *LiveAgentPlanFact         `json:"existing,omitempty"`
}

type AdoptLiveAgentPlanFactsOutput struct {
	Results   []LiveAgentPlanFactAdoptionResult `json:"results"`
	Adopted   int                               `json:"adopted"`
	Skipped   int                               `json:"skipped"`
	Blocked   int                               `json:"blocked"`
	Conflicts int                               `json:"conflicts"`
}

type LiveAgentPlanScript struct {
	ID              int64                       `json:"id"`
	TenantID        int64                       `json:"tenant_id"`
	PlanID          int64                       `json:"plan_id"`
	Title           string                      `json:"title"`
	SourceType      string                      `json:"source_type"`
	SourceAssetID   *int64                      `json:"source_asset_id,omitempty"`
	OriginalName    string                      `json:"original_name,omitempty"`
	RawText         string                      `json:"raw_text"`
	ReadableText    string                      `json:"readable_text"`
	AnalysisStatus  string                      `json:"analysis_status"`
	Analysis        LiveAgentPlanScriptAnalysis `json:"analysis"`
	ModelProvider   string                      `json:"model_provider,omitempty"`
	ModelName       string                      `json:"model_name,omitempty"`
	LatencyMS       int64                       `json:"latency_ms,omitempty"`
	AnalyzedAt      *time.Time                  `json:"analyzed_at,omitempty"`
	Status          string                      `json:"status"`
	CreatedByUserID *int64                      `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64                      `json:"updated_by_user_id,omitempty"`
	CreatedAt       time.Time                   `json:"created_at"`
	UpdatedAt       time.Time                   `json:"updated_at"`
}

type SaveLiveAgentPlanScriptInput struct {
	TenantID      int64  `json:"tenant_id,omitempty"`
	Title         string `json:"title,omitempty"`
	SourceType    string `json:"source_type,omitempty"`
	SourceAssetID *int64 `json:"source_asset_id,omitempty"`
	OriginalName  string `json:"original_name,omitempty"`
	RawText       string `json:"raw_text"`
	ReadableText  string `json:"readable_text"`
}

type LiveAgentFullShowPreviewInput struct {
	TenantID         int64                               `json:"tenant_id,omitempty"`
	RoomID           int64                               `json:"room_id,omitempty"`
	DurationMinutes  int                                 `json:"duration_minutes"`
	RoundMinutes     int                                 `json:"round_minutes"`
	VariantCount     int                                 `json:"variant_count"`
	ExpansionFreedom *int                                `json:"expansion_freedom,omitempty"`
	UseAnchorStyle   bool                                `json:"use_anchor_style"`
	UseDynamicFacts  bool                                `json:"use_dynamic_facts"`
	GenerateTTSHints bool                                `json:"generate_tts_hints"`
	AvoidRecent      bool                                `json:"avoid_recent"`
	ProductLinks     []LiveAgentPlanProductLinkCandidate `json:"product_links"`
	RhythmNodes      []LiveAgentPlanRhythmNode           `json:"rhythm_nodes"`
	AnchorStyle      LiveAgentPlanAnchorStyleProfile     `json:"anchor_style"`
	RecentTexts      []string                            `json:"recent_texts,omitempty"`
}

const LiveFactExpansionPolicyVersion = "live-fact-expansion/v1"

// LiveFactExpansionPolicy is a user authorization boundary, not a model
// temperature. Law/platform policy and explicitly locked source facts always
// take precedence; the remaining semantic space may be expanded up to Freedom.
type LiveFactExpansionPolicy struct {
	Version              string   `json:"version"`
	UserAuthorized       bool     `json:"user_authorized"`
	Freedom              int      `json:"freedom"`
	Level                string   `json:"level"`
	Allowed              []string `json:"allowed"`
	AlwaysLocked         []string `json:"always_locked"`
	BoundaryRewriteFirst bool     `json:"boundary_rewrite_first"`
}

type LiveAgentFullShowContextFact struct {
	SourceID         int64  `json:"source_id,omitempty"`
	Category         string `json:"category"`
	Key              string `json:"key"`
	Value            string `json:"value"`
	ForbiddenWording string `json:"forbidden_wording,omitempty"`
	SafeRewrite      string `json:"safe_rewrite,omitempty"`
	Version          int64  `json:"version"`
}

const LiveGenerationFactManifestVersion = "live-generation-facts/v1"

// LiveAgentGenerationFact is the common fact contract consumed by speech
// generation. Product cards, active benefits and supplemental evidence remain
// separate domain records, but are projected into this single read model so a
// renderer never has to guess whether a value is an authorized fact.
type LiveAgentGenerationFact struct {
	FactID           string     `json:"fact_id"`
	SourceKind       string     `json:"source_kind"`
	SourceID         int64      `json:"source_id,omitempty"`
	SourceKey        string     `json:"source_key"`
	ScopeKind        string     `json:"scope_kind"`
	LinkKey          string     `json:"link_key,omitempty"`
	ProductName      string     `json:"product_name,omitempty"`
	Predicate        string     `json:"predicate"`
	Label            string     `json:"label"`
	Value            string     `json:"value"`
	ValidFrom        *time.Time `json:"valid_from,omitempty"`
	ValidUntil       *time.Time `json:"valid_until,omitempty"`
	ForbiddenWording string     `json:"forbidden_wording,omitempty"`
	SafeRewrite      string     `json:"safe_rewrite,omitempty"`
	Status           string     `json:"status"`
	Version          int64      `json:"version"`
	CanGenerate      bool       `json:"can_generate"`
}

type LiveAgentFullShowContextScriptReference struct {
	ReferenceKey  string `json:"reference_key"`
	Title         string `json:"title"`
	ContentText   string `json:"content_text"`
	Goal          string `json:"goal,omitempty"`
	Transition    string `json:"transition,omitempty"`
	ExecutionMode string `json:"execution_mode"`
	Version       int64  `json:"version"`
}

const LiveSpeechExpansionVersion = "live-speech-expansion/v1"

// LiveSpeechExpansionRoomState is an internal driver input. In fixed preview it
// is simulated; in a live room the same fields are filled from Core. These
// values are never audience-facing facts and must not be spoken as claims.
type LiveSpeechExpansionRoomState struct {
	Scenario           string  `json:"scenario"`
	Heat               string  `json:"heat"`
	OnlineCount        int     `json:"online_count"`
	EntriesPerMinute   int     `json:"entries_per_minute"`
	ChatsPerMinute     int     `json:"chats_per_minute"`
	QuestionPressure   float64 `json:"question_pressure"`
	AudienceTurnover5m float64 `json:"audience_turnover_5m"`
	ConversionSignal   float64 `json:"conversion_signal"`
}

// LiveSpeechExpansionStep is one virtual-clock unit. It describes discourse
// work, not new business content. FactKeys, BenefitKeys and LinkKeys can only
// point at already-authorized data in the generation context.
type LiveSpeechExpansionStep struct {
	Index                  int                          `json:"index"`
	StartSecond            int                          `json:"start_second"`
	EndSecond              int                          `json:"end_second"`
	CycleIndex             int                          `json:"cycle_index"`
	Stage                  string                       `json:"stage"`
	Goal                   string                       `json:"goal"`
	TargetChars            int                          `json:"target_chars"`
	ExpressionMoves        []string                     `json:"expression_moves"`
	FactKeys               []string                     `json:"fact_keys,omitempty"`
	BenefitKeys            []string                     `json:"benefit_keys,omitempty"`
	LinkKeys               []string                     `json:"link_keys,omitempty"`
	StyleCapabilities      []string                     `json:"style_capabilities,omitempty"`
	InteractionOpportunity bool                         `json:"interaction_opportunity"`
	Room                   LiveSpeechExpansionRoomState `json:"room"`
}

type LiveSpeechExpansionPlan struct {
	Version         string                    `json:"version"`
	Mode            string                    `json:"mode"`
	VariantKey      string                    `json:"variant_key"`
	DurationSeconds int                       `json:"duration_seconds"`
	TargetChars     int                       `json:"target_chars"`
	Steps           []LiveSpeechExpansionStep `json:"steps"`
}

type LiveAgentFullShowGenerationContext struct {
	PlanID              int64                                     `json:"plan_id"`
	PlanName            string                                    `json:"plan_name"`
	PlanDescription     string                                    `json:"plan_description,omitempty"`
	RoomID              int64                                     `json:"room_id,omitempty"`
	IndustryCode        string                                    `json:"industry_code,omitempty"`
	PolicyRuleCount     int                                       `json:"policy_rule_count"`
	FormalFacts         []LiveAgentFullShowContextFact            `json:"formal_facts"`
	Benefits            []LiveAgentPlanBenefit                    `json:"benefits"`
	ProductLinks        []LiveAgentPlanProductLink                `json:"product_links"`
	FactManifestVersion string                                    `json:"fact_manifest_version"`
	AuthorizedFacts     []LiveAgentGenerationFact                 `json:"authorized_facts"`
	ScriptReferences    []LiveAgentFullShowContextScriptReference `json:"script_references"`
	RhythmNodes         []LiveAgentPlanRhythmNode                 `json:"rhythm_nodes"`
	AnchorStyle         LiveAgentPlanAnchorStyleProfile           `json:"anchor_style"`
	StyleOverlayPrompt  string                                    `json:"style_overlay_prompt,omitempty"`
	StyleOverlayCount   int                                       `json:"style_overlay_count"`
	FactExpansion       LiveFactExpansionPolicy                   `json:"fact_expansion"`
	ExpansionMode       string                                    `json:"expansion_mode,omitempty"`
	ExpansionPlans      []LiveSpeechExpansionPlan                 `json:"expansion_plans,omitempty"`
	DurationMinutes     int                                       `json:"duration_minutes"`
	RoundMinutes        int                                       `json:"round_minutes"`
	RoundCount          int                                       `json:"round_count"`
	VariantCount        int                                       `json:"variant_count"`
	UseAnchorStyle      bool                                      `json:"use_anchor_style"`
	UseDynamicFacts     bool                                      `json:"use_dynamic_facts"`
	GenerateTTSHints    bool                                      `json:"generate_tts_hints"`
	AvoidRecent         bool                                      `json:"avoid_recent"`
	DraftProductSource  bool                                      `json:"draft_product_source"`
	DraftRhythmSource   bool                                      `json:"draft_rhythm_source"`
	DraftStyleSource    bool                                      `json:"draft_style_source"`
}

type LiveAgentFullShowTTSHint struct {
	Segment     string  `json:"segment"`
	Instruction string  `json:"instruction"`
	Rate        float64 `json:"rate,omitempty"`
}

type LiveAgentFullShowAuditIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type LiveAgentFullShowAudit struct {
	Passed          bool                          `json:"passed"`
	Issues          []LiveAgentFullShowAuditIssue `json:"issues"`
	FactCoveragePct int                           `json:"fact_coverage_pct"`
	LinkCoveragePct int                           `json:"link_coverage_pct"`
	SimilarityPct   int                           `json:"similarity_pct"`
}

type LiveAgentFullShowVariant struct {
	Index            int                        `json:"index"`
	VariantKey       string                     `json:"variant_key"`
	Title            string                     `json:"title"`
	OpeningAngle     string                     `json:"opening_angle"`
	Text             string                     `json:"text"`
	EstimatedMinutes int                        `json:"estimated_minutes"`
	CoveredFactKeys  []string                   `json:"covered_fact_keys"`
	CoveredLinkKeys  []string                   `json:"covered_link_keys"`
	TTSHints         []LiveAgentFullShowTTSHint `json:"tts_hints,omitempty"`
	Audit            LiveAgentFullShowAudit     `json:"audit"`
}

type LiveAgentFullShowPreviewResponse struct {
	Context   LiveAgentFullShowGenerationContext `json:"context"`
	Variants  []LiveAgentFullShowVariant         `json:"variants"`
	Provider  string                             `json:"provider,omitempty"`
	Model     string                             `json:"model,omitempty"`
	LatencyMS int64                              `json:"latency_ms,omitempty"`
	Persisted bool                               `json:"persisted"`
	Preview   bool                               `json:"preview"`
}

type LiveAgentVoiceIdentity struct {
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	Source         string         `json:"source"`
	Provider       string         `json:"provider"`
	VoiceID        string         `json:"voice_id"`
	ProfileID      int64          `json:"profile_id,omitempty"`
	BindingID      int64          `json:"binding_id,omitempty"`
	Model          string         `json:"model"`
	Rate           float64        `json:"rate,omitempty"`
	EmotionEnabled *bool          `json:"emotion_enabled,omitempty"`
	Emotion        string         `json:"emotion,omitempty"`
	Style          map[string]any `json:"style,omitempty"`
}

type LiveAgentPlanTimelineSegment struct {
	SegmentID string `json:"segment_id"`
	Index     int    `json:"index"`
	StartMS   int64  `json:"start_ms"`
	EndMS     int64  `json:"end_ms"`
	Text      string `json:"text"`
	SafeCut   bool   `json:"safe_cut"`
}

type LiveAgentPlanSafePoint struct {
	ID          string   `json:"id"`
	CutMS       int64    `json:"cut_ms"`
	Score       int      `json:"score"`
	Grade       string   `json:"grade"`
	Kind        string   `json:"kind"`
	SentenceID  string   `json:"sentence_id"`
	LeftPreview string   `json:"left_preview"`
	NextPreview string   `json:"next_preview"`
	Topics      []string `json:"topics,omitempty"`
}

type LiveAgentPlanVersionVariant struct {
	Index            int                            `json:"index,omitempty"`
	VariantKey       string                         `json:"variant_key"`
	IsFormal         bool                           `json:"is_formal"`
	Title            string                         `json:"title,omitempty"`
	OpeningAngle     string                         `json:"opening_angle,omitempty"`
	Text             string                         `json:"text"`
	EstimatedMinutes int                            `json:"estimated_minutes,omitempty"`
	CoveredFactKeys  []string                       `json:"covered_fact_keys,omitempty"`
	CoveredLinkKeys  []string                       `json:"covered_link_keys,omitempty"`
	TTSHints         []LiveAgentFullShowTTSHint     `json:"tts_hints,omitempty"`
	Audit            LiveAgentFullShowAudit         `json:"audit"`
	AudioURL         string                         `json:"audio_url"`
	AudioAssetID     int64                          `json:"audio_asset_id,omitempty"`
	AudioDurationMS  int64                          `json:"audio_duration_ms,omitempty"`
	Timeline         []LiveAgentPlanTimelineSegment `json:"timeline,omitempty"`
	SRT              string                         `json:"srt,omitempty"`
	SafePoints       []LiveAgentPlanSafePoint       `json:"safe_points,omitempty"`
	AssetManifest    map[string]any                 `json:"asset_manifest,omitempty"`
	GenerationNo     int                            `json:"generation_no,omitempty"`
	VoiceIdentityKey string                         `json:"voice_identity_key"`
}

type CreateLiveAgentPlanVersionInput struct {
	TenantID          int64                         `json:"tenant_id,omitempty"`
	RoomID            int64                         `json:"room_id"`
	DurationMinutes   int                           `json:"duration_minutes"`
	RoundMinutes      int                           `json:"round_minutes"`
	VoiceIdentity     LiveAgentVoiceIdentity        `json:"voice_identity"`
	Variants          []LiveAgentPlanVersionVariant `json:"variants"`
	GenerationContext map[string]any                `json:"generation_context,omitempty"`
}

type PublishLiveAgentPlanVersionInput struct {
	TenantID int64 `json:"tenant_id,omitempty"`
	RoomID   int64 `json:"room_id"`
}

type LiveAgentPlanVersion struct {
	ID                int64                         `json:"id"`
	TenantID          int64                         `json:"tenant_id"`
	PlanID            int64                         `json:"plan_id"`
	RoomID            int64                         `json:"room_id"`
	VersionNo         int64                         `json:"version_no"`
	LifecycleStatus   string                        `json:"lifecycle_status"`
	DurationMinutes   int                           `json:"duration_minutes"`
	RoundMinutes      int                           `json:"round_minutes"`
	VoiceIdentity     LiveAgentVoiceIdentity        `json:"voice_identity"`
	Variants          []LiveAgentPlanVersionVariant `json:"variants"`
	GenerationContext map[string]any                `json:"generation_context"`
	CreatedByUserID   *int64                        `json:"created_by_user_id,omitempty"`
	PublishedByUserID *int64                        `json:"published_by_user_id,omitempty"`
	PublishedAt       *time.Time                    `json:"published_at,omitempty"`
	CreatedAt         time.Time                     `json:"created_at"`
	UpdatedAt         time.Time                     `json:"updated_at"`
}
