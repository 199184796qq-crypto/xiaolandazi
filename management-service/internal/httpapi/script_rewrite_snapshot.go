package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

// RedLines and FactsRevision determine the current generation context.
// Prompt and Facts are retained for checkpoints written by earlier releases.
type scriptRewriteSnapshot struct {
	Prompt        string  `json:"prompt,omitempty"`
	Facts         string  `json:"facts,omitempty"`
	RedLines      *string `json:"red_lines,omitempty"`
	FactsRevision string  `json:"facts_revision,omitempty"`
}

const scriptRewriteSnapshotKey = "script_rewrite_snapshot"

func scriptRewriteSnapshotFor(v model.LiveAgentPlanVersion) (scriptRewriteSnapshot, bool) {
	raw, ok := v.GenerationContext[scriptRewriteSnapshotKey]
	if !ok {
		return scriptRewriteSnapshot{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return scriptRewriteSnapshot{}, false
	}
	var snapshot scriptRewriteSnapshot
	if json.Unmarshal(data, &snapshot) != nil {
		return scriptRewriteSnapshot{}, false
	}
	return snapshot, true
}
func scriptRewriteFactsKey(v model.LiveAgentPlanVersion) string {
	snapshot, ok := scriptRewriteSnapshotFor(v)
	if !ok {
		return ""
	}
	if snapshot.RedLines != nil && snapshot.FactsRevision != "" {
		raw, _ := json.Marshal([]any{"script-text-policy-v3", v.TenantID, v.PlanID, strings.TrimSpace(*snapshot.RedLines), snapshot.FactsRevision})
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	// Equivalent facts alone cannot approve a rewrite under changed persona,
	// redlines or writing rules. Tenant/plan ownership is part of this identity.
	raw, _ := json.Marshal([]any{"script-text-policy-v2", v.TenantID, v.PlanID, strings.TrimSpace(snapshot.Prompt), strings.TrimSpace(snapshot.Facts)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func withScriptRewriteSnapshot(v model.LiveAgentPlanVersion, snapshot scriptRewriteSnapshot) model.LiveAgentPlanVersion {
	cloned := map[string]any{}
	for k, value := range v.GenerationContext {
		cloned[k] = value
	}
	cloned[scriptRewriteSnapshotKey] = snapshot
	v.GenerationContext = cloned
	return v
}
func (s *Server) loadScriptRewriteSnapshot(ctx context.Context, tenant int64, v model.LiveAgentPlanVersion) (scriptRewriteSnapshot, error) {
	if snapshot, ok := scriptRewriteSnapshotFor(v); ok {
		return snapshot, nil
	}
	if s.store == nil {
		return scriptRewriteSnapshot{}, errors.New("改写依据不可用")
	}
	sections, err := s.store.GetLiveAgentPlanInteractionSections(ctx, tenant, v.PlanID)
	if err != nil {
		return scriptRewriteSnapshot{}, err
	}
	facts, err := s.rewriteFactsJSON(ctx, tenant, v.PlanID)
	if err != nil {
		return scriptRewriteSnapshot{}, err
	}
	return newScriptRewriteSnapshot(sections.RedLinesMD, facts), nil
}

func newScriptRewriteSnapshot(redLines string, facts json.RawMessage) scriptRewriteSnapshot {
	sum := sha256.Sum256(facts)
	return scriptRewriteSnapshot{RedLines: &redLines, FactsRevision: hex.EncodeToString(sum[:])}
}

func backgroundRewriteRedLines(snapshot scriptRewriteSnapshot) (string, bool) {
	if snapshot.RedLines != nil {
		return planprompt.Sections{RedLinesMD: *snapshot.RedLines}.Normalize().RedLinesMD, true
	}
	// Older live checkpoints froze only the compiled prompt. Recover the red
	// lines from its envelope without reintroducing the old facts or sample.
	prompt := strings.ReplaceAll(snapshot.Prompt, "\r\n", "\n")
	const redlines = "\n\n【红线禁止】\n"
	const sample = "\n\n【样本话术】\n"
	if !strings.Contains(prompt, sample) {
		return "", !strings.Contains(prompt, "【红线禁止】")
	}
	facts := planprompt.Sections{FactsMD: snapshot.Facts}.Normalize().FactsMD
	if _, afterFacts, found := strings.Cut(prompt, "\n\n【事实依据】\n"+facts+redlines); found {
		if body, _, ok := strings.Cut(afterFacts, sample); ok {
			return strings.TrimSpace(body), true
		}
	}
	beforeSample, _, _ := strings.Cut(prompt, sample)
	i := strings.LastIndex(beforeSample, redlines)
	if i < 0 {
		return "", !strings.Contains(prompt, "【红线禁止】")
	}
	return strings.TrimSpace(beforeSample[i+len(redlines):]), true
}
