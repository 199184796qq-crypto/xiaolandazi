package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type speechConstraintContent struct {
	Understanding *string `json:"understanding"`
	Constraint    *string `json:"constraint"`
	Question      *string `json:"question"`
}
type speechConstraintDraft struct {
	Actor      int64                   `json:"actor"`
	FactID     int64                   `json:"fact_id"`
	Version    int64                   `json:"version_no"`
	SourceSHA  string                  `json:"source_sha"`
	Revision   int64                   `json:"revision"`
	Correction contentpools.Correction `json:"correction"`
}

func parseSpeechConstraint(raw string) (speechConstraintContent, error) {
	var c speechConstraintContent
	if decodeLectureContent(raw, &c) != nil || c.Understanding == nil || c.Constraint == nil || c.Question == nil {
		return c, errors.New("约束整理未完成，请重试")
	}
	for _, v := range []*string{c.Understanding, c.Constraint, c.Question} {
		*v = strings.TrimSpace(*v)
		if utf8.RuneCountInString(*v) > 4000 {
			return c, errors.New("约束内容过长")
		}
	}
	if *c.Understanding == "" || (*c.Constraint == "" && *c.Question == "") {
		return c, errors.New("未整理出可执行的约束")
	}
	return c, nil
}
func (s *Server) normalizeSpeechConstraint(ctx context.Context, tenant, plan int64, f model.LiveAgentPlanFact, selected, feedback string, source contentpools.Sources) (speechConstraintContent, error) {
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_SPEECH_CONSTRAINT_PATH", "configs/prompts/anchor-style/speech-constraint-normalize.md", "话术约束整理母版")
	if err != nil {
		return speechConstraintContent{}, err
	}
	review, _, err := loadReplaceablePromptAsset("ANCHOR_SPEECH_CONSTRAINT_REVIEW_PATH", "configs/prompts/anchor-style/speech-constraint-review.md", "话术约束复核母版")
	if err != nil {
		return speechConstraintContent{}, err
	}
	state := anchorcycle.State{TenantID: tenant, PlanID: plan}
	if err = installP0Mother(&state); err != nil {
		return speechConstraintContent{}, err
	}
	if err = s.installPlanP0Allowances(ctx, &state); err != nil {
		return speechConstraintContent{}, err
	}
	ids := []int64{}
	labels := []string{}
	for _, p := range source.Products {
		ids = append(ids, p.ID)
		labels = append(labels, p.Link+"号 · "+p.Name)
	}
	packet := map[string]any{"user_request": feedback, "selected_speech": selected, "confirmed_fact": factFieldsContent(model.LiveAgentPlanFactCandidate{Key: f.Key, Value: f.Value, ForbiddenWording: f.ForbiddenWording, SafeRewrite: f.SafeRewrite}), "applicable_products": labels, "existing_constraints": contentpools.CorrectionView(source, ids), "current_rules": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	for attempt := 0; attempt < 2; attempt++ {
		raw, _ := json.Marshal(packet)
		response, _, e := s.completeAnchorStyleWorkbench(ctx, "", "speech_constraint_normalize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, 1600, false)
		if e != nil {
			return speechConstraintContent{}, errors.New("约束整理模型暂时不可用，请重试")
		}
		c, e := parseSpeechConstraint(response.Text)
		if e != nil {
			return c, e
		}
		if *c.Question != "" {
			return c, nil
		}
		packet["draft"] = c
		raw, _ = json.Marshal(packet)
		checked, _, e := s.completeAnchorStyleWorkbench(ctx, "", "speech_constraint_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: review}, {Role: "user", Content: string(raw)}}, 1100, false)
		if e != nil {
			return c, errors.New("约束复核未完成，尚未保存")
		}
		if e = normalizationReviewPassed(checked.Text); e == nil {
			return c, nil
		}
		if attempt == 1 {
			return c, errors.New(strings.ReplaceAll(e.Error(), "三栏整理", "表达约束"))
		}
		packet["previous_review_rejection"] = checked.Text
		delete(packet, "draft")
	}
	return speechConstraintContent{}, errors.New("约束整理未完成")
}

// Shared authorization and optimistic fact selection; no model owns scope IDs.
func (s *Server) speechConstraintAccess(w http.ResponseWriter, r *http.Request, in struct{ TenantID, RoomID, VersionNo int64 }) (tenant, plan, actor int64, f model.LiveAgentPlanFact, ok bool) {
	a, allowed := s.resolveActor(w, r)
	if !allowed {
		return
	}
	plan, allowed = liveAgentPlanPathID(w, r)
	if !allowed {
		return
	}
	id, allowed := liveAgentPlanFactPathID(w, r)
	if !allowed {
		return
	}
	tenant, allowed = s.resolveLiveAgentPlanTenant(w, r, a, in.TenantID, true)
	if !allowed {
		return
	}
	if !s.requireLiveSupportPlanScope(w, r, a, tenant, plan, true) {
		return
	}
	rt, allowed := s.tenantForRoom(w, r, a, in.RoomID)
	if !allowed {
		return
	}
	if rt != tenant {
		writeError(w, 403, "直播间与方案范围不一致")
		return
	}
	p, e := s.store.GetLiveAgentPlan(r.Context(), tenant, plan)
	if e != nil || p.Status != "active" {
		writeError(w, 404, "方案不存在或已停用")
		return
	}
	items, e := s.store.ListLiveAgentPlanFacts(r.Context(), tenant, plan)
	if e != nil {
		writeError(w, 503, "读取事实失败")
		return
	}
	f, allowed = selectedPreviewFact(items, tenant, plan, id, in.VersionNo)
	if !allowed {
		writeError(w, 409, "事实已修改或删除，请刷新后重试")
		return
	}
	actor = a.UserID
	ok = true
	return
}
func (s *Server) speechConstraintPrepare(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	var in struct {
		TenantID     int64  `json:"tenant_id"`
		RoomID       int64  `json:"room_id"`
		VersionNo    int64  `json:"version_no"`
		SelectedText string `json:"selected_text"`
		Feedback     string `json:"feedback"`
	}
	if readJSON(w, r, &in) != nil || in.RoomID <= 0 || in.VersionNo <= 0 || strings.TrimSpace(in.Feedback) == "" || utf8.RuneCountInString(in.Feedback) > 5000 || strings.TrimSpace(in.SelectedText) == "" || utf8.RuneCountInString(in.SelectedText) > 8000 {
		writeError(w, 400, "请输入这段话术需要遵守的要求")
		return
	}
	tenant, plan, actor, f, ok := s.speechConstraintAccess(w, r, struct{ TenantID, RoomID, VersionNo int64 }{in.TenantID, in.RoomID, in.VersionNo})
	if !ok {
		return
	}
	source, e := s.captureContentSources(r.Context(), tenant, plan)
	if e != nil || len(source.Products) == 0 {
		writeError(w, 409, "当前方案没有可确定的商品范围，请先完善方案商品")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan, RoomID: in.RoomID})
	c, e := s.normalizeSpeechConstraint(ctx, tenant, plan, f, in.SelectedText, in.Feedback, source)
	if e != nil {
		writeError(w, 422, e.Error())
		return
	}
	if *c.Question != "" {
		writeJSON(w, 200, map[string]any{"state": "clarify", "understanding": *c.Understanding, "question": *c.Question})
		return
	}
	draft := speechConstraintDraft{Actor: actor, FactID: f.ID, Version: f.VersionNo, SourceSHA: source.SHA(), Revision: source.Revision, Correction: contentpools.Correction{ID: anchorcycle.ID(), Kind: contentpools.ExpressionConstraintKind, Original: in.SelectedText, Feedback: strings.TrimSpace(in.Feedback), Sources: []string{fmtKnowledgeID(f.ID)}, Content: contentpools.CorrectionContent{Summary: *c.Understanding, Forbidden: *c.Constraint}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	labels := []string{}
	for _, p := range source.Products {
		draft.Correction.Products = append(draft.Correction.Products, p.ID)
		labels = append(labels, p.Link+"号 · "+p.Name)
	}
	raw, _ := json.Marshal(draft)
	key := derivedHash(draft.Correction.ID)
	if e = s.store.SavePlanAnalysisAsset(ctx, tenant, plan, "speech_constraint_draft", key, string(raw)); e != nil {
		writeError(w, 503, "保存待采用约束失败，本次没有改变规则")
		return
	}
	writeJSON(w, 200, map[string]any{"state": "draft", "draft_id": key, "understanding": *c.Understanding, "constraint": *c.Constraint, "scope": labels})
}
func fmtKnowledgeID(id int64) string { return "knowledge:" + strconv.FormatInt(id, 10) }
func (s *Server) speechConstraintAdopt(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	var in struct {
		TenantID  int64  `json:"tenant_id"`
		RoomID    int64  `json:"room_id"`
		VersionNo int64  `json:"version_no"`
		DraftID   string `json:"draft_id"`
	}
	if readJSON(w, r, &in) != nil || in.RoomID <= 0 || in.VersionNo <= 0 || len(in.DraftID) != 64 {
		writeError(w, 400, "请先生成待采用约束")
		return
	}
	tenant, plan, actor, f, ok := s.speechConstraintAccess(w, r, struct{ TenantID, RoomID, VersionNo int64 }{in.TenantID, in.RoomID, in.VersionNo})
	if !ok {
		return
	}
	raw, e := s.store.GetPlanAnalysisAsset(r.Context(), tenant, plan, "speech_constraint_draft", in.DraftID)
	var d speechConstraintDraft
	if e != nil || json.Unmarshal([]byte(raw), &d) != nil || d.Actor != actor || d.FactID != f.ID || d.Version != f.VersionNo || derivedHash(d.Correction.ID) != in.DraftID {
		writeError(w, 409, "待采用约束不属于当前事实或用户，请重新生成")
		return
	}
	source, e := s.captureContentSources(r.Context(), tenant, plan)
	if e != nil {
		writeError(w, 503, "读取约束范围失败")
		return
	}
	already := false
	for _, c := range source.Corrections {
		already = already || c.ID == d.Correction.ID
	}
	if !already && (source.SHA() != d.SourceSHA || source.Revision != d.Revision) {
		writeError(w, 409, "生成约束后方案资料已改变，请重新整理；尚未采用")
		return
	}
	if e = s.store.AdoptSpeechExpressionConstraint(r.Context(), tenant, plan, actor, f.ID, f.VersionNo, d.Revision, d.Correction); e != nil {
		writeError(w, 409, "约束未采用，事实或方案已改变，请刷新后重试")
		return
	}
	writeJSON(w, 200, map[string]any{"state": "adopted", "constraint": d.Correction.Content.Forbidden, "notice": "已采用并记录，后续新生成的话术和互动回答会遵守这条约束。"})
}
