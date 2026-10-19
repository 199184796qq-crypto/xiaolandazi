package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func renderLectureTaskMessages(state *anchorcycle.State) ([]agentgateway.Message, error) {
	if err := requireCycleP0(state); err != nil {
		return nil, err
	}
	task := state.Task()
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_GENERATION_PROMPT_PATH", "configs/prompts/anchor-style/lecture-task-generation.md", "任务讲解母版")
	if err != nil {
		return nil, err
	}
	var expression any = "清晰自然的直播口语"
	if state.BaselineJSON != "" {
		expression, err = cycleExpressionView(state)
		if err != nil {
			return nil, err
		}
		if view, ok := expression.(map[string]any); ok {
			delete(view, "数字表达习惯")
		}
	}
	recent := []string{}
	for _, part := range state.Accepted[max(0, len(state.Accepted)-12):] {
		if part.Task.LectureTopic == task.LectureTopic {
			recent = append(recent, tailText(cycleVoiceText(part.Selected), 220))
		}
	}
	packet, _ := json.Marshal(map[string]any{"title": task.SellingPoint, "purpose": task.LecturePurpose, "angle": task.Angle, "facts": task.LectureFacts, "expression": expression, "current_rules": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope), "recent_for_dedup_only": recent, "max_chars": min(220, state.Config.MaxChars), "retry_feedback": tailText(state.PendingFeedback, 700)})
	if task.LecturePlanJSON != "" {
		plan, parseErr := parseLectureSegmentPlan(task.LecturePlanJSON, task.LectureFacts)
		if parseErr != nil {
			return nil, parseErr
		}
		var data map[string]any
		_ = json.Unmarshal(packet, &data)
		data["purpose"], data["angle"], data["segment_plan"] = plan.Purpose, plan.Angle, plan
		// Only the selected angle's verified quotations reach the text generator.
		// The full object remains available to the independent boundary reviewer.
		relation := []string{}
		for _, line := range strings.Split(task.LectureFacts, "\n") {
			if strings.HasPrefix(line, "资料对象：") || strings.HasPrefix(line, "与商品的已确认关系：") {
				relation = append(relation, line)
			}
		}
		data["facts"] = strings.Join(append(relation, plan.Quotes...), "\n")
		packet, _ = json.Marshal(data)
	}
	if task.FactScope != nil || len(state.EffectiveCorrections()) > 0 {
		_, guidance := generationMaterialViews(state.HookFacts())
		var data map[string]any
		_ = json.Unmarshal(packet, &data)
		data["expression_guidance"] = guidance
		data["user_confirmed_corrections"] = state.EffectiveCorrections()
		packet, _ = json.Marshal(data)
	}
	if len(state.Accepted) > 0 {
		last := state.Accepted[len(state.Accepted)-1]
		var data map[string]any
		_ = json.Unmarshal(packet, &data)
		data["transition_from"] = map[string]any{"previous_topic": last.Task.SellingPoint, "previous_ending": tailText(cycleVoiceText(last.Selected), 80)}
		packet, _ = json.Marshal(data)
	}
	return []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(packet)}}, nil
}
func (s *Server) lectureLibraryPreview(w http.ResponseWriter, r *http.Request) {
	_, tenant, plan, ok := s.lectureScope(w, r, false)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("lectureID"), 10, 64)
	var input struct {
		Angle    int   `json:"angle"`
		Revision int64 `json:"revision"`
	}
	if err != nil || readJSON(w, r, &input) != nil {
		writeError(w, 400, "预览请求格式错误")
		return
	}
	lib, err := s.store.LectureLibrary(r.Context(), tenant, plan, false)
	if err != nil {
		writeError(w, 503, "读取讲解任务失败")
		return
	}
	var task anchorcycle.Task
	found := false
	for _, t := range lib.Tasks {
		if t.ID == id && t.Revision == input.Revision && t.Valid && input.Angle >= 0 && input.Angle < len(t.Angles) {
			a := t.Angles[input.Angle]
			task = anchorcycle.Task{SellingPoint: t.Title, Angle: a.Title + "：" + a.Goal, ProductMode: "lecture", LecturePurpose: t.Purpose, LectureFacts: t.Evidence(), LectureTopic: t.ID}
			found = true
			break
		}
	}
	if !found {
		writeError(w, 409, "任务或事实已变化，请刷新后预览")
		return
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil {
		writeError(w, 503, "读取文字模型失败")
		return
	}
	profile := settings.Cycle.TextProfileID
	if profile == "" {
		profile = settings.SellingPointProfileID
	}
	state := anchorcycle.State{TenantID: tenant, PlanID: plan, Config: anchorcycle.Config{MaxChars: 220, TextProfileID: profile, FeedbackID: profile}, Lectures: &anchorcycle.LectureRuntime{Active: &task}}
	if card, cardErr := s.store.GetActiveLiveAnchorStyleScorecard(r.Context(), tenant, plan); cardErr == nil {
		state.BaselineJSON = card.EffectiveScorecardJSON()
	}
	if err = state.InstallP0(p0, "current_mother"); err != nil {
		writeError(w, 503, err.Error())
		return
	}
	messages, err := renderLectureTaskMessages(&state)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	ctx = context.WithValue(ctx, pinnedSpeechProfileKey{}, profile)
	if err = s.ensureLectureSegmentPlan(ctx, &state); err != nil {
		writeError(w, 409, err.Error())
		return
	}
	messages, err = renderLectureTaskMessages(&state)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "lecture_task_preview", agentgateway.ResponseText, messages, 1200, false)
	if err != nil {
		writeError(w, 502, "讲解预览未完成")
		return
	}
	candidate := lectureTextCandidate(result.Text)
	if err = state.CheckP0Candidate(&candidate); err != nil {
		writeJSON(w, 200, lecturePreviewContent(&state, candidate, err.Error()))
		return
	}
	s.reviewCycleP0Context(ctx, &state, &candidate)
	s.auditLectureCandidate(ctx, &state, &candidate)
	writeJSON(w, 200, lecturePreviewContent(&state, candidate, ""))
}

// Establish the final spoken body before review, so checking the same body does
// not change the input identity of its already completed P0 receipt.
func lectureTextCandidate(raw string) anchorcycle.Candidate {
	c := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: strings.TrimSpace(raw)}
	c.OutputText = cycleVoiceText(c)
	return c
}

type lecturePreviewIssue struct {
	Kind   string `json:"kind"`
	Quote  string `json:"quote"`
	Reason string `json:"reason"`
}

// A preview never enters the audio supply. Return the generated body even when
// review is incomplete, with program-owned diagnostics instead of losing it to
// a generic HTTP error. The live candidate still requires both review receipts.
func lecturePreviewContent(state *anchorcycle.State, c anchorcycle.Candidate, scanError string) map[string]any {
	task := state.Task()
	issues := []lecturePreviewIssue{}
	add := func(kind, quote, reason string) { issues = append(issues, lecturePreviewIssue{kind, quote, reason}) }
	if scanError != "" {
		add("review_error", "", scanError)
	} else if c.P0Report == nil {
		add("review_error", "", "文字审核尚未完成")
	} else if !c.P0Report.Passed {
		r := c.P0Report
		if r.ContextReview != nil && r.ContextReview.Output.InstructionLeak != nil && *r.ContextReview.Output.InstructionLeak {
			add("output_rule", r.ContextReview.Output.InstructionQuote, "正文包含内部指导文字")
		} else if r.Error != "" {
			add("review_error", "", r.Error)
		}
		for _, h := range r.Hits {
			if r.Error != "" {
				continue
			} // Pending semantic hits are not confirmed violations.
			reason := h.Condition
			if r.ContextReview != nil {
				for i, original := range r.ContextReview.LexicalHits {
					if original.Start == h.Start && original.End == h.End && i < len(r.ContextReview.Output.Items) {
						reason = r.ContextReview.Output.Items[i].Reason
						break
					}
				}
			}
			add("p0", h.Text, reason)
		}
	}
	if c.LectureAudit != nil && !c.LectureAudit.Passed {
		if c.LectureAudit.Error != "" {
			add("review_error", "", c.LectureAudit.Error)
		} else {
			add("fact_boundary", c.LectureAudit.Quote, c.LectureAudit.Reason)
		}
	} else if c.LectureAudit == nil && scanError == "" {
		add("review_error", "", "讲解事实复核尚未完成")
	}
	if c.ExpressionAudit != nil && !c.ExpressionAudit.Passed {
		if c.ExpressionAudit.Error != "" {
			add("review_error", "", c.ExpressionAudit.Error)
		} else {
			add("expression_constraint", c.ExpressionAudit.Quote, c.ExpressionAudit.Reason)
		}
	}
	approved := scanError == "" && state.CheckApprovedText(c, cycleVoiceText(c)) == nil && state.LectureCandidateApproved(c)
	if !approved && len(issues) == 0 {
		add("review_error", "", "审核回执与当前正文不一致")
	}
	reviewState := "approved"
	if !approved {
		reviewState = "incomplete"
		for _, issue := range issues {
			if issue.Kind != "review_error" {
				reviewState = "rejected"
				break
			}
		}
	}
	notice := "文字预览，未安排播放；生成成功不计讲解次数。"
	if reviewState == "incomplete" {
		notice = "话术已生成，审核暂未完成；这不是内容违规结论，可以重试。"
	}
	if reviewState == "rejected" {
		notice = "这段正文未通过内容审核，请核对具体原因。"
	}
	reviewNote := ""
	if c.LectureAudit != nil && c.LectureAudit.Passed {
		reviewNote = c.LectureAudit.Reason
	}
	suggestions := []anchorcycle.LectureReviewSuggestion{}
	if c.LectureAudit != nil {
		suggestions = append(suggestions, c.LectureAudit.Suggestions...)
	}
	return map[string]any{"text": cycleVoiceText(c), "raw_text": c.Text, "approved": approved, "review_state": reviewState, "issues": issues, "review_suggestions": suggestions, "review_note": reviewNote, "title": task.SellingPoint, "purpose": task.LecturePurpose, "angle": task.Angle, "notice": notice}
}

var errLectureAuditQuoteMismatch = errors.New("审核模型引用未逐字对应当前正文")

func parseLectureTaskAudit(raw, text string) (anchorcycle.LectureAudit, error) {
	var out struct {
		Passed      *bool                                 `json:"passed"`
		Quote       string                                `json:"quote"`
		Reason      string                                `json:"reason"`
		Suggestions []anchorcycle.LectureReviewSuggestion `json:"suggestions"`
	}
	if err := decodeLectureContent(raw, &out); err != nil {
		return anchorcycle.LectureAudit{}, err
	}
	if out.Passed == nil {
		return anchorcycle.LectureAudit{}, fmt.Errorf("审核未返回明确判断")
	}
	// A nonempty explanation is valid content on a passing review, not a protocol
	// failure. A rejection remains a rejection even if it has no quoted fragment.
	if !*out.Passed && strings.TrimSpace(out.Reason) == "" {
		return anchorcycle.LectureAudit{}, fmt.Errorf("未通过的审核缺少原因")
	}
	if out.Quote != "" && !strings.Contains(text, out.Quote) {
		return anchorcycle.LectureAudit{}, errLectureAuditQuoteMismatch
	}
	if len(out.Suggestions) > 8 {
		return anchorcycle.LectureAudit{}, fmt.Errorf("审核意见数量超出范围")
	}
	for _, suggestion := range out.Suggestions {
		if strings.TrimSpace(suggestion.Reason) == "" {
			return anchorcycle.LectureAudit{}, fmt.Errorf("审核意见缺少说明")
		}
		if suggestion.Quote != "" && !strings.Contains(text, suggestion.Quote) {
			return anchorcycle.LectureAudit{}, errLectureAuditQuoteMismatch
		}
	}
	return anchorcycle.LectureAudit{Passed: *out.Passed, Quote: out.Quote, Reason: out.Reason, Suggestions: out.Suggestions}, nil
}

func (s *Server) auditLectureCandidate(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
	if state.Config.SkipReview {
		return
	}
	if err := s.installCurrentExpressionConstraints(ctx, state); err != nil {
		c.P0PatchError = "读取用户采用的表达约束失败"
		return
	}
	s.auditSpeechExpressionConstraints(ctx, state, c)
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID, CandidateID: c.ID})
	task := state.Task()
	if task.ProductMode != "lecture" {
		return
	}
	text := cycleVoiceText(*c)
	audit := &anchorcycle.LectureAudit{TextSHA: anchorcycle.ProductDigest(text), ScopeSHA: anchorcycle.ProductDigest(task.LectureFacts)}
	c.LectureAudit = audit
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_TASK_REVIEW_PATH", "configs/prompts/anchor-style/lecture-task-review.md", "讲解事实边界复核母版")
	if err != nil {
		audit.Error = "讲解复核母版不可用"
		return
	}
	packet := map[string]any{"text": text, "facts": task.LectureFacts, "purpose": task.LecturePurpose, "angle": task.Angle, "effective_p0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)}
	if state.CheckApprovedText(*c, text) == nil {
		packet["matching_p0_review"] = map[string]any{"passed": true, "scope": "当前正文与当前有效P0完全一致的程序及语境核对回执"}
	}
	if task.LecturePlanJSON != "" {
		packet["segment_plan"] = json.RawMessage(task.LecturePlanJSON)
	}
	_, guidance := generationMaterialViews(state.HookFacts())
	packet["expression_guidance"] = guidance
	if task.FactScope != nil || len(state.EffectiveCorrections()) > 0 {
		packet["user_confirmed_corrections"] = state.EffectiveCorrections()
	}
	out, err := boundedLectureAudit(text, func(previous string) (string, error) {
		if previous != "" {
			packet["previous_review_format_error"] = previous
		}
		payload, _ := json.Marshal(packet)
		result, _, callErr := s.completeAnchorStyleWorkbench(ctx, state.Config.FeedbackID, "lecture_task_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}}, 1000, false)
		return result.Text, callErr
	})
	if err != nil {
		audit.Error = "内容审核暂未完成：" + err.Error()
		return
	}
	// The deterministic P0 report remains authoritative. When this broader
	// content reviewer cites P0 on an otherwise clear text, verify the actual
	// supplied clause once; invented rule "spirit" cannot create a new ban.
	if !out.Passed && strings.Contains(out.Reason, "P0") && (c.P0Report == nil || c.P0Report.Passed) {
		packet["previous_rule_rejection"] = map[string]string{"quote": out.Quote, "reason": out.Reason}
		checked, checkErr := boundedLectureAudit(text, func(previous string) (string, error) {
			if previous != "" {
				packet["previous_review_format_error"] = previous
			}
			raw, _ := json.Marshal(packet)
			res, _, e := s.completeAnchorStyleWorkbench(ctx, state.Config.FeedbackID, "lecture_task_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, 1100, false)
			return res.Text, e
		})
		if checkErr != nil {
			audit.Error = "P0条款适用复核未完成"
			return
		}
		out = checked
	}
	audit.Passed, audit.Quote, audit.Reason, audit.Suggestions = out.Passed, out.Quote, out.Reason, out.Suggestions
}

// Retry only a mismatched optional quotation, never invent a passing judgment.
func boundedLectureAudit(text string, call func(string) (string, error)) (anchorcycle.LectureAudit, error) {
	previous := ""
	for attempt := 0; attempt < 2; attempt++ {
		raw, err := call(previous)
		if err != nil {
			return anchorcycle.LectureAudit{}, err
		}
		out, err := parseLectureTaskAudit(raw, text)
		if err == nil {
			return out, nil
		}
		if !errors.Is(err, errLectureAuditQuoteMismatch) || attempt == 1 {
			return anchorcycle.LectureAudit{}, err
		}
		previous = raw
	}
	return anchorcycle.LectureAudit{}, errLectureAuditQuoteMismatch
}
