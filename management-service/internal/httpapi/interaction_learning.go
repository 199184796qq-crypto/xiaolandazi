package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionlearning"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
)

type interactionSimulationItem struct {
	Question      string   `json:"question"`
	Context       string   `json:"context"`
	Kind          string   `json:"kind"`
	Meaning       string   `json:"meaning"`
	Reason        string   `json:"reason"`
	SuggestedRule string   `json:"suggested_rule"`
	NeededFacts   []string `json:"needed_facts"`
}

func decodeStrictJSON(raw string, target any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

func decodeInteractionSimulation(raw string) ([]interactionSimulationItem, error) {
	var out struct {
		Items []interactionSimulationItem `json:"items"`
	}
	if err := decodeStrictJSON(raw, &out); err != nil {
		return nil, err
	}
	if out.Items == nil || len(out.Items) > 12 {
		return nil, fmt.Errorf("simulation items must be an array of up to 12")
	}
	for _, i := range out.Items {
		if strings.TrimSpace(i.Question) == "" || len([]rune(i.Question)) > 1000 || strings.TrimSpace(i.Meaning) == "" {
			return nil, fmt.Errorf("invalid simulated question")
		}
		r := model.InteractionLearningReview{Kind: i.Kind, Context: i.Context, Meaning: i.Meaning, Reason: i.Reason, SuggestedRule: i.SuggestedRule, NeededFacts: i.NeededFacts}
		if err := interactionlearning.ValidateReview(r); err != nil {
			return nil, err
		}
	}
	return out.Items, nil
}

func (s *Server) interactionLearningList(w http.ResponseWriter, r *http.Request) {
	_, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	planID, _ := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
	if planID <= 0 {
		p, e := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) || errors.Is(e, appdb.ErrLiveAgentPlanNotFound) {
				writeJSON(w, 200, map[string]any{"items": []any{}, "pending_count": 0})
			} else {
				writeError(w, 500, "读取当前方案失败")
			}
			return
		}
		planID = p.ID
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, 404, "当前方案不存在")
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "closed" && status != "adopted" && status != "all" {
		writeError(w, 400, "记录筛选状态无效")
		return
	}
	source := r.URL.Query().Get("source")
	if source != "" && source != "initial_simulation" && source != "live_uncertain" {
		writeError(w, 400, "记录来源无效")
		return
	}
	query := r.URL.Query().Get("q")
	if len([]rune(query)) > 200 {
		writeError(w, 400, "检索词过长")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		writeError(w, 400, "页码无效")
		return
	}
	items, count, total, err := s.store.ListInteractionLearningHistory(r.Context(), tenantID, roomID, planID, status, source, query, 30, (page-1)*30)
	if err != nil {
		writeError(w, 500, "读取待确认互动失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "pending_count": count, "total": total, "page": page})
}

func (s *Server) interactionLearningSimulate(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	var input struct {
		PlanID int64 `json:"plan_id"`
	}
	if err := readJSON(w, r, &input); err != nil || input.PlanID <= 0 {
		writeError(w, 400, "请选择当前智能体方案")
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, input.PlanID)
	if err != nil {
		writeError(w, 404, "当前方案不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
	defer cancel()
	facts, err := s.store.ListLiveAgentPlanFacts(ctx, tenantID, plan.ID)
	if err != nil {
		writeError(w, 500, "读取事实依据失败")
		return
	}
	products, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenantID, plan.ID)
	if err != nil {
		writeError(w, 500, "读取商品卡失败")
		return
	}
	benefits, err := s.store.ListLiveAgentPlanBenefits(ctx, tenantID, plan.ID)
	if err != nil {
		writeError(w, 500, "读取福利卡失败")
		return
	}
	memories, err := s.store.ListActiveAgentMemories(ctx, tenantID, roomID)
	if err != nil {
		writeError(w, 500, "读取已采用纠正失败")
		return
	}
	// Simulation uses adopted assets only; draft facts cannot become answers.
	activeFacts := facts[:0]
	for _, f := range facts {
		if f.Status == "active" {
			activeFacts = append(activeFacts, f)
		}
	}
	activeProducts := products[:0]
	for _, p := range products {
		if p.Status == "active" {
			activeProducts = append(activeProducts, p)
		}
	}
	activeBenefits := benefits[:0]
	for _, b := range benefits {
		if b.Status == "active" {
			activeBenefits = append(activeBenefits, b)
		}
	}
	template, err := interactionpolicy.Read("interaction-learning-simulation.md")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	packet, _ := json.Marshal(map[string]any{"方案": plan, "商品卡": activeProducts, "事实依据完整记录": activeFacts, "福利": activeBenefits, "已采用纠正": interactionlearning.MemoriesForPlan(memories, plan.ID)})
	roomRef := roomID
	invocation := s.beginAISingleUse(ctx, actor, &roomRef, "interaction_learning_simulation", map[string]any{"plan_id": plan.ID})
	response, err := s.speechGateway().Complete(ctx, agentgateway.Request{Stage: "interaction_learning", Messages: []agentgateway.Message{{Role: "system", Content: string(template)}, {Role: "user", Content: string(packet)}}, MaxTokens: 3500, ResponseFormat: agentgateway.ResponseJSON, EnableThinking: false, Timeout: 40 * time.Second})
	if err != nil {
		s.finishAISingleUse(ctx, invocation, "failed", response.Provider, response.Model, response.LatencyMS, map[string]any{"error": err.Error()})
		writeError(w, 502, "模拟挖掘失败，请重试")
		return
	}
	items, err := decodeInteractionSimulation(response.Text)
	if err != nil {
		s.finishAISingleUse(ctx, invocation, "failed", response.Provider, response.Model, response.LatencyMS, map[string]any{"error": err.Error(), "raw_model_output": response.Text})
		writeError(w, 502, "模拟返回格式未通过校验，原始结果已保留，请重试")
		return
	}
	batchRef := fmt.Sprintf("simulation:%d:%d", plan.ID, time.Now().UnixNano())
	for _, i := range items {
		review := model.InteractionLearningReview{PlanID: plan.ID, Kind: i.Kind, Meaning: i.Meaning, Context: i.Context, Reason: i.Reason, SuggestedRule: i.SuggestedRule, NeededFacts: i.NeededFacts, RawModelOutput: response.Text, ModelProvider: response.Provider, ModelName: response.Model, PromptSHA256: fmt.Sprintf("%x", sha256.Sum256(template))}
		if err := s.store.RecordInteractionLearningReview(ctx, model.InteractionLearningObservation{TenantID: tenantID, RoomID: roomID, ActorUserID: actor.UserID, Question: i.Question, SourceType: "initial_simulation", SourceRef: batchRef, Review: review}); err != nil {
			s.finishAISingleUse(ctx, invocation, "failed", response.Provider, response.Model, response.LatencyMS, map[string]any{"error": err.Error(), "raw_model_output": response.Text})
			writeError(w, 500, "保存模拟建议失败，已保存的建议可刷新查看")
			return
		}
	}
	s.finishAISingleUse(ctx, invocation, "succeeded", response.Provider, response.Model, response.LatencyMS, map[string]any{"items": len(items), "raw_model_output": response.Text, "prompt_sha256": fmt.Sprintf("%x", sha256.Sum256(template))})
	writeJSON(w, 200, map[string]any{"generated": len(items), "model": response.Model})
}

func (s *Server) interactionLearningClose(w http.ResponseWriter, r *http.Request) {
	_, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("sessionID"))
	if !ok {
		return
	}
	if err := s.store.CloseInteractionLearningReview(r.Context(), tenantID, roomID, id); err != nil {
		writeError(w, 409, "这条记录已处理或不存在")
		return
	}
	writeJSON(w, 200, map[string]any{"closed": true})
}

func (s *Server) interactionSimulationDelete(w http.ResponseWriter, r *http.Request) {
	_, tenant, room, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	plan, err := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
	if err != nil || plan <= 0 {
		writeError(w, 400, "请选择当前智能体方案")
		return
	}
	if _, err = s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, 404, "当前方案不存在")
		return
	}
	var id int64
	if raw := r.PathValue("sessionID"); raw != "" {
		id, ok = parsePositivePathID(w, raw)
		if !ok {
			return
		}
	}
	deleted, err := s.store.DeleteSimulationReviews(r.Context(), tenant, room, plan, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 409, "这条模拟建议已处理、已删除或不属于当前方案")
		return
	}
	if err != nil {
		writeError(w, 500, "删除模拟建议失败，请重试")
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": deleted})
}
