package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const factPreviewSurface = "fact_speech_preview"

func selectedPreviewFact(items []model.LiveAgentPlanFact, tenant, plan, id, version int64) (model.LiveAgentPlanFact, bool) {
	for _, f := range items {
		if f.TenantID == tenant && f.PlanID == plan && f.ID == id && f.VersionNo == version && f.Status == "active" {
			return f, true
		}
	}
	return model.LiveAgentPlanFact{}, false
}

func factPreviewTask(f model.LiveAgentPlanFact, corrections []map[string]any) anchorcycle.Task {
	// Only content crosses the model boundary. IDs and versions remain in the
	// HTTP request/response and exchange scope, never in the material payload.
	raw, _ := json.Marshal(map[string]string{"事实名称": f.Key, "事实内容（包含适用条件）": f.Value})
	guidance := []map[string]string{}
	if strings.TrimSpace(f.ForbiddenWording) != "" {
		guidance = append(guidance, map[string]string{"要求": "负向表达约束", "内容": f.ForbiddenWording})
	}
	if strings.TrimSpace(f.SafeRewrite) != "" {
		guidance = append(guidance, map[string]string{"要求": "正向举例模板（只参考说法，不补充事实）", "内容": f.SafeRewrite})
	}
	return anchorcycle.Task{SellingPoint: f.Key, Angle: f.Key, FactScope: &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: derivedHash(fmt.Sprintf("%d/%d/%d/%d:%s", f.TenantID, f.PlanID, f.ID, f.VersionNo, raw)), Data: string(raw), ExpressionGuidance: guidance}}, Corrections: corrections}}
}

func (s *Server) prepareFactSpeechPreview(ctx context.Context, tenant, plan, room int64, f model.LiveAgentPlanFact, roomName string) (anchorcycle.State, error) {
	corrections, err := s.store.ListContentCorrections(ctx, tenant, plan)
	if err != nil {
		return anchorcycle.State{}, errors.New("读取方案纠正规则失败")
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		return anchorcycle.State{}, errors.New("读取方案商品范围失败")
	}
	source := contentpools.Sources{Corrections: corrections}
	productIDs := []int64{}
	for _, link := range links {
		source.Products = append(source.Products, contentpools.Product{ID: link.ID, Link: link.LinkKey})
		productIDs = append(productIDs, link.ID)
	}
	task := factPreviewTask(f, contentpools.CorrectionView(source, productIDs))
	state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{MaxChars: 220})
	if err != nil {
		return state, err
	}
	state.TenantID, state.PlanID, state.RoomID = tenant, plan, room
	state.Surface, state.Phase = factPreviewSurface, "body"
	state.Facts = task.FactScope.Facts[0].Data
	// 预览不再强制要求评分基准：有基准就带表达标尺，没有就让模型自由发挥。
	if card, cardErr := s.store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan); cardErr == nil && card.Status == "confirmed" {
		state.BaselineJSON = card.EffectiveScorecardJSON()
		if _, e := cycleExpressionView(&state); e != nil {
			state.BaselineJSON = ""
		}
	}
	state.FreeStyle = strings.TrimSpace(state.BaselineJSON) == ""
	if err = installP0Mother(&state); err != nil {
		return state, err
	}
	if err = s.installPlanP0Allowances(ctx, &state); err != nil {
		return state, errors.New("读取方案规则确认记录失败")
	}
	if err = s.installCycleTextModel(ctx, &state); err != nil {
		return state, errors.New("当前真实话术模型不可用，请检查模型配置")
	}
	if state.TextModel == nil {
		return state, errors.New("请先配置口播生成模型")
	}
	// 对齐新主线四层结构：预览母版不再塞进 SystemPrompt（主线不设 system 消息），
	// 而是作为「环节约束」并入 user 的 packet。单事实讲品的边界约束由
	// fact-speech-preview.md 提供，经 SegmentDirective 携带进「后续表达加强建议」字段。
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_SPEECH_PREVIEW_PROMPT_PATH", "configs/prompts/anchor-style/fact-speech-preview.md", "事实话术预览母版")
	if err != nil {
		return state, err
	}
	previewDirective := strings.TrimSpace(mother)
	stage := "围绕所选事实讲品"
	contextData, _ := json.Marshal(map[string]string{"模拟直播间": roomName, "当前环节": stage})
	feedback := string(contextData)
	if previewDirective != "" {
		feedback = strings.TrimSpace(feedback + "\n\n【本环节生成约束，仅本段生效，不作为商品事实】\n" + previewDirective)
	}
	state.SegmentDirective = feedback
	return state, nil
}

func (s *Server) generateFactSpeechPreview(ctx context.Context, state *anchorcycle.State) (map[string]any, error) {
	return s.generateFactSpeechPreviewOnce(ctx, state)
}

func (s *Server) generateFactSpeechPreviewOnce(ctx context.Context, state *anchorcycle.State) (map[string]any, error) {
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	result, _, err := s.completeCycleText(ctx, state)
	if err != nil {
		return nil, errors.New("话术生成未完成，请重试或检查当前口播模型连接")
	}
	if strings.TrimSpace(result.Text) == "" {
		return nil, errors.New("模型未返回话术，请重新生成")
	}
	text := strings.TrimSpace(result.Text)
	task := state.Task()
	rules := []map[string]string{}
	for _, rule := range state.LatestExpressionConstraints {
		feedback, _ := rule["用户原话（理解纠正范围，不能扩张限制）"].(string)
		constraint, _ := rule["表达限制"].(string)
		rules = append(rules, map[string]string{"feedback": feedback, "constraint": constraint})
	}
	output := map[string]any{
		"text":      text,
		"raw_text":  text,
		"title":     task.SellingPoint,
		"purpose":   task.PainPoint,
		"angle":     task.Angle,
		"notice":    "使用当前方案和真实口播模型生成，仅预览文字。",
		"expression_constraints": rules,
	}
	output["model_alias"], output["model_name"] = state.TextModel.Name, state.TextModel.Model
	output["preview_kind"] = "product"
	return output, nil
}

func (s *Server) liveAgentPlanFactSpeechPreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	id, ok := liveAgentPlanFactPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID  int64 `json:"tenant_id"`
		RoomID    int64 `json:"room_id"`
		VersionNo int64 `json:"version_no"`
	}
	if readJSON(w, r, &input) != nil || input.RoomID <= 0 || input.VersionNo <= 0 {
		writeError(w, 400, "请明确当前直播间和事实版本")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	roomTenant, ok := s.tenantForRoom(w, r, actor, input.RoomID)
	if !ok {
		return
	}
	if roomTenant != tenant {
		writeError(w, 403, "直播间与方案范围不一致")
		return
	}
	planData, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) || err == nil && planData.Status != "active" {
		writeError(w, 404, "当前方案不存在或已停用")
		return
	} else if err != nil {
		writeError(w, 503, "读取方案失败")
		return
	}
	room := s.loadSpeechAnalysisRoom(r.Context(), tenant, input.RoomID)
	if room.Name == "" {
		writeError(w, 404, "当前直播间不存在或不可访问")
		return
	}
	items, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 503, "读取事实依据失败")
		return
	}
	fact, found := selectedPreviewFact(items, tenant, plan, id, input.VersionNo)
	if !found {
		writeError(w, 409, "事实已修改或删除，请刷新后预览")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	state, err := s.prepareFactSpeechPreview(ctx, tenant, plan, input.RoomID, fact, room.Name)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	output, err := s.generateFactSpeechPreview(ctx, &state)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	current, err := s.store.ListLiveAgentPlanFacts(ctx, tenant, plan)
	if err != nil {
		writeError(w, 503, "核对事实版本失败")
		return
	}
	if _, valid := selectedPreviewFact(current, tenant, plan, id, input.VersionNo); !valid {
		writeError(w, 409, "生成期间事实已修改或删除，请刷新后重新预览")
		return
	}
	output["fact_id"], output["version_no"], output["room_name"], output["plan_name"] = id, input.VersionNo, room.Name, planData.Name
	writeJSON(w, 200, output)
}
