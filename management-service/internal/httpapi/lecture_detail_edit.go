package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/lecturetask"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) normalizeLectureDetail(ctx context.Context, state *anchorcycle.State, task lecturetask.Task, f lecturetask.Fact, name, text string) (string, string, string, error) {
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_DETAIL_NORMALIZE_PATH", "configs/prompts/anchor-style/lecture-detail-normalize.md", "二级资料整理母版")
	if err != nil {
		return "", "", "", err
	}
	reviewMother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_DETAIL_REVIEW_PATH", "configs/prompts/anchor-style/lecture-detail-normalize-review.md", "二级资料复核母版")
	if err != nil {
		return "", "", "", err
	}
	details := []map[string]string{}
	for _, other := range task.Facts {
		if other.ID != f.ID || other.Storage != f.Storage {
			details = append(details, map[string]string{"name": other.Name, "text": other.Text})
		}
	}
	relation := ""
	if task.RootFact != nil {
		relation = task.RootFact.Text
	}
	allowances := []map[string]string{}
	for _, a := range state.P0Allowances {
		allowances = append(allowances, map[string]string{"已确认的相关表述": a.MatchedText, "确认判断": a.Decision})
	}
	packet := map[string]any{"original": map[string]string{"name": name, "text": text}, "subject": task.Subject, "product_relation": relation, "other_details": details, "effective_p0": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope), "已确认规则判断": allowances}
	raw, _ := json.Marshal(packet)
	result, _, err := s.completeAnchorStyleWorkbench(ctx, "", "lecture_detail_normalize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, 3000, false)
	if err != nil {
		return "", "", "", errors.New("二级资料整理未完成，原输入未保存")
	}
	var out struct {
		Name     *string `json:"name"`
		Text     *string `json:"text"`
		Question *string `json:"question"`
	}
	if decodeLectureContent(result.Text, &out) != nil || out.Name == nil || out.Text == nil || out.Question == nil {
		return "", "", "", errors.New("二级资料整理结果不完整，未保存")
	}
	if strings.TrimSpace(*out.Question) != "" {
		return "", "", "", errors.New("请修改后保存：" + *out.Question)
	}
	name, text = strings.TrimSpace(*out.Name), strings.TrimSpace(*out.Text)
	if name == "" || text == "" || utf8.RuneCountInString(name) > 100 || utf8.RuneCountInString(text) > 4000 {
		return "", "", "", errors.New("资料名称或内容为空或过长，未保存")
	}
	packet["normalized"] = map[string]string{"name": name, "text": text}
	raw, _ = json.Marshal(packet)
	result, _, err = s.completeAnchorStyleWorkbench(ctx, "", "lecture_detail_normalize_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: reviewMother}, {Role: "user", Content: string(raw)}}, 1300, false)
	if err != nil {
		return "", "", "", errors.New("二级资料复核未完成，未保存")
	}
	if err = normalizationReviewPassed(result.Text); err != nil {
		return "", "", "", err
	}
	return name, text, derivedHash(mother + reviewMother + state.P0PolicyMD), nil
}

func (s *Server) lectureDetailUpdate(w http.ResponseWriter, r *http.Request) {
	actor, tenant, plan, ok := s.lectureScope(w, r, true)
	if !ok {
		return
	}
	id, e := strconv.ParseInt(r.PathValue("lectureID"), 10, 64)
	var input struct {
		Revision int64  `json:"revision"`
		DetailID int64  `json:"detail_id"`
		Storage  string `json:"storage"`
		Name     string `json:"name"`
		Text     string `json:"text"`
	}
	if e != nil || id <= 0 || readJSON(w, r, &input) != nil || input.Revision <= 0 || input.DetailID <= 0 || (input.Storage != "" && input.Storage != "lecture") || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Text) == "" || utf8.RuneCountInString(input.Name) > 100 || utf8.RuneCountInString(input.Text) > 4000 {
		writeError(w, 400, "请填写资料名称和1—4000字已确认资料")
		return
	}
	lib, err := s.store.LectureLibrary(r.Context(), tenant, plan, true)
	if err != nil {
		writeError(w, 503, "读取当前对象资料失败")
		return
	}
	var task *lecturetask.Task
	var detail *lecturetask.Fact
	for _, t := range lib.Tasks {
		if t.ID == id && t.Revision == input.Revision && t.Subject != "" {
			copy := t
			task = &copy
			for _, f := range t.Facts {
				if f.ID == input.DetailID && f.Storage == input.Storage {
					copy := f
					detail = &copy
					break
				}
			}
			break
		}
	}
	if task == nil || detail == nil {
		writeError(w, 409, "对象资料已变化或不属于当前方案，请刷新后再修改")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, PlanID: plan})
	state := anchorcycle.State{TenantID: tenant, PlanID: plan}
	if err = installP0Mother(&state); err == nil {
		err = s.installPlanP0Allowances(ctx, &state)
	}
	if err != nil {
		writeError(w, 503, "读取当前规则失败，未保存")
		return
	}
	name, text, sha, err := s.normalizeLectureDetail(ctx, &state, *task, *detail, input.Name, input.Text)
	if err != nil {
		writeError(w, 422, err.Error())
		return
	}
	archiveID := anchorcycle.ID()
	archive, _ := json.Marshal(map[string]any{"original_task": task, "original_detail": detail, "input": input, "normalized": map[string]string{"name": name, "text": text}, "actor_user_id": actor.UserID, "policy_sha256": sha, "operation": "lecture_detail_edit_before_save"})
	if err = s.store.SavePlanAnalysisAsset(ctx, tenant, plan, "lecture_detail_edit", derivedHash(archiveID), string(archive)); err != nil {
		writeError(w, 503, "保存原始资料记录失败，未修改")
		return
	}
	if err = s.store.UpdateLectureDetail(ctx, tenant, plan, actor.UserID, id, input.Revision, input.DetailID, input.Storage, name, text, "lecture-detail-edit:"+archiveID); err != nil {
		writeError(w, 409, "资料已被修改，请刷新核对；本次输入未覆盖当前资料")
		return
	}
	writeJSON(w, 200, map[string]any{"saved": true, "name": name, "text": text, "notice": "二级资料已保存，原始素材保留；保存智能体方案后下次开播生效。"})
}
