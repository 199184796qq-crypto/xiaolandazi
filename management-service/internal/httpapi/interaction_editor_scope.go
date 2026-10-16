package httpapi

import (
	"encoding/json"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

// A safe-rewrite asset preserves source record positions. Reuse that provenance
// only when the editor exactly equals its original input and the saved scope
// completely covers its safe output. No new classification or business writes.
func editorInteractionScopeForSafety(current string, saved *anchorcycle.ProductFocus, safety model.LiveDerivedSafetyAsset) *anchorcycle.ProductFocus {
	if saved == nil || saved.MaterialVersion != productCommonMaterialVersion || current != safety.OriginalFacts || saved.FactsSHA != derivedHash(safety.SafeFacts) || validateProductFactSnapshot(safety.SafeFacts, saved) != nil {
		return nil
	}
	original, safe := productFactBlocks(safety.OriginalFacts), productFactBlocks(safety.SafeFacts)
	if len(original) == 0 || len(original) != len(safe) {
		return nil
	}
	bySafe := map[string]anchorcycle.ScopedFact{}
	for _, fact := range saved.Facts {
		if _, duplicate := bySafe[fact.Text]; duplicate {
			return nil
		}
		bySafe[fact.Text] = fact
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return nil
	}
	var focus anchorcycle.ProductFocus
	if json.Unmarshal(raw, &focus) != nil {
		return nil
	}
	focus.Facts = nil
	for i, block := range original {
		fact, ok := bySafe[safe[i]]
		if !ok {
			return nil
		}
		fact.Text = block
		focus.Facts = append(focus.Facts, fact)
	}
	focus.FactsSHA = derivedHash(current)
	if validateProductFactSnapshot(current, &focus) != nil {
		return nil
	}
	return &focus
}
