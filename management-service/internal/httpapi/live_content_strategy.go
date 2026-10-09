package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
)

func (s *Server) loadContentStrategy(ctx context.Context, tenant, plan int64, facts string) *contentstrategy.Snapshot {
	if control, err := s.store.GetLatestPlanContentStrategy(ctx, tenant, plan, "", time.Now().UTC()); err == nil && contentstrategy.Disabled(control) {
		return &contentstrategy.Snapshot{Version: 1, Enabled: false}
	}
	raw, err := s.store.GetLatestPlanContentStrategy(ctx, tenant, plan, contentstrategy.SHA(facts), time.Now().UTC())
	if errors.Is(err, appdb.ErrPlanAnalysisAssetNotFound) {
		return nil
	}
	if err == nil {
		var snapshot *contentstrategy.Snapshot
		snapshot, err = contentstrategy.Parse(raw, facts)
		if err == nil {
			return snapshot
		}
	}
	log.Printf("content strategy fallback tenant=%d plan=%d: %v", tenant, plan, err)
	return nil
}

// This prepares a separate versioned analysis asset. It cannot modify a wheel,
// published plan, room preferences, runtime session or any audio queue.
func (s *Server) prepareContentStrategy(ctx context.Context, tenant, plan int64, facts, profile string, force bool) (*contentstrategy.Snapshot, error) {
	purposeMD, purposeSource, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/fact-purpose-analysis.md", "事实沟通目的母版")
	if err != nil {
		return nil, err
	}
	angleMD, angleSource, err := loadReplaceablePromptAsset("", "configs/prompts/anchor-style/fact-purpose-angle-analysis.md", "目的角度母版")
	if err != nil {
		return nil, err
	}
	sources := []string{purposeSource, angleSource}
	if !force {
		if prior := s.loadContentStrategy(ctx, tenant, plan, facts); prior != nil && prior.Enabled && reflect.DeepEqual(prior.PromptSources, sources) {
			return prior, nil
		}
	}
	records := contentstrategy.Facts(facts)
	if len(records) == 0 {
		return nil, errors.New("缺少可分析的完整事实记录")
	}
	if strings.TrimSpace(profile) == "" {
		return nil, errors.New("请先配置卖点挖掘模型")
	}
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	payload, _ := json.Marshal(map[string]any{"完整事实包": facts, "事实记录": records})
	firstRequest := []agentgateway.Message{{Role: "system", Content: purposeMD}, {Role: "user", Content: string(payload)}}
	first, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "fact_content_purposes", agentgateway.ResponseJSON, firstRequest, 3000, false)
	if err != nil {
		return nil, fmt.Errorf("目的分析失败，原有策略保留：%w", err)
	}
	var purposes contentstrategy.Purposes
	if err = contentstrategy.Decode(first.Text, &purposes); err == nil {
		err = contentstrategy.Validate(records, purposes.Items, nil, false)
	}
	if err != nil {
		return nil, fmt.Errorf("目的返回无效，原始返回已记录，原有策略保留：%w", err)
	}
	// Preserve the actual first answer in the second model call; the program
	// validates references but never invents purposes or angles.
	secondRequest := []agentgateway.Message{{Role: "system", Content: angleMD}, {Role: "user", Content: string(payload)}, {Role: "assistant", Content: first.Text}}
	secondInstruction, _ := json.Marshal(map[string]any{"本次阶段": "按照上轮目的分析挖掘角度，暂不写口播", "目的列表": purposes.Items})
	secondRequest = append(secondRequest, agentgateway.Message{Role: "user", Content: string(secondInstruction)})
	second, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "fact_content_angles", agentgateway.ResponseJSON, secondRequest, 7000, false)
	if err != nil {
		return nil, fmt.Errorf("角度分析失败，原有策略保留：%w", err)
	}
	var table contentstrategy.Table
	if err = contentstrategy.Decode(second.Text, &table); err == nil {
		err = contentstrategy.Validate(records, purposes.Items, table.Rows, true)
	}
	if err != nil {
		return nil, fmt.Errorf("角度返回无效，原始返回已记录，原有策略保留：%w", err)
	}
	for _, row := range table.Rows {
		if err := contentstrategy.ValidateRowPolicy(row, true); err != nil {
			return nil, fmt.Errorf("角度条件协议无效，原始返回已记录：%w", err)
		}
	}
	snapshot := &contentstrategy.Snapshot{Version: 2, Enabled: true, FactsSHA: contentstrategy.SHA(facts), Facts: records, Purposes: purposes.Items, Rows: table.Rows, PromptSources: sources, PurposeRaw: first.Text, AngleRaw: second.Text, Model: second.Model}
	return snapshot, nil
}

func (s *Server) saveContentStrategy(ctx context.Context, tenant, plan int64, snapshot *contentstrategy.Snapshot) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	// Different explicit analyses/control changes are separate immutable versions.
	fingerprint := contentstrategy.SHA(string(raw) + "/" + time.Now().UTC().Format(time.RFC3339Nano))
	return s.store.SavePlanAnalysisAsset(ctx, tenant, plan, contentstrategy.Kind, fingerprint, string(raw))
}

func (s *Server) initializeContentStrategy(ctx context.Context, tenant, plan, actor int64) error {
	safety, err := s.prepareDerivedSafety(ctx, tenant, plan, actor)
	if err != nil {
		return err
	}
	// An explicit disabled version stays disabled across routine initialization.
	if prior := s.loadContentStrategy(ctx, tenant, plan, safety.SafeFacts); prior != nil {
		return nil
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return err
	}
	snapshot, err := s.prepareContentStrategy(ctx, tenant, plan, safety.SafeFacts, settings.SellingPointProfileID, false)
	if err != nil {
		return err
	}
	latest, err := s.prepareDerivedSafety(ctx, tenant, plan, actor)
	if err != nil || latest.SafeFacts != safety.SafeFacts {
		return errors.New("内容策略分析期间事实已变化，原有方案保留")
	}
	return s.saveContentStrategy(ctx, tenant, plan, snapshot)
}

func (s *Server) planContentStrategy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID   int64 `json:"tenant_id,omitempty"`
		Enabled    *bool `json:"enabled,omitempty"`
		Regenerate bool  `json:"regenerate,omitempty"`
	}
	if r.Method == http.MethodPost && readJSON(w, r, &input) != nil {
		writeError(w, 400, "内容策略请求格式错误")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, r.Method == http.MethodPost)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, 404, "直播方案不存在")
		return
	}
	if r.Method == http.MethodGet {
		raw, err := s.store.GetLatestPlanContentStrategy(r.Context(), tenant, plan, "", time.Now().UTC())
		if errors.Is(err, appdb.ErrPlanAnalysisAssetNotFound) {
			writeJSON(w, 200, map[string]any{"enabled": false, "strategy": nil})
			return
		}
		if err != nil {
			writeError(w, 500, "读取内容策略失败")
			return
		}
		writeJSON(w, 200, json.RawMessage(raw))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	safety, err := s.prepareDerivedSafety(ctx, tenant, plan, actor.UserID)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	if input.Enabled != nil && !*input.Enabled {
		snapshot := s.loadContentStrategy(ctx, tenant, plan, safety.SafeFacts)
		if snapshot == nil {
			snapshot = &contentstrategy.Snapshot{Version: 1, FactsSHA: contentstrategy.SHA(safety.SafeFacts), Facts: contentstrategy.Facts(safety.SafeFacts)}
		}
		snapshot.Enabled = false
		if err = s.saveContentStrategy(ctx, tenant, plan, snapshot); err != nil {
			writeError(w, 500, "保存关闭策略版本失败")
			return
		}
		writeJSON(w, 200, snapshot)
		return
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		writeError(w, 500, "读取分析模型配置失败")
		return
	}
	snapshot, err := s.prepareContentStrategy(ctx, tenant, plan, safety.SafeFacts, settings.SellingPointProfileID, input.Regenerate)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	// Reject a stale analysis if input changed during the two model calls.
	latest, err := s.prepareDerivedSafety(ctx, tenant, plan, actor.UserID)
	if err != nil || latest.SafeFacts != safety.SafeFacts {
		writeError(w, 409, "分析期间事实已变化，原策略保留，请重新分析")
		return
	}
	if err = s.saveContentStrategy(ctx, tenant, plan, snapshot); err != nil {
		writeError(w, 500, "内容策略保存失败，原策略保留")
		return
	}
	writeJSON(w, 200, snapshot)
}
