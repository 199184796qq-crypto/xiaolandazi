package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"livecompanion/management/internal/model"
)

const (
	sharedSourceReview   = "共享原句已更新，请核对这个属性的值和依据"
	sharedSourceConflict = "共享原句有多个历史版本，请在事实清单中编辑确认"
)

func SameMaterialSource(a, b *model.MaterialSourceReference) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID && slices.Equal(a.Ranges, b.Ranges)
}

func materialSentenceKey(claim model.PlanMaterialClaim) string {
	if claim.SharedSourceKey != "" {
		return claim.SharedSourceKey
	}
	origin := claim.InitialSourceRef
	if origin == nil {
		origin = claim.SourceRef
	}
	if origin != nil && len(origin.Ranges) > 0 {
		span := origin.Ranges[0]
		// Byte range distinguishes identical lines at different positions in a document.
		return fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "source:%s:%d:%d", origin.ID, span.Start, span.End)))
	}
	if sentence := strings.TrimSpace(claim.SourceSentence); sentence != "" {
		// Without an imported range, identical generic sentences in separate
		// objects are not sufficient evidence that they are one source.
		targets := make([]string, 0, len(claim.ObjectTargets))
		for _, target := range claim.ObjectTargets {
			targets = append(targets, fmt.Sprintf("%s/%d/%d/%s", target.Kind, target.ObjectID, target.LinkID, target.StoreName))
		}
		slices.Sort(targets)
		identity, _ := json.Marshal([]any{sentence, claim.ScopeKind, claim.ProductLinkID, claim.StoreName, claim.ScopeName, targets})
		return fmt.Sprintf("%x", sha256.Sum256(append([]byte("legacy:"), identity...)))
	}
	return ""
}

type sharedMaterialSentence struct {
	key  string
	text string
	ref  *model.MaterialSourceReference
}

func fullSharedSource(claim model.PlanMaterialClaim) bool {
	ref := claim.SourceRef
	return ref != nil && len(ref.Ranges) == 1 &&
		ref.Ranges[0] == (model.MaterialSourceRange{Start: 0, End: len(claim.SourceSentence)})
}

// Previously edited rows kept the imported range as InitialSourceRef. Use an
// unambiguous revision for siblings still pointing at that range, including
// newly imported siblings. Conflicting historical corrections require review.
func projectSharedMaterialSentences(claims []model.PlanMaterialClaim) {
	type correction struct {
		sentence  sharedMaterialSentence
		persisted bool
		conflict  bool
	}
	corrections := map[string]correction{}
	for _, claim := range claims {
		persisted := claim.SharedSourceKey != ""
		if !fullSharedSource(claim) || (!persisted && (claim.InitialSourceRef == nil || SameMaterialSource(claim.InitialSourceRef, claim.SourceRef))) {
			continue
		}
		key := materialSentenceKey(claim)
		current, exists := corrections[key]
		if !exists || (persisted && !current.persisted) {
			corrections[key] = correction{sentence: sharedMaterialSentence{key, claim.SourceSentence, claim.SourceRef}, persisted: persisted}
		} else if persisted == current.persisted && current.sentence.text != claim.SourceSentence {
			current.conflict = true
			corrections[key] = current
		}
	}
	if len(corrections) == 0 {
		return
	}
	for i := range claims {
		claim := &claims[i]
		revision, ok := corrections[materialSentenceKey(*claim)]
		if !ok {
			continue
		}
		if revision.conflict {
			claim.SharedSourceNeedsReview = true
			claim.NeedsConfirmation = true
			if claim.SourceIssue == "" {
				claim.SourceIssue = sharedSourceConflict
			}
		} else if !SameMaterialSource(claim.SourceRef, revision.sentence.ref) || claim.SourceSentence != revision.sentence.text {
			alignMaterialSentence(claim, revision.sentence)
		}
	}
}

func sentenceClauses(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return strings.ContainsRune("，,。！？；;!?\n\r", r)
	})
}

func unaffectedMaterialEvidence(claim model.PlanMaterialClaim, sentence string) (string, bool) {
	var shared []string
	before := sentenceClauses(claim.SourceSentence)
	for _, piece := range sentenceClauses(sentence) {
		piece = strings.TrimSpace(piece)
		if piece != "" && slices.ContainsFunc(before, func(old string) bool { return strings.TrimSpace(old) == piece }) {
			shared = append(shared, piece)
		}
	}
	// Preserve the value only if its own supporting clause survived unchanged.
	// A substring alone would incorrectly bless changes such as 包邮 to 不包邮.
	support := func(text string) bool {
		normalized := strings.Join(strings.Fields(text), "")
		return normalized != "" && slices.ContainsFunc(shared, func(part string) bool {
			return strings.Contains(strings.Join(strings.Fields(part), ""), normalized)
		})
	}
	quote := strings.TrimSpace(claim.SourceQuote)
	value, content := strings.TrimSpace(claim.Value), strings.TrimSpace(claim.Content)
	if !support(quote) {
		quote = content
		if !support(quote) {
			quote = value
		}
	}
	if !support(quote) || (value != "" && !support(value)) || (content != "" && !support(content)) ||
		(claim.Answer != "" && !support(claim.Answer)) {
		return sentence, false
	}
	for _, raw := range []json.RawMessage{claim.Conditions, claim.Exceptions} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var predicates []struct {
			Raw string `json:"raw"`
		}
		if json.Unmarshal(raw, &predicates) != nil {
			return sentence, false
		}
		for _, predicate := range predicates {
			if !support(predicate.Raw) {
				return sentence, false
			}
		}
	}
	return quote, true
}

func alignMaterialSentence(claim *model.PlanMaterialClaim, next sharedMaterialSentence) {
	if claim.InitialSourceRef == nil && !SameMaterialSource(claim.SourceRef, next.ref) {
		claim.InitialSourceRef = claim.SourceRef
	}
	if claim.SourceSentence != next.text {
		quote, supported := unaffectedMaterialEvidence(*claim, next.text)
		claim.SourceQuote = quote
		claim.SourceContext = nil
		if !supported {
			claim.SharedSourceNeedsReview = true
			claim.NeedsConfirmation = true
			if claim.SourceIssue == "" || claim.SourceIssue == sharedSourceConflict {
				claim.SourceIssue = sharedSourceReview
			}
		}
	}
	claim.SharedSourceKey = next.key
	claim.SourceSentence, claim.SourceRef = next.text, next.ref
}

// All affected claims point to the same immutable revision. The sentence
// columns are projections kept in the same transaction for existing readers.
func saveSharedMaterialSentence(ctx context.Context, tx *sql.Tx, previous model.PlanMaterialClaim, input *model.PlanMaterialClaimInput, claims []model.PlanMaterialClaim) error {
	if input.SourceRef == nil {
		return ErrMaterialClaimConflict
	}
	key := materialSentenceKey(previous)
	if key == "" {
		// A legacy row without any original sentence has no verifiable siblings.
		key = fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "claim:%d", previous.ID)))
	}
	next := sharedMaterialSentence{key, input.SourceSentence, input.SourceRef}
	if err := saveMaterialSource(ctx, tx, previous.TenantID, previous.PlanID, next.ref.ID, next.text); err != nil {
		return err
	}
	input.SharedSourceKey = key
	ClearSharedSourceReview(input)
	for _, claim := range claims {
		if claim.ID == previous.ID || materialSentenceKey(claim) != key {
			continue
		}
		alignMaterialSentence(&claim, next)
		details, err := json.Marshal(claim.MaterialClaimDetails)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_material_claims
			SET details_json=?,source_quote=?,source_sentence=?,updated_at=CURRENT_TIMESTAMP(3)
			WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`,
			string(details), claim.SourceQuote, claim.SourceSentence, claim.TenantID, claim.PlanID, claim.ID); err != nil {
			return err
		}
	}
	return nil
}

func ClearSharedSourceReview(input *model.PlanMaterialClaimInput) {
	if !input.SharedSourceNeedsReview {
		return
	}
	input.SharedSourceNeedsReview = false
	if input.SourceIssue == sharedSourceReview || input.SourceIssue == sharedSourceConflict {
		input.SourceIssue = ""
	}
	input.NeedsConfirmation = input.SourceIssue != "" || input.PolarityIssue != "" || input.ConditionIssue != "" || input.NeedsSplit
}

func RecheckSharedSourceReview(input *model.PlanMaterialClaimInput, previous model.PlanMaterialClaim) {
	if !previous.SharedSourceNeedsReview {
		return
	}
	input.SourceIssue = previous.SourceIssue
	input.NeedsConfirmation = true
	if previous.SourceIssue == sharedSourceConflict ||
		(input.Value == previous.Value && input.Content == previous.Content && input.Polarity == previous.Polarity) {
		return
	}
	revised := previous
	revised.Value, revised.Content, revised.SourceQuote = input.Value, input.Content, input.SourceQuote
	revised.Conditions, revised.Exceptions, revised.Answer = input.Conditions, input.Exceptions, input.Answer
	if quote, ok := unaffectedMaterialEvidence(revised, input.SourceSentence); ok {
		input.SourceQuote = quote
		ClearSharedSourceReview(input)
	}
}
