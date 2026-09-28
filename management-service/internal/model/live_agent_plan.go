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
	Category     string `json:"category"`
	Key          string `json:"key"`
	Value        string `json:"value"`
	Status       string `json:"status"`
	ReviewBucket string `json:"review_bucket,omitempty"`
	ReviewReason string `json:"review_reason,omitempty"`
	SourceQuote  string `json:"source_quote,omitempty"`
	Confidence   string `json:"confidence,omitempty"`
	Note         string `json:"note,omitempty"`
}

type LiveAgentPlanProductLinkCandidate struct {
	LinkKey       string   `json:"link_key"`
	ProductName   string   `json:"product_name,omitempty"`
	Spec          string   `json:"spec,omitempty"`
	DailyPrice    string   `json:"daily_price,omitempty"`
	ActivityPrice string   `json:"activity_price,omitempty"`
	Quantity      string   `json:"quantity,omitempty"`
	Gift          string   `json:"gift,omitempty"`
	Activity      string   `json:"activity,omitempty"`
	Audience      string   `json:"audience,omitempty"`
	ReviewBucket  string   `json:"review_bucket,omitempty"`
	ReviewReason  string   `json:"review_reason,omitempty"`
	SourceQuotes  []string `json:"source_quotes,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
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
	ID                 int64     `json:"id"`
	TenantID           int64     `json:"tenant_id"`
	PlanID             int64     `json:"plan_id"`
	LinkKey            string    `json:"link_key"`
	ProductName        string    `json:"product_name,omitempty"`
	Spec               string    `json:"spec,omitempty"`
	DailyPrice         string    `json:"daily_price,omitempty"`
	Quantity           string    `json:"quantity,omitempty"`
	Audience           string    `json:"audience,omitempty"`
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
	Summary           string                              `json:"summary,omitempty"`
	Dimensions        []LiveAgentPlanAnchorStyleDimension `json:"dimensions"`
	ReusableRules     []string                            `json:"reusable_rules"`
	CandidatePatterns []string                            `json:"candidate_patterns"`
	ExcludedFromStyle []string                            `json:"excluded_from_style"`
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
	TenantID int64  `json:"tenant_id,omitempty"`
	Category string `json:"category"`
	Key      string `json:"key"`
	Value    string `json:"value"`
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
	UseAnchorStyle   bool                                `json:"use_anchor_style"`
	UseDynamicFacts  bool                                `json:"use_dynamic_facts"`
	GenerateTTSHints bool                                `json:"generate_tts_hints"`
	AvoidRecent      bool                                `json:"avoid_recent"`
	ProductLinks     []LiveAgentPlanProductLinkCandidate `json:"product_links"`
	RhythmNodes      []LiveAgentPlanRhythmNode           `json:"rhythm_nodes"`
	AnchorStyle      LiveAgentPlanAnchorStyleProfile     `json:"anchor_style"`
	RecentTexts      []string                            `json:"recent_texts,omitempty"`
}

type LiveAgentFullShowContextFact struct {
	Category string `json:"category"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Version  int64  `json:"version"`
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

type LiveAgentFullShowGenerationContext struct {
	PlanID             int64                                     `json:"plan_id"`
	PlanName           string                                    `json:"plan_name"`
	PlanDescription    string                                    `json:"plan_description,omitempty"`
	RoomID             int64                                     `json:"room_id,omitempty"`
	IndustryCode       string                                    `json:"industry_code,omitempty"`
	PolicyRuleCount    int                                       `json:"policy_rule_count"`
	FormalFacts        []LiveAgentFullShowContextFact            `json:"formal_facts"`
	Benefits           []LiveAgentPlanBenefit                    `json:"benefits"`
	ProductLinks       []LiveAgentPlanProductLink                `json:"product_links"`
	ScriptReferences   []LiveAgentFullShowContextScriptReference `json:"script_references"`
	RhythmNodes        []LiveAgentPlanRhythmNode                 `json:"rhythm_nodes"`
	AnchorStyle        LiveAgentPlanAnchorStyleProfile           `json:"anchor_style"`
	DurationMinutes    int                                       `json:"duration_minutes"`
	RoundMinutes       int                                       `json:"round_minutes"`
	RoundCount         int                                       `json:"round_count"`
	VariantCount       int                                       `json:"variant_count"`
	UseAnchorStyle     bool                                      `json:"use_anchor_style"`
	UseDynamicFacts    bool                                      `json:"use_dynamic_facts"`
	GenerateTTSHints   bool                                      `json:"generate_tts_hints"`
	AvoidRecent        bool                                      `json:"avoid_recent"`
	DraftProductSource bool                                      `json:"draft_product_source"`
	DraftRhythmSource  bool                                      `json:"draft_rhythm_source"`
	DraftStyleSource   bool                                      `json:"draft_style_source"`
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
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	Source    string         `json:"source"`
	Provider  string         `json:"provider"`
	VoiceID   string         `json:"voice_id"`
	ProfileID int64          `json:"profile_id,omitempty"`
	Model     string         `json:"model"`
	Rate      float64        `json:"rate,omitempty"`
	Emotion   string         `json:"emotion,omitempty"`
	Style     map[string]any `json:"style,omitempty"`
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
