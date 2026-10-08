package model

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

type LiveDerivedSafetyAsset struct {
	ID                int64     `json:"id"`
	TenantID          int64     `json:"tenant_id"`
	PlanID            int64     `json:"plan_id"`
	SourceSHA256      string    `json:"source_sha256"`
	OriginalFacts     string    `json:"-"`
	OriginalWheelJSON string    `json:"-"`
	SafeFacts         string    `json:"-"`
	SafeWheelJSON     string    `json:"-"`
	PolicyHashesJSON  string    `json:"-"`
	ChangesJSON       string    `json:"-"`
	RewrittenCount    int       `json:"rewritten_count"`
	Notice            string    `json:"notice"`
	CreatedAt         time.Time `json:"created_at"`
}

type LiveRiskReview struct {
	// Recheck only the obsolete program override, never an actual model block.
	RecheckLegacyBoundaryBlock bool                   `json:"-"`
	ClaimSHA256                string                 `json:"claim_sha256"`
	SourceKey                  string                 `json:"source_key"`
	Text                       string                 `json:"text"`
	RuleID                     string                 `json:"rule_id"`
	Severity                   string                 `json:"severity"`
	RuleSHA256                 string                 `json:"rule_sha256"`
	Status                     string                 `json:"status"`
	Evidence                   string                 `json:"evidence"`
	Reason                     string                 `json:"reason"`
	ProgramDecision            string                 `json:"program_decision"`
	SemanticDecision           string                 `json:"semantic_decision"`
	SemanticPromptSHA          string                 `json:"semantic_prompt_sha256,omitempty"`
	SemanticReason             string                 `json:"semantic_reason,omitempty"`
	SemanticSuggestion         string                 `json:"semantic_suggestion,omitempty"`
	SemanticProvider           string                 `json:"semantic_provider,omitempty"`
	SemanticModel              string                 `json:"semantic_model,omitempty"`
	SemanticReviewedAt         *time.Time             `json:"semantic_reviewed_at,omitempty"`
	AppealStatus               string                 `json:"appeal_status,omitempty"`
	AppealReason               string                 `json:"appeal_reason,omitempty"`
	AppealBy                   *int64                 `json:"appeal_by_user_id,omitempty"`
	AppealAt                   *time.Time             `json:"appeal_at,omitempty"`
	ConfirmedBy                *int64                 `json:"confirmed_by_user_id,omitempty"`
	ConfirmedAt                *time.Time             `json:"confirmed_at,omitempty"`
	SemanticKey                string                 `json:"semantic_key,omitempty"`
	Subject                    string                 `json:"subject,omitempty"`
	Sources                    []LiveRiskReviewSource `json:"sources,omitempty"`
}

const GeneratedWheelAbsoluteBoundaryReason = "该排他性或绝对化说法由轮盘生成，但事实包没有提供同一完整事实；列出一项保障不等于它是唯一保障。"

type LiveRiskReviewSource struct {
	SourceKey string `json:"source_key"`
	Text      string `json:"text"`
	Area      string `json:"area,omitempty"`
	EntityID  int64  `json:"entity_id,omitempty"`
	Field     string `json:"field,omitempty"`
}

type LiveRiskSemanticDecision struct {
	ClaimSHA256 string `json:"claim_sha256"`
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	Suggestion  string `json:"suggested_text,omitempty"`
}

func NormalizeLiveRiskReviewText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func LiveRiskReviewClaimSHA256(text, ruleID, severity, ruleSHA256 string) string {
	identity := NormalizeLiveRiskReviewText(text) + "\x00" + ruleID + "\x00" + severity + "\x00" + ruleSHA256
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}
