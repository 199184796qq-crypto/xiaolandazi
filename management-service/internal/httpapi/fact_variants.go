package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
)

// factVariantsInput 支持两种来源：
//   - Facts 非空：直接用传入的事实 value 生成变体（用于前端选中若干条即时改写）。
//   - Facts 为空：自动读取该方案的正式事实包（含禁说/安全改写）逐条生成。
type factVariantsInput struct {
	TenantID int64    `json:"tenant_id,omitempty"`
	Facts    []string `json:"facts,omitempty"`
	Count    int      `json:"count,omitempty"`
}

type factVariantItem struct {
	Fact string   `json:"fact"`
	V    []string `json:"v"`
}

type factVariantsResponse struct {
	Variants  []factVariantItem `json:"variants"`
	Provider  string            `json:"provider,omitempty"`
	Model     string            `json:"model,omitempty"`
	LatencyMS int64             `json:"latency_ms,omitempty"`
}

// factVariantSource 是传给口播模型的单条事实（4层中的「改写」层靠 forbidden/safe_rewrite 生效）。
type factVariantSource struct {
	Value      string `json:"value"`
	Forbidden  string `json:"forbidden,omitempty"`
	SafeRewrite string `json:"safe_rewrite,omitempty"`
}

func normalizeFactVariants(raw string) (factVariantsResponse, error) {
	var out factVariantsResponse
	if err := decodeLectureContent(raw, &out); err != nil {
		return out, errors.New("同义变体结果不是合法 JSON")
	}
	if out.Variants == nil {
		out.Variants = []factVariantItem{}
	}
	for i := range out.Variants {
		if out.Variants[i].V == nil {
			out.Variants[i].V = []string{}
		}
	}
	return out, nil
}

// runFactVariants 用系统口播模型（cycle.text_profile_id）实时生成事实同义变体。
// 不预存、不落库，每次调用即时改写，返回模型原始 JSON 文本。
func (s *Server) runFactVariants(ctx context.Context, tenantID, planID int64, sources []factVariantSource, count int) (string, error) {
	prompt, _, err := loadFactSynonymVariantsPrompt()
	if err != nil {
		return "", err
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return "", err
	}
	speechID := settings.Cycle.TextProfileID
	if speechID == "" {
		return "", errors.New("请先在后台配置口播模型")
	}
	if count <= 0 {
		count = 3
	}
	payload, _ := json.Marshal(map[string]any{"facts": sources, "count": count})
	result, _, callErr := s.completeAnchorStyleWorkbench(ctx, speechID, "fact_synonym_variants", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(payload)}}, 4000, false)
	if callErr != nil {
		return "", errors.New("同义变体生成暂时没有响应，请稍后重试")
	}
	return result.Text, nil
}

// factVariants 实时为方案的事实生成同义变体（不预存）。
func (s *Server) factVariants(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input factVariantsInput
	_ = readJSON(w, r, &input)
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}

	sources := make([]factVariantSource, 0, len(input.Facts))
	for _, f := range input.Facts {
		if f = strings.TrimSpace(f); f != "" {
			sources = append(sources, factVariantSource{Value: f})
		}
	}
	if len(sources) == 0 {
		facts, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, planID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取正式事实包失败")
			return
		}
		for _, f := range facts {
			if f.Status != "active" || strings.TrimSpace(f.Value) == "" {
				continue
			}
			sources = append(sources, factVariantSource{
				Value:       f.Value,
				Forbidden:   f.ForbiddenWording,
				SafeRewrite: f.SafeRewrite,
			})
		}
	}
	if len(sources) == 0 {
		writeError(w, http.StatusBadRequest, "没有可用的事实，请先补充事实依据")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenantID, PlanID: planID})
	raw, err := s.runFactVariants(ctx, tenantID, planID, sources, input.Count)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	output, err := normalizeFactVariants(raw)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, output)
}

// splitFactData 从 HookFact.Data 里拆出纯内容 + 禁说 + 安全改写。
// Data 可能形如「内容；禁说=xxx；安全改写=yyy」，分隔符与 generationFactView 保持一致。
func splitFactData(data string) (value, forbidden, safeRewrite string) {
	value = strings.TrimSpace(data)
	at := strings.Index(value, "；禁说=")
	if at < 0 {
		at = strings.Index(value, ";禁说=")
	}
	if at >= 0 {
		rest := value[at+len("；禁说="):]
		end := len(rest)
		for _, next := range []string{"；安全改写=", ";安全改写="} {
			if i := strings.Index(rest, next); i >= 0 && i < end {
				end = i
			}
		}
		forbidden = strings.TrimSpace(rest[:end])
		value = strings.TrimSpace(value[:at])
	}
	at = strings.Index(value, "；安全改写=")
	if at < 0 {
		at = strings.Index(value, ";安全改写=")
	}
	if at >= 0 {
		safeRewrite = strings.TrimSpace(value[at+len("；安全改写="):])
		value = strings.TrimSpace(value[:at])
	}
	return value, forbidden, safeRewrite
}

// ensureFactSynonymVariants 为当前任务的核心事实实时生成同义变体并注入表达指导，
// 让正文模型看到「具体事实→同义说法」的正向举例，打破高频短语复读。
// 每次只取 1 个事实、生成 1 个变体（优先含数字、跳过元信息），避免输出爆炸与超时；失败降级。
func (s *Server) ensureFactSynonymVariants(ctx context.Context, state *anchorcycle.State) {
	if state == nil {
		return
	}
	task := state.Task()
	scope := task.FactScope
	if scope == nil || len(scope.Facts) == 0 {
		return
	}
	if requireCycleP0(state) != nil {
		return
	}
	// 挑当前任务最核心的那条事实句（FactScope.Facts 顺序 = 商品身份 + 任务绑定事实，
	// 跳过商品名称/福利等元信息后，第一条就是本次要讲的核心事实）。
	idx := -1
	var val, forbidden, safeRewrite string
	for i, f := range scope.Facts {
		v, fb, sr := splitFactData(f.Data)
		if v == "" || strings.HasPrefix(v, "商品名称") || strings.HasPrefix(v, "有效福利") || strings.HasPrefix(v, "福利=") {
			continue
		}
		idx, val, forbidden, safeRewrite = i, v, fb, sr
		break
	}
	if idx < 0 {
		return
	}
	genCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	genCtx = withExchangeScope(genCtx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	raw, err := s.runFactVariants(genCtx, state.TenantID, state.PlanID, []factVariantSource{{Value: val, Forbidden: forbidden, SafeRewrite: safeRewrite}}, 1)
	if err != nil {
		state.Warn("事实同义变体生成失败，按原事实生成：" + err.Error())
		return
	}
	out, err := normalizeFactVariants(raw)
	if err != nil {
		return
	}
	if len(out.Variants) == 0 {
		return
	}
	for _, v := range out.Variants[0].V {
		if strings.TrimSpace(v) == "" {
			continue
		}
		scope.Facts[idx].ExpressionGuidance = append(scope.Facts[idx].ExpressionGuidance,
			map[string]string{"要求": "多个正向举例（仅参考准确说法，按本次目的重组，不是事实）", "内容": v})
	}
}

// rewriteCycleText 对已生成的正文做同义改写：换「使用方式/动作」措辞、锁死数字与称呼，
// 打破高频短语（如"叮1分钟"）复读。失败返回错误，由调用方降级用原文。
func (s *Server) rewriteCycleText(ctx context.Context, state *anchorcycle.State, text string) (string, error) {
	prompt, _, err := loadFactSpeechRewritePrompt()
	if err != nil {
		return "", err
	}
	if state == nil || state.TextModel == nil {
		return "", errors.New("文字模型不可用")
	}
	current, err := s.store.ResolveSpeechModel(ctx, state.Config.TextProfileID)
	if err != nil || current == nil || !current.Enabled || current.APIKey == "" {
		return "", errors.New("文字模型凭据不可用")
	}
	p := *state.TextModel
	p.APIKey = current.APIKey
	msgs := []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: text}}
	// 改写也必须带完整 P0 合规规则，否则改写模型会引入绝对化用语/禁说词等违规。
	if p0 := generationP0View(state.P0PolicyMD, state.Config.ComplianceScope); p0 != nil {
		if s, ok := p0.(string); ok && s != "" {
			msgs = append([]agentgateway.Message{{Role: "system", Content: "有效P0（必须遵守的合规规则）：\n" + s}}, msgs...)
		} else if b, e := json.Marshal(p0); e == nil {
			msgs = append([]agentgateway.Message{{Role: "system", Content: "有效P0（必须遵守的合规规则）：\n" + string(b)}}, msgs...)
		}
	}
	req := agentgateway.Request{
		Stage:          "mainline_text_rewrite",
		Model:          p.Model,
		Messages:       msgs,
		ResponseFormat: agentgateway.ResponseText,
		MaxTokens:      800,
		EnableThinking: false,
		Timeout:        8 * time.Second,
	}
	req, err = agentgateway.PrepareTextRequest(p, req)
	if err != nil {
		return "", err
	}
	work, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	work = withExchangeScope(work, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(work, "mainline_text_rewrite", p.ID, req)
	if err != nil {
		return "", err
	}
	started := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(work, p, req)
	response.Provider, response.Model = "anchor:"+p.ID, p.Model
	err = s.finishModelExchange(work, id, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Text) == "" {
		return "", errors.New("改写结果为空")
	}
	return response.Text, nil
}

