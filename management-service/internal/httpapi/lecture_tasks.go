package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type lectureIssue struct {
	Quote      string `json:"quote"`
	Rule       string `json:"rule"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
}
type lectureReview struct {
	Passed *bool          `json:"passed"`
	Issues []lectureIssue `json:"issues"`
}
type lectureOrganized struct {
	Subject     string `json:"subject"`
	ProductFact *struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Quote    string `json:"quote"`
	} `json:"product_fact"`
	QuestionTerms []string            `json:"question_terms"`
	Title         string              `json:"title"`
	Purpose       string              `json:"purpose"`
	Angles        []lecturetask.Angle `json:"angles"`
	Facts         []struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Quote    string `json:"quote"`
	} `json:"facts"`
}

func decodeLectureContent(raw string, out any) error {
	d := json.NewDecoder(strings.NewReader(stripPolicyJSONFence(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("模型返回包含额外内容")
	}
	return nil
}
func parseLectureReview(raw, source string) (lectureReview, error) {
	var out lectureReview
	if err := decodeLectureContent(raw, &out); err != nil {
		return out, err
	}
	if out.Passed == nil || out.Issues == nil || (*out.Passed && len(out.Issues) > 0) || (!*out.Passed && len(out.Issues) == 0) || len(out.Issues) > 30 {
		return out, errors.New("审核结果不完整")
	}
	for _, v := range out.Issues {
		if strings.TrimSpace(v.Quote) == "" || !strings.Contains(source, v.Quote) || v.Rule == "" || v.Reason == "" || utf8.RuneCountInString(v.Suggestion) > 2000 {
			return out, errors.New("审核问题未对应原文")
		}
	}
	return out, nil
}
func parseLectureOrganized(raw, source string, catalog factCategoryCatalog) (lecturetask.Task, []model.LiveAgentPlanFactCandidate, error) {
	var out lectureOrganized
	var task lecturetask.Task
	if err := decodeLectureContent(raw, &out); err != nil {
		return task, nil, err
	}
	task.Title, task.Purpose, task.Angles = strings.TrimSpace(out.Title), strings.TrimSpace(out.Purpose), out.Angles
	task.Subject = strings.TrimSpace(out.Subject)
	if task.Subject == "" {
		task.Subject = task.Title
	}
	if utf8.RuneCountInString(task.Subject) > 100 {
		return task, nil, errors.New("资料对象名称过长")
	}
	if f := out.ProductFact; f != nil {
		if !catalog.contains(f.Category) || f.Name == "" || f.Quote == "" || utf8.RuneCountInString(f.Name) > 100 || utf8.RuneCountInString(f.Quote) > 1000 || !strings.Contains(source, f.Quote) {
			return task, nil, errors.New("商品关联事实未对应原文")
		}
		task.RootFact = &lecturetask.Fact{Category: f.Category, Name: f.Name, Text: f.Quote, Quote: f.Quote}
	}
	task.QuestionTerms = out.QuestionTerms
	if len(task.QuestionTerms) < 1 || len(task.QuestionTerms) > 12 {
		return task, nil, errors.New("相关问题措辞不完整")
	}
	for _, term := range task.QuestionTerms {
		if utf8.RuneCountInString(term) < 2 || utf8.RuneCountInString(term) > 30 {
			return task, nil, errors.New("相关问题措辞格式错误")
		}
	}
	if task.Title == "" || utf8.RuneCountInString(task.Title) > 100 || task.Purpose == "" || utf8.RuneCountInString(task.Purpose) > 600 || len(task.Angles) < 1 || len(task.Angles) > 8 || len(out.Facts) < 1 || len(out.Facts) > 20 {
		return task, nil, errors.New("没有形成完整的事实与讲解主题，请补充具体素材")
	}
	for _, a := range task.Angles {
		if a.Title == "" || a.Goal == "" || utf8.RuneCountInString(a.Title) > 80 || utf8.RuneCountInString(a.Goal) > 600 {
			return task, nil, errors.New("讲解角度不完整")
		}
	}
	facts := []model.LiveAgentPlanFactCandidate{}
	seen := map[string]bool{}
	for _, f := range out.Facts {
		key := f.Category + "\x00" + f.Name
		if !catalog.contains(f.Category) || f.Name == "" || utf8.RuneCountInString(f.Name) > 100 || f.Quote == "" || utf8.RuneCountInString(f.Quote) > 1000 || !strings.Contains(source, f.Quote) || seen[key] {
			return task, nil, errors.New("事实分类、原文出处或名称无效，未保存任何内容")
		}
		seen[key] = true
		facts = append(facts, model.LiveAgentPlanFactCandidate{Category: f.Category, Key: f.Name, Value: f.Quote, SourceQuote: f.Quote, Status: "active"})
	}
	return task, facts, nil
}

func (s *Server) lectureScope(w http.ResponseWriter, r *http.Request, write bool) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), write)
	if !ok {
		return actor, 0, 0, false
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, 404, "方案不存在或无权访问")
		return actor, 0, 0, false
	}
	return actor, tenant, plan, true
}
func (s *Server) lectureLibraryGet(w http.ResponseWriter, r *http.Request) {
	_, tenant, plan, ok := s.lectureScope(w, r, false)
	if !ok {
		return
	}
	lib, err := s.store.LectureLibrary(r.Context(), tenant, plan, true)
	if err != nil {
		writeError(w, 503, "读取讲解库失败")
		return
	}
	writeJSON(w, 200, lib)
}
func (s *Server) lectureLibraryPolicy(w http.ResponseWriter, r *http.Request) {
	_, tenant, plan, ok := s.lectureScope(w, r, true)
	if !ok {
		return
	}
	var p lecturetask.Policy
	if readJSON(w, r, &p) != nil || p.Validate() != nil {
		writeError(w, 400, "讲解设置无效")
		return
	}
	if err := s.store.SaveLecturePolicy(r.Context(), tenant, plan, p); err != nil {
		writeError(w, 503, "保存讲解设置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
func (s *Server) lectureLibraryMutate(w http.ResponseWriter, r *http.Request) {
	_, tenant, plan, ok := s.lectureScope(w, r, true)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("lectureID"), 10, 64)
	var input struct {
		Action   string `json:"action"`
		Revision int64  `json:"revision"`
	}
	if err != nil || id <= 0 || readJSON(w, r, &input) != nil {
		writeError(w, 400, "讲解操作格式错误")
		return
	}
	err = s.store.MutateLecture(r.Context(), tenant, plan, id, input.Revision, input.Action)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) lectureLibraryIngest(w http.ResponseWriter, r *http.Request) {
	actor, tenant, plan, ok := s.lectureScope(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Source string   `json:"source"`
		Links  []string `json:"links"`
	}
	if readJSON(w, r, &input) != nil || strings.TrimSpace(input.Source) == "" || utf8.RuneCountInString(input.Source) > 8000 || len(input.Links) > 50 {
		writeError(w, 400, "请填写1—8000字故事素材")
		return
	}
	input.Source = strings.TrimSpace(input.Source)
	links, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenant, plan)
	if err != nil {
		writeError(w, 503, "读取商品范围失败")
		return
	}
	allowed := map[string]bool{}
	for _, v := range links {
		if v.Status == "active" {
			allowed[v.LinkKey] = true
		}
	}
	seen := map[string]bool{}
	for _, v := range input.Links {
		if !allowed[v] || seen[v] {
			writeError(w, 400, "故事适用商品不属于当前方案")
			return
		}
		seen[v] = true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	source, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		writeError(w, 503, "读取当前业务规则和事实失败")
		return
	}
	catalog, _, err := s.tenantFactCategoryCatalog(ctx, tenant)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		writeError(w, 503, "读取模型配置失败")
		return
	}
	profile := settings.SafetyProfileID
	if profile == "" {
		profile = settings.SellingPointProfileID
	}
	if profile == "" {
		writeError(w, 409, "请先配置系统智能体模型")
		return
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/lecture-source-review.md", "故事审核母版")
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	payload, _ := json.Marshal(map[string]any{"business_scope": "当前方案的商品素材", "current_rules": []any{generationP0View(p0, "*"), p1, p2}, "current_facts": source.Facts})
	userSource, _ := json.Marshal(map[string]any{"source_text": input.Source})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "lecture_source_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}, {Role: "user", Content: string(userSource)}}, 2600, false)
	if err != nil {
		writeError(w, 502, "故事审核未完成，请稍后重试；没有内容入库")
		return
	}
	review, err := parseLectureReview(result.Text, input.Source)
	if err != nil {
		writeError(w, 502, "审核格式错误："+err.Error())
		return
	}
	if !*review.Passed {
		writeJSON(w, 200, map[string]any{"saved": false, "issues": review.Issues})
		return
	}
	mother, _, err = loadReplaceablePromptAsset("ANCHOR_LECTURE_ORGANIZE_PROMPT_PATH", "configs/prompts/anchor-style/lecture-source-organize.md", "故事整理母版")
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	payload, _ = json.Marshal(map[string]any{"source_text": input.Source, "available_fact_categories": catalog.Categories, "effective_p0": generationP0View(p0, "*")})
	result, _, err = s.completeAnchorStyleWorkbench(ctx, profile, "lecture_source_organize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}}, 4000, false)
	if err != nil {
		writeError(w, 502, "故事整理未完成，请稍后重试；没有内容入库")
		return
	}
	task, facts, err := parseLectureOrganized(result.Text, input.Source, catalog)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	task.Links = input.Links
	// A slow model result cannot commit against a different source/rule snapshot.
	nowP0, nowP1, nowP2, e := loadRiskAnnotationMothers()
	if e != nil || nowP0 != p0 || nowP1 != p1 || nowP2 != p2 {
		writeError(w, 409, "审核期间规则已更新，请重新提交")
		return
	}
	nowSource, e := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if e != nil || nowSource.Facts != source.Facts {
		writeError(w, 409, "审核期间方案事实已变化，请重新提交")
		return
	}
	reviewJSON, _ := json.Marshal(review)
	id, added, err := s.store.SaveLectureSubmission(ctx, tenant, plan, actor.UserID, input.Source, string(reviewJSON), task, facts)
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"saved": true, "task_id": id, "facts_added": added, "details_added": len(facts), "subject": task.Subject, "title": task.Title, "angles": task.Angles, "notice": "商品关联事实已保存，详细资料归入对应资料对象，未加入成品事实；保存智能体方案后，下次开播按需讲解。"})
}
