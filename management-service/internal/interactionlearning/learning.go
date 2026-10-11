package interactionlearning

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

func ValidateReview(r model.InteractionLearningReview) error {
	if r.Kind != "semantic" && r.Kind != "missing_fact" {
		return fmt.Errorf("unknown review kind %q", r.Kind)
	}
	if strings.TrimSpace(r.Context) == "" || strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("review needs context and reason")
	}
	if utf8.RuneCountInString(r.Context) > 2000 || utf8.RuneCountInString(r.Meaning) > 500 || utf8.RuneCountInString(r.Reason) > 1000 || utf8.RuneCountInString(r.SuggestedRule) > 3000 || len(r.NeededFacts) > 12 {
		return fmt.Errorf("review fields too long")
	}
	for _, f := range r.NeededFacts {
		if strings.TrimSpace(f) == "" || utf8.RuneCountInString(f) > 500 {
			return fmt.Errorf("invalid needed fact")
		}
	}
	return nil
}

type Answer struct {
	Reply         string                           `json:"reply"`
	NeedsReview   *bool                            `json:"needs_review"`
	Review        *model.InteractionLearningReview `json:"review"`
	MetadataError string                           `json:"-"`
}

func DecodeAnswer(raw string) (Answer, error) {
	var a Answer
	var fields map[string]json.RawMessage
	d := json.NewDecoder(strings.NewReader(raw))
	if err := d.Decode(&fields); err != nil {
		return a, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return a, fmt.Errorf("answer has trailing data")
	}
	if err := json.Unmarshal(fields["reply"], &a.Reply); err != nil || strings.TrimSpace(a.Reply) == "" {
		return a, fmt.Errorf("answer missing valid reply")
	}
	// The reply transport must be valid. Advisory metadata cannot veto speech;
	// report its error separately so the caller can retain the original output.
	if err := decodeAnswerMetadata(fields, &a); err != nil {
		a.MetadataError = err.Error()
		a.Review = nil
	}
	return a, nil
}

func decodeAnswerMetadata(fields map[string]json.RawMessage, a *Answer) error {
	for key := range fields {
		if key != "reply" && key != "needs_review" && key != "review" {
			return fmt.Errorf("unknown answer field %q", key)
		}
	}
	if err := json.Unmarshal(fields["needs_review"], &a.NeedsReview); err != nil || a.NeedsReview == nil {
		return fmt.Errorf("answer missing valid needs_review")
	}
	if len(fields["review"]) == 0 {
		return fmt.Errorf("answer missing review field")
	}
	d := json.NewDecoder(strings.NewReader(string(fields["review"])))
	d.DisallowUnknownFields()
	if err := d.Decode(&a.Review); err != nil {
		return err
	}
	if *a.NeedsReview {
		if a.Review == nil {
			return fmt.Errorf("needs_review requires review")
		}
		if err := ValidateReview(*a.Review); err != nil {
			return err
		}
	} else if a.Review != nil {
		return fmt.Errorf("review must be null when needs_review is false")
	}
	return nil
}

// Exact questions are grouped only within the same tenant, room, plan,
// interpreted purpose and context. Negation, modality and numbers survive.
func PendingKey(o model.InteractionLearningObservation) string {
	plain := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("?？。!！", r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(o.Question))
	raw, _ := json.Marshal([]any{o.TenantID, o.RoomID, o.Review.PlanID, plain, o.Review.Kind, strings.TrimSpace(o.Review.Meaning), strings.TrimSpace(o.Review.Context)})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func MemoriesForPlan(items []model.AgentMemoryItem, planID int64) []model.AgentMemoryItem {
	shadowed := map[int64]bool{}
	for _, m := range items {
		if m.CurrentVersion == nil || m.Status == model.AgentMemoryStatusInactive {
			continue
		}
		if numericID(m.CurrentVersion.Structured["review_plan_id"]) == planID && planID > 0 {
			if id := numericID(m.CurrentVersion.Structured["review_overrides_memory_id"]); id > 0 {
				shadowed[id] = true
			}
		}
	}
	out := make([]model.AgentMemoryItem, 0, len(items))
	for _, m := range items {
		if shadowed[m.ID] {
			continue
		}
		if m.CurrentVersion != nil {
			var scoped int64
			switch v := m.CurrentVersion.Structured["review_plan_id"].(type) {
			case int64:
				scoped = v
			case float64:
				scoped = int64(v)
			case json.Number:
				scoped, _ = v.Int64()
			}
			if scoped > 0 && scoped != planID {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

func numericID(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	}
	return 0
}

// GenerationMemories requires an explicit room and plan binding for every
// memory type. Unbound legacy records stay available to history/repair tools;
// neither their product facts nor their style labels authorize live reuse.
func GenerationMemories(items []model.AgentMemoryItem, tenantID, roomID, planID int64) []model.AgentMemoryItem {
	out := make([]model.AgentMemoryItem, 0, len(items))
	if tenantID <= 0 || roomID <= 0 || planID <= 0 {
		return out
	}
	for _, item := range items {
		if item.TenantID != tenantID || item.RoomID != roomID || item.Status != model.AgentMemoryStatusActive || item.CurrentVersion == nil {
			continue
		}
		if numericID(item.CurrentVersion.Structured["review_plan_id"]) != planID {
			continue
		}
		out = append(out, item)
	}
	return MemoriesForPlan(out, planID)
}
