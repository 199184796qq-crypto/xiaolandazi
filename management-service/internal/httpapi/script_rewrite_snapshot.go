package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
)

// Preserve the existing prompt verbatim, without adding model instructions.
type scriptRewriteSnapshot struct {
	Prompt string `json:"prompt"`
	Facts  string `json:"facts"`
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
		if s.store != nil && snapshot.Prompt != "" {
			current, err := s.store.GetLiveAgentPlanRuntimePrompts(ctx, tenant, v.PlanID)
			if err != nil {
				return scriptRewriteSnapshot{}, err
			}
			if prompt, ok := current.RewriteFor(snapshot.Prompt); ok {
				snapshot.Prompt = prompt
			}
		}
		return snapshot, nil
	}
	if s.store == nil {
		return scriptRewriteSnapshot{}, errors.New("改写依据不可用")
	}
	document, err := s.store.GetLiveAgentPlanPromptDocument(ctx, tenant, v.PlanID)
	if err != nil {
		return scriptRewriteSnapshot{}, err
	}
	prompt := strings.TrimSpace(document.RewritePromptMD)
	if prompt == "" {
		prompt, err = renderPlanRewritePrompt(document.Sections)
	}
	return scriptRewriteSnapshot{Prompt: prompt, Facts: document.Sections.FactsMD}, err
}
