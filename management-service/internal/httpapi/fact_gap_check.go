package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

type factGapCheckInput struct {
	TenantID      int64 `json:"tenant_id,omitempty"`
	ProductLinkID int64 `json:"product_link_id,omitempty"`
}

type factGapItem struct {
	Dimension string   `json:"维度"`
	Level     string   `json:"层级"`
	Missing   string   `json:"待补事实"`
	Prompts   []string `json:"填写提示"`
}

type factGapCheckResponse struct {
	Gaps      []factGapItem `json:"gaps"`
	Summary   string        `json:"summary"`
	Provider  string        `json:"provider,omitempty"`
	Model     string        `json:"model,omitempty"`
	LatencyMS int64         `json:"latency_ms,omitempty"`
}

// normalizeFactGapCheck validates and normalizes the review model's gap-check
// output. The mother contract already restricts top-level fields to gaps and
// summary; this keeps the raw model text from leaking malformed structures.
func normalizeFactGapCheck(raw string) (factGapCheckResponse, error) {
	var out factGapCheckResponse
	if err := decodeLectureContent(raw, &out); err != nil {
		return out, errors.New("查漏结果不是合法 JSON")
	}
	if out.Gaps == nil {
		out.Gaps = []factGapItem{}
	}
	for i := range out.Gaps {
		g := &out.Gaps[i]
		if g.Prompts == nil {
			g.Prompts = []string{}
		}
		if g.Dimension == "" {
			return factGapCheckResponse{}, errors.New("查漏结果缺少维度字段")
		}
	}
	return out, nil
}

// runFactGapCheck runs the "商品事实必查" checklist against the plan's formal
// fact package using the system review model and returns the raw gap JSON
// produced by the model. Shared by the fact-gap-check endpoint and the first
// product-supplement chat turn.
func (s *Server) runFactGapCheck(ctx context.Context, tenantID, planID int64, facts, p0 string, target *supplementProductTarget) (string, error) {
	prompt, _, err := loadFactGapChecklistPrompt()
	if err != nil {
		return "", err
	}
	// 行业优先：方案在「业务模式」选了行业，就把行业四件套的挖掘维度注入清单母版
	// 作为行业必查层；未选行业（或综合/其他）时占位置空，清单保持原有两层行为。
	if pack, packErr := s.loadIndustryPackForPlan(ctx, tenantID, planID); packErr == nil && pack != nil {
		prompt = injectIndustryFactChecklist(prompt, pack.IndustryName, pack.SellingDimensions)
	} else {
		prompt = strings.ReplaceAll(prompt, "{{INDUSTRY_MUST_CHECK}}", "")
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return "", err
	}
	reviewID := settings.Cycle.TextProfileID
	if reviewID == "" {
		reviewID = settings.SafetyProfileID
	}
	if reviewID == "" {
		reviewID = settings.SellingPointProfileID
	}
	if reviewID == "" {
		return "", errors.New("请先在后台配置审核模型")
	}
	payload, _ := json.Marshal(factGapCheckPayload(facts, p0, target))
	result, _, callErr := s.completeAnchorStyleWorkbench(ctx, reviewID, "fact_gap_check", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(payload)}}, 2000, false)
	if callErr != nil {
		return "", errors.New("商品查漏暂时没有响应，请稍后重试")
	}
	return result.Text, nil
}

func factGapCheckPayload(facts, p0 string, target *supplementProductTarget) map[string]any {
	return map[string]any{"fact_package": facts, "effective_p0": p0, "target_product": target}
}

// factGapCheck runs the "商品事实必查" checklist against the plan's current
// formal fact package and returns the missing / incomplete dimensions with
// plain-language prompts for the merchant to fill in. It uses the system
// review model (文字审核模型), falling back to safety then selling-point.
func (s *Server) factGapCheck(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input factGapCheckInput
	if r.ContentLength != 0 && readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "补全检查请求格式错误")
		return
	}
	if input.ProductLinkID < 0 {
		writeError(w, http.StatusBadRequest, "商品链接选择无效")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}

	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	source, err := s.loadSupplementFactPackage(r.Context(), tenantID, planID, input.ProductLinkID, p0, p1, p2)
	if err != nil {
		supplementScopeError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenantID, PlanID: planID})
	raw, err := s.runFactGapCheck(ctx, tenantID, planID, source.Facts, p0, source.Target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	output, err := normalizeFactGapCheck(raw)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, output)
}
