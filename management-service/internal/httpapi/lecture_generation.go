package httpapi

import (
	"context"
	"encoding/json"
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
	packet, _ := json.Marshal(map[string]any{"title": task.SellingPoint, "purpose": task.LecturePurpose, "angle": task.Angle, "facts": task.LectureFacts, "expression": expression, "current_rules": state.P0PolicyMD, "recent_for_dedup_only": recent, "max_chars": min(220, state.Config.MaxChars), "retry_feedback": tailText(state.PendingFeedback, 700)})
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
	result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "lecture_task_preview", agentgateway.ResponseText, messages, 1200, false)
	if err != nil {
		writeError(w, 502, "讲解预览未完成")
		return
	}
	candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: strings.TrimSpace(result.Text)}
	if err = state.CheckP0Candidate(&candidate); err != nil {
		writeError(w, 502, err.Error())
		return
	}
	s.reviewCycleP0Context(ctx, &state, &candidate)
	s.auditLectureCandidate(ctx, &state, &candidate)
	if candidate.P0Report == nil || !candidate.P0Report.Passed || !state.LectureCandidateApproved(candidate) {
		writeError(w, 409, "本次预览未通过当前文字审核，请检查事实或换个角度后再试")
		return
	}
	writeJSON(w, 200, map[string]any{"text": cycleVoiceText(candidate), "title": task.SellingPoint, "purpose": task.LecturePurpose, "angle": task.Angle, "notice": fmt.Sprintf("当前角度预览，未安排播放。")})
}

func (s *Server) auditLectureCandidate(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
	task := state.Task()
	if task.ProductMode != "lecture" {
		return
	}
	text := cycleVoiceText(*c)
	c.OutputText = text
	audit := &anchorcycle.LectureAudit{TextSHA: anchorcycle.ProductDigest(text), ScopeSHA: anchorcycle.ProductDigest(task.LectureFacts)}
	c.LectureAudit = audit
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_TASK_REVIEW_PATH", "configs/prompts/anchor-style/lecture-task-review.md", "讲解事实边界复核母版")
	if err != nil {
		audit.Reason = "讲解复核母版不可用"
		return
	}
	payload, _ := json.Marshal(map[string]any{"text": text, "facts": task.LectureFacts, "purpose": task.LecturePurpose, "angle": task.Angle, "effective_p0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope)})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, state.Config.FeedbackID, "lecture_task_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}}, 1000, false)
	if err != nil {
		audit.Reason = "讲解事实复核未完成"
		return
	}
	var out struct {
		Passed *bool  `json:"passed"`
		Quote  string `json:"quote"`
		Reason string `json:"reason"`
	}
	err = decodeLectureContent(result.Text, &out)
	if err != nil || out.Passed == nil || (!*out.Passed && (out.Quote == "" || out.Reason == "" || !strings.Contains(text, out.Quote))) || (*out.Passed && (out.Quote != "" || out.Reason != "")) {
		audit.Reason = "讲解事实复核格式无效"
		return
	}
	audit.Passed, audit.Quote, audit.Reason = *out.Passed, out.Quote, out.Reason
}
