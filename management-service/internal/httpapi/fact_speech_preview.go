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
	"livecompanion/management/internal/lecturetask"
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
	lib, err := s.store.LectureLibrary(ctx, tenant, plan, false)
	if err != nil {
		return anchorcycle.State{}, errors.New("读取二级资料关联失败")
	}
	lecture, err := selectedFactLecture(lib, f)
	if err != nil {
		return anchorcycle.State{}, err
	}
	if lecture != nil {
		task = factLecturePreviewTask(*lecture, task.FactScope)
	}
	state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{MaxChars: 220})
	if err != nil {
		return state, err
	}
	state.TenantID, state.PlanID, state.RoomID = tenant, plan, room
	state.Surface, state.Phase = factPreviewSurface, "body"
	state.Facts = task.FactScope.Facts[0].Data
	if lecture != nil {
		state.Phase = "lecture"
		state.Lectures = &anchorcycle.LectureRuntime{Active: &task, Library: lecturetask.Library{Tasks: []lecturetask.Task{*lecture}}}
	}
	card, err := s.store.GetActiveLiveAnchorStyleScorecard(ctx, tenant, plan)
	if err != nil {
		return state, errors.New("请先在当前方案确认主播风格，再预览直播话术")
	}
	state.BaselineJSON = card.EffectiveScorecardJSON()
	if _, err = cycleExpressionView(&state); err != nil {
		return state, errors.New("当前主播风格标尺不可用，请重新确认主播风格")
	}
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
	if lecture == nil {
		mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_SPEECH_PREVIEW_PROMPT_PATH", "configs/prompts/anchor-style/fact-speech-preview.md", "事实话术预览母版")
		if err != nil {
			return state, err
		}
		state.TextModel.SystemPrompt = strings.TrimSpace(state.TextModel.SystemPrompt + "\n\n" + mother)
	}
	stage := "围绕所选事实讲品"
	if lecture != nil {
		stage = "围绕所选资料对象进行任务讲解"
	}
	contextData, _ := json.Marshal(map[string]string{"模拟直播间": roomName, "当前环节": stage})
	state.PendingFeedback = string(contextData)
	return state, nil
}

func factPreviewReviewState(state anchorcycle.State) anchorcycle.State {
	if state.Task().ProductMode == "lecture" {
		return state
	}
	// Use the existing content-boundary reviewer for this single fact, while
	// generation itself still uses the normal mainline text/angle pipeline.
	task := state.Task()
	task.ProductMode, task.LectureFacts = "lecture", state.Facts
	task.LecturePurpose = task.SellingPoint
	state.Lectures = &anchorcycle.LectureRuntime{Active: &task}
	return state
}

func (s *Server) generateFactSpeechPreview(ctx context.Context, state *anchorcycle.State) (map[string]any, error) {
	feedback := state.PendingFeedback
	defer func() { state.PendingFeedback = feedback }()
	history := []map[string]any{}
	for attempt := 0; attempt < 2; attempt++ {
		output, err := s.generateFactSpeechPreviewOnce(ctx, state)
		if err != nil {
			return nil, err
		}
		if output["review_state"] != "rejected" || attempt == 1 {
			if len(history) > 0 {
				output["previous_attempts"] = history
			}
			return output, nil
		}
		history = append(history, map[string]any{"text": output["text"], "issues": output["issues"]})
		ctx = context.WithValue(ctx, factPreviewRepairKey{}, map[string]any{"text": output["text"], "issues": output["issues"]})
	}
	return nil, errors.New("话术预览未完成")
}

func (s *Server) generateFactSpeechPreviewOnce(ctx context.Context, state *anchorcycle.State) (map[string]any, error) {
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	result, _, err := s.completeCycleText(ctx, state)
	if err != nil {
		if errors.Is(err, errLecturePlanning) {
			return nil, err
		}
		return nil, errors.New("话术生成未完成，请重试或检查当前口播模型连接")
	}
	if strings.TrimSpace(result.Text) == "" {
		return nil, errors.New("模型未返回话术，请重新生成")
	}
	candidate := lectureTextCandidate(result.Text)
	reviewState := factPreviewReviewState(*state)
	scanError := ""
	if err = state.CheckP0Candidate(&candidate); err != nil {
		scanError = err.Error()
	} else {
		s.reviewCycleP0Context(ctx, state, &candidate)
		s.auditLectureCandidate(ctx, &reviewState, &candidate)
	}
	output := lecturePreviewContent(&reviewState, candidate, scanError)
	rules := []map[string]string{}
	for _, rule := range state.LatestExpressionConstraints {
		feedback, _ := rule["用户原话（理解纠正范围，不能扩张限制）"].(string)
		constraint, _ := rule["表达限制"].(string)
		rules = append(rules, map[string]string{"feedback": feedback, "constraint": constraint})
	}
	output["expression_constraints"] = rules
	if state.Task().LecturePlanJSON != "" {
		if plan, e := parseLectureSegmentPlan(state.Task().LecturePlanJSON, state.Task().LectureFacts); e == nil {
			output["purpose"], output["angle"] = plan.Purpose, plan.Angle
		}
	}
	output["model_alias"], output["model_name"] = state.TextModel.Name, state.TextModel.Model
	output["preview_kind"] = "product"
	if state.Task().ProductMode == "lecture" {
		output["preview_kind"] = "lecture"
		if state.Lectures != nil && len(state.Lectures.Library.Tasks) == 1 {
			t := state.Lectures.Library.Tasks[0]
			output["subject"] = t.Subject
			details := []map[string]string{}
			for _, f := range t.Facts {
				details = append(details, map[string]string{"name": f.Name, "text": f.Text})
			}
			output["secondary_facts"] = details
		}
	}
	output["notice"] = "使用当前方案和真实口播模型生成，仅预览文字。"
	if output["review_state"] == "incomplete" {
		output["notice"] = "话术已生成，审核暂未完成；这不是内容违规结论，可以重新生成后重试审核。"
	}
	if output["review_state"] == "rejected" {
		output["notice"] = "话术未通过内容审核，请核对下方具体原因。"
	}
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
	if state.Task().ProductMode == "lecture" {
		lib, err := s.store.LectureLibrary(ctx, tenant, plan, false)
		now, selectErr := selectedFactLecture(lib, fact)
		previous := state.Lectures.Library.Tasks[0]
		if err != nil || selectErr != nil || now == nil || now.ID != previous.ID || now.Revision != previous.Revision || now.Evidence() != previous.Evidence() {
			writeError(w, 409, "生成期间二级资料已变化，请刷新后重新预览")
			return
		}
	}
	output["fact_id"], output["version_no"], output["room_name"], output["plan_name"] = id, input.VersionNo, room.Name, planData.Name
	writeJSON(w, 200, output)
}
