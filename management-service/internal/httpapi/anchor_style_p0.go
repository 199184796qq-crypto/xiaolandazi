package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
)

func loadP0Mother() (string, string, error) {
	return loadReplaceablePromptAsset("ANCHOR_STYLE_P0_POLICY_PATH", "configs/prompts/anchor-style/compliance/L1/p0-hard-block.md", "L1 P0硬拦截母版")
}

func installP0Mother(state *anchorcycle.State) error {
	if state.P0PolicyMD != "" {
		_, err := contentguard.Compile(state.P0PolicyMD)
		return err
	}
	md, source, err := loadP0Mother()
	if err != nil {
		return err
	}
	return state.InstallP0(md, source)
}

func renderP0Repair(state *anchorcycle.State) (string, error) {
	md, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_P0_REPAIR_PROMPT_PATH", "configs/prompts/anchor-style/compliance/L1/p0-local-rewrite.md", "P0局部重写母版")
	if err != nil {
		return "", err
	}
	rules, err := state.P0RulesJSON()
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(state.P0PendingRepair)
	return renderStablePrefixTemplate(md, "{{p0_rules}}", rules, "{{repair_request}}", string(raw), "{{max_chars}}", strconv.Itoa(state.Config.MaxChars)), nil
}

func renderP0Boundary(state *anchorcycle.State) (string, error) {
	if state.P0PolicyMD == "" {
		return "", nil
	}
	md, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_P0_BOUNDARY_PROMPT_PATH", "configs/prompts/anchor-style/compliance/L1/p0-generation-boundary.md", "P0生成边界母版")
	if err != nil {
		return "", err
	}
	rules := ""
	if !state.BootstrapSent && len(state.Accepted) == 0 && len(state.Attempts) == 0 {
		rules, err = state.P0RulesJSON()
		if err != nil {
			return "", err
		}
	}
	return strings.NewReplacer("{{p0_scope}}", state.Config.ComplianceScope, "{{p0_rules}}", rules).Replace(md), nil
}

func p0AuditEvent(state *anchorcycle.State, candidate anchorcycle.Candidate) model.P0AuditEvent {
	decision := "passed"
	if candidate.P0Report != nil && !candidate.P0Report.Passed {
		decision = "rewrite_required"
		used := state.P0RewritesUsed
		if candidate.P0Patch != nil {
			used++
		}
		if used >= state.P0MaxRewrites {
			decision = "skipped"
		}
		if state.Surface == "formal_live" {
			decision = "skipped"
		}
	}
	raw, _ := json.Marshal(candidate.P0Report)
	var report map[string]any
	_ = json.Unmarshal(raw, &report)
	if state.Delivery != nil {
		report["runtime_session_id"] = state.Delivery.RuntimeSessionID
	}
	if len(candidate.ContentWarnings) > 0 {
		report["warnings"] = candidate.ContentWarnings
	}
	raw, _ = json.Marshal(report)
	return model.P0AuditEvent{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID, CandidateID: candidate.ID, RuleSHA256: candidate.P0Report.SHA256, Phase: state.Phase, TaskKey: state.Task().Key, RewriteNumber: candidate.P0RewriteNumber, Decision: decision, Text: cycleVoiceText(candidate), RawResponse: candidate.RawText, Prompt: candidate.Prompt, Report: raw}
}

func (s *Server) persistP0Audit(ctx context.Context, state *anchorcycle.State, candidate anchorcycle.Candidate) error {
	p, err := contentguard.Compile(state.P0PolicyMD)
	if err != nil {
		return err
	}
	if err = s.store.SaveP0PolicyVersion(ctx, p.SHA256, p.Data.PolicyVersion, state.P0PolicySource, state.P0PolicyMD); err != nil {
		return err
	}
	return s.store.SaveP0AuditEvent(ctx, p0AuditEvent(state, candidate))
}

func (s *Server) anchorP0AuditGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, _, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			writeError(w, 400, "查询数量须为1到200")
			return
		}
	}
	items, err := s.store.ListP0AuditEvents(r.Context(), tenant, plan, r.URL.Query().Get("run_id"), limit)
	if err != nil {
		writeError(w, 500, "读取P0审核记录失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
